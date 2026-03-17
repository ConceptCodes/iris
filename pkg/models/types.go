package models

import (
	"math"
	"strings"
)

type Embedding []float32
type Encoder string
type Embeddings map[Encoder]Embedding

const (
	EncoderCLIP    Encoder = "clip"
	EncoderSigLIP2 Encoder = "siglip2"
)

func NormalizeEncoder(value Encoder) Encoder {
	return Encoder(strings.ToLower(strings.TrimSpace(string(value))))
}

type ImageRecord struct {
	ID              string            `json:"id"`
	URL             string            `json:"url"`
	ThumbnailURL    string            `json:"thumbnail_url,omitempty"`
	Filename        string            `json:"filename,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Meta            map[string]string `json:"meta,omitempty"`
	ImageWidth      int               `json:"image_width,omitempty"`
	ImageHeight     int               `json:"image_height,omitempty"`
	FileSize        int64             `json:"file_size,omitempty"`
	ColorDepth      string            `json:"color_depth,omitempty"`
	QualityScore    float32           `json:"quality_score,omitempty"`
	DomainAuthority float32           `json:"domain_authority,omitempty"`
	IndexedAt       string            `json:"indexed_at,omitempty"`
	LastCrawled     string            `json:"last_crawled,omitempty"`
	OGTitle         string            `json:"og_title,omitempty"`
	OGDescription   string            `json:"og_description,omitempty"`
}

// QualityRank returns a normalized quality score (0.0-1.0) based on resolution and color depth.
// Higher resolution and more color channels result in higher quality scores.
func (r *ImageRecord) QualityRank() float32 {
	if r == nil {
		return 0.0
	}

	maxPixels := 8_294_400.0
	pixels := float64(r.ImageWidth * r.ImageHeight)

	resolutionScore := float32(0.0)
	if pixels > 0 {
		resolutionScore = float32(math.Log(pixels) / math.Log(maxPixels))
		if resolutionScore > 1.0 {
			resolutionScore = 1.0
		}
	}

	colorDepthScore := float32(0.5)
	switch r.ColorDepth {
	case "rgb":
		colorDepthScore = 0.8
	case "rgba":
		colorDepthScore = 1.0
	}

	qualityScore := resolutionScore*0.6 + colorDepthScore*0.4

	if qualityScore < 0.0 {
		qualityScore = 0.0
	}
	if qualityScore > 1.0 {
		qualityScore = 1.0
	}

	return qualityScore
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
	Encoder Encoder           `json:"encoder,omitempty"`
}

type TextSearchResponse struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query"`
	TookMs  int64          `json:"took_ms"`
	Encoder Encoder        `json:"encoder,omitempty"`
}

type ImageSearchResponse struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query,omitempty"`
	TookMs  int64          `json:"took_ms"`
	Encoder Encoder        `json:"encoder,omitempty"`
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
