package config

import (
	"os"
	"strconv"
)

const (
	defaultClipAddr    = "http://localhost:8001"
	defaultQdrantAddr  = "localhost:6334"
	defaultClipDim     = 512
	defaultHTTPAddr    = ":8080"
	defaultConcurrency = 4
	defaultAssetDir    = "./data/assets"
)

type Shared struct {
	ClipAddr   string
	QdrantAddr string
	ClipDim    int
	AssetDir   string
}

type Server struct {
	Shared
	HTTPAddr string
}

type Indexer struct {
	Shared
	Concurrency int
}

func LoadServer() Server {
	return Server{
		Shared: loadShared(),
		HTTPAddr: getEnv(
			"HTTP_ADDR",
			defaultHTTPAddr,
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
