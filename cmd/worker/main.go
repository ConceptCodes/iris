package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/clip"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/search"
	"iris/internal/store"
	"iris/pkg/models"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadWorker()

	seedURLFile := flag.String("seed-url-file", "", "optional path to URL file to enqueue as fetch_image jobs")
	seedDir := flag.String("seed-dir", "", "optional local directory to enqueue as index_local_file jobs")
	flag.Parse()

	slog.Info("starting worker", "mode", cfg.Mode, "backend", cfg.JobBackend)

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
		return jobs.NewPostgresStore(context.Background(), cfg.JobStoreDSN)
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

	clipClient := clip.NewClient(cfg.ClipAddr)
	qdrantStore, err := store.NewQdrantStore(cfg.QdrantAddr, cfg.ClipDim, 15*time.Second)
	if err != nil {
		return err
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(clipClient, qdrantStore)
	pipeline := indexing.NewPipeline(engine, assets.NewStore(cfg.AssetDir))

	ticker := time.NewTicker(cfg.JobPollInterval)
	defer ticker.Stop()

	for {
		job, ok, err := jobStore.LeaseNext(ctx, time.Now().UTC(), cfg.LeaseDuration, jobs.TypeFetchImage, jobs.TypeIndexLocalFile)
		if err != nil {
			return err
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}

		if err := handleIndexerJob(ctx, pipeline, job); err != nil {
			slog.Error("job failed", "job_id", job.ID, "type", job.Type, "error", err)
			retryAt := time.Now().UTC().Add(cfg.JobPollInterval)
			status, markErr := jobStore.MarkFailed(ctx, job.ID, err, retryAt)
			if markErr != nil {
				return markErr
			}
			if status == jobs.StatusDeadLetter {
				_ = incrementRunFailedForJob(ctx, crawlStore, job, err)
			}
			continue
		}

		if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
			return err
		}
		_ = incrementRunIndexedForJob(ctx, crawlStore, job)
	}
}

func runCrawler(ctx context.Context, cfg config.Worker, jobStore jobs.Store) error {
	crawlStore, err := newCrawlStore(cfg)
	if err != nil {
		return err
	}
	defer crawlStore.Close()

	ticker := time.NewTicker(cfg.JobPollInterval)
	defer ticker.Stop()

	for {
		job, ok, err := jobStore.LeaseNext(ctx, time.Now().UTC(), cfg.LeaseDuration, jobs.TypeDiscoverSource)
		if err != nil {
			return err
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}

		if err := handleCrawlerJob(ctx, cfg, jobStore, crawlStore, job); err != nil {
			slog.Error("crawler job failed", "job_id", job.ID, "error", err)
			if _, markErr := jobStore.MarkFailed(ctx, job.ID, err, time.Now().UTC().Add(cfg.JobPollInterval)); markErr != nil {
				return markErr
			}
			continue
		}
		if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
			return err
		}
	}
}

func handleIndexerJob(ctx context.Context, pipeline *indexing.Pipeline, job jobs.Job) error {
	switch job.Type {
	case jobs.TypeFetchImage:
		var payload jobs.FetchImagePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return err
		}
		_, err := pipeline.IndexFromURL(ctx, models.IndexRequest{
			URL:      payload.URL,
			Filename: payload.Filename,
			Tags:     payload.Tags,
			Meta:     payload.Meta,
		})
		return err
	case jobs.TypeIndexLocalFile:
		var payload jobs.IndexLocalFilePayload
		if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
			return err
		}
		_, err := pipeline.IndexLocalFile(ctx, payload.Path)
		return err
	default:
		return nil
	}
}

func handleCrawlerJob(ctx context.Context, cfg config.Worker, jobStore jobs.Store, crawlStore crawl.Store, job jobs.Job) error {
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
		discovered, err := enqueueLocalDirJobs(ctx, jobStore, source.LocalPath, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindURLList:
		discovered, err := enqueueURLListSource(ctx, jobStore, source.SeedURL, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindDomain:
		discovered, err := discoverDomainSource(ctx, jobStore, source, payload.RunID)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error())
			return err
		}
		if err := crawlStore.SetRunDiscovered(ctx, payload.RunID, discovered); err != nil {
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID)
	case crawl.SourceKindSitemap:
		discovered, err := discoverSitemapSource(ctx, jobStore, source, payload.RunID)
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

func newCrawlStore(cfg config.Worker) (crawl.Store, error) {
	switch cfg.JobBackend {
	case "memory":
		return crawl.NewMemoryStore(), nil
	case "postgres":
		return crawl.NewPostgresStore(context.Background(), cfg.JobStoreDSN)
	default:
		return nil, fmt.Errorf("unsupported crawl backend: %s", cfg.JobBackend)
	}
}

func enqueueLocalDirJobs(ctx context.Context, jobStore jobs.Store, dir, runID string) (int, error) {
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
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
	return count, err
}

func enqueueURLListSource(ctx context.Context, jobStore jobs.Store, seedURL, runID string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seedURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch url list: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		payload, err := json.Marshal(jobs.FetchImagePayload{URL: line, RunID: runID})
		if err != nil {
			return count, err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{
			Type:        jobs.TypeFetchImage,
			DedupKey:    dedupKey("fetch_image", runID, line),
			PayloadJSON: payload,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func discoverDomainSource(ctx context.Context, jobStore jobs.Store, source crawl.Source, runID string) (int, error) {
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
	robotsClient := crawl.NewRobotsClient(http.DefaultClient, "iris")

	type queueItem struct {
		url   string
		depth int
	}
	queue := []queueItem{{url: source.SeedURL, depth: 0}}
	visitedPages := map[string]struct{}{}
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	discovered := 0

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if _, exists := visitedPages[item.url]; exists {
			continue
		}
		visitedPages[item.url] = struct{}{}

		allowed, err := robotsClient.Allowed(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			continue
		}

		if err := wait(ctx); err != nil {
			return discovered, err
		}
		resp, err := fetchURL(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		discovery, err := crawl.ExtractHTMLLinks(resp, item.url, allowedDomains)
		if err != nil {
			return discovered, err
		}

		pageKey := item.url
		if discovery.CanonicalURL != "" {
			pageKey = discovery.CanonicalURL
			if discovery.CanonicalURL != item.url {
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
			if _, exists := seenImages[imageURL]; exists {
				continue
			}
			allowed, err := robotsClient.Allowed(ctx, imageURL)
			if err != nil {
				return discovered, err
			}
			if !allowed {
				continue
			}
			seenImages[imageURL] = struct{}{}
			if err := enqueueFetchImage(ctx, jobStore, imageURL, runID); err != nil {
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

	return discovered, nil
}

func discoverSitemapSource(ctx context.Context, jobStore jobs.Store, source crawl.Source, runID string) (int, error) {
	wait := sourceThrottle(source.RateLimitRPS)
	robotsClient := crawl.NewRobotsClient(http.DefaultClient, "iris")
	if err := wait(ctx); err != nil {
		return 0, err
	}
	locs, err := crawl.FetchSitemapLocs(ctx, source.SeedURL)
	if err != nil {
		return 0, err
	}
	discovered := 0
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	for _, loc := range locs {
		allowed, err := robotsClient.Allowed(ctx, loc)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			continue
		}

		if crawl.LooksLikeImageURL(loc) {
			if _, exists := seenImages[loc]; exists {
				continue
			}
			seenImages[loc] = struct{}{}
			if err := enqueueFetchImage(ctx, jobStore, loc, runID); err != nil {
				return discovered, err
			}
			discovered++
			continue
		}

		if err := wait(ctx); err != nil {
			return discovered, err
		}
		resp, err := fetchURL(ctx, loc)
		if err != nil {
			return discovered, err
		}
		discovery, err := crawl.ExtractHTMLLinks(resp, loc, source.AllowedDomains)
		if err != nil {
			return discovered, err
		}
		pageKey := loc
		if discovery.CanonicalURL != "" {
			pageKey = discovery.CanonicalURL
		}
		if _, exists := processedPages[pageKey]; exists {
			continue
		}
		processedPages[pageKey] = struct{}{}
		for _, imageURL := range discovery.ImageURLs {
			if _, exists := seenImages[imageURL]; exists {
				continue
			}
			allowed, err := robotsClient.Allowed(ctx, imageURL)
			if err != nil {
				return discovered, err
			}
			if !allowed {
				continue
			}
			seenImages[imageURL] = struct{}{}
			if err := enqueueFetchImage(ctx, jobStore, imageURL, runID); err != nil {
				return discovered, err
			}
			discovered++
		}
	}
	return discovered, nil
}

func fetchURL(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	return resp.Body, nil
}

func enqueueFetchImage(ctx context.Context, jobStore jobs.Store, imageURL, runID string) error {
	payload, err := json.Marshal(jobs.FetchImagePayload{URL: imageURL, RunID: runID})
	if err != nil {
		return err
	}
	_, err = jobStore.Enqueue(ctx, jobs.Job{
		Type:        jobs.TypeFetchImage,
		DedupKey:    dedupKey("fetch_image", runID, imageURL),
		PayloadJSON: payload,
	})
	return err
}

func incrementRunIndexedForJob(ctx context.Context, crawlStore crawl.Store, job jobs.Job) error {
	runID, err := extractRunID(job)
	if err != nil || runID == "" {
		return nil
	}
	return crawlStore.IncrementRunIndexed(ctx, runID, 1)
}

func incrementRunFailedForJob(ctx context.Context, crawlStore crawl.Store, job jobs.Job, failure error) error {
	runID, err := extractRunID(job)
	if err != nil || runID == "" {
		return nil
	}
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
	if runID == "" || target == "" {
		return ""
	}
	return jobType + ":" + runID + ":" + target
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
