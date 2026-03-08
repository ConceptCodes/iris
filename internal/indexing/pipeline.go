package indexing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"iris/internal/assets"
	"iris/internal/constants"
	"iris/internal/metrics"
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
	engine     Engine
	assetStore assets.Store
	options    PipelineOptions
}

type PipelineOptions struct {
	MaxFetchBytes int
	FetchClient   *http.Client
}

func NewPipeline(engine Engine, assetStore assets.Store) *Pipeline {
	return &Pipeline{
		engine:     engine,
		assetStore: assetStore,
		options: PipelineOptions{
			MaxFetchBytes: constants.MaxImageSize,
			FetchClient:   &http.Client{Timeout: constants.HTTPTimeout30s},
		},
	}
}

func NewPipelineWithOptions(engine Engine, assetStore assets.Store, options PipelineOptions) *Pipeline {
	if options.MaxFetchBytes <= 0 {
		options.MaxFetchBytes = constants.MaxImageSize
	}
	if options.FetchClient == nil {
		options.FetchClient = &http.Client{Timeout: constants.HTTPTimeout30s}
	}
	return &Pipeline{
		engine:     engine,
		assetStore: assetStore,
		options:    options,
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
	imageBytes, mimeType, err := fetchImageBytes(ctx, req.URL, p.options.FetchClient, p.options.MaxFetchBytes)
	if err != nil {
		return Result{}, err
	}
	record := models.ImageRecord{
		ID:       uuid.New().String(),
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
	imageBytes, mimeType, err := fetchImageBytes(ctx, imageURL, p.options.FetchClient, p.options.MaxFetchBytes)
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
	if p.assetStore != nil {
		assetURL, err := p.assetStore.Save(record.ID, record.Filename, imageBytes)
		if err != nil {
			return Result{}, fmt.Errorf("store image asset: %w", err)
		}
		record.URL = assetURL
	}
	if record.Meta[constants.MetaKeySourceURL] == "" && record.URL != "" {
		record.Meta[constants.MetaKeySourceURL] = record.URL
	}
	if force {
		id, err := p.engine.ReindexFromBytes(ctx, imageBytes, record)
		if err != nil {
			return Result{}, err
		}
		return Result{ID: id, Status: ResultStatusReindexed}, nil
	}
	id, err := p.engine.IndexFromBytes(ctx, imageBytes, record)
	if err != nil {
		return Result{}, err
	}
	return Result{ID: id, Status: ResultStatusIndexed}, nil
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

func fetchImageBytes(ctx context.Context, rawURL string, client *http.Client, maxBytes int) ([]byte, string, error) {
	if client == nil {
		client = &http.Client{Timeout: constants.HTTPTimeout30s}
	}
	if maxBytes <= 0 {
		maxBytes = constants.MaxImageSize
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
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
