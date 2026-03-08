package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iris/internal/crawl"
	"iris/internal/jobs"
)

func TestRouterHealth(t *testing.T) {
	router := NewRouterWithAssets(nil, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestRouterLanding(t *testing.T) {
	router := NewRouterWithAssets(nil, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestRouterEndpoints(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)

	tests := []struct {
		method string
		path   string
		expect int
	}{
		{"GET", "/search", http.StatusOK},
		{"GET", "/image/123", http.StatusNotFound}, // no results -> 404
		{"GET", "/image/123/related", http.StatusOK},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != tt.expect {
			t.Errorf("expected %d for %s %s, got %d", tt.expect, tt.method, tt.path, w.Result().StatusCode)
		}
	}
}

func TestRouterMethodEnforcement(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)
	req := httptest.NewRequest("POST", "/health", nil) // health is GET
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", w.Result().StatusCode)
	}
}

func TestRouterMiddleware(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)

	// CORS preflight
	req := httptest.NewRequest("OPTIONS", "/search/text", nil)
	req.Header.Set("Origin", "http://anywhere.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		t.Errorf("CORS preflight failed: %d", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected wildcard origin allowed")
	}

	// Request-ID is typically set in ctx by chi. We just verify the route didn't fail.
	req2 := httptest.NewRequest("GET", "/health", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Errorf("health endpoint failed under middleware")
	}
}

func TestRouterAdminEndpoints(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	crawlService := crawl.NewService(crawl.NewMemoryStore(), jobStore)
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, crawlService, "secret", jobStore)

	req := httptest.NewRequest("POST", "/admin/sources", strings.NewReader(`{"kind":"local_dir","local_path":"./images"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating source, got %d", w.Result().StatusCode)
	}
}

func TestRouterAdminLocalIndexEndpoint(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	crawlService := crawl.NewService(crawl.NewMemoryStore(), jobStore)
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, crawlService, "secret", jobStore)

	req := httptest.NewRequest("POST", "/admin/index/local", strings.NewReader(`{"path":"./images"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 enqueueing local index, got %d", w.Result().StatusCode)
	}
}

func TestRouterAdminMetrics(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "secret", nil)
	req := httptest.NewRequest("GET", "/admin/metrics", nil)
	req.Header.Set("X-Admin-Key", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestRouterAdminDisabledWithoutKey(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)
	req := httptest.NewRequest("GET", "/admin/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Result().StatusCode)
	}
}

func TestRouterAdminMetricsDisabled(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)
	req := httptest.NewRequest("GET", "/admin/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Result().StatusCode)
	}
}
