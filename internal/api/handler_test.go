package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iris/internal/indexing"
	"iris/pkg/models"
)

type mockSearchEngine struct {
	err error
	id  string
	res []models.SearchResult

	// Spies
	lastReq     models.IndexRequest
	lastURL     string
	lastTopK    int
	lastFilters map[string]string
	lastRecord  models.ImageRecord
	lastQuery   string
}

func (m *mockSearchEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	m.lastReq = req
	return m.id, m.err
}

func (m *mockSearchEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastRecord = record
	return m.id, m.err
}

func (m *mockSearchEngine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	m.lastQuery = req.Query
	m.lastTopK = req.TopK
	m.lastFilters = req.Filters
	return m.res, m.err
}

func (m *mockSearchEngine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error) {
	m.lastTopK = topK
	m.lastFilters = filters
	return m.res, m.err
}

func (m *mockSearchEngine) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string) ([]models.SearchResult, error) {
	m.lastURL = url
	m.lastTopK = topK
	m.lastFilters = filters
	return m.res, m.err
}

func (m *mockSearchEngine) GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error) {
	m.lastTopK = topK
	return m.res, m.err
}

func newTestHandler(engine *mockSearchEngine) *Handler {
	return NewHandler(engine, indexing.NewPipeline(engine, nil))
}

func TestHandler_Health(t *testing.T) {
	h := newTestHandler(&mockSearchEngine{})
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	res := w.Result()
	if ctype := res.Header.Get("Content-Type"); ctype != "application/json" {
		t.Errorf("expected application/json, got %v", ctype)
	}
	var b map[string]string
	json.NewDecoder(res.Body).Decode(&b)
	if b["status"] != "ok" {
		t.Errorf("expected status ok")
	}
}

func TestHandler_IndexFromURL(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/index/url", strings.NewReader(`{invalid`))
		w := httptest.NewRecorder()
		h.IndexFromURL(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %v", w.Code)
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/index/url", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		h.IndexFromURL(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %v", w.Code)
		}
	})

	t.Run("engine error propagation", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{err: errors.New("engine fail")})
		req := httptest.NewRequest("POST", "/index/url", strings.NewReader(`{"url":"http://example.com"}`))
		w := httptest.NewRecorder()
		h.IndexFromURL(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %v", w.Code)
		}
		var b models.ErrorResponse
		json.NewDecoder(w.Body).Decode(&b)
		if b.Error != "engine fail" {
			t.Errorf("expected engine fail")
		}
	})

	t.Run("success payload shape", func(t *testing.T) {
		mock := &mockSearchEngine{id: "123"}
		h := newTestHandler(mock)
		req := httptest.NewRequest("POST", "/index/url", strings.NewReader(`{"url":"http://example.com"}`))
		w := httptest.NewRecorder()
		h.IndexFromURL(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %v", w.Code)
		}
		var b models.IndexResponse
		json.NewDecoder(w.Body).Decode(&b)
		if b.ID != "123" || b.Message != "indexed" {
			t.Errorf("unexpected success payload: %+v", b)
		}
	})
}

func TestHandler_IndexFromUpload(t *testing.T) {
	createMultipartRequest := func(filename, tags string, meta map[string]string) (*http.Request, error) {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		if filename != "" {
			fw, err := mw.CreateFormFile("image", filename)
			if err != nil {
				return nil, err
			}
			fw.Write([]byte("fake image data"))
		}

		mw.WriteField("filename", "custom.png")
		if tags != "" {
			mw.WriteField("tags", tags)
		}
		for k, v := range meta {
			mw.WriteField("meta_"+k, v)
		}
		mw.Close()
		req := httptest.NewRequest("POST", "/index/upload", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req, nil
	}

	t.Run("multipart parse failure", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/index/upload", strings.NewReader("bad"))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=foo")
		w := httptest.NewRecorder()
		h.IndexFromUpload(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %v", w.Code)
		}
	})

	t.Run("missing image", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req, _ := createMultipartRequest("", "", nil)
		w := httptest.NewRecorder()
		h.IndexFromUpload(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %v", w.Code)
		}
	})

	t.Run("engine error", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{err: errors.New("engine fail")})
		req, _ := createMultipartRequest("test.png", "", nil)
		w := httptest.NewRecorder()
		h.IndexFromUpload(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %v", w.Code)
		}
	})

	t.Run("success parsing logic", func(t *testing.T) {
		mock := &mockSearchEngine{id: "456"}
		h := newTestHandler(mock)
		req, _ := createMultipartRequest("test.png", "tag1,tag2", map[string]string{"source": "test", "foo": "bar"})
		w := httptest.NewRecorder()
		h.IndexFromUpload(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %v", w.Code)
		}
		if len(mock.lastRecord.Tags) != 2 || mock.lastRecord.Tags[0] != "tag1" {
			t.Errorf("tags not parsed correctly")
		}
		if mock.lastRecord.Meta["source"] != "test" || mock.lastRecord.Meta["foo"] != "bar" {
			t.Errorf("meta not parsed correctly")
		}
		if mock.lastRecord.Filename != "custom.png" {
			t.Errorf("filename not parsed correctly")
		}
	})
}

func TestHandler_SearchText(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/search/text", strings.NewReader(`{invalid`))
		w := httptest.NewRecorder()
		h.SearchText(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})

	t.Run("empty query", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/search/text", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		h.SearchText(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})

	t.Run("engine error", func(t *testing.T) {
		h := newTestHandler(&mockSearchEngine{err: errors.New("engine fail")})
		req := httptest.NewRequest("POST", "/search/text", strings.NewReader(`{"query":"test"}`))
		w := httptest.NewRecorder()
		h.SearchText(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500")
		}
	})

	t.Run("success logic", func(t *testing.T) {
		mock := &mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}}
		h := newTestHandler(mock)
		req := httptest.NewRequest("POST", "/search/text", strings.NewReader(`{"query":"test", "top_k": 5, "filters": {"color": "red"}}`))
		w := httptest.NewRecorder()
		h.SearchText(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
		var b models.TextSearchResponse
		json.NewDecoder(w.Body).Decode(&b)
		if b.Query != "test" || len(b.Results) != 1 {
			t.Errorf("unexpected payload")
		}
		if mock.lastTopK != 5 || mock.lastFilters["color"] != "red" {
			t.Errorf("data not forwarded to engine")
		}
	})
}

func TestHandler_SearchImage(t *testing.T) {
	createMultipartRequest := func(topK, filters string) (*http.Request, error) {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		fw, _ := mw.CreateFormFile("image", "test.png")
		fw.Write([]byte("fake image data"))
		if topK != "" {
			mw.WriteField("top_k", topK)
		}
		if filters != "" {
			mw.WriteField("filters", filters)
		}
		mw.Close()
		req := httptest.NewRequest("POST", "/search/image", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req, nil
	}

	t.Run("multipart parse and engine error", func(t *testing.T) {
		h := NewHandler(&mockSearchEngine{err: errors.New("engine error")}, nil)
		req, _ := createMultipartRequest("", "")
		w := httptest.NewRecorder()
		h.SearchImage(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500")
		}
	})

	t.Run("success parsing valid strings", func(t *testing.T) {
		mock := &mockSearchEngine{}
		h := NewHandler(mock, nil)
		req, _ := createMultipartRequest("10", `{"color":"blue"}`)
		w := httptest.NewRecorder()
		h.SearchImage(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
		if mock.lastTopK != 10 || mock.lastFilters["color"] != "blue" {
			t.Errorf("topK/filters not parsed correctly")
		}
	})

	t.Run("success fallback for invalid top_k and filters", func(t *testing.T) {
		mock := &mockSearchEngine{}
		h := NewHandler(mock, nil)
		req, _ := createMultipartRequest("abc", `{not_json}`)
		w := httptest.NewRecorder()
		h.SearchImage(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
		if mock.lastTopK != 0 || mock.lastFilters != nil {
			t.Errorf("invalid inputs should yield default (0) and nil filters")
		}
	})
}

func TestHandler_SearchImageURL(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		h := NewHandler(&mockSearchEngine{}, nil)
		req := httptest.NewRequest("POST", "/search/image/url", strings.NewReader(`{invalid`))
		w := httptest.NewRecorder()
		h.SearchImageURL(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})
	t.Run("empty URL", func(t *testing.T) {
		h := NewHandler(&mockSearchEngine{}, nil)
		req := httptest.NewRequest("POST", "/search/image/url", strings.NewReader(`{"url": ""}`))
		w := httptest.NewRecorder()
		h.SearchImageURL(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})
	t.Run("success", func(t *testing.T) {
		mock := &mockSearchEngine{}
		h := NewHandler(mock, nil)
		req := httptest.NewRequest("POST", "/search/image/url", strings.NewReader(`{"url": "http://x.com", "top_k": 7, "filters": {"f": "v"}}`))
		w := httptest.NewRecorder()
		h.SearchImageURL(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
		if mock.lastURL != "http://x.com" || mock.lastTopK != 7 || mock.lastFilters["f"] != "v" {
			t.Errorf("values not properly forwarded")
		}
	})
}
