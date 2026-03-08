package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"iris/internal/assets"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metrics"
	"iris/internal/search"
	"iris/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type AssetsSettings struct {
	Backend    string
	LocalDir   string
	Bucket     string
	Region     string
	Endpoint   string
	AccessKey  string
	SecretKey  string
	SessionKey string
	Prefix     string
	PublicBase string
	PathStyle  bool
}

func NewRouter(engine search.Engine, assetDir string, crawlService *crawl.Service, adminAPIKey string) http.Handler {
	return NewRouterWithAssets(engine, AssetsSettings{LocalDir: assetDir}, crawlService, adminAPIKey, nil)
}

func NewRouterWithAssets(engine search.Engine, assetsCfg AssetsSettings, crawlService *crawl.Service, adminAPIKey string, jobStore jobs.Store) http.Handler {
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

	assetStore, assetDir := buildAssetStore(assetsCfg)
	indexer := indexing.NewPipeline(engine, assetStore)
	if jobStore == nil {
		jobStore = jobs.NewMemoryStore()
	}
	metrics := metrics.NewCounters()
	h := NewHandler(engine, indexer, crawlService, jobStore, metrics)
	wh := web.NewHandlers(engine)

	if adminAPIKey != "" {
		r.With(requireAdminKey(adminAPIKey)).Post("/admin/sources", h.CreateSource)
		r.With(requireAdminKey(adminAPIKey)).Post("/admin/sources/{id}/run", h.TriggerSourceRun)
		r.With(requireAdminKey(adminAPIKey)).Post("/admin/index/local", h.EnqueueLocalIndex)
		r.With(requireAdminKey(adminAPIKey)).Post("/admin/reindex", h.HandleReindex)
		r.With(requireAdminKey(adminAPIKey)).Get("/admin/runs", h.ListRuns)
		r.With(requireAdminKey(adminAPIKey)).Get("/admin/runs/{id}", h.GetRun)
		r.With(requireAdminKey(adminAPIKey)).Get("/admin/metrics", h.Metrics)
	} else {
		r.Post("/admin/sources", adminDisabled)
		r.Post("/admin/sources/{id}/run", adminDisabled)
		r.Post("/admin/index/local", adminDisabled)
		r.Post("/admin/reindex", adminDisabled)
		r.Get("/admin/runs", adminDisabled)
		r.Get("/admin/runs/{id}", adminDisabled)
		r.Get("/admin/metrics", adminDisabled)
	}

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
	if assetDir != "" {
		r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetDir))))
	}

	return r
}

func buildAssetStore(cfg AssetsSettings) (assets.Store, string) {
	store, err := assets.NewStoreFromSettings(context.Background(), assets.Settings{
		Backend:  cfg.Backend,
		LocalDir: cfg.LocalDir,
		S3: assets.S3Config{
			Bucket:       cfg.Bucket,
			Region:       cfg.Region,
			Endpoint:     cfg.Endpoint,
			AccessKey:    cfg.AccessKey,
			SecretKey:    cfg.SecretKey,
			SessionToken: cfg.SessionKey,
			Prefix:       cfg.Prefix,
			PublicBase:   cfg.PublicBase,
			UsePathStyle: cfg.PathStyle,
		},
	})
	if err != nil {
		store = assets.NewStore(cfg.LocalDir)
	}
	if dir, ok := store.LocalDir(); ok {
		return store, dir
	}
	return store, ""
}

func NewCrawlService(jobBackend, jobStoreDSN string) (*crawl.Service, jobs.Store, func(), error) {
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
			return nil, nil, nil, err
		}
		crawlStore, err = crawl.NewPostgresStore(context.Background(), jobStoreDSN)
		if err != nil {
			jobStore.Close()
			return nil, nil, nil, err
		}
	default:
		return nil, nil, nil, nil
	}

	cleanup := func() {
		jobStore.Close()
		crawlStore.Close()
	}
	return crawl.NewService(crawlStore, jobStore), jobStore, cleanup, nil
}

func requireAdminKey(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Admin-Key")
			if key == "" {
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					key = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
				}
			}
			if key != expected {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminDisabled(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "admin api disabled", http.StatusServiceUnavailable)
}
