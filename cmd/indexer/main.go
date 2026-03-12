package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/encoder"
	"iris/internal/indexing"
	"iris/internal/search"
	"iris/internal/store"
	"iris/pkg/models"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

func main() {
	mode := flag.String("mode", "", "indexing mode: 'dir' or 'urls'")
	input := flag.String("input", "", "input directory or URL file path")
	flag.Parse()

	if *mode == "" || *input == "" {
		fmt.Fprintln(os.Stderr, "usage: indexer -mode <dir|urls> -input <path>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadIndexer()

	slog.Info("starting indexer", "mode", *mode, "input", *input, "concurrency", cfg.Concurrency)

	encoderRegistry, cleanupEncoders, err := encoder.NewRegistryFromConfig(cfg.Shared)
	if err != nil {
		slog.Error("failed to create encoder registry", "error", err)
		os.Exit(1)
	}
	defer cleanupEncoders()
	qdrantStore, err := store.NewQdrantStoreWithEncoders(cfg.QdrantAddr, cfg.EncoderDims(), 15*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant", "error", err)
		os.Exit(1)
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(encoderRegistry, qdrantStore)
	assetStore, err := assets.NewStoreFromSettings(context.Background(), assets.Settings{
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
		slog.Error("failed to initialize asset store", "error", err)
		os.Exit(1)
	}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{
		AssetStore: assetStore,})

	var jobs []string
	switch *mode {
	case "dir":
		jobs = collectDirJobs(*input)
	case "urls":
		jobs = collectURLJobs(*input)
	default:
		slog.Error("unknown mode", "mode", *mode)
		os.Exit(1)
	}

	slog.Info("collected jobs", "count", len(jobs))

	jobsCh := make(chan string, cfg.Concurrency*2)
	var indexed, failed atomic.Int64

	for i := 0; i < cfg.Concurrency; i++ {
		go func() {
			for job := range jobsCh {
				start := time.Now()
				err := indexJob(context.Background(), pipeline, *mode, job)
				if err != nil {
					slog.Error("index failed", "job", job, "error", err)
					failed.Add(1)
				} else {
					slog.Info("indexed", "job", job, "elapsed_ms", time.Since(start).Milliseconds())
					indexed.Add(1)
				}
			}
		}()
	}

	for _, job := range jobs {
		jobsCh <- job
	}
	close(jobsCh)

	for indexed.Load()+failed.Load() < int64(len(jobs)) {
		time.Sleep(100 * time.Millisecond)
	}

	slog.Info("complete", "indexed", indexed.Load(), "failed", failed.Load(), "total", len(jobs))
}

func collectDirJobs(dir string) []string {
	var jobs []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if imageExts[ext] {
			abs, absErr := filepath.Abs(path)
			if absErr == nil {
				jobs = append(jobs, abs)
			}
		}
		return nil
	})
	return jobs
}

func indexJob(ctx context.Context, pipeline *indexing.Pipeline, mode, job string) error {
	switch mode {
	case "dir":
		_, err := pipeline.IndexLocalFile(ctx, job)
		return err
	case "urls":
		_, err := pipeline.IndexFromURL(ctx, models.IndexRequest{URL: job})
		return err
	default:
		return fmt.Errorf("unsupported mode: %s", mode)
	}
}

func collectURLJobs(path string) []string {
	var jobs []string
	f, err := os.Open(path)
	if err != nil {
		slog.Error("open url file", "error", err)
		os.Exit(1)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		jobs = append(jobs, line)
	}
	return jobs
}

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}
