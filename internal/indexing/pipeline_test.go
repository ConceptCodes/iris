package indexing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iris/internal/assets"
	"iris/pkg/models"
)

type mockEngine struct {
	id         string
	findID     string
	findOK     bool
	lastReq    models.IndexRequest
	lastRecord models.ImageRecord
	lastBytes  []byte
	force      bool
}

func (m *mockEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	m.lastReq = req
	return m.id, nil
}

func (m *mockEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastBytes = append([]byte(nil), imageBytes...)
	m.lastRecord = record
	m.force = false
	return m.id, nil
}

func (m *mockEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastBytes = append([]byte(nil), imageBytes...)
	m.lastRecord = record
	m.force = true
	return m.id, nil
}

func (m *mockEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return m.findID, m.findOK, nil
}

func TestPipelineIndexFromURL(t *testing.T) {
	engine := &mockEngine{id: "url-id"}
	pipeline := NewPipeline(engine, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if engine.lastRecord.Meta["source_url"] == "" {
		t.Fatalf("expected source_url metadata")
	}
	if engine.lastRecord.Meta["content_sha256"] == "" {
		t.Fatalf("expected content_sha256 metadata")
	}
}

func TestPipelineIndexUploadedBytesStoresAsset(t *testing.T) {
	engine := &mockEngine{id: "upload-id"}
	assetDir := t.TempDir()
	pipeline := NewPipeline(engine, assets.NewStore(assetDir))

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
	if engine.lastRecord.URL == "" || !strings.HasPrefix(engine.lastRecord.URL, "/assets/") {
		t.Fatalf("expected asset url, got %q", engine.lastRecord.URL)
	}
	files, err := os.ReadDir(assetDir)
	if err != nil {
		t.Fatalf("read asset dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one stored asset, got %d", len(files))
	}
}

func TestPipelineIndexUploadedBytesSkipsAssetForDuplicate(t *testing.T) {
	engine := &mockEngine{id: "upload-id", findID: "existing-id", findOK: true}
	assetDir := t.TempDir()
	pipeline := NewPipeline(engine, assets.NewStore(assetDir))

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
	files, err := os.ReadDir(assetDir)
	if err != nil {
		t.Fatalf("read asset dir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no stored assets for duplicate, got %d", len(files))
	}
}

func TestPipelineIndexLocalFile(t *testing.T) {
	engine := &mockEngine{id: "local-id"}
	assetDir := t.TempDir()
	pipeline := NewPipeline(engine, assets.NewStore(assetDir))

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
	pipeline := NewPipeline(engine, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
