package crawl

import (
	"context"
	"encoding/json"
	"fmt"

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
	run, err := s.store.CreateRun(ctx, source.ID, trigger)
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

func (s *Service) ListRuns(ctx context.Context) ([]Run, error) {
	return s.store.ListRuns(ctx)
}

func (s *Service) GetRun(ctx context.Context, id string) (Run, error) {
	return s.store.GetRun(ctx, id)
}
