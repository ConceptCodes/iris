// Package indexing provides a pipeline for ingesting and processing images
// from URLs, uploads, and local files with metadata enrichment,
// quality scoring, deduplication, and thumbnail generation.
package indexing

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"iris/internal/assets"
	"iris/internal/constants"
	"iris/internal/metadata"
	"iris/internal/metrics"
	"iris/internal/indexing/stages"
	"iris/pkg/models"

	"github.com/google/uuid"
)

type Engine interface {
	IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error)
	IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error)
	FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error)
}

type ResultStatus string

const (
	ResultStatusIndexed   ResultStatus = "indexed"
	ResultStatusDuplicate ResultStatus = "duplicate"
	ResultStatusReindexed ResultStatus = "reindexed"
)

type Result struct {
	ID     string
	Status ResultStatus
}

type Pipeline struct {
	engine  Engine
	options PipelineOptions
}

type PipelineOptions struct {
	AssetStore               assets.Store
	Enricher                 metadata.Enricher
	MaxFetchBytes            int
	FetchClient              *http.Client
	UserAgent                string
	SSRFAllowPrivateNetworks bool
	ThumbnailWidth           int
	ThumbnailHeight          int
	ThumbnailQuality         int
}

func NewPipeline(engine Engine) *Pipeline {
	return &Pipeline{
		engine: engine,
		options: PipelineOptions{
			MaxFetchBytes:    constants.MaxImageSize,
			FetchClient:      &http.Client{Timeout: constants.HTTPTimeout30s},
			UserAgent:        constants.DefaultCrawlerUserAgent,
			ThumbnailWidth:   250,
			ThumbnailHeight:  0, // Preserve aspect ratio
			ThumbnailQuality: 80,
		},
	}
}

func NewPipelineWithOptions(engine Engine, options PipelineOptions) *Pipeline {
	if options.MaxFetchBytes <= 0 {
		options.MaxFetchBytes = constants.MaxImageSize
	}
	if options.FetchClient == nil {
		options.FetchClient = &http.Client{Timeout: constants.HTTPTimeout30s}
	}
	if strings.TrimSpace(options.UserAgent) == "" {
		options.UserAgent = constants.DefaultCrawlerUserAgent
	}
	if options.ThumbnailWidth <= 0 {
		options.ThumbnailWidth = 250
	}
	if options.ThumbnailQuality <= 0 {
		options.ThumbnailQuality = 80
	}
	return &Pipeline{
		engine:  engine,
		options: options,
	}
}

func (p *Pipeline) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	result, err := p.IndexFromURLResult(ctx, req)
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

func (p *Pipeline) IndexFromURLResult(ctx context.Context, req models.IndexRequest) (Result, error) {
	if req.URL == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	fetchResult, err := stages.FetchImageBytes(ctx, req.URL, p.fetchConfig())
	if err != nil {
		return Result{}, err
	}
	record := models.ImageRecord{
		ID:       uuid.New().String(),
		URL:      req.URL,
		Filename: req.Filename,
		Tags:     req.Tags,
		Meta:     cloneMeta(req.Meta),
	}
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	record.Meta[constants.MetaKeyOriginURL] = req.URL
	if fetchResult.MIMEType != "" {
		record.Meta[constants.MetaKeyMIMEType] = fetchResult.MIMEType
	}
	return p.indexBytes(ctx, fetchResult.Bytes, record, false)
}

func (p *Pipeline) IndexUploadedBytes(ctx context.Context, imageBytes []byte, filename string, tags []string, meta map[string]string) (string, error) {
	result, err := p.IndexUploadedBytesResult(ctx, imageBytes, filename, tags, meta)
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

func (p *Pipeline) IndexUploadedBytesResult(ctx context.Context, imageBytes []byte, filename string, tags []string, meta map[string]string) (Result, error) {
	record := models.ImageRecord{
		ID:       uuid.New().String(),
		Filename: filename,
		Tags:     tags,
		Meta:     cloneMeta(meta),
	}
	return p.indexBytes(ctx, imageBytes, record, false)
}

func (p *Pipeline) IndexLocalFile(ctx context.Context, path string) (string, error) {
	result, err := p.IndexLocalFileResult(ctx, path)
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

func (p *Pipeline) IndexLocalFileResult(ctx context.Context, path string) (Result, error) {
	imageBytes, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read local image: %w", err)
	}

	record := models.ImageRecord{
		ID:       uuid.New().String(),
		Filename: filepath.Base(path),
		Meta: map[string]string{
			constants.MetaKeySource:     constants.KeywordLocal,
			constants.MetaKeySourcePath: path,
		},
	}
	return p.indexBytes(ctx, imageBytes, record, false)
}

func (p *Pipeline) ReindexFromURL(ctx context.Context, imageURL string, record models.ImageRecord) (string, error) {
	result, err := p.ReindexFromURLResult(ctx, imageURL, record)
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

func (p *Pipeline) ReindexFromURLResult(ctx context.Context, imageURL string, record models.ImageRecord) (Result, error) {
	if imageURL == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	fetchResult, err := stages.FetchImageBytes(ctx, imageURL, p.fetchConfig())
	if err != nil {
		return Result{}, err
	}
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	record.Meta[constants.MetaKeyOriginURL] = imageURL
	if fetchResult.MIMEType != "" {
		record.Meta[constants.MetaKeyMIMEType] = fetchResult.MIMEType
	}
	return p.indexBytes(ctx, fetchResult.Bytes, record, true)
}

func (p *Pipeline) indexBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord, force bool) (Result, error) {
	var err error
	record, err = stages.PrepareRecord(ctx, imageBytes, record, force, p.options.Enricher)
	if err != nil {
		return Result{}, err
	}
	if !force {
		existing, ok, err := p.engine.FindExistingID(ctx, record.Meta, record.Meta[constants.MetaKeySourceURL])
		if err != nil {
			return Result{}, err
		}
		if ok {
			metrics.IncDedupeEvent(dedupeReason(record.Meta))
			return Result{ID: existing, Status: ResultStatusDuplicate}, nil
		}
	}
	record, err = stages.EnrichRecord(ctx, imageBytes, record, p.options.Enricher)
	if err != nil {
		return Result{}, err
	}
	record = stages.AnalyzeQuality(ctx, imageBytes, record)
	thumbURL, thumbErr := stages.SaveThumbnail(ctx, p.options.AssetStore, record.ID, record.Filename, imageBytes, p.recordConfig())
	if thumbErr != nil {
		fmt.Fprintf(os.Stderr, "failed to save thumbnail for %s: %v\n", record.ID, thumbErr)
	} else if thumbURL != "" {
		record.ThumbnailURL = thumbURL
	}
	var id string
	if force {
		id, err = p.engine.ReindexFromBytes(ctx, imageBytes, record)
		if err != nil {
			return Result{}, err
		}
	} else {
		id, err = p.engine.IndexFromBytes(ctx, imageBytes, record)
		if err != nil {
			return Result{}, err
		}
	}
	if force {
		return Result{ID: id, Status: ResultStatusReindexed}, nil
	}
	return Result{ID: id, Status: ResultStatusIndexed}, nil
}

func (p *Pipeline) generateThumbnail(data []byte) ([]byte, error) {
	return stages.GenerateThumbnail(data, p.options.ThumbnailWidth, p.options.ThumbnailHeight, p.options.ThumbnailQuality)
}

func (p *Pipeline) fetchConfig() stages.FetchConfig {
	return stages.FetchConfig{
		Client:                   p.options.FetchClient,
		MaxBytes:                 p.options.MaxFetchBytes,
		UserAgent:                p.options.UserAgent,
		SSRFAllowPrivateNetworks: p.options.SSRFAllowPrivateNetworks,
	}
}

func (p *Pipeline) recordConfig() stages.RecordConfig {
	return stages.RecordConfig{
		ThumbnailWidth:   p.options.ThumbnailWidth,
		ThumbnailHeight:  p.options.ThumbnailHeight,
		ThumbnailQuality: p.options.ThumbnailQuality,
		AssetStore:       p.options.AssetStore,
		Enricher:         p.options.Enricher,
	}
}

func dedupeReason(meta map[string]string) string {
	if meta == nil {
		return "unknown"
	}
	if meta[constants.MetaKeyContentSHA256] != "" {
		return constants.MetaKeyContentSHA256
	}
	if meta[constants.MetaKeySourceURL] != "" {
		return constants.MetaKeySourceURL
	}
	return "unknown"
}

func fetchImageBytes(ctx context.Context, rawURL string, client *http.Client, maxBytes int, userAgent string, ssrfAllowPrivateNetworks bool) ([]byte, string, error) {
	result, err := stages.FetchImageBytes(ctx, rawURL, stages.FetchConfig{
		Client:                   client,
		MaxBytes:                 maxBytes,
		UserAgent:                userAgent,
		SSRFAllowPrivateNetworks: ssrfAllowPrivateNetworks,
	})
	if err != nil {
		return nil, "", err
	}
	return result.Bytes, result.MIMEType, nil
}

func hashBytes(data []byte) string {
	return stages.HashBytes(data)
}

func cloneMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}
