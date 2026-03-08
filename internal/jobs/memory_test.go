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
		if err := store.MarkFailed(context.Background(), job.ID, assertErr("boom"), time.Now()); err != nil {
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

type assertErr string

func (e assertErr) Error() string { return string(e) }
