package indexing

import (
	"context"
	"fmt"
	"os"
	"testing"

	"iris/internal/assets"
)

func benchmarkImageBytes(size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i)
	}
	return buf
}

func BenchmarkPipelineIndexUploadedBytes(b *testing.B) {
	ctx := context.Background()
	imageBytes := benchmarkImageBytes(256 * 1024)

	b.Run("unique_no_asset_store", func(b *testing.B) {
		engine := &mockEngine{id: "indexed-id"}
		pipeline := NewPipeline(engine, nil)

		b.SetBytes(int64(len(imageBytes)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			meta := map[string]string{
				"source_url": fmt.Sprintf("https://example.com/%d.jpg", i),
			}
			if _, err := pipeline.IndexUploadedBytesResult(ctx, imageBytes, "photo.jpg", nil, meta); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("duplicate_no_asset_store", func(b *testing.B) {
		engine := &mockEngine{id: "indexed-id", findID: "existing-id", findOK: true}
		pipeline := NewPipeline(engine, nil)

		b.SetBytes(int64(len(imageBytes)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			meta := map[string]string{
				"content_sha256": "duplicate-hash",
			}
			if _, err := pipeline.IndexUploadedBytesResult(ctx, imageBytes, "photo.jpg", nil, meta); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("unique_with_local_asset_store", func(b *testing.B) {
		engine := &mockEngine{id: "indexed-id"}
		pipeline := NewPipeline(engine, assets.NewStore(b.TempDir()))

		b.SetBytes(int64(len(imageBytes)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			meta := map[string]string{
				"source_url": fmt.Sprintf("https://example.com/%d.jpg", i),
			}
			if _, err := pipeline.IndexUploadedBytesResult(ctx, imageBytes, "photo.jpg", nil, meta); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPipelineIndexLocalFile(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	path := dir + "/photo.jpg"
	imageBytes := benchmarkImageBytes(256 * 1024)
	if err := os.WriteFile(path, imageBytes, 0o644); err != nil {
		b.Fatal(err)
	}

	engine := &mockEngine{id: "local-id"}
	pipeline := NewPipeline(engine, nil)

	b.SetBytes(int64(len(imageBytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pipeline.IndexLocalFileResult(ctx, path); err != nil {
			b.Fatal(err)
		}
	}
}
