package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/crawl"
	"iris/internal/encoder"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metadata"
	"iris/internal/metrics"
	"iris/internal/search"
	"iris/internal/ssrf"
	"iris/internal/store"
	"iris/internal/tracing"
	"iris/pkg/models"
)

// errorType indicates whether an error is transient (retryable) or permanent (non-retryable)
type errorType int

const (
	errorTypeTransient errorType = iota
	errorTypePermanent
)

// classifyError determines if an error is transient or permanent based on its nature.
// Transient errors: network timeouts, temporary failures, rate limits (429, 502, 503, 504), context deadlines
// Permanent errors: not found (404), bad request (400), authentication failures (401, 403), validation errors
func classifyError(err error) errorType {
	if err == nil {
		return errorTypeTransient
	}

	// Check for HTTP status code errors
	var httpErr interface{ StatusCode() int }
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode() {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			// 400, 401, 403, 404 are permanent errors
			return errorTypePermanent
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			// 429, 502, 503, 504 are transient errors
			return errorTypeTransient
		}
	}

	// Check for context errors (timeout, cancellation)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errorTypeTransient
	}

	// Check for network-related errors
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errorTypeTransient
	}

	// Check for specific error messages that indicate permanent failures
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "unsupported content type") ||
		strings.Contains(errMsg, "image exceeds") ||
		strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "invalid") ||
		strings.Contains(errMsg, "is required") {
		return errorTypePermanent
	}

	// Default to transient for unknown errors to allow retry
	return errorTypeTransient
}

// calculateRetryBackoff implements exponential backoff with jitter to avoid thundering herd.
// Formula: baseDelay * (2 ^ (attempt - 1)) + random jitter [0, baseDelay * 0.5)
// Capped at a reasonable maximum (5 minutes).
func calculateRetryBackoff(attempt int, baseDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Exponential backoff: baseDelay * (2 ^ (attempt - 1))
	backoff := baseDelay * time.Duration(1<<(attempt-1))

	// Add jitter: random value from [0, baseDelay * 0.5) to distribute retries
	jitterWindow := int64(baseDelay / 2)
	var jitter time.Duration
	if jitterWindow > 0 {
		jitter = time.Duration(rand.Int63n(jitterWindow))
	}
	backoff += jitter

	// Cap at maximum delay (5 minutes)
	maxDelay := 5 * time.Minute
	if backoff > maxDelay {
		backoff = maxDelay
	}

	return backoff
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type crawlerRuntime struct {
	fetcher    *crawl.CachedFetcher
	robots     *crawl.RobotsClient
	cacheStore crawl.CacheStore
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadWorker()

	seedURLFile := flag.String("seed-url-file", "", "optional path to URL file to enqueue as fetch_image jobs")
	seedDir := flag.String("seed-dir", "", "optional local directory to enqueue as index_local_file jobs")
	flag.Parse()

	slog.Info("starting worker", "mode", cfg.Mode, "backend", cfg.JobBackend, "otel_enabled", cfg.OtelEnabled)

	// Initialize OpenTelemetry tracer if enabled
	var otelShutdown func()
	if cfg.OtelEnabled {
		var err error
		otelShutdown, err = tracing.InitTracer(context.Background(), "iris-worker", cfg.OtelEndpoint)
		if err != nil {
			slog.Warn("failed to initialize tracer, continuing without tracing", "error", err)
			otelShutdown = nil
		} else {
			defer otelShutdown()
			slog.Info("tracing initialized", "endpoint", cfg.OtelEndpoint)
		}
	}

	jobStore, err := newJobStore(cfg)
	if err != nil {
		slog.Error("failed to initialize job store", "error", err)
		os.Exit(1)
	}
	defer jobStore.Close()

	if *seedURLFile != "" {
		if err := enqueueURLFile(context.Background(), jobStore, *seedURLFile); err != nil {
			slog.Error("failed to enqueue url jobs", "error", err)
			os.Exit(1)
		}
	}
	if *seedDir != "" {
		if err := enqueueDir(context.Background(), jobStore, *seedDir); err != nil {
			slog.Error("failed to enqueue local jobs", "error", err)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.WorkerModeIndexer:
		if err := runIndexer(ctx, cfg, jobStore); err != nil {
			slog.Error("indexer worker stopped", "error", err)
			os.Exit(1)
		}
	case config.WorkerModeCrawler:
		if err := runCrawler(ctx, cfg, jobStore); err != nil {
			slog.Error("crawler worker stopped", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unknown worker mode", "mode", cfg.Mode)
		os.Exit(1)
	}
}

func newJobStore(cfg config.Worker) (jobs.Store, error) {
	switch cfg.JobBackend {
	case "memory":
		return jobs.NewMemoryStore(), nil
	case "postgres":
		return jobs.NewPostgresStore(context.Background(), cfg.JobStoreDSN, cfg.PostgresPool)
	default:
		return nil, fmt.Errorf("unsupported job backend: %s", cfg.JobBackend)
	}
}

func runIndexer(ctx context.Context, cfg config.Worker, jobStore jobs.Store) error {
	crawlStore, err := newCrawlStore(cfg)
	if err != nil {
		return err
	}
	defer crawlStore.Close()

	encoderRegistry, cleanupEncoders, err := encoder.NewRegistryFromConfig(cfg.Shared)
	if err != nil {
		return fmt.Errorf("create encoder registry: %w", err)
	}
	defer cleanupEncoders()
	qdrantStore, err := store.NewQdrantStoreWithEncoders(cfg.QdrantAddr, cfg.EncoderDims(), 15*time.Second)
	if err != nil {
		return err
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(encoderRegistry, qdrantStore)
	assetStore, err := assets.NewStoreFromSettings(ctx, assets.Settings{
		Backend: cfg.AssetBackend,
		S3: assets.S3Config{
			Bucket:       cfg.AssetBucket,
			Region:       cfg.AssetRegion,
			Endpoint:     cfg.AssetEndpoint,
			AccessKey:    cfg.AssetAccessKey,
			SecretKey:    cfg.AssetSecretKey,
			SessionToken: cfg.AssetSessionKey,
			Prefix:       cfg.AssetPrefix,
			PublicBase:   cfg.AssetPublicBase,
			UsePathStyle: cfg.AssetPathStyle,
		},
	})
	if err != nil {
		return err
	}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{
		AssetStore:               assetStore,
		Enricher:                 metadata.NewComposite(metadata.EXIFEnricher{}, metadata.NewClient(cfg.MetadataAddr, 45*time.Second)),
		MaxFetchBytes:            cfg.MaxImageBytes,
		FetchClient:              &http.Client{Timeout: cfg.FetchTimeout},
		UserAgent:                cfg.UserAgent,
		SSRFAllowPrivateNetworks: cfg.SSRFAllowPrivateNetworks,
	})

	ticker := time.NewTicker(cfg.JobPollInterval)
	defer ticker.Stop()

	for {
		job, ok, err := jobStore.LeaseNext(ctx, time.Now().UTC(), cfg.LeaseDuration, jobs.TypeFetchImage, jobs.TypeIndexLocalFile, jobs.TypeReindexImage)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("lease next failed", "error", err)
			if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
				return nil
			}
			continue
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}

		jobStart := time.Now()
		result, err := handleIndexerJob(ctx, pipeline, job)
		if err != nil {
			slog.Error("job failed", "job_id", job.ID, "type", job.Type, "error", err)

			// Classify error and calculate appropriate retry backoff
			errType := classifyError(err)
			markStatus := jobs.StatusPending
			if errType == errorTypePermanent {
				if markErr := jobStore.MarkDeadLetter(ctx, job.ID, err); markErr != nil {
					if ctx.Err() != nil {
						return nil
					}
					slog.Error("mark dead letter failed", "job_id", job.ID, "error", markErr)
					if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
						return nil
					}
					continue
				}
				markStatus = jobs.StatusDeadLetter
			} else {
				retryAt := time.Now().UTC().Add(calculateRetryBackoff(job.Attempts+1, cfg.JobPollInterval))
				var markErr error
				markStatus, markErr = jobStore.MarkFailed(ctx, job.ID, err, retryAt)
				if markErr != nil {
					if ctx.Err() != nil {
						return nil
					}
					slog.Error("mark failed failed", "job_id", job.ID, "error", markErr)
					if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
						return nil
					}
					continue
				}
			}
			if markStatus == jobs.StatusDeadLetter {
				_ = incrementRunFailedForJob(ctx, crawlStore, job, err)
			}
			metrics.IncWorkerJobFailed()
			continue
		}

		if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("mark succeeded failed", "job_id", job.ID, "error", err)
			if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
				return nil
			}
			continue
		}
		metrics.IncWorkerJobSucceeded()
		metrics.ObserveWorkerJobLatency(time.Since(jobStart))
		switch result.Status {
		case indexing.ResultStatusDuplicate:
			_ = incrementRunDuplicateForJob(ctx, crawlStore, job)
		default:
			_ = incrementRunIndexedForJob(ctx, crawlStore, job)
		}
	}
}

func runCrawler(ctx context.Context, cfg config.Worker, jobStore jobs.Store) error {
	crawlStore, err := newCrawlStore(cfg)
	if err != nil {
		return err
	}
	defer crawlStore.Close()

	runtime, err := newCrawlerRuntime(cfg)
	if err != nil {
		return err
	}
	defer runtime.close()
	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		runtime.runCachePruneLoop(ctx, cfg)
	}()
	go func() {
		defer background.Done()
		runSchedulerLoop(ctx, cfg, crawl.NewService(crawlStore, jobStore))
	}()
	defer background.Wait()

	ticker := time.NewTicker(cfg.JobPollInterval)
	defer ticker.Stop()

	for {
		job, ok, err := jobStore.LeaseNext(ctx, time.Now().UTC(), cfg.LeaseDuration, jobs.TypeDiscoverSource)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("lease next failed", "error", err)
			if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
				return nil
			}
			continue
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}

		jobStart := time.Now()
		if err := handleCrawlerJob(ctx, cfg, runtime, jobStore, crawlStore, job); err != nil {
			slog.Error("crawler job failed", "job_id", job.ID, "error", err)

			// Classify error and calculate appropriate retry backoff
			errType := classifyError(err)
			if errType == errorTypePermanent {
				if markErr := jobStore.MarkDeadLetter(ctx, job.ID, err); markErr != nil {
					if ctx.Err() != nil {
						return nil
					}
					slog.Error("mark dead letter failed", "job_id", job.ID, "error", markErr)
					if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
						return nil
					}
					continue
				}
			} else {
				retryAt := time.Now().UTC().Add(calculateRetryBackoff(job.Attempts+1, cfg.JobPollInterval))
				if _, markErr := jobStore.MarkFailed(ctx, job.ID, err, retryAt); markErr != nil {
					if ctx.Err() != nil {
						return nil
					}
					slog.Error("mark failed failed", "job_id", job.ID, "error", markErr)
					if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
						return nil
					}
					continue
				}
			}
			metrics.IncWorkerJobFailed()
			continue
		}
		if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("mark succeeded failed", "job_id", job.ID, "error", err)
			if err := sleepWithContext(ctx, cfg.JobPollInterval); err != nil {
				return nil
			}
			continue
		}
		metrics.IncWorkerJobSucceeded()
		metrics.ObserveWorkerJobLatency(time.Since(jobStart))
	}
}

func runSchedulerLoop(ctx context.Context, cfg config.Worker, service *crawl.Service) {
	interval := cfg.SchedulePollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processSchedules(ctx, service)
		}
	}
}

func processSchedules(ctx context.Context, service *crawl.Service) {
	now := time.Now().UTC()
	sources, err := service.DueSources(ctx, now)
	if err != nil {
		slog.Warn("schedule check failed", "error", err)
		return
	}
	for _, source := range sources {
		next := now.Add(source.ScheduleEvery)
		if err := service.SetSourceNextRun(ctx, source.ID, next); err != nil {
			slog.Warn("failed to set next run", "source_id", source.ID, "error", err)
			continue
		}
		metrics.ObserveSchedulerDecision(schedulerDecisionForSource(source), next.Sub(now))
		if _, err := service.TriggerRunForSource(ctx, source, "scheduled", now); err != nil {
			slog.Warn("failed to trigger scheduled run", "source_id", source.ID, "error", err)
		}
	}
}

func handleIndexerJob(ctx context.Context, pipeline *indexing.Pipeline, job jobs.Job) (indexing.Result, error) {
	switch job.Type {
	case jobs.TypeFetchImage:
		var payload jobs.FetchImagePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return indexing.Result{}, err
		}
		meta := make(map[string]string)
		for k, v := range payload.Meta {
			meta[k] = v
		}
		if payload.PageURL != "" {
			meta["page_url"] = payload.PageURL
		}
		if payload.Title != "" {
			meta["title"] = payload.Title
		}
		if payload.CrawlSourceID != "" {
			meta["crawl_source_id"] = payload.CrawlSourceID
		}
		if payload.SourceDomain != "" {
			meta["source_domain"] = payload.SourceDomain
		}
		if payload.MimeType != "" {
			meta["mime_type"] = payload.MimeType
		}
		return pipeline.IndexFromURLResult(ctx, models.IndexRequest{
			URL:      payload.URL,
			Filename: payload.Filename,
			Tags:     payload.Tags,
			Meta:     meta,
		})
	case jobs.TypeIndexLocalFile:
		var payload jobs.IndexLocalFilePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return indexing.Result{}, err
		}
		return pipeline.IndexLocalFileResult(ctx, payload.Path)
	case jobs.TypeReindexImage:
		var payload jobs.ReindexImagePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return indexing.Result{}, err
		}
		record := models.ImageRecord{ID: payload.ID}
		return pipeline.ReindexFromURLResult(ctx, payload.URL, record)
	default:
		return indexing.Result{}, nil
	}
}

func handleCrawlerJob(ctx context.Context, cfg config.Worker, runtime *crawlerRuntime, jobStore jobs.Store, crawlStore crawl.Store, job jobs.Job) error {
	var payload jobs.DiscoverSourcePayload
	if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
		return err
	}

	source, err := crawlStore.GetSource(ctx, payload.SourceID)
	if err != nil {
		return err
	}

	switch source.Kind {
	case crawl.SourceKindLocalDir:
		discovered, err := enqueueLocalDirJobs(ctx, jobStore, source.LocalPath, payload.RunID, source.MaxImagesPerRun)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindURLList:
		discovered, err := enqueueURLListSource(ctx, jobStore, source.SeedURL, payload.RunID, source.MaxImagesPerRun)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindDomain:
		discovered, err := discoverDomainSource(ctx, cfg, runtime, jobStore, source, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindSitemap:
		discovered, err := discoverSitemapSource(ctx, cfg, runtime, jobStore, source, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	default:
		err := fmt.Errorf("source kind %s not implemented in crawler", source.Kind)
		_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
		return err
	}
}

func newCrawlerRuntime(cfg config.Worker) (*crawlerRuntime, error) {
	cacheStore, err := newCacheStore(cfg)
	if err != nil {
		return nil, err
	}

	validator := ssrf.NewValidator()
	safeClient := validator.NewSafeClient(30 * time.Second)

	fetcherOptions := crawl.FetcherOptions{
		DefaultTTL:      cfg.HTTPCacheTTL,
		Retries:         cfg.FetchRetries,
		RetryBackoff:    cfg.FetchRetryBackoff,
		HostConcurrency: cfg.HostConcurrency,
		Store:           cacheStore,
	}
	robotsOptions := crawl.FetcherOptions{
		DefaultTTL:      cfg.RobotsCacheTTL,
		Retries:         cfg.FetchRetries,
		RetryBackoff:    cfg.FetchRetryBackoff,
		HostConcurrency: cfg.HostConcurrency,
		Store:           cacheStore,
	}
	return &crawlerRuntime{
		fetcher:    crawl.NewCachedFetcher(safeClient, "iris", fetcherOptions),
		robots:     crawl.NewRobotsClientWithOptions(safeClient, "iris", robotsOptions),
		cacheStore: cacheStore,
	}, nil
}

func newCacheStore(cfg config.Worker) (crawl.CacheStore, error) {
	switch cfg.JobBackend {
	case "memory":
		return crawl.NewNoopCacheStore(), nil
	case "postgres":
		return crawl.NewPostgresCacheStore(context.Background(), cfg.JobStoreDSN, cfg.PostgresPool)
	default:
		return nil, fmt.Errorf("unsupported crawl cache backend: %s", cfg.JobBackend)
	}
}

func (r *crawlerRuntime) close() error {
	if r == nil || r.cacheStore == nil {
		return nil
	}
	return r.cacheStore.Close()
}

func (r *crawlerRuntime) runCachePruneLoop(ctx context.Context, cfg config.Worker) {
	if r == nil || r.cacheStore == nil || cfg.CachePruneInterval <= 0 {
		return
	}
	if _, err := r.cacheStore.PruneExpired(ctx, time.Now().UTC(), cfg.CachePruneBatch); err != nil {
		slog.Warn("crawl cache prune failed", "error", err)
	}

	ticker := time.NewTicker(cfg.CachePruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned, err := r.cacheStore.PruneExpired(ctx, time.Now().UTC(), cfg.CachePruneBatch)
			if err != nil {
				slog.Warn("crawl cache prune failed", "error", err)
				continue
			}
			if pruned > 0 {
				slog.Info("crawl cache pruned", "rows", pruned)
			}
		}
	}
}

func newCrawlStore(cfg config.Worker) (crawl.Store, error) {
	switch cfg.JobBackend {
	case "memory":
		return crawl.NewMemoryStore(), nil
	case "postgres":
		return crawl.NewPostgresStore(context.Background(), cfg.JobStoreDSN, cfg.PostgresPool)
	default:
		return nil, fmt.Errorf("unsupported crawl backend: %s", cfg.JobBackend)
	}
}

func enqueueLocalDirJobs(ctx context.Context, jobStore jobs.Store, dir, runID string, maxImages int) (int, error) {
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if maxImages > 0 && count >= maxImages {
			metrics.IncCrawlBudgetHit("images")
			return filepath.SkipAll
		}
		if !imageExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		payload, err := json.Marshal(jobs.IndexLocalFilePayload{Path: path, RunID: runID})
		if err != nil {
			return err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.TypeIndexLocalFile,
			DedupKey:    dedupKey("index_local_file", runID, path),
			PayloadJSON: payload,
		}); err != nil {
			return err
		}
		count++
		return nil
	})
	metrics.IncCrawlJobsDiscovered()
	return count, err
}

func enqueueURLListSource(ctx context.Context, jobStore jobs.Store, seedURL, runID string, maxImages int) (int, error) {
	validator := ssrf.NewValidator()
	if err := validator.ValidateURL(ctx, seedURL); err != nil {
		return 0, fmt.Errorf("SSRF blocked: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seedURL, nil)
	if err != nil {
		return 0, err
	}

	safeClient := validator.NewSafeClient(30 * time.Second)

	resp, err := safeClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch url list: status %d", resp.StatusCode)
	}

	// Limit reading to prevent OOM
	limited := io.LimitReader(resp.Body, 10*1024*1024) // 10MB limit
	body, err := io.ReadAll(limited)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(body), "\n") {
		if maxImages > 0 && count >= maxImages {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalizedURL, err := crawl.NormalizeURL(line)
		if err != nil {
			continue
		}
		payload, err := json.Marshal(jobs.FetchImagePayload{URL: normalizedURL, RunID: runID})
		if err != nil {
			return count, err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.TypeFetchImage,
			DedupKey:    dedupKey("fetch_image", runID, normalizedURL),
			PayloadJSON: payload,
		}); err != nil {
			return count, err
		}
		count++
	}
	metrics.IncCrawlJobsDiscovered()
	return count, nil
}

func discoverDomainSource(ctx context.Context, cfg config.Worker, runtime *crawlerRuntime, jobStore jobs.Store, source crawl.Source, runID string) (int, error) {
	seed, err := url.Parse(source.SeedURL)
	if err != nil {
		return 0, err
	}
	allowedDomains := source.AllowedDomains
	if len(allowedDomains) == 0 {
		allowedDomains = []string{seed.Hostname()}
	}
	maxDepth := source.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	wait := sourceThrottle(source.RateLimitRPS)

	type queueItem struct {
		url   string
		depth int
	}
	normalizedSeedURL, err := crawl.NormalizeURL(source.SeedURL)
	if err != nil {
		return 0, err
	}
	queue := []queueItem{{url: normalizedSeedURL, depth: 0}}
	visitedPages := map[string]struct{}{}
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	discovered := 0

	for len(queue) > 0 {
		if source.MaxPagesPerRun > 0 && len(processedPages) >= source.MaxPagesPerRun {
			metrics.IncCrawlBudgetHit("pages")
			break
		}
		if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		item := queue[0]
		queue = queue[1:]
		if _, exists := visitedPages[item.url]; exists {
			continue
		}
		visitedPages[item.url] = struct{}{}

		allowed, err := runtime.robots.Allowed(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			metrics.IncCrawlSkip("robots")
			continue
		}

		if err := wait(ctx); err != nil {
			return discovered, err
		}
		result, err := runtime.fetcher.Fetch(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		discovery, err := crawl.ExtractHTMLLinks(strings.NewReader(string(result.Body)), result.URL, allowedDomains)
		if err != nil {
			return discovered, err
		}

		pageKey := result.URL
		if discovery.CanonicalURL != "" {
			pageKey = discovery.CanonicalURL
			if discovery.CanonicalURL != result.URL {
				if _, exists := visitedPages[discovery.CanonicalURL]; !exists {
					queue = append(queue, queueItem{url: discovery.CanonicalURL, depth: item.depth})
				}
			}
		}
		if _, exists := processedPages[pageKey]; exists {
			continue
		}
		processedPages[pageKey] = struct{}{}

		for _, imageURL := range discovery.ImageURLs {
			if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
				metrics.IncCrawlBudgetHit("images")
				break
			}
			if _, exists := seenImages[imageURL]; exists {
				continue
			}
			allowed, err := runtime.robots.Allowed(ctx, imageURL)
			if err != nil {
				return discovered, err
			}
			if !allowed {
				metrics.IncCrawlSkip("robots")
				continue
			}
			seenImages[imageURL] = struct{}{}
			pageURL := result.URL
			if discovery.CanonicalURL != "" {
				pageURL = discovery.CanonicalURL
			}
			if err := enqueueFetchImage(ctx, jobStore, imageURL, runID, pageURL, discovery.Title, source.ID); err != nil {
				return discovered, err
			}
			discovered++
		}

		if item.depth >= maxDepth {
			continue
		}
		for _, pageURL := range discovery.PageURLs {
			if _, exists := visitedPages[pageURL]; exists {
				continue
			}
			queue = append(queue, queueItem{url: pageURL, depth: item.depth + 1})
		}
	}
	metrics.IncCrawlJobsDiscovered()
	return discovered, nil
}

func discoverSitemapSource(ctx context.Context, cfg config.Worker, runtime *crawlerRuntime, jobStore jobs.Store, source crawl.Source, runID string) (int, error) {
	wait := sourceThrottle(source.RateLimitRPS)
	if err := wait(ctx); err != nil {
		return 0, err
	}
	sitemapResult, err := runtime.fetcher.Fetch(ctx, source.SeedURL)
	if err != nil {
		return 0, err
	}
	locs, err := crawl.ExtractSitemapLocs(strings.NewReader(string(sitemapResult.Body)))
	if err != nil {
		return 0, err
	}
	discovered := 0
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	for _, loc := range locs {
		if source.MaxPagesPerRun > 0 && len(processedPages) >= source.MaxPagesPerRun {
			metrics.IncCrawlBudgetHit("pages")
			break
		}
		if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		normalizedLoc, err := crawl.NormalizeURL(loc)
		if err != nil {
			continue
		}
		allowed, err := runtime.robots.Allowed(ctx, loc)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			metrics.IncCrawlSkip("robots")
			continue
		}

		if crawl.LooksLikeImageURL(normalizedLoc) {
			if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
				metrics.IncCrawlBudgetHit("images")
				break
			}
			if _, exists := seenImages[normalizedLoc]; exists {
				continue
			}
			seenImages[normalizedLoc] = struct{}{}
			if err := enqueueFetchImage(ctx, jobStore, normalizedLoc, runID, "", "", source.ID); err != nil {
				return discovered, err
			}
			discovered++
			continue
		}

		if err := wait(ctx); err != nil {
			return discovered, err
		}
		result, err := runtime.fetcher.Fetch(ctx, normalizedLoc)
		if err != nil {
			return discovered, err
		}
		discovery, err := crawl.ExtractHTMLLinks(strings.NewReader(string(result.Body)), result.URL, source.AllowedDomains)
		if err != nil {
			return discovered, err
		}
		pageKey := result.URL
		if discovery.CanonicalURL != "" {
			pageKey = discovery.CanonicalURL
		}
		if _, exists := processedPages[pageKey]; exists {
			continue
		}
		processedPages[pageKey] = struct{}{}
		for _, imageURL := range discovery.ImageURLs {
			if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
				metrics.IncCrawlBudgetHit("images")
				break
			}
			if _, exists := seenImages[imageURL]; exists {
				continue
			}
			allowed, err := runtime.robots.Allowed(ctx, imageURL)
			if err != nil {
				return discovered, err
			}
			if !allowed {
				metrics.IncCrawlSkip("robots")
				continue
			}
			seenImages[imageURL] = struct{}{}
			pageURL := result.URL
			if discovery.CanonicalURL != "" {
				pageURL = discovery.CanonicalURL
			}
			if err := enqueueFetchImage(ctx, jobStore, imageURL, runID, pageURL, discovery.Title, source.ID); err != nil {
				return discovered, err
			}
			discovered++
		}
	}
	metrics.IncCrawlJobsDiscovered()
	return discovered, nil
}

func enqueueFetchImage(ctx context.Context, jobStore jobs.Store, imageURL, runID string, pageURL, title, sourceID string) error {
	normalizedURL, err := crawl.NormalizeURL(imageURL)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(jobs.FetchImagePayload{
		URL:           normalizedURL,
		RunID:         runID,
		PageURL:       pageURL,
		Title:         title,
		CrawlSourceID: sourceID,
	})
	if err != nil {
		return err
	}
	_, err = jobStore.Enqueue(ctx, jobs.Job{
		Type:        jobs.TypeFetchImage,
		DedupKey:    dedupKey("fetch_image", runID, normalizedURL),
		PayloadJSON: payload,
	})
	return err
}

func incrementRunIndexedForJob(ctx context.Context, crawlStore crawl.Store, job jobs.Job) error {
	runID, err := extractRunID(job)
	if err != nil || runID == "" {
		return nil
	}
	metrics.IncCrawlJobsIndexed()
	return crawlStore.IncrementRunIndexed(ctx, runID, 1)
}

func incrementRunDuplicateForJob(ctx context.Context, crawlStore crawl.Store, job jobs.Job) error {
	runID, err := extractRunID(job)
	if err != nil || runID == "" {
		return nil
	}
	metrics.IncCrawlJobsDuplicate()
	return crawlStore.IncrementRunDuplicate(ctx, runID, 1)
}

func incrementRunFailedForJob(ctx context.Context, crawlStore crawl.Store, job jobs.Job, failure error) error {
	runID, err := extractRunID(job)
	if err != nil || runID == "" {
		return nil
	}
	metrics.IncCrawlJobsFailed()
	return crawlStore.IncrementRunFailed(ctx, runID, 1, failure.Error())
}

func extractRunID(job jobs.Job) (string, error) {
	switch job.Type {
	case jobs.TypeFetchImage:
		var payload jobs.FetchImagePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return "", err
		}
		return payload.RunID, nil
	case jobs.TypeIndexLocalFile:
		var payload jobs.IndexLocalFilePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return "", err
		}
		return payload.RunID, nil
	default:
		return "", nil
	}
}

func dedupKey(jobType, runID, target string) string {
	if target == "" {
		return ""
	}
	if runID == "" {
		return jobType + ":" + target
	}
	return jobType + ":" + runID + ":" + target
}

func schedulerDecisionForSource(source crawl.Source) string {
	switch {
	case source.ConsecutiveFailures > 0:
		return "failure_backoff"
	case source.LastIndexedCount > 0:
		return "maintain"
	case source.LastDiscoveredCount == 0 && !source.LastRunAt.IsZero():
		return "low_yield_backoff"
	case source.LastDuplicateCount > 0 && source.LastIndexedCount == 0:
		return "duplicate_backoff"
	default:
		return "initial"
	}
}

func sourceThrottle(rps int) func(context.Context) error {
	if rps <= 0 {
		return func(context.Context) error { return nil }
	}
	interval := time.Second / time.Duration(rps)
	var last time.Time
	return func(ctx context.Context) error {
		if last.IsZero() {
			last = time.Now()
			return nil
		}
		wait := time.Until(last.Add(interval))
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		last = time.Now()
		return nil
	}
}
