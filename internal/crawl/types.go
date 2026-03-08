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
	ID                  string
	Kind                SourceKind
	SeedURL             string
	LocalPath           string
	Status              SourceStatus
	MaxDepth            int
	RateLimitRPS        int
	MaxPagesPerRun      int
	MaxImagesPerRun     int
	AllowedDomains      []string
	ScheduleEvery       time.Duration
	NextRunAt           time.Time
	LastRunAt           time.Time
	LastSuccessAt       time.Time
	LastContentChangeAt time.Time
	ConsecutiveFailures int
	LastDiscoveredCount int
	LastIndexedCount    int
	LastDuplicateCount  int
	LastFailedCount     int
	CreatedAt           time.Time
}

type Run struct {
	ID              string
	SourceID        string
	Trigger         string
	Status          RunStatus
	DiscoveredCount int
	IndexedCount    int
	DuplicateCount  int
	FailedCount     int
	LastError       string
	ScheduledAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateSourceInput struct {
	Kind            SourceKind `json:"kind"`
	SeedURL         string     `json:"seed_url,omitempty"`
	LocalPath       string     `json:"local_path,omitempty"`
	MaxDepth        int        `json:"max_depth,omitempty"`
	RateLimitRPS    int        `json:"rate_limit_rps,omitempty"`
	MaxPagesPerRun  int        `json:"max_pages_per_run,omitempty"`
	MaxImagesPerRun int        `json:"max_images_per_run,omitempty"`
	AllowedDomains  []string   `json:"allowed_domains,omitempty"`
	ScheduleEvery   string     `json:"schedule_every,omitempty"`
}

type TriggerRunInput struct {
	Trigger string `json:"trigger,omitempty"`
}
