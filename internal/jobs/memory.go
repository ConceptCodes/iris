package jobs

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

const defaultMaxAttempts = 5

type MemoryStore struct {
	mu   sync.Mutex
	jobs []Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Enqueue(ctx context.Context, job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.Status == "" {
		job.Status = StatusPending
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = defaultMaxAttempts
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}
	if job.DedupKey != "" {
		for _, existing := range s.jobs {
			if existing.DedupKey == job.DedupKey && existing.Status != StatusDeadLetter {
				return existing, nil
			}
		}
	}
	job.CreatedAt = now
	job.UpdatedAt = now

	s.jobs = append(s.jobs, job)
	return job, nil
}

func (s *MemoryStore) LeaseNext(ctx context.Context, now time.Time, leaseDuration time.Duration, allowedTypes ...Type) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, job := range s.jobs {
		if !isAllowedType(job.Type, allowedTypes) {
			continue
		}
		if job.Status != StatusPending {
			continue
		}
		if job.AvailableAt.After(now) {
			continue
		}
		job.Status = StatusLeased
		job.Attempts++
		job.LeasedUntil = now.Add(leaseDuration)
		job.UpdatedAt = now
		s.jobs[index] = job
		return job, true, nil
	}

	return Job{}, false, nil
}

func (s *MemoryStore) MarkSucceeded(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, job := range s.jobs {
		if job.ID != id {
			continue
		}
		job.Status = StatusSucceeded
		job.UpdatedAt = time.Now().UTC()
		s.jobs[index] = job
		return nil
	}
	return fmt.Errorf("job not found: %s", id)
}

func (s *MemoryStore) MarkFailed(ctx context.Context, id string, err error, retryAt time.Time) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, job := range s.jobs {
		if job.ID != id {
			continue
		}
		job.LastError = err.Error()
		job.UpdatedAt = time.Now().UTC()
		job.LeasedUntil = time.Time{}
		if job.Attempts >= job.MaxAttempts {
			job.Status = StatusDeadLetter
		} else {
			job.Status = StatusPending
			job.AvailableAt = retryAt
		}
		s.jobs[index] = job
		return job.Status, nil
	}
	return "", fmt.Errorf("job not found: %s", id)
}

func (s *MemoryStore) MarkDeadLetter(ctx context.Context, id string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, job := range s.jobs {
		if job.ID != id {
			continue
		}
		job.Status = StatusDeadLetter
		job.LastError = err.Error()
		job.LeasedUntil = time.Time{}
		job.UpdatedAt = time.Now().UTC()
		s.jobs[index] = job
		return nil
	}
	return fmt.Errorf("job not found: %s", id)
}

func (s *MemoryStore) Close() error {
	return nil
}

func isAllowedType(jobType Type, allowedTypes []Type) bool {
	return len(allowedTypes) == 0 || slices.Contains(allowedTypes, jobType)
}
