package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iris/internal/crawl"
	"iris/internal/jobs"
	"iris/internal/metrics"
	"iris/internal/ssrf"
)

type CrawlerRuntime struct {
	Fetcher    *crawl.CachedFetcher
	Robots     *crawl.RobotsClient
	CacheStore crawl.CacheStore
}

type EnqueueFunc func(ctx context.Context, jobType, dedupKey string, payload json.RawMessage) error

type DiscoverConfig struct {
	SSRFAllowPrivateNetworks bool
	FetchRetries             int
	FetchRetryBackoff        time.Duration
	HostConcurrency          int
	HTTPCacheTTL             time.Duration
	RobotsCacheTTL           time.Duration
}

func NewCrawlerRuntime(cfg DiscoverConfig, cacheStore crawl.CacheStore) (*CrawlerRuntime, error) {
	validator := ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(cfg.SSRFAllowPrivateNetworks))
	safeClient := validator.NewSafeClient(30 * time.Second)
	fetcherOptions := crawl.FetcherOptions{
		DefaultTTL:      cfg.HTTPCacheTTL,
		Retries:         cfg.FetchRetries,
		RetryBackoff:    cfg.FetchRetryBackoff,
		HostConcurrency: cfg.HostConcurrency,
		Store:           cacheStore,
	}
	robotsOptions := crawl.FetcherOptions{
		DefaultTTL:      cfg.RobotsCacheTTL,
		Retries:         cfg.FetchRetries,
		RetryBackoff:    cfg.FetchRetryBackoff,
		HostConcurrency: cfg.HostConcurrency,
		Store:           cacheStore,
	}
	return &CrawlerRuntime{
		Fetcher:    crawl.NewCachedFetcher(safeClient, "iris", fetcherOptions),
		Robots:     crawl.NewRobotsClientWithOptions(safeClient, "iris", robotsOptions),
		CacheStore: cacheStore,
	}, nil
}

func (r *CrawlerRuntime) Close() error {
	if r == nil || r.CacheStore == nil {
		return nil
	}
	return r.CacheStore.Close()
}

func (r *CrawlerRuntime) RunCachePruneLoop(ctx context.Context, pruneInterval time.Duration, batchSize int) {
	if r == nil || r.CacheStore == nil || pruneInterval <= 0 {
		return
	}
	if _, err := r.CacheStore.PruneExpired(ctx, time.Now().UTC(), batchSize); err != nil {
		slog.Warn("crawl cache prune failed", "error", err)
	}
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned, err := r.CacheStore.PruneExpired(ctx, time.Now().UTC(), batchSize)
			if err != nil {
				slog.Warn("crawl cache prune failed", "error", err)
				continue
			}
			if pruned > 0 {
				slog.Info("crawl cache pruned", "rows", pruned)
			}
		}
	}
}

type queueItem struct {
	url   string
	depth int
}

type DiscoverDomainConfig struct {
	AllowedDomains  []string
	MaxDepth        int
	MaxPagesPerRun  int
	MaxImagesPerRun int
	RateLimitRPS    int
}

func InitializeDomainCrawl(source crawl.Source) (DiscoverDomainConfig, func(context.Context) error, string, error) {
	seed, err := url.Parse(source.SeedURL)
	if err != nil {
		return DiscoverDomainConfig{}, nil, "", err
	}
	allowedDomains := source.AllowedDomains
	if len(allowedDomains) == 0 {
		allowedDomains = []string{seed.Hostname()}
	}
	maxDepth := source.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	wait := SourceThrottle(source.RateLimitRPS)
	normalizedSeedURL, err := crawl.NormalizeURL(source.SeedURL)
	if err != nil {
		return DiscoverDomainConfig{}, nil, "", err
	}
	return DiscoverDomainConfig{
		AllowedDomains:  allowedDomains,
		MaxDepth:        maxDepth,
		MaxPagesPerRun:  source.MaxPagesPerRun,
		MaxImagesPerRun: source.MaxImagesPerRun,
		RateLimitRPS:    source.RateLimitRPS,
	}, wait, normalizedSeedURL, nil
}

func DiscoverDomain(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, source crawl.Source, runID string) (int, error) {
	cfg, wait, normalizedSeedURL, err := InitializeDomainCrawl(source)
	if err != nil {
		return 0, err
	}
	queue := []queueItem{{url: normalizedSeedURL, depth: 0}}
	visitedPages := map[string]struct{}{}
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	discovered := 0
	for len(queue) > 0 {
		pagesOK, imagesOK := CheckCrawlBudgets(cfg.MaxPagesPerRun, cfg.MaxImagesPerRun, len(processedPages), discovered)
		if !pagesOK {
			metrics.IncCrawlBudgetHit("pages")
			break
		}
		if !imagesOK {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		item := queue[0]
		queue = queue[1:]
		if _, exists := visitedPages[item.url]; exists {
			continue
		}
		visitedPages[item.url] = struct{}{}
		allowed, err := runtime.Robots.Allowed(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			metrics.IncCrawlSkip("robots")
			continue
		}
		if err := wait(ctx); err != nil {
			return discovered, err
		}
		result, err := runtime.Fetcher.Fetch(ctx, item.url)
		if err != nil {
			return discovered, err
		}
		discovery, err := crawl.ExtractHTMLLinks(strings.NewReader(string(result.Body)), result.URL, cfg.AllowedDomains)
		if err != nil {
			return discovered, err
		}
		pageKey := result.URL
		if discovery.CanonicalURL != "" {
			pageKey = discovery.CanonicalURL
			if discovery.CanonicalURL != result.URL {
				if _, exists := visitedPages[discovery.CanonicalURL]; !exists {
					queue = append(queue, queueItem{url: discovery.CanonicalURL, depth: item.depth})
				}
			}
		}
		if _, exists := processedPages[pageKey]; exists {
			continue
		}
		processedPages[pageKey] = struct{}{}
		imagesEnqueued, err := enqueueDiscoveredImages(ctx, runtime, enqueueFn, discovery.ImageURLs, pageKey, discovery.CanonicalURL, discovery.Title, source.ID, runID, cfg.MaxImagesPerRun-discovered, seenImages)
		if err != nil {
			return discovered, err
		}
		discovered += imagesEnqueued
		if item.depth < cfg.MaxDepth {
			for _, pageURL := range discovery.PageURLs {
				if _, exists := visitedPages[pageURL]; exists {
					continue
				}
				queue = append(queue, queueItem{url: pageURL, depth: item.depth + 1})
			}
		}
	}
	metrics.IncCrawlJobsDiscovered()
	return discovered, nil
}

func DiscoverSitemap(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, source crawl.Source, runID string) (int, error) {
	wait := SourceThrottle(source.RateLimitRPS)
	if err := wait(ctx); err != nil {
		return 0, err
	}
	sitemapResult, err := runtime.Fetcher.Fetch(ctx, source.SeedURL)
	if err != nil {
		return 0, err
	}
	locs, err := crawl.ExtractSitemapLocs(strings.NewReader(string(sitemapResult.Body)))
	if err != nil {
		return 0, err
	}
	discovered := 0
	processedPages := map[string]struct{}{}
	seenImages := map[string]struct{}{}
	for _, loc := range locs {
		if source.MaxPagesPerRun > 0 && len(processedPages) >= source.MaxPagesPerRun {
			metrics.IncCrawlBudgetHit("pages")
			break
		}
		if source.MaxImagesPerRun > 0 && discovered >= source.MaxImagesPerRun {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		normalizedLoc, err := crawl.NormalizeURL(loc)
		if err != nil {
			continue
		}
		allowed, err := runtime.Robots.Allowed(ctx, loc)
		if err != nil {
			return discovered, err
		}
		if !allowed {
			metrics.IncCrawlSkip("robots")
			continue
		}
		if crawl.LooksLikeImageURL(normalizedLoc) {
			discovered, _ = processSitemapImageURL(ctx, runtime, enqueueFn, normalizedLoc, source.ID, runID, source.MaxImagesPerRun, discovered, seenImages)
			continue
		}
		if err := wait(ctx); err != nil {
			return discovered, err
		}
		newDiscovered, budgetHit, err := processSitemapPage(ctx, runtime, enqueueFn, source, normalizedLoc, runID, source.MaxImagesPerRun, discovered, seenImages, processedPages)
		if err != nil {
			return discovered, err
		}
		if budgetHit {
			break
		}
		discovered = newDiscovered
	}
	metrics.IncCrawlJobsDiscovered()
	return discovered, nil
}

func EnqueueURLListSource(ctx context.Context, enqueueFn EnqueueFunc, seedURL, runID string, maxImages int, allowPrivateNetworks bool) (int, error) {
	validator := ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(allowPrivateNetworks))
	if err := validator.ValidateURL(ctx, seedURL); err != nil {
		return 0, fmt.Errorf("SSRF blocked: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seedURL, nil)
	if err != nil {
		return 0, err
	}
	safeClient := validator.NewSafeClient(30 * time.Second)
	resp, err := safeClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch url list: status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 10*1024*1024)
	body, err := io.ReadAll(limited)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(body), "\n") {
		if maxImages > 0 && count >= maxImages {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		normalizedURL, err := crawl.NormalizeURL(line)
		if err != nil {
			continue
		}
		if err := enqueueFetchImage(ctx, enqueueFn, normalizedURL, runID, "", "", ""); err != nil {
			return count, fmt.Errorf("enqueue url list item: %w", err)
		}
		count++
	}
	metrics.IncCrawlJobsDiscovered()
	return count, nil
}

func enqueueDiscoveredImages(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, imageURLs []string, pageURL, canonicalURL, title, sourceID, runID string, maxImages int, seenImages map[string]struct{}) (int, error) {
	processed := 0
	currentPageURL := pageURL
	if canonicalURL != "" {
		currentPageURL = canonicalURL
	}
	for _, imageURL := range imageURLs {
		if maxImages > 0 && processed >= maxImages {
			metrics.IncCrawlBudgetHit("images")
			break
		}
		if _, exists := seenImages[imageURL]; exists {
			if err := processSingleImage(ctx, runtime, enqueueFn, imageURL, currentPageURL, title, sourceID, runID, seenImages); err != nil {
				return processed, err
			}
			continue
		}
		if err := processSingleImage(ctx, runtime, enqueueFn, imageURL, currentPageURL, title, sourceID, runID, seenImages); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func processSingleImage(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, imageURL, pageURL, title, sourceID, runID string, seenImages map[string]struct{}) error {
	if _, exists := seenImages[imageURL]; exists {
		return nil
	}
	allowed, err := runtime.Robots.Allowed(ctx, imageURL)
	if err != nil {
		return err
	}
	if !allowed {
		metrics.IncCrawlSkip("robots")
		return nil
	}
	seenImages[imageURL] = struct{}{}
	return enqueueFetchImage(ctx, enqueueFn, imageURL, runID, pageURL, title, sourceID)
}

func enqueueFetchImage(ctx context.Context, enqueueFn EnqueueFunc, imageURL, runID, pageURL, title, sourceID string) error {
	normalizedURL, err := crawl.NormalizeURL(imageURL)
	if err != nil {
		return err
	}
	payload := jobs.FetchImagePayload{
		URL:           normalizedURL,
		RunID:         runID,
		PageURL:       pageURL,
		Title:         title,
		CrawlSourceID: sourceID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal fetch_image payload: %w", err)
	}
	dedupKey := DedupKey(string(jobs.TypeFetchImage), runID, normalizedURL)
	return enqueueFn(ctx, string(jobs.TypeFetchImage), dedupKey, raw)
}

func processSitemapImageURL(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, normalizedLoc, sourceID, runID string, maxImages, discovered int, seenImages map[string]struct{}) (int, bool) {
	if maxImages > 0 && discovered >= maxImages {
		metrics.IncCrawlBudgetHit("images")
		return discovered, true
	}
	if _, exists := seenImages[normalizedLoc]; exists {
		return discovered, false
	}
	seenImages[normalizedLoc] = struct{}{}
	if err := enqueueFetchImage(ctx, enqueueFn, normalizedLoc, runID, "", "", sourceID); err != nil {
		return discovered, false
	}
	return discovered + 1, false
}

func processSitemapPage(ctx context.Context, runtime *CrawlerRuntime, enqueueFn EnqueueFunc, source crawl.Source, normalizedLoc, runID string, maxImages, discovered int, seenImages map[string]struct{}, processedPages map[string]struct{}) (int, bool, error) {
	result, err := runtime.Fetcher.Fetch(ctx, normalizedLoc)
	if err != nil {
		return discovered, false, err
	}
	discovery, err := crawl.ExtractHTMLLinks(strings.NewReader(string(result.Body)), result.URL, source.AllowedDomains)
	if err != nil {
		return discovered, false, err
	}
	pageKey := result.URL
	if discovery.CanonicalURL != "" {
		pageKey = discovery.CanonicalURL
	}
	if _, exists := processedPages[pageKey]; exists {
		return discovered, false, nil
	}
	processedPages[pageKey] = struct{}{}
	for _, imageURL := range discovery.ImageURLs {
		if maxImages > 0 && discovered >= maxImages {
			metrics.IncCrawlBudgetHit("images")
			return discovered, true, nil
		}
		if _, exists := seenImages[imageURL]; exists {
			continue
		}
		allowed, err := runtime.Robots.Allowed(ctx, imageURL)
		if err != nil {
			return discovered, false, err
		}
		if !allowed {
			metrics.IncCrawlSkip("robots")
			continue
		}
		seenImages[imageURL] = struct{}{}
		pageURL := result.URL
		if discovery.CanonicalURL != "" {
			pageURL = discovery.CanonicalURL
		}
		if err := enqueueFetchImage(ctx, enqueueFn, imageURL, runID, pageURL, discovery.Title, source.ID); err != nil {
			return discovered, false, err
		}
		discovered++
	}
	return discovered, false, nil
}
