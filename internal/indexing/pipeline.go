package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"iris/internal/assets"
	"iris/pkg/models"
)

type Engine interface {
	IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error)
	IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
}

type Pipeline struct {
	engine     Engine
	assetStore *assets.Store
}

func NewPipeline(engine Engine, assetStore *assets.Store) *Pipeline {
	return &Pipeline{
		engine:     engine,
		assetStore: assetStore,
	}
}

func (p *Pipeline) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	if req.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	return p.engine.IndexFromURL(ctx, req)
}

func (p *Pipeline) IndexUploadedBytes(ctx context.Context, imageBytes []byte, filename string, tags []string, meta map[string]string) (string, error) {
	record := models.ImageRecord{
		ID:       uuid.New().String(),
		Filename: filename,
		Tags:     tags,
		Meta:     cloneMeta(meta),
	}
	return p.indexBytes(ctx, imageBytes, record)
}

func (p *Pipeline) IndexLocalFile(ctx context.Context, path string) (string, error) {
	imageBytes, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read local image: %w", err)
	}

	record := models.ImageRecord{
		ID:       uuid.New().String(),
		Filename: filepath.Base(path),
		Meta: map[string]string{
			"source":      "local",
			"source_path": path,
		},
	}
	return p.indexBytes(ctx, imageBytes, record)
}

func (p *Pipeline) indexBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	if p.assetStore != nil {
		assetURL, err := p.assetStore.Save(record.ID, record.Filename, imageBytes)
		if err != nil {
			return "", fmt.Errorf("store image asset: %w", err)
		}
		record.URL = assetURL
	}
	return p.engine.IndexFromBytes(ctx, imageBytes, record)
}

func cloneMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}
