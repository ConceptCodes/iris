package models

type Embedding []float32

type ImageRecord struct {
	ID       string            `json:"id"`
	URL      string            `json:"url"`
	Filename string            `json:"filename,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

type SearchResult struct {
	Record ImageRecord `json:"record"`
	Score  float32     `json:"score"`
}

type IndexRequest struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

type TextSearchRequest struct {
	Query   string            `json:"query"`
	TopK    int               `json:"top_k,omitempty"`
	Filters map[string]string `json:"filters,omitempty"`
}

type TextSearchResponse struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query"`
	TookMs  int64          `json:"took_ms"`
}

type ImageSearchResponse struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query,omitempty"`
	TookMs  int64          `json:"took_ms"`
}

type IndexResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CrawlSourceRequest struct {
	Kind            string   `json:"kind"`
	SeedURL         string   `json:"seed_url,omitempty"`
	LocalPath       string   `json:"local_path,omitempty"`
	MaxDepth        int      `json:"max_depth,omitempty"`
	RateLimitRPS    int      `json:"rate_limit_rps,omitempty"`
	MaxPagesPerRun  int      `json:"max_pages_per_run,omitempty"`
	MaxImagesPerRun int      `json:"max_images_per_run,omitempty"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	ScheduleEvery   string   `json:"schedule_every,omitempty"`
}

type CrawlSourceResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type LocalIndexRequest struct {
	Path string `json:"path"`
}

type LocalIndexResponse struct {
	SourceID string `json:"source_id"`
	RunID    string `json:"run_id"`
	Status   string `json:"status"`
}

type TriggerRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type ReindexRequest struct {
	SourceID string `json:"source_id,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

type ReindexResponse struct {
	EnqueuedCount int      `json:"enqueued_count"`
	Errors        []string `json:"errors,omitempty"`
}
