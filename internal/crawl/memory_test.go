package crawl

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"iris/internal/jobs"
)

func TestMemoryStoreCreateSourceDefaults(t *testing.T) {
	store := NewMemoryStore()
	source, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "1h",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if source.ID == "" {
		t.Fatal("expected source ID to be set")
	}
	if source.Status != SourceStatusActive {
		t.Fatalf("expected active status, got %s", source.Status)
	}
	if source.ScheduleEvery <= 0 {
		t.Fatalf("expected schedule every to be parsed, got %v", source.ScheduleEvery)
	}
	if source.NextRunAt.IsZero() {
		t.Fatal("expected next run time to be set")
	}
}

func TestMemoryStoreCreateSourceClonesAllowedDomains(t *testing.T) {
	store := NewMemoryStore()
	allowed := []string{"example.com", "images.example.com"}
	source, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:           SourceKindDomain,
		SeedURL:        "https://example.com",
		AllowedDomains: allowed,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	allowed[0] = "mutated.com"
	if source.AllowedDomains[0] != "example.com" {
		t.Fatalf("expected allowed domains to be cloned")
	}
}

func TestMemoryStoreGetSourceNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.GetSource(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMemoryStoreCreateAndGetRun(t *testing.T) {
	store := NewMemoryStore()
	run, err := store.CreateRun(context.Background(), "source-1", "manual", time.Now().UTC())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected run ID to be set")
	}
	if run.Status != RunStatusRunning {
		t.Fatalf("expected running status, got %s", run.Status)
	}
	got, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.ID != run.ID {
		t.Fatalf("expected run ID %s, got %s", run.ID, got.ID)
	}
}

func TestMemoryStoreSetRunDiscoveredUpdatesRun(t *testing.T) {
	store := NewMemoryStore()
	run, err := store.CreateRun(context.Background(), "source-1", "manual", time.Now().UTC())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.SetRunDiscovered(context.Background(), run.ID, 5); err != nil {
		t.Fatalf("set run discovered: %v", err)
	}
	got, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.DiscoveredCount != 5 {
		t.Fatalf("expected discovered count 5, got %d", got.DiscoveredCount)
	}
}

func TestMemoryStoreIncrementRunCounters(t *testing.T) {
	store := NewMemoryStore()
	run, err := store.CreateRun(context.Background(), "source-1", "manual", time.Now().UTC())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.IncrementRunIndexed(context.Background(), run.ID, 3); err != nil {
		t.Fatalf("increment indexed: %v", err)
	}
	if err := store.IncrementRunDuplicate(context.Background(), run.ID, 2); err != nil {
		t.Fatalf("increment duplicate: %v", err)
	}
	if err := store.IncrementRunFailed(context.Background(), run.ID, 1, "boom"); err != nil {
		t.Fatalf("increment failed: %v", err)
	}
	got, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.IndexedCount != 3 {
		t.Fatalf("expected indexed count 3, got %d", got.IndexedCount)
	}
	if got.DuplicateCount != 2 {
		t.Fatalf("expected duplicate count 2, got %d", got.DuplicateCount)
	}
	if got.FailedCount != 1 {
		t.Fatalf("expected failed count 1, got %d", got.FailedCount)
	}
	if got.LastError != "boom" {
		t.Fatalf("expected last error to be set")
	}
}

func TestMemoryStoreMarkRunCompletedUpdatesSource(t *testing.T) {
	store := NewMemoryStore()
	source, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "10m",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	run, err := store.CreateRun(context.Background(), source.ID, "manual", time.Now().UTC())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.SetRunDiscovered(context.Background(), run.ID, 4); err != nil {
		t.Fatalf("set run discovered: %v", err)
	}
	if err := store.IncrementRunIndexed(context.Background(), run.ID, 2); err != nil {
		t.Fatalf("increment indexed: %v", err)
	}
	if err := store.IncrementRunDuplicate(context.Background(), run.ID, 1); err != nil {
		t.Fatalf("increment duplicate: %v", err)
	}
	if err := store.MarkRunCompleted(context.Background(), run.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	updated, err := store.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updated.LastDiscoveredCount != 4 {
		t.Fatalf("expected discovered count 4, got %d", updated.LastDiscoveredCount)
	}
	if updated.LastIndexedCount != 2 {
		t.Fatalf("expected indexed count 2, got %d", updated.LastIndexedCount)
	}
	if updated.LastDuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1, got %d", updated.LastDuplicateCount)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Fatalf("expected consecutive failures to reset")
	}
}

func TestMemoryStoreMarkRunFailedUpdatesSource(t *testing.T) {
	store := NewMemoryStore()
	source, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "10m",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	run, err := store.CreateRun(context.Background(), source.ID, "manual", time.Now().UTC())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := store.MarkRunFailed(context.Background(), run.ID, "boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	updated, err := store.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if updated.ConsecutiveFailures != 1 {
		t.Fatalf("expected consecutive failures to increment, got %d", updated.ConsecutiveFailures)
	}
}

func TestMemoryStoreListSourcesDue(t *testing.T) {
	store := NewMemoryStore()
	active, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "1s",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	paused, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://paused.example.com",
		ScheduleEvery: "1s",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	paused.Status = SourceStatusPaused
	store.sources = []Source{active, paused}
	if err := store.UpdateSourceNextRun(context.Background(), active.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("update next run: %v", err)
	}
	if err := store.UpdateSourceNextRun(context.Background(), paused.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("update next run: %v", err)
	}

	results, err := store.ListSourcesDue(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("list sources due: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 due source, got %d", len(results))
	}
	if results[0].ID != active.ID {
		t.Fatalf("expected active source to be due")
	}
}

func TestMemoryStoreUpdateSourceNextRun(t *testing.T) {
	store := NewMemoryStore()
	source, err := store.CreateSource(context.Background(), CreateSourceInput{
		Kind:          SourceKindDomain,
		SeedURL:       "https://example.com",
		ScheduleEvery: "1s",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	next := time.Now().Add(2 * time.Hour)
	if err := store.UpdateSourceNextRun(context.Background(), source.ID, next); err != nil {
		t.Fatalf("update next run: %v", err)
	}
	updated, err := store.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !updated.NextRunAt.Equal(next) {
		t.Fatalf("expected next run to be updated")
	}
}

func TestMemoryStoreErrorsForMissingRun(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SetRunDiscovered(context.Background(), "missing", 1); err == nil {
		t.Fatal("expected error for missing run")
	}
	if err := store.IncrementRunIndexed(context.Background(), "missing", 1); err == nil {
		t.Fatal("expected error for missing run")
	}
	if err := store.IncrementRunDuplicate(context.Background(), "missing", 1); err == nil {
		t.Fatal("expected error for missing run")
	}
	if err := store.IncrementRunFailed(context.Background(), "missing", 1, "boom"); err == nil {
		t.Fatal("expected error for missing run")
	}
	if err := store.MarkRunCompleted(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing run")
	}
	if err := store.MarkRunFailed(context.Background(), "missing", "boom"); err == nil {
		t.Fatal("expected error for missing run")
	}
}

func TestServiceCreateSourceValidation(t *testing.T) {
	service := NewService(NewMemoryStore(), jobs.NewMemoryStore())
	tests := []struct {
		name  string
		input CreateSourceInput
	}{
		{"missing_kind", CreateSourceInput{}},
		{"domain_missing_seed", CreateSourceInput{Kind: SourceKindDomain}},
		{"local_dir_missing_path", CreateSourceInput{Kind: SourceKindLocalDir}},
		{"invalid_schedule", CreateSourceInput{Kind: SourceKindDomain, SeedURL: "https://example.com", ScheduleEvery: "nope"}},
		{"negative_pages", CreateSourceInput{Kind: SourceKindDomain, SeedURL: "https://example.com", MaxPagesPerRun: -1}},
		{"negative_images", CreateSourceInput{Kind: SourceKindDomain, SeedURL: "https://example.com", MaxImagesPerRun: -1}},
		{"unsupported_kind", CreateSourceInput{Kind: SourceKind("wat")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.CreateSource(context.Background(), tt.input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestServiceTriggerRunDefaultsToManual(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	store := NewMemoryStore()
	service := NewService(store, jobStore)
	source, err := service.CreateSource(context.Background(), CreateSourceInput{
		Kind:      SourceKindLocalDir,
		LocalPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	run, err := service.TriggerRun(context.Background(), source.ID, "")
	if err != nil {
		t.Fatalf("trigger run: %v", err)
	}
	if run.Trigger != "manual" {
		t.Fatalf("expected manual trigger, got %q", run.Trigger)
	}

	job, ok, err := jobStore.LeaseNext(context.Background(), time.Now().Add(time.Second), time.Second, jobs.TypeDiscoverSource)
	if err != nil || !ok {
		t.Fatalf("expected discover source job, ok=%v err=%v", ok, err)
	}
	var payload jobs.DiscoverSourcePayload
	if err := json.Unmarshal(job.PayloadJSON, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.SourceID != source.ID || payload.RunID != run.ID {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestServiceTriggerRunForSourceDefaultsToScheduled(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	store := NewMemoryStore()
	service := NewService(store, jobStore)
	source, err := service.CreateSource(context.Background(), CreateSourceInput{
		Kind:      SourceKindLocalDir,
		LocalPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	scheduledAt := time.Now().UTC()
	run, err := service.TriggerRunForSource(context.Background(), source, "", scheduledAt)
	if err != nil {
		t.Fatalf("trigger run for source: %v", err)
	}
	if run.Trigger != "scheduled" {
		t.Fatalf("expected scheduled trigger, got %q", run.Trigger)
	}
}

func TestServiceListRunsAndGetRun(t *testing.T) {
	jobStore := jobs.NewMemoryStore()
	store := NewMemoryStore()
	service := NewService(store, jobStore)
	source, err := service.CreateSource(context.Background(), CreateSourceInput{
		Kind:      SourceKindLocalDir,
		LocalPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	run, err := service.TriggerRun(context.Background(), source.ID, "manual")
	if err != nil {
		t.Fatalf("trigger run: %v", err)
	}

	runs, err := service.ListRuns(context.Background(), 100)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("unexpected runs: %+v", runs)
	}

	got, err := service.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.ID != run.ID {
		t.Fatalf("expected run ID %q, got %q", run.ID, got.ID)
	}
}
