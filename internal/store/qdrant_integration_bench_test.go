package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"iris/internal/constants"
	"iris/pkg/models"
)

func requireQdrantBenchStore(b *testing.B) *QdrantStore {
	b.Helper()

	addr := os.Getenv("QDRANT_BENCH_ADDR")
	if addr == "" {
		b.Skip("set QDRANT_BENCH_ADDR to run Qdrant integration benchmarks")
	}

	store, err := NewQdrantStore(addr, 4, 5*time.Second)
	if err != nil {
		b.Fatalf("create qdrant store: %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func BenchmarkQdrantStoreIntegration(b *testing.B) {
	store := requireQdrantBenchStore(b)
	ctx := context.Background()

	record := models.ImageRecord{
		ID:       "bench-seed",
		URL:      "https://example.com/seed.jpg",
		Filename: "seed.jpg",
		Meta: map[string]string{
			constants.MetaKeyContentSHA256: "bench-seed-hash",
			constants.MetaKeySourceDomain:  "example.com",
		},
	}
	embedding := models.Embedding{1, 2, 3, 4}
	if _, err := store.Upsert(ctx, record, models.Embeddings{models.EncoderCLIP: embedding}); err != nil {
		b.Fatalf("seed qdrant record: %v", err)
	}

	b.Run("search", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.Search(ctx, models.EncoderCLIP, embedding, 10, nil); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("find_id_by_meta", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeyContentSHA256, "bench-seed-hash"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("upsert", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			record.ID = fmt.Sprintf("bench-upsert-%d", i%32)
			record.Meta[constants.MetaKeyContentSHA256] = fmt.Sprintf("bench-hash-%d", i%32)
			if _, err := store.Upsert(ctx, record, models.Embeddings{models.EncoderCLIP: embedding}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
