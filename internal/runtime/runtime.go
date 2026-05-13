package runtime

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/authority"
	"iris/internal/encoder"
	"iris/internal/indexing"
	"iris/internal/metadata"
	"iris/internal/search"
	"iris/internal/store"
	"iris/pkg/models"
)

type Config struct {
	QdrantAddr               string
	EncoderDims              map[models.Encoder]int
	ConnectTimeout           time.Duration
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
	MetadataAddr             string
	MetadataTimeout          time.Duration
	MaxFetchBytes            int
	FetchClient              *http.Client
	FetchTimeout             time.Duration
	UserAgent                string
	SSRFAllowPrivateNetworks bool
}

func ConfigFromShared(s config.Shared) Config {
	return Config{
		QdrantAddr:               s.QdrantAddr,
		EncoderDims:              s.EncoderDims(),
		ConnectTimeout:           15 * time.Second,
		AssetBackend:             s.AssetBackend,
		AssetBucket:              s.AssetBucket,
		AssetRegion:              s.AssetRegion,
		AssetEndpoint:            s.AssetEndpoint,
		AssetAccessKey:           s.AssetAccessKey,
		AssetSecretKey:           s.AssetSecretKey,
		AssetSessionKey:          s.AssetSessionKey,
		AssetPrefix:              s.AssetPrefix,
		AssetPublicBase:          s.AssetPublicBase,
		AssetPathStyle:           s.AssetPathStyle,
		MetadataAddr:             s.MetadataAddr,
		MetadataTimeout:          45 * time.Second,
		MaxFetchBytes:            20 << 20,
		FetchTimeout:             30 * time.Second,
		UserAgent:                "iris/1.0",
		SSRFAllowPrivateNetworks: s.SSRFAllowPrivateNetworks,
	}
}

type IngestionRuntime struct {
	EncoderRegistry *encoder.Registry
	QdrantStore     *store.QdrantStore
	Ranker          *search.Ranker
	Authority       authority.Tracker
	Engine          search.Engine
	AssetStore      assets.Store
	Pipeline        *indexing.Pipeline
	cleanupEncoders func()
}

type SearchRuntime struct {
	EncoderRegistry *encoder.Registry
	QdrantStore     *store.QdrantStore
	QdrantErr       error
	Ranker          *search.Ranker
	Authority       authority.Tracker
	Engine          search.Engine
	cleanupEncoders func()
}

func NewSearchRuntime(shared config.Shared, cfg Config, allowUnavailableStore bool) (*SearchRuntime, error) {
	encoderRegistry, cleanupEncoders, err := encoder.NewRegistryFromConfig(shared)
	if err != nil {
		return nil, fmt.Errorf("create encoder registry: %w", err)
	}

	qdrantStore, err := store.NewQdrantStoreWithEncoders(cfg.QdrantAddr, cfg.EncoderDims, cfg.ConnectTimeout)
	if err != nil {
		if !allowUnavailableStore {
			cleanupEncoders()
			return nil, fmt.Errorf("connect to qdrant: %w", err)
		}
		qdrantStore = nil
	}

	tracker := authority.NewMemoryStore(nil)
	ranker := search.NewRanker(tracker)
	engine := search.NewEngine(encoderRegistry, qdrantStore, ranker, tracker)

	return &SearchRuntime{
		EncoderRegistry: encoderRegistry,
		QdrantStore:     qdrantStore,
		QdrantErr:       err,
		Ranker:          ranker,
		Authority:       tracker,
		Engine:          engine,
		cleanupEncoders: cleanupEncoders,
	}, nil
}

func (r *SearchRuntime) Close() {
	if r.QdrantStore != nil {
		r.QdrantStore.Close()
	}
	if r.cleanupEncoders != nil {
		r.cleanupEncoders()
	}
}

func NewIngestionRuntime(ctx context.Context, shared config.Shared, cfg Config) (*IngestionRuntime, error) {
	searchRuntime, err := NewSearchRuntime(shared, cfg, false)
	if err != nil {
		return nil, err
	}

	assetStore, err := assets.NewStoreFromSettings(ctx, assets.Settings{
		Backend: cfg.AssetBackend,
		S3: assets.S3Config{
			Bucket:       cfg.AssetBucket,
			Region:       cfg.AssetRegion,
			Endpoint:     cfg.AssetEndpoint,
			AccessKey:    cfg.AssetAccessKey,
			SecretKey:    cfg.AssetSecretKey,
			SessionToken: cfg.AssetSessionKey,
			Prefix:       cfg.AssetPrefix,
			PublicBase:   cfg.AssetPublicBase,
			UsePathStyle: cfg.AssetPathStyle,
		},
	})
	if err != nil {
		searchRuntime.Close()
		return nil, fmt.Errorf("create asset store: %w", err)
	}

	pipeline := indexing.NewPipelineWithOptions(searchRuntime.Engine, indexing.PipelineOptions{
		AssetStore:               assetStore,
		Enricher:                 newMetadataEnricher(cfg.MetadataAddr, cfg.MetadataTimeout),
		MaxFetchBytes:            cfg.MaxFetchBytes,
		FetchClient:              cfg.FetchClient,
		FetchTimeout:             cfg.FetchTimeout,
		UserAgent:                cfg.UserAgent,
		SSRFAllowPrivateNetworks: cfg.SSRFAllowPrivateNetworks,
	})

	return &IngestionRuntime{
		EncoderRegistry: searchRuntime.EncoderRegistry,
		QdrantStore:     searchRuntime.QdrantStore,
		Ranker:          searchRuntime.Ranker,
		Authority:       searchRuntime.Authority,
		Engine:          searchRuntime.Engine,
		AssetStore:      assetStore,
		Pipeline:        pipeline,
		cleanupEncoders: searchRuntime.cleanupEncoders,
	}, nil
}

func (r *IngestionRuntime) Close() {
	if r.QdrantStore != nil {
		r.QdrantStore.Close()
	}
	if r.cleanupEncoders != nil {
		r.cleanupEncoders()
	}
}

func newMetadataEnricher(addr string, timeout time.Duration) metadata.Enricher {
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return metadata.NewComposite(
		metadata.EXIFEnricher{},
		metadata.NewClient(addr, timeout),
	)
}
