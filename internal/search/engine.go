package search

import (
	"context"
	"fmt"
	"math"

	"iris/internal/constants"
	"iris/internal/encoder"
	"iris/internal/tracing"
	"iris/pkg/models"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
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
}

var errSearchUnavailable = fmt.Errorf("search engine unavailable: qdrant store not connected")

func NewEngine(encoders *encoder.Registry, qdrantStore VectorStore) Engine {
	return &engineImpl{
		encoders: encoders,
		store:    qdrantStore,
	}
}

var tracer = otel.Tracer("iris/search")

func (e *engineImpl) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "IndexFromURL",
		[]attribute.KeyValue{
			attribute.String("url", req.URL),
			attribute.String("filename", req.Filename),
			attribute.Int("tags_count", len(req.Tags)),
		},
	)
	defer span.End()

	if e.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return "", errSearchUnavailable
	}
	if existing, ok, err := e.FindExistingID(ctx, req.Meta, req.URL); err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	} else if ok {
		return existing, nil
	}
	embeddings, err := e.embedAllImageURLs(ctx, req.URL)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	record := models.ImageRecord{
		ID:       uuid.New().String(),
		URL:      req.URL,
		Filename: req.Filename,
		Tags:     req.Tags,
		Meta:     req.Meta,
	}
	id, err := e.store.Upsert(ctx, record, embeddings)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	return id, nil
}

func (e *engineImpl) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "IndexFromBytes",
		[]attribute.KeyValue{
			attribute.Int("image_size", len(imageBytes)),
			attribute.String("filename", record.Filename),
			attribute.Int("tags_count", len(record.Tags)),
		},
	)
	defer span.End()

	if e.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return "", errSearchUnavailable
	}
	if existing, ok, err := e.FindExistingID(ctx, record.Meta, ""); err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	} else if ok {
		return existing, nil
	}
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	embeddings, err := e.embedAllImageBytes(ctx, imageBytes)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	id, err := e.store.Upsert(ctx, record, embeddings)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	return id, nil
}

func (e *engineImpl) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	if e.store == nil {
		return "", errSearchUnavailable
	}
	if record.ID == "" {
		if existing, ok, err := e.FindExistingID(ctx, record.Meta, ""); err != nil {
			return "", err
		} else if ok {
			record.ID = existing
		} else {
			record.ID = uuid.New().String()
		}
	}
	embeddings, err := e.embedAllImageBytes(ctx, imageBytes)
	if err != nil {
		return "", err
	}
	return e.store.Upsert(ctx, record, embeddings)
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
	return e.store.Search(ctx, encoderName, embedding, topK, filters)
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
	return e.store.Search(ctx, encoderName, embedding, topK+1, nil)
}

func (e *engineImpl) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	if meta == nil {
		meta = map[string]string{}
	}
	if hash := meta[constants.MetaKeyContentSHA256]; hash != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeyContentSHA256, hash); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if source := meta[constants.MetaKeySourceURL]; source != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeySourceURL, source); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if fallbackURL != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeySourceURL, fallbackURL); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	return "", false, nil
}

func (e *engineImpl) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	if e.store == nil {
		return nil, errSearchUnavailable
	}
	return e.store.ListImages(ctx, filters, limit, offset)
}

func (e *engineImpl) embedAllImageURLs(ctx context.Context, imageURL string) (models.Embeddings, error) {
	embeddings := make(models.Embeddings, len(e.encoders.Names()))
	for _, name := range e.encoders.Names() {
		_, client, err := e.encoders.Resolve(name)
		if err != nil {
			return nil, err
		}
		embedding, err := client.EmbedImageURL(ctx, imageURL)
		if err != nil {
			return nil, fmt.Errorf("%s embed image url: %w", name, err)
		}
		normalize(embedding)
		embeddings[name] = embedding
	}
	return embeddings, nil
}

func (e *engineImpl) embedAllImageBytes(ctx context.Context, imageBytes []byte) (models.Embeddings, error) {
	embeddings := make(models.Embeddings, len(e.encoders.Names()))
	for _, name := range e.encoders.Names() {
		_, client, err := e.encoders.Resolve(name)
		if err != nil {
			return nil, err
		}
		embedding, err := client.EmbedImageBytes(ctx, imageBytes)
		if err != nil {
			return nil, fmt.Errorf("%s embed image bytes: %w", name, err)
		}
		normalize(embedding)
		embeddings[name] = embedding
	}
	return embeddings, nil
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
