package crawl

import "context"

type Store interface {
	CreateSource(ctx context.Context, input CreateSourceInput) (Source, error)
	GetSource(ctx context.Context, id string) (Source, error)
	CreateRun(ctx context.Context, sourceID, trigger string) (Run, error)
	ListRuns(ctx context.Context) ([]Run, error)
	GetRun(ctx context.Context, id string) (Run, error)
	MarkRunCompleted(ctx context.Context, id string, discovered, indexed, failed int) error
	MarkRunFailed(ctx context.Context, id, message string, discovered, indexed, failed int) error
	Close() error
}
