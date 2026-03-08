package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"iris/internal/indexing"
	"iris/internal/search"
	"iris/pkg/models"
)

const maxUploadSize = 20 << 20

type Handler struct {
	engine  search.Engine
	indexer *indexing.Pipeline
}

func NewHandler(engine search.Engine, indexer *indexing.Pipeline) *Handler {
	return &Handler{engine: engine, indexer: indexer}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) IndexFromURL(w http.ResponseWriter, r *http.Request) {
	var req models.IndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	id, err := h.indexer.IndexFromURL(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.IndexResponse{ID: id, Message: "indexed"})
}

func (h *Handler) IndexFromUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()
	buf := make([]byte, header.Size)
	if _, err := file.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	filename := r.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}
	var tags []string
	if t := r.FormValue("tags"); t != "" {
		tags = strings.Split(t, ",")
	}
	meta := make(map[string]string)
	for key, values := range r.MultipartForm.Value {
		if strings.HasPrefix(key, "meta_") {
			meta[strings.TrimPrefix(key, "meta_")] = values[0]
		}
	}
	id, err := h.indexer.IndexUploadedBytes(r.Context(), buf, filename, tags, meta)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.IndexResponse{ID: id, Message: "indexed"})
}

func (h *Handler) SearchText(w http.ResponseWriter, r *http.Request) {
	var req models.TextSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	start := time.Now()
	results, err := h.engine.SearchByText(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.TextSearchResponse{
		Results: results,
		Query:   req.Query,
		TookMs:  time.Since(start).Milliseconds(),
	})
}

func (h *Handler) SearchImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()
	buf := make([]byte, header.Size)
	if _, err := file.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	topK := parseIntFormValue(r.FormValue("top_k"), 0)
	filters := parseFilters(r.FormValue("filters"))
	start := time.Now()
	results, err := h.engine.SearchByImageBytes(r.Context(), buf, topK, filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ImageSearchResponse{
		Results: results,
		TookMs:  time.Since(start).Milliseconds(),
	})
}

func (h *Handler) SearchImageURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string            `json:"url"`
		TopK    int               `json:"top_k"`
		Filters map[string]string `json:"filters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	start := time.Now()
	results, err := h.engine.SearchByImageURL(r.Context(), req.URL, req.TopK, req.Filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ImageSearchResponse{
		Results: results,
		Query:   req.URL,
		TookMs:  time.Since(start).Milliseconds(),
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

func parseIntFormValue(s string, def int) int {
	if s == "" {
		return def
	}
	var v int
	if err := json.NewDecoder(strings.NewReader(s)).Decode(&v); err == nil {
		return v
	}
	return def
}

func parseFilters(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
