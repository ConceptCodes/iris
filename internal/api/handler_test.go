package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iris/internal/constants"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metrics"
	"iris/pkg/models"
)

type stubEngine struct {
	listImages []models.ImageRecord
	listErr    error
}

func (s *stubEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return "", nil
}

func (s *stubEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "", nil
}

func (s *stubEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "", nil
}

func (s *stubEngine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	return nil, nil
}

func (s *stubEngine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string) ([]models.SearchResult, error) {
	return nil, nil
}

func (s *stubEngine) SearchByImageURL(ctx context.Context, imageURL string, topK int, filters map[string]string) ([]models.SearchResult, error) {
	return nil, nil
}

func (s *stubEngine) GetSimilar(ctx context.Context, id string, topK int) ([]models.SearchResult, error) {
	return nil, nil
}

func (s *stubEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, nil
}

func (s *stubEngine) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	return s.listImages, s.listErr
}

type stubIndexer struct {
	result indexing.Result
	err    error
}

func (s *stubIndexer) IndexFromURLResult(ctx context.Context, req models.IndexRequest) (indexing.Result, error) {
	return s.result, s.err
}

func (s *stubIndexer) IndexUploadedBytesResult(ctx context.Context, imageBytes []byte, filename string, tags []string, meta map[string]string) (indexing.Result, error) {
	return s.result, s.err
}

func (s *stubIndexer) IndexLocalFileResult(ctx context.Context, path string) (indexing.Result, error) {
	return s.result, s.err
}

func (s *stubIndexer) ReindexFromURLResult(ctx context.Context, imageURL string, record models.ImageRecord) (indexing.Result, error) {
	return s.result, s.err
}

type stubJobStore struct {
	enqueued []jobs.Job
	err      error
}

func (s *stubJobStore) Enqueue(ctx context.Context, job jobs.Job) (jobs.Job, error) {
	if s.err != nil {
		return jobs.Job{}, s.err
	}
	s.enqueued = append(s.enqueued, job)
	return job, nil
}

func (s *stubJobStore) LeaseNext(ctx context.Context, now time.Time, leaseDuration time.Duration, allowedTypes ...jobs.Type) (jobs.Job, bool, error) {
	return jobs.Job{}, false, nil
}

func (s *stubJobStore) MarkSucceeded(ctx context.Context, id string) error {
	return nil
}

func (s *stubJobStore) MarkFailed(ctx context.Context, id string, err error, retryAt time.Time) (jobs.Status, error) {
	return jobs.StatusFailed, nil
}

func (s *stubJobStore) Close() error { return nil }

func TestHandlerCreateSourceRequiresService(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	res := httptest.NewRecorder()

	h.CreateSource(res, req)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d", res.Code)
	}
}

func TestHandlerTriggerRunRequiresService(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	req.SetPathValue("id", "source-id")
	res := httptest.NewRecorder()

	h.TriggerSourceRun(res, req)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d", res.Code)
	}
}

func TestHandlerListRunsRequiresService(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()

	h.ListRuns(res, req)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d", res.Code)
	}
}

func TestHandlerMetricsReturnsEmptyWhenNil(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()

	h.Metrics(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
}

func TestHandlerHandleReindexRequiresJobStore(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	res := httptest.NewRecorder()

	h.HandleReindex(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", res.Code)
	}
}

func TestHandlerHandleReindexEnqueuesJobs(t *testing.T) {
	engine := &stubEngine{
		listImages: []models.ImageRecord{
			{ID: "img-1", URL: "https://example.com/a.jpg"},
			{ID: "img-2", Meta: map[string]string{constants.MetaKeyOriginURL: "https://example.com/b.jpg"}},
		},
	}
	jobStore := &stubJobStore{}
	h := NewHandler(engine, nil, nil, jobStore, metrics.NewCounters())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	res := httptest.NewRecorder()

	h.HandleReindex(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
	if len(jobStore.enqueued) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobStore.enqueued))
	}
}

func TestHandlerHandleReindexSkipsMissingSourceURL(t *testing.T) {
	engine := &stubEngine{
		listImages: []models.ImageRecord{
			{ID: "img-1"},
		},
	}
	jobStore := &stubJobStore{}
	h := NewHandler(engine, nil, nil, jobStore, metrics.NewCounters())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	res := httptest.NewRecorder()

	h.HandleReindex(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
	if len(jobStore.enqueued) != 0 {
		t.Fatalf("expected no jobs, got %d", len(jobStore.enqueued))
	}
	var response models.ReindexResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.EnqueuedCount != 0 || len(response.Errors) != 1 {
		t.Fatalf("expected error for missing source URL")
	}
}

func TestHandlerHandleReindexListsWithFilters(t *testing.T) {
	engine := &stubEngine{}
	jobStore := &stubJobStore{}
	h := NewHandler(engine, nil, nil, jobStore, metrics.NewCounters())

	body := `{"source_id":"source-123","run_id":"run-456","limit":20,"offset":10}`
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))

	h.HandleReindex(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
}

func TestHandlerEnqueueLocalIndexRequiresService(t *testing.T) {
	h := NewHandler(&stubEngine{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
	res := httptest.NewRecorder()

	h.EnqueueLocalIndex(res, req)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d", res.Code)
	}
}

func TestHandlerEnqueueLocalIndexRequiresPath(t *testing.T) {
	service := crawl.NewService(crawl.NewMemoryStore(), jobs.NewMemoryStore())
	h := NewHandler(&stubEngine{}, nil, service, jobs.NewMemoryStore(), metrics.NewCounters())
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"path":""}`))
	res := httptest.NewRecorder()

	h.EnqueueLocalIndex(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", res.Code)
	}
}
