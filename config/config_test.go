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
	t.Setenv("ASSET_BACKEND", "")
	t.Setenv("ASSET_S3_BUCKET", "")
	t.Setenv("ASSET_S3_REGION", "")
	t.Setenv("ASSET_S3_ENDPOINT", "")
	t.Setenv("ASSET_S3_PREFIX", "")
	t.Setenv("ASSET_S3_PUBLIC_BASE", "")
	t.Setenv("ASSET_S3_PATH_STYLE", "")
	t.Setenv("JOB_BACKEND", "")
	t.Setenv("JOB_STORE_DSN", "")
	t.Setenv("ADMIN_API_KEY", "")
	t.Setenv("ADMIN_READONLY_API_KEYS", "")

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
	if cfg.AssetBackend != defaultAssetBackend {
		t.Fatalf("expected default asset backend, got %q", cfg.AssetBackend)
	}
	if cfg.AssetBucket != defaultAssetBucket {
		t.Fatalf("expected default asset bucket, got %q", cfg.AssetBucket)
	}
	if cfg.AssetRegion != defaultAssetRegion {
		t.Fatalf("expected default asset region, got %q", cfg.AssetRegion)
	}
	if cfg.AssetEndpoint != defaultAssetEndpoint {
		t.Fatalf("expected default asset endpoint, got %q", cfg.AssetEndpoint)
	}
	if cfg.AssetPrefix != defaultAssetPrefix {
		t.Fatalf("expected default asset prefix, got %q", cfg.AssetPrefix)
	}
	if cfg.AssetPublicBase != defaultAssetPublicBase {
		t.Fatalf("expected default asset public base, got %q", cfg.AssetPublicBase)
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
	if len(cfg.AdminReadOnlyAPIKeys) != 0 {
		t.Fatalf("expected no readonly admin api keys, got %v", cfg.AdminReadOnlyAPIKeys)
	}
}

func TestLoadServerReadOnlyAdminKeys(t *testing.T) {
	t.Setenv("ADMIN_READONLY_API_KEYS", "viewer-1, viewer-2 ,viewer-3")

	cfg := LoadServer()

	if len(cfg.AdminReadOnlyAPIKeys) != 3 {
		t.Fatalf("expected 3 readonly keys, got %d", len(cfg.AdminReadOnlyAPIKeys))
	}
	if cfg.AdminReadOnlyAPIKeys[1] != "viewer-2" {
		t.Fatalf("unexpected readonly key parsing: %v", cfg.AdminReadOnlyAPIKeys)
	}
}

func TestLoadIndexerOverrides(t *testing.T) {
	t.Setenv("CLIP_ADDR", "clip:9000")
	t.Setenv("QDRANT_ADDR", "qdrant:7334")
	t.Setenv("CLIP_DIM", "768")
	t.Setenv("CONCURRENCY", "12")
	t.Setenv("ASSET_DIR", "/tmp/assets")
	t.Setenv("ASSET_BACKEND", "s3")
	t.Setenv("ASSET_S3_BUCKET", "bucket")
	t.Setenv("ASSET_S3_REGION", "us-east-1")
	t.Setenv("ASSET_S3_ENDPOINT", "http://minio:9000")
	t.Setenv("ASSET_S3_PREFIX", "images")
	t.Setenv("ASSET_S3_PUBLIC_BASE", "https://cdn.example.com")
	t.Setenv("ASSET_S3_PATH_STYLE", "true")

	cfg := LoadIndexer()

	if cfg.ClipAddr != "clip:9000" {
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
	if cfg.AssetBackend != "s3" {
		t.Fatalf("unexpected asset backend: %q", cfg.AssetBackend)
	}
	if cfg.AssetBucket != "bucket" {
		t.Fatalf("unexpected asset bucket: %q", cfg.AssetBucket)
	}
	if cfg.AssetRegion != "us-east-1" {
		t.Fatalf("unexpected asset region: %q", cfg.AssetRegion)
	}
	if cfg.AssetEndpoint != "http://minio:9000" {
		t.Fatalf("unexpected asset endpoint: %q", cfg.AssetEndpoint)
	}
	if cfg.AssetPrefix != "images" {
		t.Fatalf("unexpected asset prefix: %q", cfg.AssetPrefix)
	}
	if cfg.AssetPublicBase != "https://cdn.example.com" {
		t.Fatalf("unexpected asset public base: %q", cfg.AssetPublicBase)
	}
	if !cfg.AssetPathStyle {
		t.Fatalf("expected asset path style true")
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
	t.Setenv("SCHEDULE_POLL_INTERVAL", "45s")

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
	if cfg.SchedulePollInterval != 45*time.Second {
		t.Fatalf("unexpected schedule poll interval: %s", cfg.SchedulePollInterval)
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

func TestLoadWorkerNewFieldsDefaults(t *testing.T) {
	// Clear any env vars that might be set
	t.Setenv("MAX_IMAGE_BYTES", "")
	t.Setenv("FETCH_TIMEOUT", "")
	t.Setenv("USER_AGENT", "")
	t.Setenv("CRAWL_MAX_DEPTH", "")
	t.Setenv("CRAWL_RPS", "")

	cfg := LoadWorker()

	if cfg.MaxImageBytes != defaultMaxImageBytes {
		t.Fatalf("expected default max image bytes %d, got %d", defaultMaxImageBytes, cfg.MaxImageBytes)
	}
	if cfg.FetchTimeout != defaultFetchTimeout {
		t.Fatalf("expected default fetch timeout %s, got %s", defaultFetchTimeout, cfg.FetchTimeout)
	}
	if cfg.UserAgent != defaultUserAgent {
		t.Fatalf("expected default user agent %q, got %q", defaultUserAgent, cfg.UserAgent)
	}
	if cfg.CrawlMaxDepth != defaultCrawlMaxDepth {
		t.Fatalf("expected default crawl max depth %d, got %d", defaultCrawlMaxDepth, cfg.CrawlMaxDepth)
	}
	if cfg.CrawlRPS != defaultCrawlRPS {
		t.Fatalf("expected default crawl rps %d, got %d", defaultCrawlRPS, cfg.CrawlRPS)
	}
}

func TestLoadWorkerNewFieldsOverrides(t *testing.T) {
	t.Setenv("MAX_IMAGE_BYTES", "10485760") // 10MB
	t.Setenv("FETCH_TIMEOUT", "60s")
	t.Setenv("USER_AGENT", "test-agent/1.0")
	t.Setenv("CRAWL_MAX_DEPTH", "5")
	t.Setenv("CRAWL_RPS", "10")

	cfg := LoadWorker()

	if cfg.MaxImageBytes != 10485760 {
		t.Fatalf("expected max image bytes 10485760, got %d", cfg.MaxImageBytes)
	}
	if cfg.FetchTimeout != 60*time.Second {
		t.Fatalf("expected fetch timeout 60s, got %s", cfg.FetchTimeout)
	}
	if cfg.UserAgent != "test-agent/1.0" {
		t.Fatalf("expected user agent 'test-agent/1.0', got %q", cfg.UserAgent)
	}
	if cfg.CrawlMaxDepth != 5 {
		t.Fatalf("expected crawl max depth 5, got %d", cfg.CrawlMaxDepth)
	}
	if cfg.CrawlRPS != 10 {
		t.Fatalf("expected crawl rps 10, got %d", cfg.CrawlRPS)
	}
}
