package search

import (
	"context"
	"fmt"
	"testing"

	"iris/internal/encoder"
	"iris/pkg/models"
)

func benchmarkEmbedding(dim int) models.Embedding {
	emb := make(models.Embedding, dim)
	for i := range emb {
		emb[i] = float32((i % 13) + 1)
	}
	return emb
}

func benchmarkResults(n int) []models.SearchResult {
	results := make([]models.SearchResult, n)
	for i := range results {
		results[i] = models.SearchResult{
			Record: models.ImageRecord{
				ID:       fmt.Sprintf("img-%d", i),
				URL:      fmt.Sprintf("https://example.com/%d.jpg", i),
				Filename: fmt.Sprintf("%d.jpg", i),
				Meta: map[string]string{
					"source_domain": "example.com",
				},
			},
			Score: 0.99,
		}
	}
	return results
}

func BenchmarkEngineSearchByText(b *testing.B) {
	engine := NewEngine(
		mustBenchRegistry(b, &mockClip{emb: benchmarkEmbedding(768)}),
		&mockStore{res: benchmarkResults(40)},
		nil,
		nil,
	)
	req := models.TextSearchRequest{
		Query: "golden retriever on a beach",
		TopK:  40,
		Filters: map[string]string{
			"meta_source_domain": "example.com",
		},
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.SearchByText(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineSearchByImageBytes(b *testing.B) {
	engine := NewEngine(
		mustBenchRegistry(b, &mockClip{emb: benchmarkEmbedding(768)}),
		&mockStore{res: benchmarkResults(40)},
		nil,
		nil,
	)
	imageBytes := make([]byte, 256*1024)
	for i := range imageBytes {
		imageBytes[i] = byte(i)
	}
	ctx := context.Background()

	b.SetBytes(int64(len(imageBytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.SearchByImageBytes(ctx, imageBytes, 40, nil, ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineIndexFromBytes(b *testing.B) {
	imageBytes := make([]byte, 256*1024)
	for i := range imageBytes {
		imageBytes[i] = byte(i)
	}
	ctx := context.Background()

	b.Run("unique", func(b *testing.B) {
		engine := NewEngine(
			mustBenchRegistry(b, &mockClip{emb: benchmarkEmbedding(768)}),
			&mockStore{id: "new-id"},
			nil,
			nil,
		)

		b.SetBytes(int64(len(imageBytes)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			record := models.ImageRecord{
				Filename: "photo.jpg",
				Meta: map[string]string{
					"source_url": fmt.Sprintf("https://example.com/%d.jpg", i),
				},
			}
			if _, err := engine.IndexFromBytes(ctx, imageBytes, record); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("duplicate", func(b *testing.B) {
		engine := NewEngine(
			mustBenchRegistry(b, &mockClip{emb: benchmarkEmbedding(768)}),
			&mockStore{findID: "existing-id", findOK: true},
			nil,
			nil,
		)

		b.SetBytes(int64(len(imageBytes)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			record := models.ImageRecord{
				Filename: "photo.jpg",
				Meta: map[string]string{
					"content_sha256": "duplicate-hash",
				},
			}
			if _, err := engine.IndexFromBytes(ctx, imageBytes, record); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func mustBenchRegistry(b *testing.B, client encoder.Client) *encoder.Registry {
	b.Helper()

	registry, err := encoder.NewRegistry(models.EncoderCLIP, map[models.Encoder]encoder.Client{
		models.EncoderCLIP: client,
	})
	if err != nil {
		b.Fatalf("create benchmark registry: %v", err)
	}
	return registry
}
