package config

import (
	"testing"
	"time"
)

func TestLoadServerDefaults(t *testing.T) {
	t.Setenv("CLIP_ADDR", "")
	t.Setenv("QDRANT_ADDR", "")
	t.Setenv("CLIP_DIM", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("ASSET_DIR", "")
	t.Setenv("JOB_BACKEND", "")
	t.Setenv("JOB_STORE_DSN", "")
	t.Setenv("ADMIN_API_KEY", "")

	cfg := LoadServer()

	if cfg.ClipAddr != defaultClipAddr {
		t.Fatalf("expected default clip addr, got %q", cfg.ClipAddr)
	}
	if cfg.QdrantAddr != defaultQdrantAddr {
		t.Fatalf("expected default qdrant addr, got %q", cfg.QdrantAddr)
	}
	if cfg.ClipDim != defaultClipDim {
		t.Fatalf("expected default clip dim, got %d", cfg.ClipDim)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("expected default http addr, got %q", cfg.HTTPAddr)
	}
	if cfg.AssetDir != defaultAssetDir {
		t.Fatalf("expected default asset dir, got %q", cfg.AssetDir)
	}
	if cfg.JobBackend != defaultJobBackend {
		t.Fatalf("expected default job backend, got %q", cfg.JobBackend)
	}
	if cfg.JobStoreDSN != defaultJobStoreDSN {
		t.Fatalf("expected default job store dsn, got %q", cfg.JobStoreDSN)
	}
	if cfg.AdminAPIKey != defaultAdminAPIKey {
		t.Fatalf("expected default admin api key, got %q", cfg.AdminAPIKey)
	}
}

func TestLoadIndexerOverrides(t *testing.T) {
	t.Setenv("CLIP_ADDR", "http://clip:9000")
	t.Setenv("QDRANT_ADDR", "qdrant:7334")
	t.Setenv("CLIP_DIM", "768")
	t.Setenv("CONCURRENCY", "12")
	t.Setenv("ASSET_DIR", "/tmp/assets")

	cfg := LoadIndexer()

	if cfg.ClipAddr != "http://clip:9000" {
		t.Fatalf("unexpected clip addr: %q", cfg.ClipAddr)
	}
	if cfg.QdrantAddr != "qdrant:7334" {
		t.Fatalf("unexpected qdrant addr: %q", cfg.QdrantAddr)
	}
	if cfg.ClipDim != 768 {
		t.Fatalf("unexpected clip dim: %d", cfg.ClipDim)
	}
	if cfg.Concurrency != 12 {
		t.Fatalf("unexpected concurrency: %d", cfg.Concurrency)
	}
	if cfg.AssetDir != "/tmp/assets" {
		t.Fatalf("unexpected asset dir: %q", cfg.AssetDir)
	}
}

func TestLoadIndexerInvalidIntFallbacks(t *testing.T) {
	t.Setenv("CLIP_DIM", "bad")
	t.Setenv("CONCURRENCY", "bad")

	cfg := LoadIndexer()

	if cfg.ClipDim != defaultClipDim {
		t.Fatalf("expected default clip dim fallback, got %d", cfg.ClipDim)
	}
	if cfg.Concurrency != defaultConcurrency {
		t.Fatalf("expected default concurrency fallback, got %d", cfg.Concurrency)
	}
}

func TestLoadWorkerDefaultsAndOverrides(t *testing.T) {
	t.Setenv("WORKER_MODE", "crawler")
	t.Setenv("JOB_BACKEND", "memory")
	t.Setenv("JOB_STORE_DSN", "postgres://worker:test@db:5432/iris?sslmode=disable")
	t.Setenv("JOB_POLL_INTERVAL", "2s")
	t.Setenv("LEASE_DURATION", "45s")
	t.Setenv("FETCH_RETRIES", "4")
	t.Setenv("FETCH_RETRY_BACKOFF", "750ms")
	t.Setenv("HOST_CONCURRENCY", "3")
	t.Setenv("HTTP_CACHE_TTL", "15m")
	t.Setenv("ROBOTS_CACHE_TTL", "12h")
	t.Setenv("CACHE_PRUNE_INTERVAL", "20m")
	t.Setenv("CACHE_PRUNE_BATCH", "250")

	cfg := LoadWorker()

	if cfg.Mode != "crawler" {
		t.Fatalf("unexpected mode: %q", cfg.Mode)
	}
	if cfg.JobBackend != "memory" {
		t.Fatalf("unexpected backend: %q", cfg.JobBackend)
	}
	if cfg.JobStoreDSN != "postgres://worker:test@db:5432/iris?sslmode=disable" {
		t.Fatalf("unexpected dsn: %q", cfg.JobStoreDSN)
	}
	if cfg.JobPollInterval != 2*time.Second {
		t.Fatalf("unexpected poll interval: %s", cfg.JobPollInterval)
	}
	if cfg.LeaseDuration != 45*time.Second {
		t.Fatalf("unexpected lease duration: %s", cfg.LeaseDuration)
	}
	if cfg.FetchRetries != 4 {
		t.Fatalf("unexpected fetch retries: %d", cfg.FetchRetries)
	}
	if cfg.FetchRetryBackoff != 750*time.Millisecond {
		t.Fatalf("unexpected fetch retry backoff: %s", cfg.FetchRetryBackoff)
	}
	if cfg.HostConcurrency != 3 {
		t.Fatalf("unexpected host concurrency: %d", cfg.HostConcurrency)
	}
	if cfg.HTTPCacheTTL != 15*time.Minute {
		t.Fatalf("unexpected http cache ttl: %s", cfg.HTTPCacheTTL)
	}
	if cfg.RobotsCacheTTL != 12*time.Hour {
		t.Fatalf("unexpected robots cache ttl: %s", cfg.RobotsCacheTTL)
	}
	if cfg.CachePruneInterval != 20*time.Minute {
		t.Fatalf("unexpected cache prune interval: %s", cfg.CachePruneInterval)
	}
	if cfg.CachePruneBatch != 250 {
		t.Fatalf("unexpected cache prune batch: %d", cfg.CachePruneBatch)
	}
}
