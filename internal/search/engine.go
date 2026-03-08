package search

import (
	"context"
	"math"

	"github.com/davidojo/google-images/internal/clip"
	"github.com/davidojo/google-images/internal/store"
	"github.com/davidojo/google-images/pkg/models"
	"github.com/google/uuid"
)

const defaultTopK = 20

type Engine struct {
	clip  *clip.Client
	store *store.QdrantStore
}

func NewEngine(clipClient *clip.Client, qdrantStore *store.QdrantStore) *Engine {
	return &Engine{
		clip:  clipClient,
		store: qdrantStore,
	}
}

func (e *Engine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
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

func (e *Engine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
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

func (e *Engine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
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

func (e *Engine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error) {
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

func (e *Engine) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string) ([]models.SearchResult, error) {
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

func (e *Engine) GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error) {
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
