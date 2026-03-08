package search

import (
	"context"
	"fmt"
	"math"
	"testing"

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
	engine := NewEngine(nil, nil)

	ctx := context.Background()

	_, err := engine.SearchByText(ctx, models.TextSearchRequest{})
	if err == nil {
		t.Error("expected error when store is nil")
	}

	_, err = engine.SearchByImageURL(ctx, "http://example.com/image.jpg", 10, nil)
	if err == nil {
		t.Error("expected error when store is nil")
	}

	_, err = engine.GetSimilar(ctx, "123", 10)
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

	lastID      string
	lastRecord  models.ImageRecord
	lastTopK    int
	lastFilters map[string]string
	findID      string
	findOK      bool
}

func (m *mockStore) Upsert(ctx context.Context, record models.ImageRecord, embedding models.Embedding) (string, error) {
	m.lastRecord = record
	return m.id, m.err
}
func (m *mockStore) Search(ctx context.Context, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error) {
	m.lastTopK = topK
	m.lastFilters = filters
	return m.res, m.err
}
func (m *mockStore) GetVector(ctx context.Context, id string) (models.Embedding, error) {
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
		e := NewEngine(mc, ms)

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
		e := NewEngine(&mockClip{err: fmt.Errorf("clip fail")}, &mockStore{})
		_, err := e.IndexFromURL(context.Background(), models.IndexRequest{})
		if err == nil || err.Error() != "clip fail" {
			t.Errorf("expected clip fail")
		}
	})
}

func TestEngine_IndexFromBytes(t *testing.T) {
	t.Run("auto id", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(&mockClip{}, ms)
		e.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{})
		if ms.lastRecord.ID == "" {
			t.Errorf("UUID not generated")
		}
	})
	t.Run("provided id", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(&mockClip{}, ms)
		e.IndexFromBytes(context.Background(), []byte("data"), models.ImageRecord{ID: "my-id"})
		if ms.lastRecord.ID != "my-id" {
			t.Errorf("provided ID overwritten")
		}
	})
	t.Run("dedupe returns existing", func(t *testing.T) {
		ms := &mockStore{findID: "existing", findOK: true}
		e := NewEngine(&mockClip{}, ms)
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
		e := NewEngine(&mockClip{}, ms)
		id, ok, err := e.FindExistingID(context.Background(), map[string]string{"content_sha256": "abc"}, "")
		if err != nil || !ok || id != "existing" {
			t.Fatalf("unexpected result: id=%s ok=%v err=%v", id, ok, err)
		}
	})
}

func TestEngine_SearchByText(t *testing.T) {
	t.Run("default topK", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(&mockClip{}, ms)
		e.SearchByText(context.Background(), models.TextSearchRequest{})
		if ms.lastTopK != defaultTopK {
			t.Errorf("expected default topK, got %d", ms.lastTopK)
		}
	})
	t.Run("clip error", func(t *testing.T) {
		e := NewEngine(&mockClip{err: fmt.Errorf("fail")}, &mockStore{})
		_, err := e.SearchByText(context.Background(), models.TextSearchRequest{})
		if err == nil {
			t.Errorf("expected error")
		}
	})
}

func TestEngine_SearchByImageBytes(t *testing.T) {
	ms := &mockStore{}
	e := NewEngine(&mockClip{}, ms)
	e.SearchByImageBytes(context.Background(), nil, 5, map[string]string{"k": "v"})
	if ms.lastTopK != 5 {
		t.Errorf("topK not forwarded")
	}
	if ms.lastFilters["k"] != "v" {
		t.Errorf("filters not forwarded")
	}
	e.SearchByImageBytes(context.Background(), nil, 0, nil)
	if ms.lastTopK != defaultTopK {
		t.Errorf("default topK not applied")
	}
}

func TestEngine_SearchByImageURL(t *testing.T) {
	ms := &mockStore{}
	e := NewEngine(&mockClip{}, ms)
	e.SearchByImageURL(context.Background(), "x", 0, nil)
	if ms.lastTopK != defaultTopK {
		t.Errorf("default topK not applied")
	}
}

func TestEngine_GetSimilar(t *testing.T) {
	t.Run("success topK+1", func(t *testing.T) {
		ms := &mockStore{}
		e := NewEngine(&mockClip{}, ms)
		e.GetSimilar(context.Background(), "1", 5)
		if ms.lastTopK != 6 {
			t.Errorf("did not query topK+1")
		}
	})
	t.Run("store get vector error", func(t *testing.T) {
		e := NewEngine(&mockClip{}, &mockStore{err: fmt.Errorf("fail")})
		_, err := e.GetSimilar(context.Background(), "1", 5)
		if err == nil {
			t.Errorf("expected error")
		}
	})
}
