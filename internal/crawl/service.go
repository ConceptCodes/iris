// Package crawl provides web crawling functionality for discovering and indexing images.
// It supports multiple source types (URLs, domains, sitemaps, local directories)
// with robots.txt compliance, rate limiting, and job queue integration.
package crawl

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"iris/internal/jobs"
)

type Service struct {
	store    Store
	jobStore jobs.Store
}

func NewService(store Store, jobStore jobs.Store) *Service {
	return &Service{store: store, jobStore: jobStore}
}

func (s *Service) CreateSource(ctx context.Context, input CreateSourceInput) (Source, error) {
	if input.Kind == "" {
		return Source{}, fmt.Errorf("kind is required")
	}
	switch input.Kind {
	case SourceKindURLList, SourceKindDomain, SourceKindSitemap:
		if input.SeedURL == "" {
			return Source{}, fmt.Errorf("seed_url is required")
		}
	case SourceKindLocalDir:
		if input.LocalPath == "" {
			return Source{}, fmt.Errorf("local_path is required")
		}
	default:
		return Source{}, fmt.Errorf("unsupported source kind: %s", input.Kind)
	}
	if input.ScheduleEvery != "" {
		if _, err := time.ParseDuration(input.ScheduleEvery); err != nil {
			return Source{}, fmt.Errorf("invalid schedule_every: %w", err)
		}
	}
	if input.MaxPagesPerRun < 0 {
		return Source{}, fmt.Errorf("max_pages_per_run must be >= 0")
	}
	if input.MaxImagesPerRun < 0 {
		return Source{}, fmt.Errorf("max_images_per_run must be >= 0")
	}
	return s.store.CreateSource(ctx, input)
}

func (s *Service) TriggerRun(ctx context.Context, sourceID, trigger string) (Run, error) {
	if trigger == "" {
		trigger = "manual"
	}
	source, err := s.store.GetSource(ctx, sourceID)
	if err != nil {
		return Run{}, err
	}
	run, err := s.store.CreateRun(ctx, source.ID, trigger, time.Now().UTC())
	if err != nil {
		return Run{}, err
	}
	payload, err := json.Marshal(jobs.DiscoverSourcePayload{
		SourceID: source.ID,
		RunID:    run.ID,
	})
	if err != nil {
		return Run{}, err
	}
	if _, err := s.jobStore.Enqueue(ctx, jobs.Job{Type: jobs.TypeDiscoverSource, PayloadJSON: payload}); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) TriggerRunForSource(ctx context.Context, source Source, trigger string, scheduledAt time.Time) (Run, error) {
	if trigger == "" {
		trigger = "scheduled"
	}
	run, err := s.store.CreateRun(ctx, source.ID, trigger, scheduledAt)
	if err != nil {
		return Run{}, err
	}
	payload, err := json.Marshal(jobs.DiscoverSourcePayload{
		SourceID: source.ID,
		RunID:    run.ID,
	})
	if err != nil {
		return Run{}, err
	}
	if _, err := s.jobStore.Enqueue(ctx, jobs.Job{Type: jobs.TypeDiscoverSource, PayloadJSON: payload}); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) DueSources(ctx context.Context, now time.Time) ([]Source, error) {
	return s.store.ListSourcesDue(ctx, now)
}

func (s *Service) SetSourceNextRun(ctx context.Context, id string, next time.Time) error {
	return s.store.UpdateSourceNextRun(ctx, id, next)
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	return s.store.ListRuns(ctx, limit)
}

func (s *Service) GetRun(ctx context.Context, id string) (Run, error) {
	return s.store.GetRun(ctx, id)
}
