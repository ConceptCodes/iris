package web

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iris/pkg/models"
)

func TestLandingPage(t *testing.T) {
	handlers := NewHandlers(nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handlers.LandingPage(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

type mockSearchEngine struct {
	err error
	id  string
	res []models.SearchResult
}

func (m *mockSearchEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return m.id, m.err
}
func (m *mockSearchEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return m.id, m.err
}
func (m *mockSearchEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return m.id, m.err
}
func (m *mockSearchEngine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	return m.res, m.err
}
func (m *mockSearchEngine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return m.res, m.err
}
func (m *mockSearchEngine) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return m.res, m.err
}
func (m *mockSearchEngine) GetSimilar(ctx context.Context, id string, topK int, enc models.Encoder) ([]models.SearchResult, error) {
	return m.res, m.err
}

func (m *mockSearchEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, m.err
}

func (m *mockSearchEngine) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	return []models.ImageRecord{}, m.err
}

func TestSearchResults(t *testing.T) {
	t.Run("engine error full page", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{err: errors.New("err")})
		req := httptest.NewRequest("GET", "/search?q=test", nil)
		w := httptest.NewRecorder()
		h.SearchResults(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 rendering error page")
		}
	})

	t.Run("engine error htmx", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{err: errors.New("err")})
		req := httptest.NewRequest("GET", "/search?q=test", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.SearchResults(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 empty state error")
		}
	})

	t.Run("empty results", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{})
		req := httptest.NewRequest("GET", "/search?q=test", nil)
		w := httptest.NewRecorder()
		h.SearchResults(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 rendering no results page")
		}
	})

	t.Run("success htmx", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		req := httptest.NewRequest("GET", "/search?q=test&page=2", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.SearchResults(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 HTMX image cards")
		}
	})
}

func TestImageDetail(t *testing.T) {
	t.Run("error or empty", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{err: errors.New("fail")})
		req := httptest.NewRequest("GET", "/image/123", nil)
		req.SetPathValue("id", "123")
		w := httptest.NewRecorder()
		h.ImageDetail(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404")
		}
	})

	t.Run("success", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		req := httptest.NewRequest("GET", "/image/123", nil)
		req.SetPathValue("id", "123")
		w := httptest.NewRecorder()
		h.ImageDetail(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
	})
}

func TestRelatedImages(t *testing.T) {
	t.Run("error branch", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{err: errors.New("fail")})
		req := httptest.NewRequest("GET", "/image/123/related", nil)
		w := httptest.NewRecorder()
		h.RelatedImages(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500")
		}
	})

	t.Run("success limits and trims", func(t *testing.T) {
		// create 12 results, it should drop the first one and cap at 9
		res := make([]models.SearchResult, 12)
		h := NewHandlers(&mockSearchEngine{res: res})
		req := httptest.NewRequest("GET", "/image/123/related", nil)
		w := httptest.NewRecorder()
		h.RelatedImages(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
	})
}

func TestReverseImageSearch(t *testing.T) {
	createMultipartRequest := func() (*http.Request, error) {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		fw, _ := mw.CreateFormFile("image", "test.png")
		fw.Write([]byte("fake image data"))
		mw.Close()
		req := httptest.NewRequest("POST", "/search/reverse", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req, nil
	}

	t.Run("multipart error", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/search/reverse", strings.NewReader("bad"))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=foo")
		w := httptest.NewRecorder()
		h.ReverseImageSearch(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})

	t.Run("engine error", func(t *testing.T) {
		req, _ := createMultipartRequest()
		h := NewHandlers(&mockSearchEngine{err: errors.New("fail")})
		w := httptest.NewRecorder()
		h.ReverseImageSearch(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500")
		}
	})

	t.Run("success redirect", func(t *testing.T) {
		req, _ := createMultipartRequest()
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		w := httptest.NewRecorder()
		h.ReverseImageSearch(w, req)
		if w.Code != http.StatusSeeOther {
			t.Errorf("expected 303")
		}
	})

	t.Run("success htmx grid", func(t *testing.T) {
		req, _ := createMultipartRequest()
		req.Header.Set("HX-Request", "true")
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		w := httptest.NewRecorder()
		h.ReverseImageSearch(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
	})
}

func TestReverseImageSearchURL(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{})
		req := httptest.NewRequest("POST", "/search/reverse/url", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ReverseImageSearchURL(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400")
		}
	})

	t.Run("engine error", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{err: errors.New("fail")})
		req := httptest.NewRequest("POST", "/search/reverse/url", strings.NewReader("url=http://example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ReverseImageSearchURL(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500")
		}
	})

	t.Run("success redirect", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		req := httptest.NewRequest("POST", "/search/reverse/url", strings.NewReader("url=http://example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ReverseImageSearchURL(w, req)
		if w.Code != http.StatusSeeOther {
			t.Errorf("expected 303")
		}
	})

	t.Run("success htmx", func(t *testing.T) {
		h := NewHandlers(&mockSearchEngine{res: []models.SearchResult{{Score: 0.99}}})
		req := httptest.NewRequest("POST", "/search/reverse/url", strings.NewReader("url=http://example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.ReverseImageSearchURL(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200")
		}
	})
}
