// Package api provides HTTP handlers for image search and indexing endpoints.
// It handles text and reverse image search, image indexing from URLs and uploads,
// crawl source management, and metrics collection with integrated monitoring.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"iris/internal/constants"
	"iris/internal/crawl"
	"iris/internal/httputil"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metrics"
	"iris/internal/search"
	"iris/pkg/models"
)

type Handler struct {
	engine       SearchService
	indexer      IngestionService
	crawlService CrawlAdminService
	jobStore     jobs.Store
	metrics      *metrics.Counters
}

type SearchService interface {
	SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error)
	SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error)
	SearchByImageURL(ctx context.Context, url string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error)
	ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error)
}

type IngestionService interface {
	IndexFromURLResult(ctx context.Context, req models.IndexRequest) (indexing.Result, error)
	IndexUploadedBytesResult(ctx context.Context, imageBytes []byte, filename string, tags []string, meta map[string]string) (indexing.Result, error)
}

type CrawlAdminService interface {
	CreateSource(ctx context.Context, input crawl.CreateSourceInput) (crawl.Source, error)
	TriggerRun(ctx context.Context, sourceID, trigger string) (crawl.Run, error)
	ListRuns(ctx context.Context, limit int) ([]crawl.Run, error)
	GetRun(ctx context.Context, id string) (crawl.Run, error)
}

const maxJSONBodyBytes = 1 << 20
const defaultRunsLimit = 100

func NewHandler(engine search.Engine, indexer *indexing.Pipeline, crawlService *crawl.Service, jobStore jobs.Store, metrics *metrics.Counters) *Handler {
	var ingestion IngestionService
	if indexer != nil {
		ingestion = indexer
	}
	var crawlAdmin CrawlAdminService
	if crawlService != nil {
		crawlAdmin = crawlService
	}
	return &Handler{engine: engine, indexer: ingestion, crawlService: crawlAdmin, jobStore: jobStore, metrics: metrics}
}

func NewHandlerWithServices(engine SearchService, indexer IngestionService, crawlService CrawlAdminService, jobStore jobs.Store, metrics *metrics.Counters) *Handler {
	return &Handler{engine: engine, indexer: indexer, crawlService: crawlService, jobStore: jobStore, metrics: metrics}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) IndexFromURL(w http.ResponseWriter, r *http.Request) {
	if h.metrics != nil {
		h.metrics.IncIndexRequest()
	}
	metrics.IncIndexRequestPrometheus("url")
	var req models.IndexRequest
	if err := httputil.DecodeJSONBody(r, &req); err != nil {
		if h.metrics != nil {
			h.metrics.IncIndexError()
		}
		metrics.IncIndexErrorPrometheus("url")
		writeError(w, http.StatusBadRequest, constants.MessageInvalidJSON)
		return
	}
	if req.URL == "" {
		if h.metrics != nil {
			h.metrics.IncIndexError()
		}
		metrics.IncIndexErrorPrometheus("url")
		writeError(w, http.StatusBadRequest, constants.MessageURLRequired)
		return
	}
	start := time.Now()
	result, err := h.indexer.IndexFromURLResult(r.Context(), req)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncIndexError()
		}
		metrics.IncIndexErrorPrometheus("url")
		h.writeInternalError(w, "index from url failed", err)
		return
	}
	metrics.ObserveIndexLatency("url", time.Since(start))
	writeJSON(w, http.StatusOK, models.IndexResponse{ID: result.ID, Message: string(result.Status)})
}

func (h *Handler) IndexFromUpload(w http.ResponseWriter, r *http.Request) {
	if h.metrics != nil {
		h.metrics.IncIndexRequest()
	}
	metrics.IncIndexRequestPrometheus("upload")
	upload, err := httputil.ParseMultipartImage(r, constants.MaxImageSize)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncIndexError()
		}
		metrics.IncIndexErrorPrometheus("upload")
		if httpErr, ok := err.(*httputil.HTTPError); ok {
			writeError(w, httpErr.Status, httpErr.Message)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	tags := httputil.ParseUploadTags(r)
	meta := httputil.ParseUploadMeta(r)
	start := time.Now()
	result, err := h.indexer.IndexUploadedBytesResult(r.Context(), upload.Bytes, upload.Filename, tags, meta)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncIndexError()
		}
		metrics.IncIndexErrorPrometheus("upload")
		h.writeInternalError(w, "index from upload failed", err)
		return
	}
	metrics.ObserveIndexLatency("upload", time.Since(start))
	writeJSON(w, http.StatusOK, models.IndexResponse{ID: result.ID, Message: string(result.Status)})
}

func (h *Handler) SearchText(w http.ResponseWriter, r *http.Request) {
	if h.metrics != nil {
		h.metrics.IncSearchRequest()
	}
	metrics.IncSearchRequestPrometheus("text")
	var req models.TextSearchRequest
	if err := httputil.DecodeJSONBody(r, &req); err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("text")
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Query == "" {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("text")
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	start := time.Now()
	results, err := h.engine.SearchByText(r.Context(), req)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("text")
		h.writeInternalError(w, "search by text failed", err)
		return
	}
	metrics.ObserveSearchLatency("text", time.Since(start))
	writeJSON(w, http.StatusOK, models.TextSearchResponse{
		Results: results,
		Query:   req.Query,
		TookMs:  time.Since(start).Milliseconds(),
		Encoder: models.NormalizeEncoder(req.Encoder),
	})
}

func (h *Handler) SearchImage(w http.ResponseWriter, r *http.Request) {
	if h.metrics != nil {
		h.metrics.IncSearchRequest()
	}
	metrics.IncSearchRequestPrometheus("image_upload")
	input, err := httputil.ParseSearchImageInput(r)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("image_upload")
		if httpErr, ok := err.(*httputil.HTTPError); ok {
			writeError(w, httpErr.Status, httpErr.Message)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	start := time.Now()
	results, err := h.engine.SearchByImageBytes(r.Context(), input.ImageBytes, input.TopK, input.Filters, input.Encoder)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("image_upload")
		h.writeInternalError(w, "search by image failed", err)
		return
	}
	metrics.ObserveSearchLatency("image_upload", time.Since(start))
	writeJSON(w, http.StatusOK, models.ImageSearchResponse{
		Results: results,
		TookMs:  time.Since(start).Milliseconds(),
		Encoder: input.Encoder,
	})
}

func (h *Handler) SearchImageURL(w http.ResponseWriter, r *http.Request) {
	if h.metrics != nil {
		h.metrics.IncSearchRequest()
	}
	metrics.IncSearchRequestPrometheus("image_url")
	input, err := httputil.ParseSearchImageURLInput(r)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("image_url")
		if httpErr, ok := err.(*httputil.HTTPError); ok {
			writeError(w, httpErr.Status, httpErr.Message)
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	start := time.Now()
	results, err := h.engine.SearchByImageURL(r.Context(), input.URL, input.TopK, input.Filters, input.Encoder)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncSearchError()
		}
		metrics.IncSearchErrorPrometheus("image_url")
		h.writeInternalError(w, "search by image url failed", err)
		return
	}
	metrics.ObserveSearchLatency("image_url", time.Since(start))
	writeJSON(w, http.StatusOK, models.ImageSearchResponse{
		Results: results,
		Query:   input.URL,
		TookMs:  time.Since(start).Milliseconds(),
		Encoder: input.Encoder,
	})
}

func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	if h.crawlService == nil {
		writeError(w, http.StatusNotImplemented, constants.MessageCrawlServiceUnavailable)
		return
	}
	var req models.CrawlSourceRequest
	if err := httputil.DecodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	source, err := h.crawlService.CreateSource(r.Context(), crawl.CreateSourceInput{
		Kind:            crawl.SourceKind(req.Kind),
		SeedURL:         req.SeedURL,
		LocalPath:       req.LocalPath,
		MaxDepth:        req.MaxDepth,
		RateLimitRPS:    req.RateLimitRPS,
		MaxPagesPerRun:  req.MaxPagesPerRun,
		MaxImagesPerRun: req.MaxImagesPerRun,
		AllowedDomains:  req.AllowedDomains,
		ScheduleEvery:   req.ScheduleEvery,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.CrawlSourceResponse{ID: source.ID, Status: string(source.Status)})
}

func (h *Handler) TriggerSourceRun(w http.ResponseWriter, r *http.Request) {
	if h.crawlService == nil {
		writeError(w, http.StatusNotImplemented, constants.MessageCrawlServiceUnavailable)
		return
	}
	var req crawl.TriggerRunInput
	_ = httputil.DecodeJSONBody(r, &req)
	run, err := h.crawlService.TriggerRun(r.Context(), r.PathValue("id"), req.Trigger)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.metrics != nil {
		h.metrics.IncCrawlRunsQueued()
	}
	metrics.IncCrawlRunsQueuedPrometheus()
	writeJSON(w, http.StatusOK, models.TriggerRunResponse{RunID: run.ID, Status: string(run.Status)})
}

func (h *Handler) EnqueueLocalIndex(w http.ResponseWriter, r *http.Request) {
	if h.crawlService == nil {
		writeError(w, http.StatusNotImplemented, "crawl service unavailable")
		return
	}
	var req models.LocalIndexRequest
	if err := httputil.DecodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	source, err := h.crawlService.CreateSource(r.Context(), crawl.CreateSourceInput{
		Kind:      crawl.SourceKindLocalDir,
		LocalPath: req.Path,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	run, err := h.crawlService.TriggerRun(r.Context(), source.ID, constants.TriggerManual)
	if err != nil {
		h.writeInternalError(w, "enqueue local index trigger failed", err)
		return
	}
	if h.metrics != nil {
		h.metrics.IncCrawlRunsQueued()
	}
	metrics.IncCrawlRunsQueuedPrometheus()
	writeJSON(w, http.StatusOK, models.LocalIndexResponse{
		SourceID: source.ID,
		RunID:    run.ID,
		Status:   string(run.Status),
	})
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	if h.crawlService == nil {
		writeError(w, http.StatusNotImplemented, "crawl service unavailable")
		return
	}
	limit := defaultRunsLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit = httputil.ParseIntFormValue(raw, defaultRunsLimit)
		if limit <= 0 {
			limit = defaultRunsLimit
		}
		if limit > 500 {
			limit = 500
		}
	}
	runs, err := h.crawlService.ListRuns(r.Context(), limit)
	if err != nil {
		h.writeInternalError(w, "list runs failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	if h.crawlService == nil {
		writeError(w, http.StatusNotImplemented, constants.MessageCrawlServiceUnavailable)
		return
	}
	run, err := h.crawlService.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, constants.MessageNotFound)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	if h.metrics == nil {
		writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics.Snapshot{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": h.metrics.Snapshot()})
}

func (h *Handler) HandleReindex(w http.ResponseWriter, r *http.Request) {
	if h.jobStore == nil {
		writeError(w, http.StatusServiceUnavailable, constants.MessageJobStoreUnavailable)
		return
	}
	var req models.ReindexRequest
	if err := httputil.DecodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, constants.MessageInvalidJSON)
		return
	}

	filters := make(map[string]string)
	if req.SourceID != "" {
		filters[constants.MetaKeySourceID] = req.SourceID
	}
	if req.RunID != "" {
		filters[constants.MetaKeyRunID] = req.RunID
	}

	limit := uint32(constants.DefaultLimit100)
	if req.Limit > 0 {
		limit = uint32(req.Limit)
	}
	offset := uint32(0)
	if req.Offset > 0 {
		offset = uint32(req.Offset)
	}

	images, err := h.engine.ListImages(r.Context(), filters, limit, offset)
	if err != nil {
		h.writeInternalError(w, "list images for reindex failed", err)
		return
	}

	errors := []string{}
	enqueuedCount := 0
	for _, image := range images {
		sourceURL := image.URL
		if image.Meta != nil {
			if s := image.Meta[constants.MetaKeyOriginURL]; s != "" {
				sourceURL = s
			} else if s := image.Meta[constants.MetaKeySourceURL]; s != "" {
				sourceURL = s
			}
		}
		if sourceURL == "" {
			errors = append(errors, fmt.Sprintf("no source URL for image %s", image.ID))
			continue
		}

		payload, err := json.Marshal(jobs.ReindexImagePayload{
			ID:  image.ID,
			URL: sourceURL,
		})
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to marshal payload for %s: %v", image.ID, err))
			continue
		}

		if _, err := h.jobStore.Enqueue(r.Context(), jobs.Job{
			Type:        jobs.TypeReindexImage,
			PayloadJSON: payload,
		}); err != nil {
			errors = append(errors, fmt.Sprintf("failed to enqueue reindex job for %s: %v", image.ID, err))
			continue
		}
		enqueuedCount++
	}

	writeJSON(w, http.StatusOK, models.ReindexResponse{
		EnqueuedCount: enqueuedCount,
		Errors:        errors,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse{Error: message})
}

func (h *Handler) writeInternalError(w http.ResponseWriter, message string, err error) {
	slog.Error(message, "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
