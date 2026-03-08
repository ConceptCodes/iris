package crawl

import "time"

type SourceKind string

const (
	SourceKindDomain   SourceKind = "domain"
	SourceKindSitemap  SourceKind = "sitemap"
	SourceKindURLList  SourceKind = "url_list"
	SourceKindLocalDir SourceKind = "local_dir"
)

type SourceStatus string

const (
	SourceStatusActive   SourceStatus = "active"
	SourceStatusPaused   SourceStatus = "paused"
	SourceStatusDisabled SourceStatus = "disabled"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

type Source struct {
	ID             string
	Kind           SourceKind
	SeedURL        string
	LocalPath      string
	Status         SourceStatus
	MaxDepth       int
	RateLimitRPS   int
	AllowedDomains []string
	CreatedAt      time.Time
}

type Run struct {
	ID              string
	SourceID        string
	Trigger         string
	Status          RunStatus
	DiscoveredCount int
	IndexedCount    int
	FailedCount     int
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateSourceInput struct {
	Kind           SourceKind `json:"kind"`
	SeedURL        string     `json:"seed_url,omitempty"`
	LocalPath      string     `json:"local_path,omitempty"`
	MaxDepth       int        `json:"max_depth,omitempty"`
	RateLimitRPS   int        `json:"rate_limit_rps,omitempty"`
	AllowedDomains []string   `json:"allowed_domains,omitempty"`
}

type TriggerRunInput struct {
	Trigger string `json:"trigger,omitempty"`
}
