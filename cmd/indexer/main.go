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

	"github.com/google/uuid"
	"iris/config"
	"iris/internal/assets"
	"iris/internal/clip"
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

	slog.Info("starting indexer", "mode", *mode, "input", *input, "concurrency", cfg.Concurrency, "asset_dir", cfg.AssetDir)

	clipClient := clip.NewClient(cfg.ClipAddr)
	qdrantStore, err := store.NewQdrantStore(cfg.QdrantAddr, cfg.ClipDim, 15*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant", "error", err)
		os.Exit(1)
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(clipClient, qdrantStore)
	assetStore := assets.NewStore(cfg.AssetDir)

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
				err := indexJob(context.Background(), engine, assetStore, *mode, job)
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

func indexJob(ctx context.Context, engine search.Engine, assetStore *assets.Store, mode, job string) error {
	switch mode {
	case "dir":
		return indexLocalFile(ctx, engine, assetStore, job)
	case "urls":
		_, err := engine.IndexFromURL(ctx, models.IndexRequest{URL: job})
		return err
	default:
		return fmt.Errorf("unsupported mode: %s", mode)
	}
}

func indexLocalFile(ctx context.Context, engine search.Engine, assetStore *assets.Store, path string) error {
	imageBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read local image: %w", err)
	}

	id := uuid.New().String()
	filename := filepath.Base(path)
	record := models.ImageRecord{
		ID:       id,
		Filename: filename,
		Meta: map[string]string{
			"source":      "local",
			"source_path": path,
		},
	}
	if assetStore != nil {
		assetURL, err := assetStore.Save(id, filename, imageBytes)
		if err != nil {
			return fmt.Errorf("store local image: %w", err)
		}
		record.URL = assetURL
	}

	_, err = engine.IndexFromBytes(ctx, imageBytes, record)
	return err
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
