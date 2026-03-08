package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"iris/internal/assets"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/search"
	"iris/internal/web"
)

func NewRouter(engine search.Engine, assetDir string, crawlService *crawl.Service) http.Handler {
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
	indexer := indexing.NewPipeline(engine, assetStore)
	h := NewHandler(engine, indexer, crawlService)
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
	r.Post("/admin/sources", h.CreateSource)
	r.Post("/admin/sources/{id}/run", h.TriggerSourceRun)
	r.Get("/admin/runs", h.ListRuns)
	r.Get("/admin/runs/{id}", h.GetRun)
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetStore.Dir()))))

	return r
}

func NewCrawlService(jobBackend, jobStoreDSN string) (*crawl.Service, func(), error) {
	var (
		jobStore   jobs.Store
		crawlStore crawl.Store
		err        error
	)
	switch jobBackend {
	case "memory":
		jobStore = jobs.NewMemoryStore()
		crawlStore = crawl.NewMemoryStore()
	case "postgres":
		jobStore, err = jobs.NewPostgresStore(context.Background(), jobStoreDSN)
		if err != nil {
			return nil, nil, err
		}
		crawlStore, err = crawl.NewPostgresStore(context.Background(), jobStoreDSN)
		if err != nil {
			jobStore.Close()
			return nil, nil, err
		}
	default:
		return nil, nil, nil
	}

	cleanup := func() {
		jobStore.Close()
		crawlStore.Close()
	}
	return crawl.NewService(crawlStore, jobStore), cleanup, nil
}
