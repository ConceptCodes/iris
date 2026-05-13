package search

import (
	"context"
	"fmt"

	"iris/internal/authority"
	"iris/internal/encoder"
	"iris/internal/tracing"
	"iris/pkg/models"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("iris/search")

type Indexer struct {
	encoders *encoder.Registry
	store    VectorStore
	tracker  authority.Tracker
}

func NewIndexer(encoders *encoder.Registry, store VectorStore, tracker authority.Tracker) *Indexer {
	return &Indexer{
		encoders: encoders,
		store:    store,
		tracker:  tracker,
	}
}

func (ix *Indexer) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "IndexFromURL",
		[]attribute.KeyValue{
			attribute.String("url", req.URL),
			attribute.String("filename", req.Filename),
			attribute.Int("tags_count", len(req.Tags)),
		},
	)
	defer span.End()

	if ix.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return "", errSearchUnavailable
	}
	deduper := NewDeduper(ix.store)
	if existing, ok, err := deduper.FindExistingID(ctx, req.Meta, req.URL); err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	} else if ok {
		return existing, nil
	}
	embeddings, err := embedAllImageURLs(ctx, ix.encoders, req.URL)
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
	id, err := ix.store.Upsert(ctx, record, embeddings)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	if ix.tracker != nil {
		ix.tracker.RecordDomain(ctx, req.URL)
	}
	return id, nil
}

func (ix *Indexer) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "IndexFromBytes",
		[]attribute.KeyValue{
			attribute.Int("image_size", len(imageBytes)),
			attribute.String("filename", record.Filename),
			attribute.Int("tags_count", len(record.Tags)),
		},
	)
	defer span.End()

	if ix.store == nil {
		tracing.AddErrorToSpan(span, errSearchUnavailable)
		return "", errSearchUnavailable
	}
	deduper := NewDeduper(ix.store)
	if existing, ok, err := deduper.FindExistingID(ctx, record.Meta, ""); err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	} else if ok {
		return existing, nil
	}
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	embeddings, err := embedAllImageBytes(ctx, ix.encoders, imageBytes)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	id, err := ix.store.Upsert(ctx, record, embeddings)
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", err
	}
	if ix.tracker != nil && record.URL != "" {
		ix.tracker.RecordDomain(ctx, record.URL)
	}
	return id, nil
}

func (ix *Indexer) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	if ix.store == nil {
		return "", errSearchUnavailable
	}
	if record.ID == "" {
		deduper := NewDeduper(ix.store)
		if existing, ok, err := deduper.FindExistingID(ctx, record.Meta, ""); err != nil {
			return "", err
		} else if ok {
			record.ID = existing
		} else {
			record.ID = uuid.New().String()
		}
	}
	embeddings, err := embedAllImageBytes(ctx, ix.encoders, imageBytes)
	if err != nil {
		return "", err
	}
	return ix.store.Upsert(ctx, record, embeddings)
}

func embedAllImageURLs(ctx context.Context, encoders *encoder.Registry, imageURL string) (models.Embeddings, error) {
	embeddings := make(models.Embeddings, len(encoders.Names()))
	for _, name := range encoders.Names() {
		_, client, err := encoders.Resolve(name)
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

func embedAllImageBytes(ctx context.Context, encoders *encoder.Registry, imageBytes []byte) (models.Embeddings, error) {
	embeddings := make(models.Embeddings, len(encoders.Names()))
	for _, name := range encoders.Names() {
		_, client, err := encoders.Resolve(name)
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
