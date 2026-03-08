package crawl

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	mu      sync.Mutex
	sources []Source
	runs    []Run
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) CreateSource(ctx context.Context, input CreateSourceInput) (Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	source := Source{
		ID:             uuid.NewString(),
		Kind:           input.Kind,
		SeedURL:        input.SeedURL,
		LocalPath:      input.LocalPath,
		Status:         SourceStatusActive,
		MaxDepth:       input.MaxDepth,
		RateLimitRPS:   input.RateLimitRPS,
		AllowedDomains: slices.Clone(input.AllowedDomains),
		CreatedAt:      now,
	}
	s.sources = append(s.sources, source)
	return source, nil
}

func (s *MemoryStore) GetSource(ctx context.Context, id string) (Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, source := range s.sources {
		if source.ID == id {
			return source, nil
		}
	}
	return Source{}, fmt.Errorf("source not found: %s", id)
}

func (s *MemoryStore) CreateRun(ctx context.Context, sourceID, trigger string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	run := Run{
		ID:        uuid.NewString(),
		SourceID:  sourceID,
		Trigger:   trigger,
		Status:    RunStatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.runs = append(s.runs, run)
	return run, nil
}

func (s *MemoryStore) ListRuns(ctx context.Context) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Run, len(s.runs))
	copy(out, s.runs)
	return out, nil
}

func (s *MemoryStore) GetRun(ctx context.Context, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, run := range s.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return Run{}, fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) MarkRunCompleted(ctx context.Context, id string, discovered, indexed, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.Status = RunStatusCompleted
		run.DiscoveredCount = discovered
		run.IndexedCount = indexed
		run.FailedCount = failed
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) MarkRunFailed(ctx context.Context, id, message string, discovered, indexed, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.Status = RunStatusFailed
		run.DiscoveredCount = discovered
		run.IndexedCount = indexed
		run.FailedCount = failed
		run.LastError = message
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) Close() error {
	return nil
}
