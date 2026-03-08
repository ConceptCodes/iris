package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
			if markErr := jobStore.MarkFailed(ctx, job.ID, err, retryAt); markErr != nil {
				return markErr
			}
			continue
		}

		if err := jobStore.MarkSucceeded(ctx, job.ID); err != nil {
			return err
		}
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

		if err := handleCrawlerJob(ctx, jobStore, crawlStore, job); err != nil {
			slog.Error("crawler job failed", "job_id", job.ID, "error", err)
			if markErr := jobStore.MarkFailed(ctx, job.ID, err, time.Now().UTC().Add(cfg.JobPollInterval)); markErr != nil {
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

func handleCrawlerJob(ctx context.Context, jobStore jobs.Store, crawlStore crawl.Store, job jobs.Job) error {
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
		discovered, err := enqueueLocalDirJobs(ctx, jobStore, source.LocalPath)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error(), discovered, 0, 1)
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID, discovered, 0, 0)
	case crawl.SourceKindURLList:
		discovered, err := enqueueURLListSource(ctx, jobStore, source.SeedURL)
		if err != nil {
			_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error(), discovered, 0, 1)
			return err
		}
		return crawlStore.MarkRunCompleted(ctx, payload.RunID, discovered, 0, 0)
	default:
		err := fmt.Errorf("source kind %s not implemented in crawler", source.Kind)
		_ = crawlStore.MarkRunFailed(ctx, payload.RunID, err.Error(), 0, 0, 1)
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

func enqueueLocalDirJobs(ctx context.Context, jobStore jobs.Store, dir string) (int, error) {
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !imageExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		payload, err := json.Marshal(jobs.IndexLocalFilePayload{Path: path})
		if err != nil {
			return err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{Type: jobs.TypeIndexLocalFile, PayloadJSON: payload}); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func enqueueURLListSource(ctx context.Context, jobStore jobs.Store, seedURL string) (int, error) {
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
		payload, err := json.Marshal(jobs.FetchImagePayload{URL: line})
		if err != nil {
			return count, err
		}
		if _, err := jobStore.Enqueue(ctx, jobs.Job{Type: jobs.TypeFetchImage, PayloadJSON: payload}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
