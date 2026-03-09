package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultClipAddr             = "localhost:8001"
	defaultQdrantAddr           = "localhost:6334"
	defaultClipDim              = 512
	defaultHTTPAddr             = ":8080"
	defaultConcurrency          = 4
	defaultAssetDir             = "./data/assets"
	defaultAssetBackend         = "local"
	defaultAssetBucket          = ""
	defaultAssetRegion          = ""
	defaultAssetEndpoint        = ""
	defaultAssetPrefix          = ""
	defaultAssetPublicBase      = ""
	defaultWorkerMode           = "indexer"
	defaultJobBackend           = "memory"
	defaultJobStoreDSN          = "postgres://iris:iris@localhost:5432/iris?sslmode=disable"
	defaultAdminAPIKey          = ""
	defaultFetchRetries         = 2
	defaultHostConcurrency      = 2
	defaultCachePruneBatch      = 500
	defaultMaxImageBytes        = 20 << 20
	defaultFetchTimeout         = 30 * time.Second
	defaultUserAgent            = "iris/1.0"
	defaultCrawlMaxDepth        = 1
	defaultCrawlRPS             = 0
	defaultOtelEnabled          = true
	defaultOtelEndpoint         = "localhost:4317"
	defaultSSRFAllowPrivateNets = false
)

const (
	WorkerModeIndexer = "indexer"
	WorkerModeCrawler = "crawler"
)

type Shared struct {
	ClipAddr                 string
	QdrantAddr               string
	ClipDim                  int
	AssetDir                 string
	AssetBackend             string
	AssetBucket              string
	AssetRegion              string
	AssetEndpoint            string
	AssetAccessKey           string
	AssetSecretKey           string
	AssetSessionKey          string
	AssetPrefix              string
	AssetPublicBase          string
	AssetPathStyle           bool
	OtelEnabled              bool
	OtelEndpoint             string
	SSRFAllowPrivateNetworks bool
}

type Server struct {
	Shared
	HTTPAddr             string
	JobBackend           string
	JobStoreDSN          string
	AdminAPIKey          string
	AdminReadOnlyAPIKeys []string
}

type Indexer struct {
	Shared
	Concurrency int
}

type Worker struct {
	Shared
	Mode                 string
	JobBackend           string
	JobStoreDSN          string
	JobPollInterval      time.Duration
	SchedulePollInterval time.Duration
	LeaseDuration        time.Duration
	FetchRetries         int
	FetchRetryBackoff    time.Duration
	HostConcurrency      int
	HTTPCacheTTL         time.Duration
	RobotsCacheTTL       time.Duration
	CachePruneInterval   time.Duration
	CachePruneBatch      int
	MaxImageBytes        int           // Maximum size of image files to fetch, in bytes
	FetchTimeout         time.Duration // Timeout for HTTP fetch requests
	UserAgent            string        // User-Agent header for HTTP requests
	CrawlMaxDepth        int           // Default maximum crawl depth for sources without explicit MaxDepth
	CrawlRPS             int           // Default crawl requests per second for sources without explicit RateLimitRPS
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
		AdminReadOnlyAPIKeys: getEnvCSV("ADMIN_READONLY_API_KEYS"),
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
		SchedulePollInterval: getEnvDuration(
			"SCHEDULE_POLL_INTERVAL",
			30*time.Second,
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
		MaxImageBytes: getEnvInt(
			"MAX_IMAGE_BYTES",
			defaultMaxImageBytes,
		),
		FetchTimeout: getEnvDuration(
			"FETCH_TIMEOUT",
			defaultFetchTimeout,
		),
		UserAgent: getEnv(
			"USER_AGENT",
			defaultUserAgent,
		),
		CrawlMaxDepth: getEnvInt(
			"CRAWL_MAX_DEPTH",
			defaultCrawlMaxDepth,
		),
		CrawlRPS: getEnvInt(
			"CRAWL_RPS",
			defaultCrawlRPS,
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
		AssetBackend: getEnv(
			"ASSET_BACKEND",
			defaultAssetBackend,
		),
		AssetBucket: getEnv(
			"ASSET_S3_BUCKET",
			defaultAssetBucket,
		),
		AssetRegion: getEnv(
			"ASSET_S3_REGION",
			defaultAssetRegion,
		),
		AssetEndpoint: getEnv(
			"ASSET_S3_ENDPOINT",
			defaultAssetEndpoint,
		),
		AssetAccessKey: getEnv(
			"ASSET_S3_ACCESS_KEY",
			"",
		),
		AssetSecretKey: getEnv(
			"ASSET_S3_SECRET_KEY",
			"",
		),
		AssetSessionKey: getEnv(
			"ASSET_S3_SESSION_TOKEN",
			"",
		),
		AssetPrefix: getEnv(
			"ASSET_S3_PREFIX",
			defaultAssetPrefix,
		),
		AssetPublicBase: getEnv(
			"ASSET_S3_PUBLIC_BASE",
			defaultAssetPublicBase,
		),
		AssetPathStyle: getEnvBool(
			"ASSET_S3_PATH_STYLE",
			false,
		),
		OtelEnabled: getEnvBool(
			"OTEL_ENABLED",
			defaultOtelEnabled,
		),
		OtelEndpoint: getEnv(
			"OTEL_ENDPOINT",
			defaultOtelEndpoint,
		),
		SSRFAllowPrivateNetworks: getEnvBool(
			"SSRF_ALLOW_PRIVATE_NETWORKS",
			defaultSSRFAllowPrivateNets,
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

func getEnvBool(key string, def bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return def
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return def
	}
	return parsed
}

func getEnvCSV(key string) []string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
