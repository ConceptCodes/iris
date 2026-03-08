package crawl

import (
	"context"
	"testing"
	"time"

	"iris/internal/jobs"
)

func TestServiceCreateAndTriggerRun(t *testing.T) {
	store := NewMemoryStore()
	jobStore := jobs.NewMemoryStore()
	service := NewService(store, jobStore)

	source, err := service.CreateSource(context.Background(), CreateSourceInput{
		Kind:      SourceKindLocalDir,
		LocalPath: "/tmp/images",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	run, err := service.TriggerRun(context.Background(), source.ID, "manual")
	if err != nil {
		t.Fatalf("trigger run: %v", err)
	}
	if run.SourceID != source.ID {
		t.Fatalf("unexpected run source id: %s", run.SourceID)
	}

	if _, ok, err := jobStore.LeaseNext(context.Background(), time.Now().Add(time.Second), 0, jobs.TypeDiscoverSource); err != nil || !ok {
		t.Fatalf("expected discover job, err=%v", err)
	}

	if err := store.SetRunDiscovered(context.Background(), run.ID, 3); err != nil {
		t.Fatalf("set discovered: %v", err)
	}
	if err := store.IncrementRunIndexed(context.Background(), run.ID, 2); err != nil {
		t.Fatalf("increment indexed: %v", err)
	}
	if err := store.IncrementRunFailed(context.Background(), run.ID, 1, "boom"); err != nil {
		t.Fatalf("increment failed: %v", err)
	}
	if err := store.MarkRunCompleted(context.Background(), run.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
}
