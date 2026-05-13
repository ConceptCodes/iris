// Package search provides image search functionality with hybrid ranking.
//
// The search engine combines vector similarity with domain authority, image quality,
// and freshness signals to rank results. This multi-signal approach is inspired by
// Google Images architecture and provides more relevant results than pure vector
// similarity alone.
//
// The engine supports text-to-image search (using CLIP/SigLIP embeddings), reverse
// image search (visual similarity), and metadata-based filtering. Ranking weights
// can be configured via constants to tune the relative importance of each signal.
package search

import (
	"context"
	"fmt"
	"math"

	"iris/internal/authority"
	"iris/internal/constants"
	"iris/internal/encoder"
	"iris/internal/tracing"
	"iris/pkg/models"

	"go.opentelemetry.io/otel/attribute"
)

const defaultTopK = constants.DefaultLimit40

type Engine interface {
	IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error)
	IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error)
	SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error)
	SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error)
	GetSimilar(ctx context.Context, id string, topK int, enc models.Encoder) ([]models.SearchResult, error)
	FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error)
	ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error)
}

type VectorStore interface {
	Upsert(ctx context.Context, record models.ImageRecord, embeddings models.Embeddings) (string, error)
	Search(ctx context.Context, enc models.Encoder, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error)
	GetVector(ctx context.Context, id string, enc models.Encoder) (models.Embedding, error)
	FindIDByMeta(ctx context.Context, key, value string) (string, bool, error)
	ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error)
}

type engineImpl struct {
	encoders *encoder.Registry
	store    VectorStore
	ranker   *Ranker
	tracker  authority.Tracker
}

var errSearchUnavailable = fmt.Errorf("search engine unavailable: qdrant store not connected")

// NewEngine creates a new search engine with the provided components.
//
// The encoders parameter provides access to registered embedding models (CLIP, SigLIP2).
// The qdrantStore handles vector persistence and retrieval. The ranker applies hybrid
// scoring combining similarity, authority, quality, and freshness. The tracker maintains
// domain authority data derived from crawled image counts.
func NewEngine(encoders *encoder.Registry, qdrantStore VectorStore, ranker *Ranker, tracker authority.Tracker) Engine {
	return &engineImpl{
		encoders: encoders,
		store:    qdrantStore,
		ranker:   ranker,
		tracker:  tracker,
	}
}

func (e *engineImpl) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	ix := NewIndexer(e.encoders, e.store, e.tracker)
	return ix.IndexFromURL(ctx, req)
}

func (e *engineImpl) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	ix := NewIndexer(e.encoders, e.store, e.tracker)
	return ix.IndexFromBytes(ctx, imageBytes, record)
}

func (e *engineImpl) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	ix := NewIndexer(e.encoders, e.store, e.tracker)
	return ix.ReindexFromBytes(ctx, imageBytes, record)
}

func (e *engineImpl) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "SearchByText",
		[]attribute.KeyValue{
			attribute.String("query", req.Query),
			attribute.Int("top_k", req.TopK),
		},
	)
	defer span.End()

	if e.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return nil, errSearchUnavailable
	}
	topK := req.TopK
	if topK == 0 {
		topK = defaultTopK
	}
	encoderName, client, err := e.encoders.Resolve(req.Encoder)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	embedding, err := client.EmbedText(ctx, req.Query)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	normalize(embedding)
	results, err := e.store.Search(ctx, encoderName, embedding, topK, req.Filters)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	// Apply hybrid re-ranking
	if e.ranker != nil {
		results = e.ranker.RankResults(ctx, results)
	}
	return results, nil
}

func (e *engineImpl) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "SearchByImageBytes",
		[]attribute.KeyValue{
			attribute.Int("top_k", topK),
			attribute.Int("image_size", len(imageBytes)),
		},
	)
	defer span.End()

	if e.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return nil, errSearchUnavailable
	}
	if topK == 0 {
		topK = defaultTopK
	}
	encoderName, client, err := e.encoders.Resolve(enc)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	embedding, err := client.EmbedImageBytes(ctx, imageBytes)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	normalize(embedding)
	results, err := e.store.Search(ctx, encoderName, embedding, topK, filters)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	// Apply hybrid re-ranking
	if e.ranker != nil {
		results = e.ranker.RankResults(ctx, results)
	}
	return results, nil
}

func (e *engineImpl) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	if e.store == nil {
		return nil, errSearchUnavailable
	}
	if topK == 0 {
		topK = defaultTopK
	}
	encoderName, client, err := e.encoders.Resolve(enc)
	if err != nil {
		return nil, err
	}
	embedding, err := client.EmbedImageURL(ctx, url)
	if err != nil {
		return nil, err
	}
	normalize(embedding)
	results, err := e.store.Search(ctx, encoderName, embedding, topK, filters)
	if err != nil {
		return nil, err
	}
	// Apply hybrid re-ranking
	if e.ranker != nil {
		results = e.ranker.RankResults(ctx, results)
	}
	return results, nil
}

func (e *engineImpl) GetSimilar(ctx context.Context, id string, topK int, enc models.Encoder) ([]models.SearchResult, error) {
	if e.store == nil {
		return nil, errSearchUnavailable
	}
	if topK == 0 {
		topK = defaultTopK
	}
	encoderName, _, err := e.encoders.Resolve(enc)
	if err != nil {
		return nil, err
	}
	embedding, err := e.store.GetVector(ctx, id, encoderName)
	if err != nil {
		return nil, err
	}
	results, err := e.store.Search(ctx, encoderName, embedding, topK+1, nil)
	if err != nil {
		return nil, err
	}
	// Apply hybrid re-ranking
	if e.ranker != nil {
		results = e.ranker.RankResults(ctx, results)
	}
	return results, nil
}

func (e *engineImpl) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	deduper := NewDeduper(e.store)
	return deduper.FindExistingID(ctx, meta, fallbackURL)
}

func (e *engineImpl) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	catalog := NewCatalog(e.store)
	return catalog.ListImages(ctx, filters, limit, offset)
}

func normalize(vec models.Embedding) {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
}
