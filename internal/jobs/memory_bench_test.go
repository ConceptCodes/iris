package jobs

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func benchmarkMemoryStore(size int) *MemoryStore {
	store := NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < size; i++ {
		_, _ = store.Enqueue(ctx, Job{
			ID:          fmt.Sprintf("job-%d", i),
			Type:        TypeFetchImage,
			DedupKey:    fmt.Sprintf("dedupe-%d", i),
			PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
			AvailableAt: now,
		})
	}
	return store
}

func BenchmarkMemoryStoreLeaseNext(b *testing.B) {
	ctx := context.Background()

	b.Run("single_worker", func(b *testing.B) {
		store := benchmarkMemoryStore(1)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				b.Fatal("expected job")
			}
			store.mu.Lock()
			store.jobs[0].Status = StatusPending
			store.jobs[0].LeasedUntil = time.Time{}
			store.jobs[0].UpdatedAt = time.Now().UTC()
			store.mu.Unlock()
			_ = job
		}
	})

	b.Run("scan_1000_jobs", func(b *testing.B) {
		store := &MemoryStore{
			jobs: make([]Job, 1000),
		}
		now := time.Now().UTC()
		for i := 0; i < 999; i++ {
			store.jobs[i] = Job{
				ID:          fmt.Sprintf("blocked-%d", i),
				Type:        TypeDiscoverSource,
				Status:      StatusPending,
				AvailableAt: now,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
		}
		store.jobs[999] = Job{
			ID:          "eligible",
			Type:        TypeFetchImage,
			Status:      StatusPending,
			AvailableAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				b.Fatal("expected job")
			}
			store.mu.Lock()
			store.jobs[999].Status = StatusPending
			store.jobs[999].LeasedUntil = time.Time{}
			store.jobs[999].UpdatedAt = time.Now().UTC()
			store.mu.Unlock()
		}
	})
}

func BenchmarkBuildLeaseQuery(b *testing.B) {
	now := time.Now().UTC()
	types := []Type{TypeFetchImage, TypeIndexLocalFile, TypeReindexImage}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = buildLeaseQuery(now, types)
	}
}
