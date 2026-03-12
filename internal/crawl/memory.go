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
	scheduleEvery, _ := time.ParseDuration(input.ScheduleEvery)
	source := Source{
		ID:              uuid.NewString(),
		Kind:            input.Kind,
		SeedURL:         input.SeedURL,
		LocalPath:       input.LocalPath,
		Status:          SourceStatusActive,
		MaxDepth:        input.MaxDepth,
		RateLimitRPS:    input.RateLimitRPS,
		MaxPagesPerRun:  input.MaxPagesPerRun,
		MaxImagesPerRun: input.MaxImagesPerRun,
		AllowedDomains:  slices.Clone(input.AllowedDomains),
		ScheduleEvery:   scheduleEvery,
		NextRunAt:       nextRunTime(now, scheduleEvery),
		CreatedAt:       now,
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

func (s *MemoryStore) CreateRun(ctx context.Context, sourceID, trigger string, scheduledAt time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	run := Run{
		ID:          uuid.NewString(),
		SourceID:    sourceID,
		Trigger:     trigger,
		Status:      RunStatusRunning,
		ScheduledAt: scheduledAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.runs = append(s.runs, run)
	return run, nil
}

func (s *MemoryStore) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 || limit > len(s.runs) {
		limit = len(s.runs)
	}
	start := len(s.runs) - limit
	out := make([]Run, limit)
	copy(out, s.runs[start:])
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

func (s *MemoryStore) SetRunDiscovered(ctx context.Context, id string, discovered int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.DiscoveredCount = discovered
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) IncrementRunIndexed(ctx context.Context, id string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.IndexedCount += delta
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) IncrementRunDuplicate(ctx context.Context, id string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.DuplicateCount += delta
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) IncrementRunFailed(ctx context.Context, id string, delta int, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.FailedCount += delta
		run.LastError = lastError
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) MarkRunCompleted(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.Status = RunStatusCompleted
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		s.applySourceOutcomeLocked(run, true)
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) MarkRunFailed(ctx context.Context, id, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, run := range s.runs {
		if run.ID != id {
			continue
		}
		run.Status = RunStatusFailed
		run.LastError = message
		run.UpdatedAt = time.Now().UTC()
		s.runs[index] = run
		s.applySourceOutcomeLocked(run, false)
		return nil
	}
	return fmt.Errorf("run not found: %s", id)
}

func (s *MemoryStore) ListSourcesDue(ctx context.Context, now time.Time) ([]Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []Source
	for _, source := range s.sources {
		if source.Status != SourceStatusActive {
			continue
		}
		if source.ScheduleEvery <= 0 {
			continue
		}
		if source.NextRunAt.IsZero() || !source.NextRunAt.After(now) {
			due = append(due, source)
		}
	}
	return due, nil
}

func (s *MemoryStore) UpdateSourceNextRun(ctx context.Context, id string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, source := range s.sources {
		if source.ID != id {
			continue
		}
		source.NextRunAt = next
		s.sources[index] = source
		return nil
	}
	return fmt.Errorf("source not found: %s", id)
}

func nextRunTime(now time.Time, every time.Duration) time.Time {
	if every <= 0 {
		return time.Time{}
	}
	return now.Add(every)
}

func (s *MemoryStore) applySourceOutcomeLocked(run Run, success bool) {
	for index, source := range s.sources {
		if source.ID != run.SourceID {
			continue
		}
		now := time.Now().UTC()
		source.LastRunAt = now
		source.LastDiscoveredCount = run.DiscoveredCount
		source.LastIndexedCount = run.IndexedCount
		source.LastDuplicateCount = run.DuplicateCount
		source.LastFailedCount = run.FailedCount
		if success {
			source.LastSuccessAt = now
			source.ConsecutiveFailures = 0
			if run.IndexedCount > 0 {
				source.LastContentChangeAt = now
			}
		} else {
			source.ConsecutiveFailures++
		}
		source.NextRunAt = nextAdaptiveRunTime(now, source, run, success)
		s.sources[index] = source
		return
	}
}

func nextAdaptiveRunTime(now time.Time, source Source, run Run, success bool) time.Time {
	base := source.ScheduleEvery
	if base <= 0 {
		return time.Time{}
	}
	interval := base
	switch {
	case !success:
		interval = clampDuration(base*time.Duration(source.ConsecutiveFailures+2), base, base*8)
	case run.IndexedCount > 0 && (run.IndexedCount >= 5 || (source.MaxImagesPerRun > 0 && run.IndexedCount >= max(1, source.MaxImagesPerRun/2))):
		interval = clampDuration(base/2, time.Minute, base)
	case run.IndexedCount > 0:
		interval = base
	case run.DiscoveredCount == 0:
		interval = clampDuration(base*4, base, base*8)
	case run.DuplicateCount > 0 && run.IndexedCount == 0:
		interval = clampDuration(base*3, base, base*8)
	default:
		interval = clampDuration(base*2, base, base*8)
	}
	return now.Add(interval)
}

func clampDuration(v, minV, maxV time.Duration) time.Duration {
	if minV > 0 && v < minV {
		return minV
	}
	if maxV > 0 && v > maxV {
		return maxV
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *MemoryStore) Close() error {
	return nil
}
