package indexing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"iris/internal/assets"
	"iris/internal/constants"
	"iris/internal/metrics"
	"iris/internal/ssrf"
	"iris/pkg/models"

	"github.com/disintegration/imaging"
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
	imageBytes, mimeType, err := fetchImageBytes(ctx, req.URL, p.options.FetchClient, p.options.MaxFetchBytes, p.options.UserAgent, p.options.SSRFAllowPrivateNetworks)
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
	if mimeType != "" {
		record.Meta[constants.MetaKeyMIMEType] = mimeType
	}
	return p.indexBytes(ctx, imageBytes, record, false)
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
	imageBytes, mimeType, err := fetchImageBytes(ctx, imageURL, p.options.FetchClient, p.options.MaxFetchBytes, p.options.UserAgent, p.options.SSRFAllowPrivateNetworks)
	if err != nil {
		return Result{}, err
	}
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	record.Meta[constants.MetaKeyOriginURL] = imageURL
	if mimeType != "" {
		record.Meta[constants.MetaKeyMIMEType] = mimeType
	}
	return p.indexBytes(ctx, imageBytes, record, true)
}

func (p *Pipeline) indexBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord, force bool) (Result, error) {
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	if _, ok := record.Meta[constants.MetaKeyContentSHA256]; !ok {
		record.Meta[constants.MetaKeyContentSHA256] = hashBytes(imageBytes)
	}
	if record.Meta[constants.MetaKeySourceURL] == "" {
		if original, ok := record.Meta[constants.MetaKeyOriginURL]; ok && original != "" {
			record.Meta[constants.MetaKeySourceURL] = original
		} else if record.URL != "" {
			record.Meta[constants.MetaKeySourceURL] = record.URL
		}
	}
	// Extract source_domain from source_url if not already set
	if record.Meta[constants.MetaKeySourceDomain] == "" && record.Meta[constants.MetaKeySourceURL] != "" {
		if u, err := url.Parse(record.Meta[constants.MetaKeySourceURL]); err == nil {
			record.Meta[constants.MetaKeySourceDomain] = u.Hostname()
		}
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
	if p.options.AssetStore != nil && p.options.ThumbnailWidth > 0 {
		thumbBytes, err := p.generateThumbnail(imageBytes)
		if err != nil {
			// Non-fatal: log and continue without a thumbnail
			fmt.Fprintf(os.Stderr, "failed to generate thumbnail for %s: %v\n", record.ID, err)
		} else {
			thumbURL, err := p.options.AssetStore.Save(record.ID+"_thumb", record.Filename, thumbBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to save thumbnail for %s: %v\n", record.ID, err)
			} else {
				record.ThumbnailURL = thumbURL
			}
		}
	}
	var id string
	var err error
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
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	thumb := imaging.Resize(img, p.options.ThumbnailWidth, p.options.ThumbnailHeight, imaging.Lanczos)
	return p.encodeThumbnail(thumb)
}

func (p *Pipeline) encodeThumbnail(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(p.options.ThumbnailQuality)); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return buf.Bytes(), nil
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
	if client == nil {
		client = &http.Client{Timeout: constants.HTTPTimeout30s}
	}
	if maxBytes <= 0 {
		maxBytes = constants.MaxImageSize
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = constants.DefaultCrawlerUserAgent
	}

	validator := ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(ssrfAllowPrivateNetworks))
	if err := validator.ValidateURL(ctx, rawURL); err != nil {
		return nil, "", fmt.Errorf("SSRF blocked: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(constants.HeaderUserAgent, userAgent)

	safeClient := validator.NewSafeClient(constants.HTTPTimeout30s)

	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch image url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch image url: status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get(constants.HeaderContentType))
	if contentType != "" && !strings.HasPrefix(contentType, constants.MIMETypeImagePrefix) {
		return nil, "", fmt.Errorf("unsupported content type: %s", contentType)
	}
	limited := io.LimitReader(resp.Body, int64(maxBytes+1))
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("read image bytes: %w", err)
	}
	if len(buf) > maxBytes {
		return nil, "", fmt.Errorf("image exceeds %d bytes limit", maxBytes)
	}
	if contentType == "" {
		detected := http.DetectContentType(buf)
		if !strings.HasPrefix(detected, constants.MIMETypeImagePrefix) {
			return nil, "", fmt.Errorf("unsupported content type: %s", detected)
		}
		contentType = detected
	}
	return buf, contentType, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
