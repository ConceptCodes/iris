package api

import (
	"context"
	"net/http"
	"strings"

	"iris/internal/assets"
	"iris/internal/constants"
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
	r.Use(middleware.Timeout(constants.HTTPTimeout60s))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{constants.MethodGET, constants.MethodPOST, constants.MethodOPTIONS},
		AllowedHeaders:   []string{"Accept", constants.HeaderAuthorization, constants.HeaderContentType, constants.HeaderXCSRFToken},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           constants.CORSMaxAge,
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
		r.With(requireAdminKey(adminAPIKey)).Post(constants.PathAdminSources, h.CreateSource)
		r.With(requireAdminKey(adminAPIKey)).Post(constants.PathAdminSourceRun, h.TriggerSourceRun)
		r.With(requireAdminKey(adminAPIKey)).Post(constants.PathAdminIndexLocal, h.EnqueueLocalIndex)
		r.With(requireAdminKey(adminAPIKey)).Post(constants.PathAdminReindex, h.HandleReindex)
		r.With(requireAdminKey(adminAPIKey)).Get(constants.PathAdminRuns, h.ListRuns)
		r.With(requireAdminKey(adminAPIKey)).Get(constants.PathAdminRunDetail, h.GetRun)
		r.With(requireAdminKey(adminAPIKey)).Get(constants.PathAdminMetrics, h.Metrics)
	} else {
		r.Post(constants.PathAdminSources, adminDisabled)
		r.Post(constants.PathAdminSourceRun, adminDisabled)
		r.Post(constants.PathAdminIndexLocal, adminDisabled)
		r.Post(constants.PathAdminReindex, adminDisabled)
		r.Get(constants.PathAdminRuns, adminDisabled)
		r.Get(constants.PathAdminRunDetail, adminDisabled)
		r.Get(constants.PathAdminMetrics, adminDisabled)
	}

	r.Get(constants.PathHealth, h.Health)

	r.Get(constants.PathLanding, wh.LandingPage)
	r.Get(constants.PathSearch, wh.SearchResults)
	r.Get(constants.PathImage+"/{id}", wh.ImageDetail)
	r.Get(constants.PathImageRelated, wh.RelatedImages)
	r.Post(constants.PathSearchReverse, wh.ReverseImageSearch)
	r.Post(constants.PathSearchReverseURL, wh.ReverseImageSearchURL)

	r.Post(constants.PathIndexURL, h.IndexFromURL)
	r.Post(constants.PathIndexUpload, h.IndexFromUpload)
	r.Post(constants.PathSearchText, h.SearchText)
	r.Post(constants.PathSearchImage, h.SearchImage)
	r.Post(constants.PathSearchImageURL, h.SearchImageURL)
	if assetDir != "" {
		r.Handle(constants.PathAssets+"/*", http.StripPrefix(constants.PathAssets+"/", http.FileServer(http.Dir(assetDir))))
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
	case constants.KeywordMemory:
		jobStore = jobs.NewMemoryStore()
		crawlStore = crawl.NewMemoryStore()
	case constants.KeywordPostgres:
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
			key := r.Header.Get(constants.HeaderXAdminKey)
			if key == "" {
				auth := r.Header.Get(constants.HeaderAuthorization)
				if strings.HasPrefix(auth, constants.BearerPrefix) {
					key = strings.TrimSpace(strings.TrimPrefix(auth, constants.BearerPrefix))
				}
			}
			if key != expected {
				http.Error(w, constants.MessageUnauthorized, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminDisabled(w http.ResponseWriter, r *http.Request) {
	http.Error(w, constants.MessageAdminAPIDisabled, http.StatusServiceUnavailable)
}
