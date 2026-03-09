package jobs

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreLeaseAndComplete(t *testing.T) {
	store := NewMemoryStore()
	job, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	leased, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeFetchImage)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if !ok {
		t.Fatalf("expected a leased job")
	}
	if leased.ID != job.ID {
		t.Fatalf("unexpected leased job: %s", leased.ID)
	}

	if err := store.MarkSucceeded(context.Background(), job.ID); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
}

func TestMemoryStoreFailureRetriesThenDeadLetters(t *testing.T) {
	store := NewMemoryStore()
	job, err := store.Enqueue(context.Background(), Job{
		Type:        TypeFetchImage,
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		leased, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeFetchImage)
		if err != nil {
			t.Fatalf("lease: %v", err)
		}
		if !ok || leased.ID != job.ID {
			t.Fatalf("expected job lease on attempt %d", attempt+1)
		}
		if _, err := store.MarkFailed(context.Background(), job.ID, assertErr("boom"), time.Now()); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	}

	leased, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeFetchImage)
	if err != nil {
		t.Fatalf("lease after dead letter: %v", err)
	}
	if ok {
		t.Fatalf("expected no lease after dead letter, got %s", leased.ID)
	}
}

func TestMemoryStoreDefaults(t *testing.T) {
	store := NewMemoryStore()
	job, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected job ID to be set")
	}
	if job.Status != StatusPending {
		t.Fatalf("expected status pending, got %s", job.Status)
	}
	if job.MaxAttempts != defaultMaxAttempts {
		t.Fatalf("expected default max attempts %d, got %d", defaultMaxAttempts, job.MaxAttempts)
	}
	if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}
}

func TestMemoryStoreDeduplicationReturnsExisting(t *testing.T) {
	store := NewMemoryStore()
	first, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage, DedupKey: "dup-key"})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage, DedupKey: "dup-key"})
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected dedup to return existing job, got %s", second.ID)
	}
}

func TestMemoryStoreDedupAllowsAfterDeadLetter(t *testing.T) {
	store := NewMemoryStore()
	job, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage, DedupKey: "dup-key", MaxAttempts: 1})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	leased, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeFetchImage)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if !ok || leased.ID != job.ID {
		t.Fatalf("expected leased job")
	}
	if _, err := store.MarkFailed(context.Background(), job.ID, assertErr("boom"), time.Now()); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	newJob, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage, DedupKey: "dup-key"})
	if err != nil {
		t.Fatalf("enqueue new job: %v", err)
	}
	if newJob.ID == job.ID {
		t.Fatalf("expected new job ID after dead letter, got %s", newJob.ID)
	}
}

func TestMemoryStoreLeaseRespectsAvailableAt(t *testing.T) {
	store := NewMemoryStore()
	future := time.Now().Add(10 * time.Minute)
	job, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage, AvailableAt: future})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeFetchImage); err != nil {
		t.Fatalf("lease: %v", err)
	} else if ok {
		t.Fatal("expected no lease before AvailableAt")
	}
	if leased, ok, err := store.LeaseNext(context.Background(), future.Add(time.Second), 30*time.Second, TypeFetchImage); err != nil {
		t.Fatalf("lease after available: %v", err)
	} else if !ok || leased.ID != job.ID {
		t.Fatalf("expected lease after available, got ok=%v id=%s", ok, leased.ID)
	}
}

func TestMemoryStoreLeaseRespectsAllowedTypes(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Enqueue(context.Background(), Job{Type: TypeFetchImage})
	if err != nil {
		t.Fatalf("enqueue fetch_image: %v", err)
	}
	_, err = store.Enqueue(context.Background(), Job{Type: TypeIndexLocalFile})
	if err != nil {
		t.Fatalf("enqueue index_local_file: %v", err)
	}
	leased, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, TypeIndexLocalFile)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if !ok {
		t.Fatal("expected lease for allowed type")
	}
	if leased.Type != TypeIndexLocalFile {
		t.Fatalf("expected index_local_file type, got %s", leased.Type)
	}
}

func TestMemoryStoreMarkSucceededErrorsForUnknownJob(t *testing.T) {
	store := NewMemoryStore()
	if err := store.MarkSucceeded(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing job")
	}
}

func TestMemoryStoreMarkFailedErrorsForUnknownJob(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.MarkFailed(context.Background(), "missing", assertErr("boom"), time.Now()); err == nil {
		t.Fatal("expected error for missing job")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
