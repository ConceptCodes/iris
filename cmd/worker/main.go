package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"iris/config"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metrics"
	appruntime "iris/internal/runtime"
	"iris/internal/tracing"
	workerpkg "iris/internal/worker"
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
	if workerpkg.ClassifyError(err) == workerpkg.ErrorTypePermanent {
		return errorTypePermanent
	}
	return errorTypeTransient
}

// calculateRetryBackoff implements exponential backoff with jitter to avoid thundering herd.
// Formula: baseDelay * (2 ^ (attempt - 1)) + random jitter [0, baseDelay * 0.5)
// Capped at a reasonable maximum (5 minutes).
func calculateRetryBackoff(attempt int, baseDelay time.Duration) time.Duration {
	return workerpkg.CalculateRetryBackoff(attempt, baseDelay)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	return workerpkg.SleepWithContext(ctx, delay)
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

type indexerPipeline struct {
	pipeline   *indexing.Pipeline
	crawlStore crawl.Store
	runtime    *appruntime.IngestionRuntime
}

func initializeIndexerPipeline(ctx context.Context, cfg config.Worker) (*indexerPipeline, error) {
	crawlStore, err := newCrawlStore(cfg)
	if err != nil {
		return nil, err
	}

	runtimeCfg := appruntime.ConfigFromShared(cfg.Shared)
	runtimeCfg.MaxFetchBytes = cfg.MaxImageBytes
	runtimeCfg.FetchTimeout = cfg.FetchTimeout
	runtimeCfg.UserAgent = cfg.UserAgent
	ingestionRuntime, err := appruntime.NewIngestionRuntime(ctx, cfg.Shared, runtimeCfg)
	if err != nil {
		crawlStore.Close()
		return nil, err
	}

	return &indexerPipeline{
		pipeline:   ingestionRuntime.Pipeline,
		crawlStore: crawlStore,
		runtime:    ingestionRuntime,
	}, nil
}

func (ip *indexerPipeline) close() {
	if ip.runtime != nil {
		ip.runtime.Close()
	}
	if ip.crawlStore != nil {
		ip.crawlStore.Close()
	}
}

func processIndexerJobSuccess(ctx context.Context, jobStore jobs.Store, crawlStore crawl.Store, job jobs.Job, result indexing.Result, jobStart time.Time, jobPollInterval time.Duration) bool {
	if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
		if ctx.Err() != nil {
			return false
		}
		slog.Error("mark succeeded failed", "job_id", job.ID, "error", err)
		if err := sleepWithContext(ctx, jobPollInterval); err != nil {
			return false
		}
		return true
	}
	metrics.IncWorkerJobSucceeded()
	metrics.ObserveWorkerJobLatency(time.Since(jobStart))
	switch result.Status {
	case indexing.ResultStatusDuplicate:
		_ = incrementRunDuplicateForJob(ctx, crawlStore, job)
	default:
		_ = incrementRunIndexedForJob(ctx, crawlStore, job)
	}
	return true
}

func processIndexerJobError(ctx context.Context, jobStore jobs.Store, crawlStore crawl.Store, job jobs.Job, jobErr error, jobPollInterval time.Duration) bool {
	slog.Error("job failed", "job_id", job.ID, "type", job.Type, "error", jobErr)

	// Classify error and calculate appropriate retry backoff
	errType := classifyError(jobErr)
	markStatus := jobs.StatusPending
	if errType == errorTypePermanent {
		if markErr := jobStore.MarkDeadLetter(ctx, job.ID, jobErr); markErr != nil {
			if ctx.Err() != nil {
				return false
			}
			slog.Error("mark dead letter failed", "job_id", job.ID, "error", markErr)
			if err := sleepWithContext(ctx, jobPollInterval); err != nil {
				return false
			}
			return true
		}
		markStatus = jobs.StatusDeadLetter
	} else {
		retryAt := time.Now().UTC().Add(calculateRetryBackoff(job.Attempts+1, jobPollInterval))
		var markErr error
		markStatus, markErr = jobStore.MarkFailed(ctx, job.ID, jobErr, retryAt)
		if markErr != nil {
			if ctx.Err() != nil {
				return false
			}
			slog.Error("mark failed failed", "job_id", job.ID, "error", markErr)
			if err := sleepWithContext(ctx, jobPollInterval); err != nil {
				return false
			}
			return true
		}
	}
	if markStatus == jobs.StatusDeadLetter {
		_ = incrementRunFailedForJob(ctx, crawlStore, job, jobErr)
	}
	metrics.IncWorkerJobFailed()
	return true
}

func runIndexer(ctx context.Context, cfg config.Worker, jobStore jobs.Store) error {
	pipeline, err := initializeIndexerPipeline(ctx, cfg)
	if err != nil {
		return err
	}
	defer pipeline.close()

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
		result, err := handleIndexerJob(ctx, pipeline.pipeline, job)
		if err != nil {
			if !processIndexerJobError(ctx, jobStore, pipeline.crawlStore, job, err, cfg.JobPollInterval) {
				return nil
			}
			continue
		}

		if !processIndexerJobSuccess(ctx, jobStore, pipeline.crawlStore, job, result, jobStart, cfg.JobPollInterval) {
			return nil
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
		discovered, err := workerpkg.EnqueueLocalDirJobs(ctx, enqueueJobFunc(jobStore), source.LocalPath, payload.RunID, source.MaxImagesPerRun, workerpkg.IsImageExt, metrics.IncCrawlBudgetHit)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		metrics.IncCrawlJobsDiscovered()
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindURLList:
		discovered, err := workerpkg.EnqueueURLListSource(ctx, enqueueJobFunc(jobStore), source.SeedURL, payload.RunID, source.MaxImagesPerRun, cfg.SSRFAllowPrivateNetworks)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindDomain:
		discovered, err := workerpkg.DiscoverDomain(ctx, toWorkerRuntime(runtime), enqueueJobFunc(jobStore), source, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindSitemap:
		discovered, err := workerpkg.DiscoverSitemap(ctx, toWorkerRuntime(runtime), enqueueJobFunc(jobStore), source, payload.RunID)
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

func enqueueJobFunc(jobStore jobs.Store) workerpkg.EnqueueFunc {
	return func(ctx context.Context, jobType, dedupKey string, payload json.RawMessage) error {
		_, err := jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.Type(jobType),
			DedupKey:    dedupKey,
			PayloadJSON: payload,
		})
		return err
	}
}

func toWorkerRuntime(r *crawlerRuntime) *workerpkg.CrawlerRuntime {
	if r == nil {
		return nil
	}
	return &workerpkg.CrawlerRuntime{
		Fetcher:    r.fetcher,
		Robots:     r.robots,
		CacheStore: r.cacheStore,
	}
}

func newCrawlerRuntime(cfg config.Worker) (*crawlerRuntime, error) {
	cacheStore, err := newCacheStore(cfg)
	if err != nil {
		return nil, err
	}

	runtime, err := workerpkg.NewCrawlerRuntime(workerpkg.DiscoverConfig{
		SSRFAllowPrivateNetworks: cfg.SSRFAllowPrivateNetworks,
		FetchRetries:             cfg.FetchRetries,
		FetchRetryBackoff:        cfg.FetchRetryBackoff,
		HostConcurrency:          cfg.HostConcurrency,
		HTTPCacheTTL:             cfg.HTTPCacheTTL,
		RobotsCacheTTL:           cfg.RobotsCacheTTL,
	}, cacheStore)
	if err != nil {
		cacheStore.Close()
		return nil, err
	}
	return &crawlerRuntime{
		fetcher:    runtime.Fetcher,
		robots:     runtime.Robots,
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
	return workerpkg.SchedulerDecision(
		source.ConsecutiveFailures,
		source.LastIndexedCount,
		source.LastDiscoveredCount,
		source.LastDuplicateCount,
		!source.LastRunAt.IsZero(),
	)
}
