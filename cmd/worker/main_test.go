package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iris/config"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/pkg/models"
)

type mockEngine struct {
	id         string
	lastRecord *models.ImageRecord
	force      bool
}

func (m *mockEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return m.id, nil
}

func (m *mockEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastRecord = &record
	m.force = false
	return m.id, nil
}

func (m *mockEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastRecord = &record
	m.force = true
	return m.id, nil
}

func (m *mockEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, nil
}

func TestEnqueueURLFile(t *testing.T) {
	store := jobs.NewMemoryStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(path, []byte("https://example.com/a.jpg\n# comment\n\nhttps://example.com/b.jpg\n"), 0o644); err != nil {
		t.Fatalf("write url file: %v", err)
	}

	if err := enqueueURLFile(context.Background(), store, path); err != nil {
		t.Fatalf("enqueue url file: %v", err)
	}

	if _, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, jobs.TypeFetchImage); err != nil || !ok {
		t.Fatalf("expected queued fetch_image job, err=%v", err)
	}
}

func TestEnqueueDir(t *testing.T) {
	store := jobs.NewMemoryStore()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cat.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	if err := enqueueDir(context.Background(), store, dir); err != nil {
		t.Fatalf("enqueue dir: %v", err)
	}

	if _, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, jobs.TypeIndexLocalFile); err != nil || !ok {
		t.Fatalf("expected queued index_local_file job, err=%v", err)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected errorType
	}{
		{"nil_is_transient", nil, errorTypeTransient},
		{"deadline_exceeded_is_transient", context.DeadlineExceeded, errorTypeTransient},
		{"canceled_is_transient", context.Canceled, errorTypeTransient},
		{"eof_is_transient", io.EOF, errorTypeTransient},
		{"unexpected_eof_is_transient", io.ErrUnexpectedEOF, errorTypeTransient},
		{"unsupported_content_type_is_permanent", errors.New("unsupported content type for image"), errorTypePermanent},
		{"image_exceeds_is_permanent", errors.New("image exceeds maximum size"), errorTypePermanent},
		{"invalid_is_permanent", errors.New("invalid URL format"), errorTypePermanent},
		{"is_required_is_permanent", errors.New("field is required"), errorTypePermanent},
		{"not_found_is_permanent", errors.New("image not found in store"), errorTypePermanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyError(tt.err)
			if result != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClassifyErrorHTTPStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expected   errorType
	}{
		{"400_bad_request_is_permanent", http.StatusBadRequest, errorTypePermanent},
		{"401_unauthorized_is_permanent", http.StatusUnauthorized, errorTypePermanent},
		{"403_forbidden_is_permanent", http.StatusForbidden, errorTypePermanent},
		{"404_not_found_is_permanent", http.StatusNotFound, errorTypePermanent},
		{"429_too_many_requests_is_transient", http.StatusTooManyRequests, errorTypeTransient},
		{"502_bad_gateway_is_transient", http.StatusBadGateway, errorTypeTransient},
		{"503_service_unavailable_is_transient", http.StatusServiceUnavailable, errorTypeTransient},
		{"504_gateway_timeout_is_transient", http.StatusGatewayTimeout, errorTypeTransient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &statusCodeError{code: tt.statusCode}
			result := classifyError(err)
			if result != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

type statusCodeError struct {
	code int
}

func (e *statusCodeError) Error() string {
	return fmt.Sprintf("status code %d", e.code)
}

func (e *statusCodeError) StatusCode() int {
	return e.code
}

func TestCalculateRetryBackoffIncreasesByAttempt(t *testing.T) {
	baseDelay := time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		backoff := calculateRetryBackoff(attempt, baseDelay)
		if backoff < baseDelay {
			t.Fatalf("attempt %d: backoff %v should be >= base %v", attempt, backoff, baseDelay)
		}
	}
}

func TestCalculateRetryBackoffIsCappedAt5Minutes(t *testing.T) {
	baseDelay := time.Minute
	for attempt := 1; attempt <= 20; attempt++ {
		backoff := calculateRetryBackoff(attempt, baseDelay)
		if backoff > 5*time.Minute {
			t.Fatalf("attempt %d: backoff %v exceeds 5 minute cap", attempt, backoff)
		}
	}
}

func TestCalculateRetryBackoffNormalizesAttemptBelow1(t *testing.T) {
	baseDelay := time.Second
	for _, attempt := range []int{-5, 0, -1} {
		backoff := calculateRetryBackoff(attempt, baseDelay)
		if backoff < baseDelay {
			t.Fatalf("attempt %d: backoff %v should be normalized to attempt 1 behavior", attempt, backoff)
		}
	}
}

func TestNewJobStoreReturnsMemoryForMemoryBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "memory"}
	store, err := newJobStore(cfg)
	if err != nil {
		t.Fatalf("newJobStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	defer store.Close()
}

func TestNewJobStoreErrorsForUnsupportedBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "unsupported"}
	_, err := newJobStore(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
	if !strings.Contains(err.Error(), "unsupported job backend") {
		t.Fatalf("expected unsupported backend error, got: %v", err)
	}
}

func TestNewCacheStoreReturnsNoopForMemoryBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "memory"}
	store, err := newCacheStore(cfg)
	if err != nil {
		t.Fatalf("newCacheStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestNewCacheStoreErrorsForUnsupportedBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "unsupported"}
	_, err := newCacheStore(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
	if !strings.Contains(err.Error(), "unsupported crawl cache backend") {
		t.Fatalf("expected unsupported backend error, got: %v", err)
	}
}

func TestNewCrawlStoreReturnsMemoryForMemoryBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "memory"}
	store, err := newCrawlStore(cfg)
	if err != nil {
		t.Fatalf("newCrawlStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	defer store.Close()
}

func TestNewCrawlStoreErrorsForUnsupportedBackend(t *testing.T) {
	cfg := config.Worker{JobBackend: "unsupported"}
	_, err := newCrawlStore(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
	if !strings.Contains(err.Error(), "unsupported crawl backend") {
		t.Fatalf("expected unsupported backend error, got: %v", err)
	}
}

func TestHandleIndexerJobFetchImageMapsMetadata(t *testing.T) {
	engine := &mockEngine{id: "test-id"}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{
		UserAgent:                "test-agent/1.0",
		SSRFAllowPrivateNetworks: true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()
	meta := map[string]string{
		"custom_key": "custom_value",
	}
	payload, _ := json.Marshal(jobs.FetchImagePayload{
		URL:           server.URL + "/image.jpg",
		Filename:      "image.jpg",
		Tags:          []string{"tag1"},
		Meta:          meta,
		PageURL:       "https://example.com/page",
		Title:         "Test Title",
		CrawlSourceID: "source-123",
		SourceDomain:  "example.com",
		MimeType:      "image/jpeg",
	})
	job := jobs.Job{
		Type:        jobs.TypeFetchImage,
		PayloadJSON: payload,
	}
	result, err := handleIndexerJob(context.Background(), pipeline, job)
	if err != nil {
		t.Fatalf("handleIndexerJob: %v", err)
	}
	if result.ID != "test-id" {
		t.Fatalf("unexpected id: %s", result.ID)
	}
	if engine.lastRecord == nil {
		t.Fatal("expected engine to receive record")
	}
	if engine.lastRecord.Filename != "image.jpg" {
		t.Fatalf("unexpected Filename: %s", engine.lastRecord.Filename)
	}
	if len(engine.lastRecord.Tags) != 1 || engine.lastRecord.Tags[0] != "tag1" {
		t.Fatalf("unexpected Tags: %v", engine.lastRecord.Tags)
	}
	if engine.lastRecord.Meta["custom_key"] != "custom_value" {
		t.Fatalf("custom_key not preserved")
	}
	if engine.lastRecord.Meta["page_url"] != "https://example.com/page" {
		t.Fatalf("page_url not set")
	}
	if engine.lastRecord.Meta["title"] != "Test Title" {
		t.Fatalf("title not set")
	}
	if engine.lastRecord.Meta["crawl_source_id"] != "source-123" {
		t.Fatalf("crawl_source_id not set")
	}
	if engine.lastRecord.Meta["source_domain"] != "example.com" {
		t.Fatalf("source_domain not set")
	}
	if engine.lastRecord.Meta["mime_type"] != "image/jpeg" {
		t.Fatalf("mime_type not set")
	}
}

func TestHandleIndexerJobIndexLocalFile(t *testing.T) {
	engine := &mockEngine{id: "local-id"}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{})

	path := filepath.Join(t.TempDir(), "image.jpg")
	if err := os.WriteFile(path, []byte("local-bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	payload, _ := json.Marshal(jobs.IndexLocalFilePayload{
		Path:  path,
		RunID: "run-123",
	})
	job := jobs.Job{
		Type:        jobs.TypeIndexLocalFile,
		PayloadJSON: payload,
	}
	result, err := handleIndexerJob(context.Background(), pipeline, job)
	if err != nil {
		t.Fatalf("handleIndexerJob: %v", err)
	}
	if result.ID != "local-id" {
		t.Fatalf("unexpected id: %s", result.ID)
	}
	if engine.lastRecord == nil {
		t.Fatal("expected engine to receive record")
	}
	if engine.lastRecord.Meta["source_path"] != path {
		t.Fatalf("unexpected source_path: %s", engine.lastRecord.Meta["source_path"])
	}
}

func TestHandleIndexerJobReindexImage(t *testing.T) {
	engine := &mockEngine{id: "reindex-id"}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{
		UserAgent:                "test-agent/1.0",
		SSRFAllowPrivateNetworks: true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()

	payload, _ := json.Marshal(jobs.ReindexImagePayload{
		ID:  "existing-image-id",
		URL: server.URL + "/image.jpg",
	})
	job := jobs.Job{
		Type:        jobs.TypeReindexImage,
		PayloadJSON: payload,
	}
	result, err := handleIndexerJob(context.Background(), pipeline, job)
	if err != nil {
		t.Fatalf("handleIndexerJob: %v", err)
	}
	if result.ID != "reindex-id" {
		t.Fatalf("unexpected id: %s", result.ID)
	}
	if engine.lastRecord == nil {
		t.Fatal("expected engine to receive record")
	}
	if engine.lastRecord.ID != "existing-image-id" {
		t.Fatalf("unexpected record ID: %s", engine.lastRecord.ID)
	}
	if engine.lastRecord.Meta["origin_url"] != server.URL+"/image.jpg" {
		t.Fatalf("unexpected origin_url: %s", engine.lastRecord.Meta["origin_url"])
	}
}

func TestHandleIndexerJobMalformedJSON(t *testing.T) {
	engine := &mockEngine{}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{})
	job := jobs.Job{
		Type:        jobs.TypeFetchImage,
		PayloadJSON: []byte("not valid json"),
	}
	_, err := handleIndexerJob(context.Background(), pipeline, job)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestHandleIndexerJobUnknownType(t *testing.T) {
	engine := &mockEngine{}
	pipeline := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{})
	job := jobs.Job{
		Type:        "unknown_type",
		PayloadJSON: []byte("{}"),
	}
	result, err := handleIndexerJob(context.Background(), pipeline, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "" {
		t.Fatalf("expected empty result for unknown type, got: %s", result.ID)
	}
}

type mockPipeline struct {
	result      indexing.Result
	lastRequest models.IndexRequest
	lastPath    string
	lastRecord  models.ImageRecord
	lastURL     string
}

func (m *mockPipeline) IndexFromURLResult(ctx context.Context, req models.IndexRequest) (indexing.Result, error) {
	m.lastRequest = req
	return m.result, nil
}

func TestProcessSchedulesAdvancesNextRunAndQueuesRun(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	store := crawl.NewMemoryStore()
	service := crawl.NewService(store, jobStore)

	source, err := service.CreateSource(context.Background(), crawl.CreateSourceInput{
		Kind:          crawl.SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "1m",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	dueAt := time.Now().UTC().Add(-time.Minute)
	if err := service.SetSourceNextRun(context.Background(), source.ID, dueAt); err != nil {
		t.Fatalf("set source next run: %v", err)
	}

	processSchedules(context.Background(), service)

	updatedSource, err := store.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !updatedSource.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected next run to be advanced, got %v", updatedSource.NextRunAt)
	}

	runs, err := service.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Trigger != "scheduled" {
		t.Fatalf("expected one scheduled run, got %+v", runs)
	}

	job, ok, err := jobStore.LeaseNext(context.Background(), time.Now().UTC().Add(time.Second), time.Second, jobs.TypeDiscoverSource)
	if err != nil || !ok {
		t.Fatalf("expected queued discover job, ok=%v err=%v", ok, err)
	}
	var payload jobs.DiscoverSourcePayload
	if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.SourceID != source.ID || payload.RunID != runs[0].ID {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func (m *mockPipeline) IndexLocalFileResult(ctx context.Context, path string) (indexing.Result, error) {
	m.lastPath = path
	return m.result, nil
}

func (m *mockPipeline) ReindexFromURLResult(ctx context.Context, url string, record models.ImageRecord) (indexing.Result, error) {
	m.lastURL = url
	m.lastRecord = record
	return m.result, nil
}
