package jobs

import (
	"context"
	"encoding/json"
	"time"

	"iris/internal/constants"
)

type Type string

const (
	TypeDiscoverSource Type = Type(constants.JobTypeDiscoverSource)
	TypeFetchImage     Type = Type(constants.JobTypeFetchImage)
	TypeIndexLocalFile Type = Type(constants.JobTypeIndexLocalFile)
	TypeReindexImage   Type = Type(constants.JobTypeReindexImage)
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
	DedupKey    string
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
	MarkFailed(ctx context.Context, id string, err error, retryAt time.Time) (Status, error)
	Close() error
}

type FetchImagePayload struct {
	URL           string            `json:"url"`
	Filename      string            `json:"filename,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	RunID         string            `json:"run_id,omitempty"`
	SourceDomain  string            `json:"source_domain,omitempty"`
	MimeType      string            `json:"mime_type,omitempty"`
	PageURL       string            `json:"page_url,omitempty"`
	Title         string            `json:"title,omitempty"`
	CrawlSourceID string            `json:"crawl_source_id,omitempty"`
}

type IndexLocalFilePayload struct {
	Path  string `json:"path"`
	RunID string `json:"run_id,omitempty"`
}

type ReindexImagePayload struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}

type DiscoverSourcePayload struct {
	SourceID string `json:"source_id"`
	RunID    string `json:"run_id"`
}
