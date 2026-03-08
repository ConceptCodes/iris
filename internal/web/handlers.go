package web

import (
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"iris/internal/search"
	"iris/pkg/models"
	"iris/web/templates"
)

type Handlers struct {
	engine search.Engine
}

func NewHandlers(engine search.Engine) *Handlers {
	return &Handlers{engine: engine}
}

func (h *Handlers) LandingPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.LandingPage()).ServeHTTP(w, r)
}

func (h *Handlers) SearchResults(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	filterType := r.URL.Query().Get("type")
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}

	topK := 40
	filters := map[string]string{}
	if filterType != "" {
		filters["meta_type"] = filterType
	}

	results, err := h.engine.SearchByText(r.Context(), models.TextSearchRequest{
		Query:   query,
		TopK:    topK,
		Filters: filters,
	})
	if err != nil {
		if r.Header.Get("HX-Request") == "true" {
			templ.Handler(templates.EmptyState(query, "error")).ServeHTTP(w, r)
			return
		}
		templ.Handler(templates.ErrorPage(query, filterType)).ServeHTTP(w, r)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
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
	results, err := h.engine.GetSimilar(r.Context(), id, 2)
	if err != nil || len(results) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	templ.Handler(templates.DetailPanel(results[0])).ServeHTTP(w, r)
}

func (h *Handlers) RelatedImages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	results, err := h.engine.GetSimilar(r.Context(), id, 10)
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
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	buf := make([]byte, header.Size)
	if _, err := file.Read(buf); err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}
	results, err := h.engine.SearchByImageBytes(r.Context(), buf, 40, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		templ.Handler(templates.ResultsGrid(results, "", 1)).ServeHTTP(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) ReverseImageSearchURL(w http.ResponseWriter, r *http.Request) {
	url := r.FormValue("url")
	if url == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	results, err := h.engine.SearchByImageURL(r.Context(), url, 40, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		templ.Handler(templates.ResultsGrid(results, url, 1)).ServeHTTP(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
