package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iris/config"
	"iris/internal/api"
	"iris/internal/clip"
	"iris/internal/search"
	"iris/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadServer()

	slog.Info("starting server", "clip_addr", cfg.ClipAddr, "qdrant_addr", cfg.QdrantAddr, "dim", cfg.ClipDim, "asset_dir", cfg.AssetDir)

	clipClient := clip.NewClient(cfg.ClipAddr)
	qdrantStore, err := store.NewQdrantStore(cfg.QdrantAddr, cfg.ClipDim, 3*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant, search will be unavailable", "error", err)
	} else {
		defer qdrantStore.Close()
	}

	engine := search.NewEngine(clipClient, qdrantStore)
	crawlService, cleanup, err := api.NewCrawlService(cfg.JobBackend, cfg.JobStoreDSN)
	if err != nil {
		slog.Error("failed to initialize crawl service", "error", err)
	} else if cleanup != nil {
		defer cleanup()
	}
	router := api.NewRouter(engine, cfg.AssetDir, crawlService, cfg.AdminAPIKey)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("server stopped")
}
