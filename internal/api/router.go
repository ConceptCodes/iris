package api

import (
	"context"
	"net/http"
	"strings"

	"iris/config"
	"iris/internal/assets"
	"iris/internal/constants"
	"iris/internal/crawl"
	"iris/internal/indexing"
	"iris/internal/jobs"
	"iris/internal/metadata"
	"iris/internal/metrics"
	"iris/internal/search"
	"iris/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/riandyrn/otelchi"
)

type AssetsSettings struct {
	Backend      string
	Bucket       string
	Region       string
	Endpoint     string
	AccessKey    string
	SecretKey    string
	SessionKey   string
	Prefix       string
	PublicBase   string
	PathStyle    bool
	MetadataAddr string
}

type AdminAuthSettings struct {
	AdminAPIKey     string
	ReadOnlyAPIKeys []string
}

type adminRole int

const (
	adminRoleRead adminRole = iota
	adminRoleWrite
)

// securityHeaders adds browser security headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Note: script-src allows cdn.tailwindcss.com and unpkg.com for HTMX.
		// Inline script is currently used for Tailwind config and theme bootstrapping.
		// In production, these should be self-hosted with SRI
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self';")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

func NewRouter(engine search.Engine, crawlService *crawl.Service, adminAPIKey string) http.Handler {
	return NewRouterWithAssets(engine, AssetsSettings{}, crawlService, adminAPIKey, nil)
}

func NewRouterWithAssets(engine search.Engine, assetsCfg AssetsSettings, crawlService *crawl.Service, adminAPIKey string, jobStore jobs.Store) http.Handler {
	return NewRouterWithAssetsAndAuth(engine, assetsCfg, crawlService, AdminAuthSettings{AdminAPIKey: adminAPIKey}, jobStore)
}

func NewRouterWithAssetsAndAuth(engine search.Engine, assetsCfg AssetsSettings, crawlService *crawl.Service, authSettings AdminAuthSettings, jobStore jobs.Store) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(StructuredLogger())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(constants.HTTPTimeout60s))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{constants.MethodGET, constants.MethodPOST, constants.MethodOPTIONS},
		AllowedHeaders:   []string{"Accept", constants.HeaderAuthorization, constants.HeaderContentType, constants.HeaderXCSRFToken},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           constants.CORSMaxAge,
	}))
	r.Use(securityHeaders)
	// Add OpenTelemetry instrumentation
	r.Use(otelchi.Middleware("iris-server", otelchi.WithChiRoutes(r)))

	assetStore := buildAssetStore(assetsCfg)
	indexer := indexing.NewPipelineWithOptions(engine, indexing.PipelineOptions{
		AssetStore: assetStore,
		Enricher: metadata.NewComposite(
			metadata.EXIFEnricher{},
			metadata.NewClient(assetsCfg.MetadataAddr, constants.HTTPTimeout60s),
		),
	})
	if jobStore == nil {
		jobStore = jobs.NewMemoryStore()
	}
	counters := metrics.NewCounters()
	h := NewHandler(engine, indexer, crawlService, jobStore, counters)
	wh := web.NewHandlers(engine)

	authorizer := newAdminAuthorizer(authSettings)
	if authorizer.enabled() {
		r.With(authorizer.requireRole(adminRoleWrite)).Post(constants.PathAdminSources, h.CreateSource)
		r.With(authorizer.requireRole(adminRoleWrite)).Post(constants.PathAdminSourceRun, h.TriggerSourceRun)
		r.With(authorizer.requireRole(adminRoleWrite)).Post(constants.PathAdminIndexLocal, h.EnqueueLocalIndex)
		r.With(authorizer.requireRole(adminRoleWrite)).Post(constants.PathAdminReindex, h.HandleReindex)
		r.With(authorizer.requireRole(adminRoleRead)).Get(constants.PathAdminRuns, h.ListRuns)
		r.With(authorizer.requireRole(adminRoleRead)).Get(constants.PathAdminRunDetail, h.GetRun)
		r.With(authorizer.requireRole(adminRoleRead)).Get(constants.PathAdminMetrics, h.Metrics)
		// Metrics require admin authentication
		r.With(authorizer.requireRole(adminRoleRead)).Handle("/metrics", metrics.Handler())
	} else {
		r.Post(constants.PathAdminSources, adminDisabled)
		r.Post(constants.PathAdminSourceRun, adminDisabled)
		r.Post(constants.PathAdminIndexLocal, adminDisabled)
		r.Post(constants.PathAdminReindex, adminDisabled)
		r.Get(constants.PathAdminRuns, adminDisabled)
		r.Get(constants.PathAdminRunDetail, adminDisabled)
		r.Get(constants.PathAdminMetrics, adminDisabled)
		r.Handle("/metrics", http.HandlerFunc(adminDisabled))
	}

	r.Get(constants.PathHealth, h.Health)
	r.Get(constants.PathLanding, wh.LandingPage)

	// Public search routes with rate limiting
	r.Group(func(r chi.Router) {
		r.Use(RateLimit(10, 20)) // Moderate rate limit for search
		r.Get(constants.PathSearch, wh.SearchResults)
		r.Get(constants.PathImage+"/{id}", wh.ImageDetail)
		r.Get(constants.PathImageRelated, wh.RelatedImages)

		r.Group(func(r chi.Router) {
			r.Use(MaxRequestSize(constants.MaxImageSize))
			r.Post(constants.PathSearchReverse, wh.ReverseImageSearch)
			r.Post(constants.PathSearchReverseURL, wh.ReverseImageSearchURL)
			r.Post(constants.PathSearchText, h.SearchText)
			r.Post(constants.PathSearchImage, h.SearchImage)
			r.Post(constants.PathSearchImageURL, h.SearchImageURL)
		})
	})

	// Indexing routes - moved to authorized group for mutation
	if authorizer.enabled() {
		r.Group(func(r chi.Router) {
			r.Use(authorizer.requireRole(adminRoleWrite), RateLimit(2, 5), MaxRequestSize(constants.MaxImageSize))
			r.Post(constants.PathIndexURL, h.IndexFromURL)
			r.Post(constants.PathIndexUpload, h.IndexFromUpload)
		})
	} else {
		r.Post(constants.PathIndexURL, adminDisabled)
		r.Post(constants.PathIndexUpload, adminDisabled)
	}

	// Serve static UI assets (CSS, JS)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	return r
}

func buildAssetStore(cfg AssetsSettings) assets.Store {
	store, err := assets.NewStoreFromSettings(context.Background(), assets.Settings{
		Backend: cfg.Backend,
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
		// Log the misconfiguration and continue without thumbnail storage.
		// The pipeline handles a nil store gracefully.
		return nil
	}
	return store
}

func NewCrawlService(jobBackend, jobStoreDSN string, pool config.PostgresPool) (*crawl.Service, jobs.Store, func(), error) {
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
		jobStore, err = jobs.NewPostgresStore(context.Background(), jobStoreDSN, pool)
		if err != nil {
			return nil, nil, nil, err
		}
		crawlStore, err = crawl.NewPostgresStore(context.Background(), jobStoreDSN, pool)
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

type adminAuthorizer struct {
	adminKeys    map[string]struct{}
	readOnlyKeys map[string]struct{}
}

func newAdminAuthorizer(settings AdminAuthSettings) adminAuthorizer {
	authz := adminAuthorizer{
		adminKeys:    make(map[string]struct{}),
		readOnlyKeys: make(map[string]struct{}),
	}
	if settings.AdminAPIKey != "" {
		authz.adminKeys[settings.AdminAPIKey] = struct{}{}
	}
	for _, key := range settings.ReadOnlyAPIKeys {
		if key != "" {
			authz.readOnlyKeys[key] = struct{}{}
		}
	}
	return authz
}

func (a adminAuthorizer) enabled() bool {
	return len(a.adminKeys) > 0 || len(a.readOnlyKeys) > 0
}

func (a adminAuthorizer) requireRole(required adminRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := a.roleForToken(adminTokenFromRequest(r))
			if !ok {
				http.Error(w, constants.MessageUnauthorized, http.StatusUnauthorized)
				return
			}
			if required == adminRoleWrite && role != adminRoleWrite {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a adminAuthorizer) roleForToken(token string) (adminRole, bool) {
	if token == "" {
		return adminRoleRead, false
	}
	if _, ok := a.adminKeys[token]; ok {
		return adminRoleWrite, true
	}
	if _, ok := a.readOnlyKeys[token]; ok {
		return adminRoleRead, true
	}
	return adminRoleRead, false
}

func adminTokenFromRequest(r *http.Request) string {
	key := r.Header.Get(constants.HeaderXAdminKey)
	if key != "" {
		return key
	}
	auth := r.Header.Get(constants.HeaderAuthorization)
	if strings.HasPrefix(auth, constants.BearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(auth, constants.BearerPrefix))
	}
	return ""
}

func adminDisabled(w http.ResponseWriter, r *http.Request) {
	http.Error(w, constants.MessageNotFound, http.StatusNotFound)
}
