package api

import (
	"net/http"
	"time"

	"iris/internal/assets"
	"iris/internal/search"
	"iris/internal/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewRouter(engine search.Engine, assetDir string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	assetStore := assets.NewStore(assetDir)
	h := NewHandler(engine, assetStore)
	wh := web.NewHandlers(engine)

	r.Get("/health", h.Health)

	r.Get("/", wh.LandingPage)
	r.Get("/search", wh.SearchResults)
	r.Get("/image/{id}", wh.ImageDetail)
	r.Get("/image/{id}/related", wh.RelatedImages)
	r.Post("/search/reverse", wh.ReverseImageSearch)
	r.Post("/search/reverse/url", wh.ReverseImageSearchURL)

	r.Post("/index/url", h.IndexFromURL)
	r.Post("/index/upload", h.IndexFromUpload)
	r.Post("/search/text", h.SearchText)
	r.Post("/search/image", h.SearchImage)
	r.Post("/search/image/url", h.SearchImageURL)
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetStore.Dir()))))

	return r
}
