package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/clip"
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
		runCrawler(ctx, cfg)
	default:
		slog.Error("unknown worker mode", "mode", cfg.Mode)
		os.Exit(1)
	}
}

func newJobStore(cfg config.Worker) (jobs.Store, error) {
	switch cfg.JobBackend {
	case "memory":
		return jobs.NewMemoryStore(), nil
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

func runCrawler(ctx context.Context, cfg config.Worker) {
	slog.Info("crawler worker scaffold is running", "mode", cfg.Mode, "note", "crawl discovery is not implemented yet")
	<-ctx.Done()
}
