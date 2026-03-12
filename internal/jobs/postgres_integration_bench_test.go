package jobs

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"iris/config"
)

func requirePostgresBenchStore(b *testing.B) *PostgresStore {
	b.Helper()

	dsn := os.Getenv("JOB_STORE_BENCH_DSN")
	if dsn == "" {
		b.Skip("set JOB_STORE_BENCH_DSN to run Postgres job-store integration benchmarks")
	}

	store, err := NewPostgresStore(context.Background(), dsn, config.PostgresPool{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	})
	if err != nil {
		b.Fatalf("create postgres store: %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func cleanupBenchJobs(ctx context.Context, b *testing.B, store *PostgresStore, prefix string) {
	b.Helper()

	if _, err := store.db.ExecContext(
		ctx,
		`DELETE FROM jobs WHERE id LIKE $1 OR dedup_key LIKE $1`,
		prefix+"%",
	); err != nil {
		b.Fatalf("cleanup benchmark jobs: %v", err)
	}
}

func seedPendingJobs(ctx context.Context, b *testing.B, store *PostgresStore, prefix string, start, count int) {
	b.Helper()

	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		index := start + i
		if _, err := store.Enqueue(ctx, Job{
			ID:          fmt.Sprintf("%s-job-%d", prefix, index),
			Type:        TypeFetchImage,
			DedupKey:    fmt.Sprintf("%s-dedup-%d", prefix, index),
			PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
			AvailableAt: now,
		}); err != nil {
			b.Fatalf("seed pending jobs: %v", err)
		}
	}
}

func BenchmarkPostgresStoreIntegration(b *testing.B) {
	store := requirePostgresBenchStore(b)
	ctx := context.Background()
	prefix := fmt.Sprintf("bench-%d", time.Now().UnixNano())
	cleanupBenchJobs(ctx, b, store, prefix)
	b.Cleanup(func() {
		cleanupBenchJobs(ctx, b, store, prefix)
	})

	b.Run("enqueue", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.Enqueue(ctx, Job{
				ID:          fmt.Sprintf("%s-enqueue-%d", prefix, i),
				Type:        TypeFetchImage,
				DedupKey:    fmt.Sprintf("%s-enqueue-dedup-%d", prefix, i),
				PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
				AvailableAt: time.Now().UTC(),
			}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("lease_next", func(b *testing.B) {
		nextSeed := 0
		seedPendingJobs(ctx, b, store, prefix+"-lease", nextSeed, 2048)
		nextSeed += 2048

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			job, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				b.StopTimer()
				seedPendingJobs(ctx, b, store, prefix+"-lease", nextSeed, 2048)
				nextSeed += 2048
				b.StartTimer()
				i--
				continue
			}
			_ = job
		}
	})

	b.Run("mark_succeeded", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			jobID := fmt.Sprintf("%s-success-%d", prefix, i)
			if _, err := store.Enqueue(ctx, Job{
				ID:          jobID,
				Type:        TypeFetchImage,
				DedupKey:    fmt.Sprintf("%s-success-dedup-%d", prefix, i),
				PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
				AvailableAt: time.Now().UTC(),
			}); err != nil {
				b.Fatal(err)
			}
			job, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				b.Fatal("expected leased job")
			}
			b.StartTimer()
			if err := store.MarkSucceeded(ctx, job.ID); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("mark_failed_retryable", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			jobID := fmt.Sprintf("%s-failed-%d", prefix, i)
			if _, err := store.Enqueue(ctx, Job{
				ID:          jobID,
				Type:        TypeFetchImage,
				DedupKey:    fmt.Sprintf("%s-failed-dedup-%d", prefix, i),
				PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
				AvailableAt: time.Now().UTC(),
			}); err != nil {
				b.Fatal(err)
			}
			job, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				b.Fatal("expected leased job")
			}
			b.StartTimer()
			status, err := store.MarkFailed(ctx, job.ID, fmt.Errorf("temporary failure"), time.Now().UTC().Add(time.Second))
			if err != nil {
				b.Fatal(err)
			}
			if status != StatusPending {
				b.Fatalf("unexpected status: %s", status)
			}
		}
	})
}

func BenchmarkPostgresStoreConcurrentIntegration(b *testing.B) {
	store := requirePostgresBenchStore(b)
	ctx := context.Background()
	prefix := fmt.Sprintf("bench-concurrent-%d", time.Now().UnixNano())
	cleanupBenchJobs(ctx, b, store, prefix)
	b.Cleanup(func() {
		cleanupBenchJobs(ctx, b, store, prefix)
	})

	b.Run("enqueue_parallel", func(b *testing.B) {
		var seq atomic.Int64

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				n := seq.Add(1)
				if _, err := store.Enqueue(ctx, Job{
					ID:          fmt.Sprintf("%s-enqueue-%d", prefix, n),
					Type:        TypeFetchImage,
					DedupKey:    fmt.Sprintf("%s-enqueue-dedup-%d", prefix, n),
					PayloadJSON: []byte(`{"url":"https://example.com/image.jpg"}`),
					AvailableAt: time.Now().UTC(),
				}); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})

	b.Run("lease_mark_succeeded_parallel", func(b *testing.B) {
		seedPendingJobs(ctx, b, store, prefix+"-parallel-lease", 0, b.N)

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				job, ok, err := store.LeaseNext(ctx, time.Now().UTC(), time.Minute, TypeFetchImage)
				if err != nil {
					b.Error(err)
					return
				}
				if !ok {
					b.Error("expected leased job")
					return
				}
				if err := store.MarkSucceeded(ctx, job.ID); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
}
