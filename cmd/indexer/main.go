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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/davidojo/google-images/internal/clip"
	"github.com/davidojo/google-images/internal/search"
	"github.com/davidojo/google-images/internal/store"
	"github.com/davidojo/google-images/pkg/models"
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

	clipAddr := getEnv("CLIP_ADDR", "http://localhost:8001")
	qdrantAddr := getEnv("QDRANT_ADDR", "localhost:6334")
	clipDim := getEnvInt("CLIP_DIM", 512)
	concurrency := getEnvInt("CONCURRENCY", 4)

	slog.Info("starting indexer", "mode", *mode, "input", *input, "concurrency", concurrency)

	clipClient := clip.NewClient(clipAddr)
	qdrantStore, err := store.NewQdrantStore(qdrantAddr, clipDim, 15*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant", "error", err)
		os.Exit(1)
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(clipClient, qdrantStore)

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

	jobsCh := make(chan string, concurrency*2)
	var indexed, failed atomic.Int64

	for i := 0; i < concurrency; i++ {
		go func() {
			for url := range jobsCh {
				start := time.Now()
				_, err := engine.IndexFromURL(context.Background(), models.IndexRequest{URL: url})
				if err != nil {
					slog.Error("index failed", "url", url, "error", err)
					failed.Add(1)
				} else {
					slog.Info("indexed", "url", url, "elapsed_ms", time.Since(start).Milliseconds())
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
			abs, _ := filepath.Abs(path)
			jobs = append(jobs, "file://"+abs)
		}
		return nil
	})
	return jobs
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

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}
