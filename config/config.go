package config

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultClipAddr        = "http://localhost:8001"
	defaultQdrantAddr      = "localhost:6334"
	defaultClipDim         = 512
	defaultHTTPAddr        = ":8080"
	defaultConcurrency     = 4
	defaultAssetDir        = "./data/assets"
	defaultWorkerMode      = "indexer"
	defaultJobBackend      = "memory"
	defaultJobStoreDSN     = "postgres://iris:iris@localhost:5432/iris?sslmode=disable"
	defaultAdminAPIKey     = ""
	defaultFetchRetries    = 2
	defaultHostConcurrency = 2
	defaultCachePruneBatch = 500
)

const (
	WorkerModeIndexer = "indexer"
	WorkerModeCrawler = "crawler"
)

type Shared struct {
	ClipAddr   string
	QdrantAddr string
	ClipDim    int
	AssetDir   string
}

type Server struct {
	Shared
	HTTPAddr    string
	JobBackend  string
	JobStoreDSN string
	AdminAPIKey string
}

type Indexer struct {
	Shared
	Concurrency int
}

type Worker struct {
	Shared
	Mode               string
	JobBackend         string
	JobStoreDSN        string
	JobPollInterval    time.Duration
	LeaseDuration      time.Duration
	FetchRetries       int
	FetchRetryBackoff  time.Duration
	HostConcurrency    int
	HTTPCacheTTL       time.Duration
	RobotsCacheTTL     time.Duration
	CachePruneInterval time.Duration
	CachePruneBatch    int
}

func LoadServer() Server {
	return Server{
		Shared: loadShared(),
		HTTPAddr: getEnv(
			"HTTP_ADDR",
			defaultHTTPAddr,
		),
		JobBackend: getEnv(
			"JOB_BACKEND",
			defaultJobBackend,
		),
		JobStoreDSN: getEnv(
			"JOB_STORE_DSN",
			defaultJobStoreDSN,
		),
		AdminAPIKey: getEnv(
			"ADMIN_API_KEY",
			defaultAdminAPIKey,
		),
	}
}

func LoadIndexer() Indexer {
	return Indexer{
		Shared: loadShared(),
		Concurrency: getEnvInt(
			"CONCURRENCY",
			defaultConcurrency,
		),
	}
}

func LoadWorker() Worker {
	return Worker{
		Shared:     loadShared(),
		Mode:       getEnv("WORKER_MODE", defaultWorkerMode),
		JobBackend: getEnv("JOB_BACKEND", defaultJobBackend),
		JobStoreDSN: getEnv(
			"JOB_STORE_DSN",
			defaultJobStoreDSN,
		),
		JobPollInterval: getEnvDuration(
			"JOB_POLL_INTERVAL",
			time.Second,
		),
		LeaseDuration: getEnvDuration(
			"LEASE_DURATION",
			30*time.Second,
		),
		FetchRetries: getEnvInt(
			"FETCH_RETRIES",
			defaultFetchRetries,
		),
		FetchRetryBackoff: getEnvDuration(
			"FETCH_RETRY_BACKOFF",
			500*time.Millisecond,
		),
		HostConcurrency: getEnvInt(
			"HOST_CONCURRENCY",
			defaultHostConcurrency,
		),
		HTTPCacheTTL: getEnvDuration(
			"HTTP_CACHE_TTL",
			10*time.Minute,
		),
		RobotsCacheTTL: getEnvDuration(
			"ROBOTS_CACHE_TTL",
			24*time.Hour,
		),
		CachePruneInterval: getEnvDuration(
			"CACHE_PRUNE_INTERVAL",
			15*time.Minute,
		),
		CachePruneBatch: getEnvInt(
			"CACHE_PRUNE_BATCH",
			defaultCachePruneBatch,
		),
	}
}

func loadShared() Shared {
	return Shared{
		ClipAddr: getEnv(
			"CLIP_ADDR",
			defaultClipAddr,
		),
		QdrantAddr: getEnv(
			"QDRANT_ADDR",
			defaultQdrantAddr,
		),
		ClipDim: getEnvInt(
			"CLIP_DIM",
			defaultClipDim,
		),
		AssetDir: getEnv(
			"ASSET_DIR",
			defaultAssetDir,
		),
	}
}

func getEnv(key, def string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return def
}

func getEnvInt(key string, def int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return def
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return parsed
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return def
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return def
	}
	return parsed
}
