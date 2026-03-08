package jobs

import (
	"context"
	"encoding/json"
	"time"
)

type Type string

const (
	TypeDiscoverSource Type = "discover_source"
	TypeDiscoverPage   Type = "discover_page"
	TypeFetchImage     Type = "fetch_image"
	TypeIndexLocalFile Type = "index_local_file"
	TypeReindexImage   Type = "reindex_image"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusLeased     Status = "leased"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusDeadLetter Status = "dead_letter"
)

type Job struct {
	ID          string
	Type        Type
	Status      Status
	PayloadJSON json.RawMessage
	Attempts    int
	MaxAttempts int
	AvailableAt time.Time
	LeasedUntil time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store interface {
	Enqueue(ctx context.Context, job Job) (Job, error)
	LeaseNext(ctx context.Context, now time.Time, leaseDuration time.Duration, allowedTypes ...Type) (Job, bool, error)
	MarkSucceeded(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, err error, retryAt time.Time) error
	Close() error
}

type FetchImagePayload struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

type IndexLocalFilePayload struct {
	Path string `json:"path"`
}

type DiscoverSourcePayload struct {
	SourceID string `json:"source_id"`
	RunID    string `json:"run_id"`
}
