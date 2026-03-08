package indexing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iris/internal/assets"
	"iris/pkg/models"
)

type mockEngine struct {
	id         string
	lastReq    models.IndexRequest
	lastRecord models.ImageRecord
	lastBytes  []byte
}

func (m *mockEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	m.lastReq = req
	return m.id, nil
}

func (m *mockEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	m.lastBytes = append([]byte(nil), imageBytes...)
	m.lastRecord = record
	return m.id, nil
}

func TestPipelineIndexFromURL(t *testing.T) {
	engine := &mockEngine{id: "url-id"}
	pipeline := NewPipeline(engine, nil)

	id, err := pipeline.IndexFromURL(context.Background(), models.IndexRequest{
		URL:      "https://example.com/cat.jpg",
		Filename: "cat.jpg",
	})
	if err != nil {
		t.Fatalf("index from url: %v", err)
	}
	if id != "url-id" {
		t.Fatalf("unexpected id: %s", id)
	}
	if engine.lastReq.URL != "https://example.com/cat.jpg" {
		t.Fatalf("request not forwarded")
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
