package crawl

import (
	"context"
	"time"
)

type Store interface {
	CreateSource(ctx context.Context, input CreateSourceInput) (Source, error)
	GetSource(ctx context.Context, id string) (Source, error)
	CreateRun(ctx context.Context, sourceID, trigger string, scheduledAt time.Time) (Run, error)
	ListRuns(ctx context.Context) ([]Run, error)
	GetRun(ctx context.Context, id string) (Run, error)
	SetRunDiscovered(ctx context.Context, id string, discovered int) error
	IncrementRunIndexed(ctx context.Context, id string, delta int) error
	IncrementRunDuplicate(ctx context.Context, id string, delta int) error
	IncrementRunFailed(ctx context.Context, id string, delta int, lastError string) error
	MarkRunCompleted(ctx context.Context, id string) error
	MarkRunFailed(ctx context.Context, id, message string) error
	ListSourcesDue(ctx context.Context, now time.Time) ([]Source, error)
	UpdateSourceNextRun(ctx context.Context, id string, next time.Time) error
	Close() error
}
