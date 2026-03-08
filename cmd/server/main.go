package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/davidojo/google-images/internal/api"
	"github.com/davidojo/google-images/internal/clip"
	"github.com/davidojo/google-images/internal/search"
	"github.com/davidojo/google-images/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	clipAddr := getEnv("CLIP_ADDR", "http://localhost:8001")
	qdrantAddr := getEnv("QDRANT_ADDR", "localhost:6334")
	clipDim := getEnvInt("CLIP_DIM", 512)
	httpAddr := getEnv("HTTP_ADDR", ":8080")

	slog.Info("starting server", "clip_addr", clipAddr, "qdrant_addr", qdrantAddr, "dim", clipDim)

	clipClient := clip.NewClient(clipAddr)
	qdrantStore, err := store.NewQdrantStore(qdrantAddr, clipDim, 15*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant", "error", err)
		os.Exit(1)
	}
	defer qdrantStore.Close()

	engine := search.NewEngine(clipClient, qdrantStore)
	router := api.NewRouter(engine)

	srv := &http.Server{
		Addr:         httpAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", httpAddr)
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
