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
	"iris/internal/constants"
	"iris/internal/encoder"
	"iris/internal/search"
	"iris/internal/store"
	"iris/internal/tracing"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadServer()

	slog.Info("starting server", "clip_addr", cfg.ClipAddr, "siglip2_addr", cfg.SigLIP2Addr, "qdrant_addr", cfg.QdrantAddr, "encoders", cfg.EnabledEncoders(), "otel_enabled", cfg.OtelEnabled)

	// Initialize OpenTelemetry tracer if enabled
	var otelShutdown func()
	if cfg.OtelEnabled {
		var err error
		otelShutdown, err = tracing.InitTracer(context.Background(), "iris-server", cfg.OtelEndpoint)
		if err != nil {
			slog.Warn("failed to initialize tracer, continuing without tracing", "error", err)
			otelShutdown = nil
		} else {
			defer otelShutdown()
			slog.Info("tracing initialized", "endpoint", cfg.OtelEndpoint)
		}
	}

	encoderRegistry, cleanupEncoders, err := encoder.NewRegistryFromConfig(cfg.Shared)
	if err != nil {
		slog.Error("failed to create encoder registry", "error", err)
		os.Exit(1)
	}
	defer cleanupEncoders()
	qdrantStore, err := store.NewQdrantStoreWithEncoders(cfg.QdrantAddr, cfg.EncoderDims(), 3*time.Second)
	if err != nil {
		slog.Error("failed to connect to qdrant, search will be unavailable", "error", err)
	} else {
		defer qdrantStore.Close()
	}

	engine := search.NewEngine(encoderRegistry, qdrantStore)
	crawlService, jobStore, cleanup, err := api.NewCrawlService(cfg.JobBackend, cfg.JobStoreDSN)
	if err != nil {
		slog.Error("failed to initialize crawl service", "error", err)
	} else if cleanup != nil {
		defer cleanup()
	}
	router := api.NewRouterWithAssetsAndAuth(engine, api.AssetsSettings{
		Backend:    cfg.AssetBackend,
		Bucket:     cfg.AssetBucket,
		Region:     cfg.AssetRegion,
		Endpoint:   cfg.AssetEndpoint,
		AccessKey:  cfg.AssetAccessKey,
		SecretKey:  cfg.AssetSecretKey,
		SessionKey: cfg.AssetSessionKey,
		Prefix:     cfg.AssetPrefix,
		PublicBase: cfg.AssetPublicBase,
		PathStyle:  cfg.AssetPathStyle,
	}, crawlService, api.AdminAuthSettings{
		AdminAPIKey:     cfg.AdminAPIKey,
		ReadOnlyAPIKeys: cfg.AdminReadOnlyAPIKeys,
	}, jobStore)

	srv := newHTTPServer(cfg.HTTPAddr, router)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout10s)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("server stopped")
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       constants.HTTPTimeout30s,
		WriteTimeout:      constants.HTTPTimeout60s,
		IdleTimeout:       constants.HTTPTimeout120s,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
}
