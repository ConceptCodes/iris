package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"iris/internal/crawl"
	"iris/internal/jobs"
	"iris/internal/search"
	"iris/pkg/models"
)

type mockSearchEngine struct{}

func (m *mockSearchEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return "", nil
}

func (m *mockSearchEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "", nil
}

func (m *mockSearchEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "", nil
}

func (m *mockSearchEngine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	return nil, nil
}

func (m *mockSearchEngine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return nil, nil
}

func (m *mockSearchEngine) SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return nil, nil
}

func (m *mockSearchEngine) GetSimilar(ctx context.Context, id string, topK int, enc models.Encoder) ([]models.SearchResult, error) {
	return nil, nil
}

func (m *mockSearchEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, nil
}

func (m *mockSearchEngine) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	return nil, nil
}

var _ search.Engine = (*mockSearchEngine)(nil)

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

func TestRouterAdminReadOnlyRole(t *testing.T) {
	router := NewRouterWithAssetsAndAuth(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, AdminAuthSettings{
		AdminAPIKey:     "secret",
		ReadOnlyAPIKeys: []string{"viewer"},
	}, nil)

	getReq := httptest.NewRequest("GET", "/admin/metrics", nil)
	getReq.Header.Set("X-Admin-Key", "viewer")
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected readonly access to GET admin route, got %d", getW.Result().StatusCode)
	}

	postReq := httptest.NewRequest("POST", "/admin/reindex", strings.NewReader(`{}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("X-Admin-Key", "viewer")
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)
	if postW.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected readonly write denial, got %d", postW.Result().StatusCode)
	}
}

func TestRouterAdminSupportsBearerToken(t *testing.T) {
	router := NewRouterWithAssetsAndAuth(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, AdminAuthSettings{
		AdminAPIKey: "secret",
	}, nil)

	req := httptest.NewRequest("GET", "/admin/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected bearer token to authorize, got %d", res.Code)
	}
}

func TestRouterAdminRejectsReadOnlyBearerOnWriteRoute(t *testing.T) {
	router := NewRouterWithAssetsAndAuth(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, AdminAuthSettings{
		AdminAPIKey:     "secret",
		ReadOnlyAPIKeys: []string{"viewer"},
	}, nil)

	req := httptest.NewRequest("POST", "/admin/reindex", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer viewer")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected readonly bearer token to be forbidden on write route, got %d", res.Code)
	}
}

func TestRouterAdminDisabledWithoutKey(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)
	req := httptest.NewRequest("GET", "/admin/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestRouterAdminMetricsDisabled(t *testing.T) {
	router := NewRouterWithAssets(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, "", nil)
	req := httptest.NewRequest("GET", "/admin/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestRouterPrometheusMetricsRouteIsAdminProtected(t *testing.T) {
	router := NewRouterWithAssetsAndAuth(&mockSearchEngine{}, AssetsSettings{LocalDir: t.TempDir()}, nil, AdminAuthSettings{
		AdminAPIKey: "secret",
	}, nil)

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", unauthorized.Code)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authorizedReq.Header.Set("X-Admin-Key", "secret")
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, authorizedReq)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected 200 with admin auth, got %d", authorized.Code)
	}
}

func TestBuildAssetStoreFallsBackToLocalStoreOnInvalidSettings(t *testing.T) {
	dir := t.TempDir()
	store, assetDir := buildAssetStore(AssetsSettings{
		Backend:  "s3",
		LocalDir: dir,
	})
	if store == nil {
		t.Fatal("expected fallback local store")
	}
	if assetDir != dir {
		t.Fatalf("expected asset dir %q, got %q", dir, assetDir)
	}
}

func TestNewCrawlServiceReturnsMemoryService(t *testing.T) {
	service, jobStore, cleanup, err := NewCrawlService("memory", "")
	if err != nil {
		t.Fatalf("NewCrawlService: %v", err)
	}
	if service == nil || jobStore == nil || cleanup == nil {
		t.Fatalf("expected non-nil service, store, and cleanup")
	}
	defer cleanup()

	source, err := service.CreateSource(context.Background(), crawl.CreateSourceInput{
		Kind:      crawl.SourceKindLocalDir,
		LocalPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create source via service: %v", err)
	}
	if source.ID == "" {
		t.Fatal("expected source ID")
	}
}

func TestNewCrawlServiceReturnsNilForUnsupportedBackend(t *testing.T) {
	service, jobStore, cleanup, err := NewCrawlService("unsupported", "")
	if err != nil {
		t.Fatalf("expected nil error for unsupported backend, got %v", err)
	}
	if service != nil || jobStore != nil || cleanup != nil {
		t.Fatalf("expected nil return values for unsupported backend")
	}
}

func TestNewCrawlServicePostgresFailureReturnsError(t *testing.T) {
	service, jobStore, cleanup, err := NewCrawlService("postgres", "")
	if err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("expected error for invalid postgres configuration")
	}
	if service != nil || jobStore != nil || cleanup != nil {
		t.Fatalf("expected nil return values on postgres init failure")
	}
}

func TestBuildAssetStoreReturnsUsableLocalStore(t *testing.T) {
	dir := t.TempDir()
	store, assetDir := buildAssetStore(AssetsSettings{LocalDir: dir})
	if assetDir != dir {
		t.Fatalf("expected local asset dir %q, got %q", dir, assetDir)
	}
	url, err := store.Save("asset-1", "photo.jpg", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("save asset: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty asset URL")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 asset written, got %d", len(entries))
	}
}
