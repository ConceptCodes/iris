package indexing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iris/internal/constants"
	"iris/internal/metadata"
	"iris/pkg/models"
)

type mockEngine struct {
	id         string
	findID     string
	findOK     bool
	findErr    error
	indexErr   error
	reindexErr error
	lastReq    models.IndexRequest
	lastRecord *models.ImageRecord
	lastBytes  []byte
	force      bool
}

func (m *mockEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	m.lastReq = req
	return m.id, nil
}

func (m *mockEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastBytes = append([]byte(nil), imageBytes...)
	m.lastRecord = &record
	m.force = false
	return m.id, m.indexErr
}

func (m *mockEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastBytes = append([]byte(nil), imageBytes...)
	m.lastRecord = &record
	m.force = true
	return m.id, m.reindexErr
}

func (m *mockEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return m.findID, m.findOK, m.findErr
}

type failingAssetStore struct {
	err error
}

func (s failingAssetStore) Save(ctx context.Context, id, filename string, data []byte) (string, error) {
	return "", s.err
}

type stubEnricher struct {
	result metadata.Result
	err    error
}

func (s stubEnricher) Enrich(ctx context.Context, imageBytes []byte, record models.ImageRecord) (metadata.Result, error) {
	return s.result, s.err
}

func TestPipelineIndexFromURL(t *testing.T) {
	engine := &mockEngine{id: "url-id"}
	pipeline := NewPipelineWithOptions(engine, PipelineOptions{
		UserAgent:                "test-agent/1.0",
		SSRFAllowPrivateNetworks: true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "test-agent/1.0" {
			t.Fatalf("unexpected user agent: %q", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()

	id, err := pipeline.IndexFromURL(context.Background(), models.IndexRequest{
		URL:      server.URL + "/cat.jpg",
		Filename: "cat.jpg",
	})
	if err != nil {
		t.Fatalf("index from url: %v", err)
	}
	if id != "url-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if engine.lastRecord == nil || engine.lastRecord.Meta["source_url"] == "" {
		t.Fatalf("expected source_url metadata")
	}
	if engine.lastRecord.Meta["content_sha256"] == "" {
		t.Fatalf("expected content_sha256 metadata")
	}
	if engine.lastRecord.Meta[constants.MetaKeySourceDomain] != "127.0.0.1" && engine.lastRecord.Meta[constants.MetaKeySourceDomain] != "localhost" {
		t.Fatalf("expected source_domain metadata to be derived, got %q", engine.lastRecord.Meta[constants.MetaKeySourceDomain])
	}
}

func TestPipelineIndexUploadedBytesSucceedsWithoutAssetStore(t *testing.T) {
	engine := &mockEngine{id: "upload-id"}
	pipeline := NewPipeline(engine)

	id, err := pipeline.IndexUploadedBytes(
		context.Background(),
		[]byte("image-bytes"),
		"kitten.png",
		[]string{"cat"},
		map[string]string{"source": "upload"},
	)
	if err != nil {
		t.Fatalf("index upload: %v", err)
	}
	if id != "upload-id" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestPipelineIndexUploadedBytesSkipsAssetForDuplicate(t *testing.T) {
	engine := &mockEngine{id: "upload-id", findID: "existing-id", findOK: true}
	pipeline := NewPipeline(engine)

	result, err := pipeline.IndexUploadedBytesResult(
		context.Background(),
		[]byte("image-bytes"),
		"kitten.png",
		[]string{"cat"},
		map[string]string{"source": "upload"},
	)
	if err != nil {
		t.Fatalf("index upload duplicate: %v", err)
	}
	if result.Status != ResultStatusDuplicate {
		t.Fatalf("expected duplicate status, got %q", result.Status)
	}
	if result.ID != "existing-id" {
		t.Fatalf("unexpected existing id: %s", result.ID)
	}
}

func TestPipelineIndexUploadedBytesAppliesEnrichment(t *testing.T) {
	engine := &mockEngine{id: "upload-id"}
	pipeline := NewPipelineWithOptions(engine, PipelineOptions{
		Enricher: stubEnricher{
			result: metadata.Result{
				Tags: []string{"receipt", "text"},
				Meta: map[string]string{
					"caption":  "a receipt on a table",
					"ocr_text": "total 42.19",
				},
			},
		},
	})

	id, err := pipeline.IndexUploadedBytes(
		context.Background(),
		[]byte("image-bytes"),
		"receipt.png",
		[]string{"finance"},
		map[string]string{"source": "upload"},
	)
	if err != nil {
		t.Fatalf("index upload with enrichment: %v", err)
	}
	if id != "upload-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if engine.lastRecord == nil {
		t.Fatal("expected indexed record")
	}
	if got := engine.lastRecord.Meta["caption"]; got != "a receipt on a table" {
		t.Fatalf("unexpected caption: %q", got)
	}
	if got := engine.lastRecord.Meta["ocr_text"]; got != "total 42.19" {
		t.Fatalf("unexpected ocr_text: %q", got)
	}
	if len(engine.lastRecord.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", engine.lastRecord.Tags)
	}
}

func TestPipelineIndexLocalFile(t *testing.T) {
	engine := &mockEngine{id: "local-id"}
	pipeline := NewPipeline(engine)

	inputDir := t.TempDir()
	path := filepath.Join(inputDir, "photo.jpg")
	if err := os.WriteFile(path, []byte("local-image"), 0o644); err != nil {
		t.Fatalf("write local image: %v", err)
	}

	id, err := pipeline.IndexLocalFile(context.Background(), path)
	if err != nil {
		t.Fatalf("index local file: %v", err)
	}
	if id != "local-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if engine.lastRecord.Meta["source"] != "local" {
		t.Fatalf("expected local source metadata")
	}
	if engine.lastRecord.Meta["source_path"] != path {
		t.Fatalf("expected source_path metadata")
	}
}

func TestPipelineReindexFromURL(t *testing.T) {
	engine := &mockEngine{id: "reindex-id"}
	pipeline := NewPipelineWithOptions(engine, PipelineOptions{
		UserAgent:                "test-agent/1.0",
		SSRFAllowPrivateNetworks: true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "test-agent/1.0" {
			t.Fatalf("unexpected user agent: %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("reindex-bytes"))
	}))
	defer server.Close()

	record := models.ImageRecord{ID: "existing-id"}
	id, err := pipeline.ReindexFromURL(context.Background(), server.URL+"/img.png", record)
	if err != nil {
		t.Fatalf("reindex from url: %v", err)
	}
	if id != "reindex-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if !engine.force {
		t.Fatalf("expected reindex to force embedding")
	}
	if engine.lastRecord.Meta[constants.MetaKeyOriginURL] != server.URL+"/img.png" {
		t.Fatalf("expected origin_url metadata to be set")
	}
	if engine.lastRecord.Meta[constants.MetaKeyMIMEType] != "image/png" {
		t.Fatalf("expected mime_type metadata to be set")
	}
}

func TestPipelineIndexFromURLEmptyURL(t *testing.T) {
	pipeline := NewPipelineWithOptions(&mockEngine{}, PipelineOptions{})
	if _, err := pipeline.IndexFromURLResult(context.Background(), models.IndexRequest{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetchImageBytesRejectsUnsupportedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer server.Close()

	_, _, err := fetchImageBytes(context.Background(), server.URL, nil, 1024, "test-agent", true)
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("expected unsupported content type error, got %v", err)
	}
}

func TestFetchImageBytesRejectsOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("123456"))
	}))
	defer server.Close()

	_, _, err := fetchImageBytes(context.Background(), server.URL, nil, 5, "test-agent", true)
	if err == nil || !strings.Contains(err.Error(), "image exceeds 5 bytes limit") {
		t.Fatalf("expected image size limit error, got %v", err)
	}
}

func TestFetchImageBytesUsesContentSniffingWhenContentTypeMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Minimal GIF header so DetectContentType treats it as image/gif.
		_, _ = w.Write([]byte("GIF89a"))
	}))
	defer server.Close()

	_, mimeType, err := fetchImageBytes(context.Background(), server.URL, nil, 1024, "test-agent", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mimeType != "image/gif" {
		t.Fatalf("expected image/gif, got %q", mimeType)
	}
}

func TestPipelinePreservesExistingContentHash(t *testing.T) {
	engine := &mockEngine{id: "hash-id"}
	pipeline := NewPipeline(engine)

	existingHash := "already-set"
	result, err := pipeline.IndexUploadedBytesResult(context.Background(), []byte("image-bytes"), "photo.jpg", nil, map[string]string{
		constants.MetaKeyContentSHA256: existingHash,
	})
	if err != nil {
		t.Fatalf("index upload: %v", err)
	}
	if result.ID != "hash-id" {
		t.Fatalf("unexpected id: %s", result.ID)
	}
	if engine.lastRecord.Meta[constants.MetaKeyContentSHA256] != existingHash {
		t.Fatalf("expected existing content hash to be preserved")
	}
}

func TestPipelineIndexUploadedBytesPropagatesDuplicateLookupError(t *testing.T) {
	engine := &mockEngine{findErr: errors.New("dedupe failed")}
	pipeline := NewPipeline(engine)

	_, err := pipeline.IndexUploadedBytesResult(context.Background(), []byte("image-bytes"), "photo.jpg", nil, nil)
	if err == nil || err.Error() != "dedupe failed" {
		t.Fatalf("expected dedupe error, got %v", err)
	}
}

func TestPipelineIndexUploadedBytesSucceedsEvenIfAssetStoreFails(t *testing.T) {
	// Thumbnail save failures are non-fatal: the pipeline logs and continues.
	engine := &mockEngine{id: "upload-id"}
	pipeline := NewPipelineWithOptions(engine, PipelineOptions{
		AssetStore:     failingAssetStore{err: errors.New("disk full")},
		ThumbnailWidth: 0, // Disable thumbnail generation so we reach the store call
	})

	result, err := pipeline.IndexUploadedBytesResult(context.Background(), []byte("image-bytes"), "photo.jpg", nil, nil)
	if err != nil {
		t.Fatalf("expected pipeline to succeed even if store fails, got %v", err)
	}
	if result.ID != "upload-id" {
		t.Fatalf("unexpected id: %s", result.ID)
	}
}

func TestPipelineReindexFromURLReturnsReindexedStatus(t *testing.T) {
	engine := &mockEngine{id: "reindex-id"}
	pipeline := NewPipelineWithOptions(engine, PipelineOptions{
		SSRFAllowPrivateNetworks: true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()

	result, err := pipeline.ReindexFromURLResult(context.Background(), server.URL+"/img.jpg", models.ImageRecord{ID: "existing"})
	if err != nil {
		t.Fatalf("reindex from url: %v", err)
	}
	if result.Status != ResultStatusReindexed {
		t.Fatalf("expected reindexed status, got %q", result.Status)
	}
}
