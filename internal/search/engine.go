package search

import (
	"context"
	"fmt"
	"math"
	"reflect"

	"iris/pkg/models"
	"github.com/google/uuid"
)

const defaultTopK = 20

type Engine interface {
	IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error)
	IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error)
	SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error)
	SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string) ([]models.SearchResult, error)
	GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error)
}

type ClipClient interface {
	EmbedText(ctx context.Context, text string) (models.Embedding, error)
	EmbedImageBytes(ctx context.Context, imageBytes []byte) (models.Embedding, error)
	EmbedImageURL(ctx context.Context, imageURL string) (models.Embedding, error)
}

type VectorStore interface {
	Upsert(ctx context.Context, record models.ImageRecord, embedding models.Embedding) (string, error)
	Search(ctx context.Context, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error)
	GetVector(ctx context.Context, id string) (models.Embedding, error)
}

type engineImpl struct {
	clip  ClipClient
	store VectorStore
}

func NewEngine(clipClient ClipClient, qdrantStore VectorStore) Engine {
	return &engineImpl{
		clip:  clipClient,
		store: qdrantStore,
	}
}

func (e *engineImpl) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return "", fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	embedding, err := e.clip.EmbedImageURL(ctx, req.URL)
	if err != nil {
		return "", err
	}
	normalize(embedding)
	record := models.ImageRecord{
		ID:       uuid.New().String(),
		URL:      req.URL,
		Filename: req.Filename,
		Tags:     req.Tags,
		Meta:     req.Meta,
	}
	return e.store.Upsert(ctx, record, embedding)
}

func (e *engineImpl) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return "", fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	embedding, err := e.clip.EmbedImageBytes(ctx, imageBytes)
	if err != nil {
		return "", err
	}
	normalize(embedding)
	return e.store.Upsert(ctx, record, embedding)
}

func (e *engineImpl) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return nil, fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	topK := req.TopK
	if topK == 0 {
		topK = defaultTopK
	}
	embedding, err := e.clip.EmbedText(ctx, req.Query)
	if err != nil {
		return nil, err
	}
	normalize(embedding)
	return e.store.Search(ctx, embedding, topK, req.Filters)
}

func (e *engineImpl) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return nil, fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	if topK == 0 {
		topK = defaultTopK
	}
	embedding, err := e.clip.EmbedImageBytes(ctx, imageBytes)
	if err != nil {
		return nil, err
	}
	normalize(embedding)
	return e.store.Search(ctx, embedding, topK, filters)
}

func (e *engineImpl) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string) ([]models.SearchResult, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return nil, fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	if topK == 0 {
		topK = defaultTopK
	}
	embedding, err := e.clip.EmbedImageURL(ctx, url)
	if err != nil {
		return nil, err
	}
	normalize(embedding)
	return e.store.Search(ctx, embedding, topK, filters)
}

func (e *engineImpl) GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return nil, fmt.Errorf("search engine unavailable: qdrant store not connected")
	}
	if topK == 0 {
		topK = defaultTopK
	}
	embedding, err := e.store.GetVector(ctx, id)
	if err != nil {
		return nil, err
	}
	return e.store.Search(ctx, embedding, topK+1, nil)
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
