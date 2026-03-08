package search

import (
	"context"
	"fmt"
	"math"
	"reflect"

	"github.com/google/uuid"
	"iris/pkg/models"
)

const defaultTopK = 20

type Engine interface {
	IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error)
	IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error)
	SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error)
	SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string) ([]models.SearchResult, error)
	GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error)
	FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error)
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
	FindIDByMeta(ctx context.Context, key, value string) (string, bool, error)
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
	if existing, ok, err := e.FindExistingID(ctx, req.Meta, req.URL); err != nil {
		return "", err
	} else if ok {
		return existing, nil
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
	if existing, ok, err := e.FindExistingID(ctx, record.Meta, ""); err != nil {
		return "", err
	} else if ok {
		return existing, nil
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

func (e *engineImpl) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	if e.store == nil || reflect.ValueOf(e.store).IsNil() {
		return "", fmt.Errorf("search engine unavailable: qdrant store not connected")
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

func (e *engineImpl) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	if meta == nil {
		meta = map[string]string{}
	}
	if hash := meta["content_sha256"]; hash != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, "meta_content_sha256", hash); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if source := meta["source_url"]; source != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, "meta_source_url", source); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if fallbackURL != "" {
		if id, ok, err := e.store.FindIDByMeta(ctx, "meta_source_url", fallbackURL); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	return "", false, nil
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
