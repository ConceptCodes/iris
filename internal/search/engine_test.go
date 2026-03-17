package search

import (
	"context"
	"fmt"
	"math"
	"testing"

	"iris/internal/encoder"
	"iris/pkg/models"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    models.Embedding
		expected models.Embedding
	}{
		{
			name:     "zero vector",
			input:    models.Embedding{0, 0, 0},
			expected: models.Embedding{0, 0, 0},
		},
		{
			name:     "simple vector",
			input:    models.Embedding{3, 4},
			expected: models.Embedding{3.0 / 5.0, 4.0 / 5.0},
		},
		{
			name:     "already normalized",
			input:    models.Embedding{0, 1},
			expected: models.Embedding{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalize(tt.input)
			for i := range tt.expected {
				if math.Abs(float64(tt.input[i]-tt.expected[i])) > 1e-6 {
					t.Errorf("expected %v, got %v", tt.expected, tt.input)
				}
			}
		})
	}
}

func TestEngineOffline(t *testing.T) {
	engine := NewEngine(mustTestRegistry(t, &mockClip{}), nil, nil, nil)

	ctx := context.Background()

	_, err := engine.SearchByText(ctx, models.TextSearchRequest{})
	if err == nil {
		t.Error("expected error when store is nil")
	}

	_, err = engine.SearchByImageURL(ctx, "http://example.com/image.jpg", 10, nil, "")
	if err == nil {
		t.Error("expected error when store is nil")
	}

	_, err = engine.GetSimilar(ctx, "123", 10, "")
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

type mockClip struct {
	err error
	emb models.Embedding

	lastText     string
	lastImgBytes []byte
	lastURL      string
}

func (m *mockClip) EmbedText(ctx context.Context, text string) (models.Embedding, error) {
	m.lastText = text
	return m.emb, m.err
}
func (m *mockClip) EmbedImageBytes(ctx context.Context, imageBytes []byte) (models.Embedding, error) {
	m.lastImgBytes = imageBytes
	return m.emb, m.err
}
func (m *mockClip) EmbedImageURL(ctx context.Context, imageURL string) (models.Embedding, error) {
	m.lastURL = imageURL
	return m.emb, m.err
}

type mockStore struct {
	err error
	id  string
	res []models.SearchResult
	emb models.Embedding

	lastID            string
	lastRecord        models.ImageRecord
	lastEmbeddings    models.Embeddings
	lastTopK          int
	lastFilters       map[string]string
	lastSearchEncoder models.Encoder
	findID            string
	findOK            bool
}

func (m *mockStore) Upsert(ctx context.Context, record models.ImageRecord, embeddings models.Embeddings) (string, error) {
	m.lastRecord = record
	m.lastEmbeddings = embeddings
	return m.id, m.err
}
func (m *mockStore) Search(ctx context.Context, enc models.Encoder, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error) {
	m.lastTopK = topK
	m.lastFilters = filters
	m.lastSearchEncoder = enc
	return m.res, m.err
}
func (m *mockStore) GetVector(ctx context.Context, id string, enc models.Encoder) (models.Embedding, error) {
	m.lastID = id
	return m.emb, m.err
}

func (m *mockStore) FindIDByMeta(ctx context.Context, key, value string) (string, bool, error) {
	return m.findID, m.findOK, m.err
}

func (m *mockStore) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	return []models.ImageRecord{}, m.err
}

func TestEngine_IndexFromURL(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mc := &mockClip{emb: models.Embedding{1.0, 0.0}}
		ms := &mockStore{id: "new-id"}
		e := NewEngine(mustTestRegistry(t, mc), ms, nil, nil)

		id, err := e.IndexFromURL(context.Background(), models.IndexRequest{URL: "http://x", Filename: "x.jpg"})
		if err != nil {
			t.Fatal(err)
		}
		if id != "new-id" {
			t.Errorf("expected new-id")
		}
		if mc.lastURL != "http://x" {
			t.Errorf("did not pass URL to clip")
		}
		if ms.lastRecord.ID == "" {
			t.Errorf("UUID not generated")
		}
	})
	t.Run("clip error", func(t *testing.T) {
		e := NewEngine(mustTestRegistry(t, &mockClip{err: fmt.Errorf("clip fail")}), &mockStore{}, nil, nil)
		_, err := e.IndexFromURL(context.Background(), models.IndexRequest{URL: "http://x"})
		if err == nil || err.Error() != "clip embed image url: clip fail" {
			t.Errorf("expected wrapped clip fail, got %v", err)
		}
	})
}

func TestEngine_IndexFromBytes(t *testing.T) {
	t.Run("auto id", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		e.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{})
		if ms.lastRecord.ID == "" {
			t.Errorf("UUID not generated")
		}
	})
	t.Run("provided id", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		e.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{ID: "my-id"})
		if ms.lastRecord.ID != "my-id" {
			t.Errorf("provided ID overwritten")
		}
	})
	t.Run("dedupe returns existing", func(t *testing.T) {
		ms := &mockStore{findID: "existing", findOK: true}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		id, err := e.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{Meta: map[string]string{"content_sha256": "abc"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "existing" {
			t.Fatalf("expected existing id")
		}
	})

	t.Run("FindExistingID passthrough", func(t *testing.T) {
		ms := &mockStore{findID: "existing", findOK: true}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		id, ok, err := e.FindExistingID(context.Background(), map[string]string{"content_sha256": "abc"}, "")
		if err != nil || !ok || id != "existing" {
			t.Fatalf("unexpected result: id=%s ok=%v err=%v", id, ok, err)
		}
	})
}

func TestEngine_SearchByText(t *testing.T) {
	t.Run("default topK", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		e.SearchByText(context.Background(), models.TextSearchRequest{})
		if ms.lastTopK != defaultTopK {
			t.Errorf("expected default topK, got %d", ms.lastTopK)
		}
	})
	t.Run("clip error", func(t *testing.T) {
		e := NewEngine(mustTestRegistry(t, &mockClip{err: fmt.Errorf("fail")}), &mockStore{}, nil, nil)
		_, err := e.SearchByText(context.Background(), models.TextSearchRequest{})
		if err == nil {
			t.Errorf("expected error")
		}
	})
}

func TestEngine_IndexFromBytesStoresAllEncoderEmbeddings(t *testing.T) {
	clipClient := &mockClip{emb: models.Embedding{1, 0}}
	siglipClient := &mockClip{emb: models.Embedding{0, 1}}
	store := &mockStore{id: "img-id"}
	engine := NewEngine(mustNamedRegistry(t, models.EncoderCLIP, map[models.Encoder]encoder.Client{
		models.EncoderCLIP:    clipClient,
		models.EncoderSigLIP2: siglipClient,
	}), store, nil, nil)

	id, err := engine.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{Filename: "photo.jpg"})
	if err != nil {
		t.Fatalf("index from bytes: %v", err)
	}
	if id != "img-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if len(store.lastEmbeddings) != 2 {
		t.Fatalf("expected 2 encoder embeddings, got %d", len(store.lastEmbeddings))
	}
	if len(store.lastEmbeddings[models.EncoderCLIP]) == 0 {
		t.Fatalf("expected clip embedding to be stored")
	}
	if len(store.lastEmbeddings[models.EncoderSigLIP2]) == 0 {
		t.Fatalf("expected siglip2 embedding to be stored")
	}
}

func TestEngine_SearchByTextUsesRequestedEncoder(t *testing.T) {
	clipClient := &mockClip{emb: models.Embedding{1, 0}}
	siglipClient := &mockClip{emb: models.Embedding{0, 1}}
	store := &mockStore{}
	engine := NewEngine(mustNamedRegistry(t, models.EncoderCLIP, map[models.Encoder]encoder.Client{
		models.EncoderCLIP:    clipClient,
		models.EncoderSigLIP2: siglipClient,
	}), store, nil, nil)

	_, err := engine.SearchByText(context.Background(), models.TextSearchRequest{
		Query:   "mountain lake",
		Encoder: models.EncoderSigLIP2,
	})
	if err != nil {
		t.Fatalf("search by text: %v", err)
	}
	if siglipClient.lastText != "mountain lake" {
		t.Fatalf("expected siglip2 client to receive query")
	}
	if clipClient.lastText != "" {
		t.Fatalf("expected clip client not to be used")
	}
	if store.lastSearchEncoder != models.EncoderSigLIP2 {
		t.Fatalf("expected siglip2 search encoder, got %s", store.lastSearchEncoder)
	}
}

func TestEngine_SearchByImageBytes(t *testing.T) {
	ms := &mockStore{}
	e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
	e.SearchByImageBytes(context.Background(), nil, 5, map[string]string{"k": "v"}, "")
	if ms.lastTopK != 5 {
		t.Errorf("topK not forwarded")
	}
	if ms.lastFilters["k"] != "v" {
		t.Errorf("filters not forwarded")
	}
	e.SearchByImageBytes(context.Background(), nil, 0, nil, "")
	if ms.lastTopK != defaultTopK {
		t.Errorf("default topK not applied")
	}
}

func TestEngine_SearchByImageURL(t *testing.T) {
	ms := &mockStore{}
	e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
	e.SearchByImageURL(context.Background(), "x", 0, nil, "")
	if ms.lastTopK != defaultTopK {
		t.Errorf("default topK not applied")
	}
}

func TestEngine_GetSimilar(t *testing.T) {
	t.Run("success topK+1", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(mustTestRegistry(t, &mockClip{}), ms, nil, nil)
		e.GetSimilar(context.Background(), "1", 5, "")
		if ms.lastTopK != 6 {
			t.Errorf("did not query topK+1")
		}
	})
	t.Run("store get vector error", func(t *testing.T) {
		e := NewEngine(mustTestRegistry(t, &mockClip{}), &mockStore{err: fmt.Errorf("fail")}, nil, nil)
		_, err := e.GetSimilar(context.Background(), "1", 5, "")
		if err == nil {
			t.Errorf("expected error")
		}
	})
}

func mustTestRegistry(t *testing.T, client encoder.Client) *encoder.Registry {
	t.Helper()

	registry, err := encoder.NewRegistry(models.EncoderCLIP, map[models.Encoder]encoder.Client{
		models.EncoderCLIP: client,
	})
	if err != nil {
		t.Fatalf("create test registry: %v", err)
	}
	return registry
}

func mustNamedRegistry(t *testing.T, defaultEncoder models.Encoder, clients map[models.Encoder]encoder.Client) *encoder.Registry {
	t.Helper()

	registry, err := encoder.NewRegistry(defaultEncoder, clients)
	if err != nil {
		t.Fatalf("create named test registry: %v", err)
	}
	return registry
}
