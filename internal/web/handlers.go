// Package web provides HTTP handlers for the search UI and web interface.
// It serves landing pages, search results, image detail views, and
// related image queries with template rendering.
package web

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"iris/internal/constants"
	"iris/internal/search"
	"iris/internal/ssrf"
	"iris/pkg/models"
	"iris/web/templates"
)

type Handlers struct {
	engine search.Engine
}

var supportedUploadMIMETypes = map[string]struct{}{
	constants.MIMETypeJPEG: {},
	constants.MIMETypePNG:  {},
	constants.MIMETypeWEBP: {},
	constants.MIMETypeGIF:  {},
	constants.MIMETypeBMP:  {},
	constants.MIMETypeTIFF: {},
}

func NewHandlers(engine search.Engine) *Handlers {
	return &Handlers{engine: engine}
}

func (h *Handlers) LandingPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.LandingPage()).ServeHTTP(w, r)
}

func (h *Handlers) SearchResults(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	filterType := r.URL.Query().Get("type")
	encoder := models.NormalizeEncoder(models.Encoder(r.URL.Query().Get("encoder")))
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}

	topK := constants.DefaultLimit40
	filters := map[string]string{}
	if filterType != "" {
		filters[constants.PayloadFieldMetaPrefix+constants.MetaKeyContentType] = filterType
	}

	results, err := h.engine.SearchByText(r.Context(), models.TextSearchRequest{
		Query:   query,
		TopK:    topK,
		Filters: filters,
		Encoder: encoder,
	})
	if err != nil {
		if r.Header.Get(constants.HeaderHXRequest) == "true" {
			templ.Handler(templates.EmptyState(query, "error")).ServeHTTP(w, r)
			return
		}
		templ.Handler(templates.ErrorPage(query, filterType)).ServeHTTP(w, r)
		return
	}

	if r.Header.Get(constants.HeaderHXRequest) == "true" {
		for i := range results {
			templ.Handler(templates.ImageCard(results[i], (page-1)*topK+i)).ServeHTTP(w, r)
		}
		return
	}

	if len(results) == 0 {
		templ.Handler(templates.NoResultsPage(query, filterType)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SearchResultsPage(results, query, filterType, page)).ServeHTTP(w, r)
}

func (h *Handlers) ImageDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	encoder := models.NormalizeEncoder(models.Encoder(r.URL.Query().Get("encoder")))
	results, err := h.engine.GetSimilar(r.Context(), id, 2, encoder)
	if err != nil || len(results) == 0 {
		http.Error(w, constants.MessageNotFound, http.StatusNotFound)
		return
	}
	templ.Handler(templates.DetailPanel(results[0])).ServeHTTP(w, r)
}

func (h *Handlers) RelatedImages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	encoder := models.NormalizeEncoder(models.Encoder(r.URL.Query().Get("encoder")))
	results, err := h.engine.GetSimilar(r.Context(), id, 10, encoder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(results) > 0 {
		results = results[1:]
	}
	if len(results) > 9 {
		results = results[:9]
	}
	for i := range results {
		templ.Handler(templates.ImageCard(results[i], i)).ServeHTTP(w, r)
	}
}

func (h *Handlers) ReverseImageSearch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(constants.MaxImageSize); err != nil {
		http.Error(w, constants.MessageFileTooLarge, http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, constants.MessageImageRequired, http.StatusBadRequest)
		return
	}
	defer file.Close()

	contentType := header.Header.Get(constants.HeaderContentType)
	if contentType == "" {
		sniff := make([]byte, 512)
		n, _ := io.ReadFull(file, sniff)
		contentType = http.DetectContentType(sniff[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.Error(w, constants.MsgFailedToReadFile, http.StatusInternalServerError)
			return
		}
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if _, ok := supportedUploadMIMETypes[contentType]; !ok {
		http.Error(w, "unsupported image type: use JPG, PNG, WebP, GIF, BMP, or TIFF", http.StatusBadRequest)
		return
	}

	buf, err := io.ReadAll(io.LimitReader(file, constants.MaxImageSize+1))
	if err != nil {
		http.Error(w, constants.MsgFailedToReadFile, http.StatusInternalServerError)
		return
	}
	if len(buf) > constants.MaxImageSize {
		http.Error(w, constants.MessageFileTooLarge, http.StatusBadRequest)
		return
	}
	if len(buf) == 0 {
		http.Error(w, constants.MsgFailedToReadFile, http.StatusInternalServerError)
		return
	}
	encoder := models.NormalizeEncoder(models.Encoder(r.FormValue("encoder")))
	results, err := h.engine.SearchByImageBytes(r.Context(), buf, constants.DefaultLimit40, nil, encoder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get(constants.HeaderHXRequest) == "true" {
		templ.Handler(templates.ResultsGrid(results, "", 1)).ServeHTTP(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) ReverseImageSearchURL(w http.ResponseWriter, r *http.Request) {
	imageURL := r.FormValue("url")
	encoder := models.NormalizeEncoder(models.Encoder(r.FormValue("encoder")))
	if imageURL == "" {
		http.Error(w, constants.ErrorMsgURLRequired, http.StatusBadRequest)
		return
	}

	validator := ssrf.NewValidator()
	if err := validator.ValidateURL(r.Context(), imageURL); err != nil {
		http.Error(w, "SSRF blocked: "+err.Error(), http.StatusBadRequest)
		return
	}

	results, err := h.engine.SearchByImageURL(r.Context(), imageURL, constants.DefaultLimit40, nil, encoder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get(constants.HeaderHXRequest) == "true" {
		templ.Handler(templates.ResultsGrid(results, imageURL, 1)).ServeHTTP(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
