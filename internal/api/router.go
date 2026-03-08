package api

import (
	"net/http"
	"time"

	"github.com/davidojo/google-images/internal/search"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewRouter(engine *search.Engine) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	h := NewHandler(engine)

	r.Get("/health", h.Health)
	r.Post("/index/url", h.IndexFromURL)
	r.Post("/index/upload", h.IndexFromUpload)
	r.Post("/search/text", h.SearchText)
	r.Post("/search/image", h.SearchImage)
	r.Post("/search/image/url", h.SearchImageURL)

	return r
}
