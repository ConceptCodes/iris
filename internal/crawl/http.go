package crawl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"iris/internal/constants"
)

const maxFetchSize = 10 * 1024 * 1024 // 10 MB limit for HTML/sitemap content

type CachedFetcher struct {
	client          *http.Client
	userAgent       string
	defaultTTL      time.Duration
	retries         int
	backoff         time.Duration
	hostConcurrency int
	store           CacheStore

	mu          sync.Mutex
	cache       map[string]cachedResource
	hostLimiter map[string]chan struct{}
}

type cachedResource struct {
	body         []byte
	etag         string
	lastModified string
	expiresAt    time.Time
}

type FetchResult struct {
	URL  string
	Body []byte
}

type FetcherOptions struct {
	DefaultTTL      time.Duration
	Retries         int
	RetryBackoff    time.Duration
	HostConcurrency int
	Store           CacheStore
}

func NewCachedFetcher(client *http.Client, userAgent string, options FetcherOptions) *CachedFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = constants.DefaultCrawlerUserAgent
	}
	if options.DefaultTTL <= 0 {
		options.DefaultTTL = constants.DefaultTTL5m
	}
	if options.RetryBackoff <= 0 {
		options.RetryBackoff = constants.BackoffDelay500ms
	}
	if options.HostConcurrency <= 0 {
		options.HostConcurrency = constants.DefaultHostConcurrency
	}
	if options.Store == nil {
		options.Store = NewNoopCacheStore()
	}
	return &CachedFetcher{
		client:          client,
		userAgent:       userAgent,
		defaultTTL:      options.DefaultTTL,
		retries:         options.Retries,
		backoff:         options.RetryBackoff,
		hostConcurrency: options.HostConcurrency,
		store:           options.Store,
		cache:           make(map[string]cachedResource),
		hostLimiter:     map[string]chan struct{}{},
	}
}

func (f *CachedFetcher) Fetch(ctx context.Context, rawURL string) (FetchResult, error) {
	normalizedURL, err := NormalizeURL(rawURL)
	if err != nil {
		return FetchResult{}, err
	}

	now := time.Now().UTC()
	f.mu.Lock()
	cached, ok := f.cache[normalizedURL]
	f.mu.Unlock()
	if !ok {
		persisted, found, err := f.store.Get(ctx, normalizedURL)
		if err != nil {
			return FetchResult{}, err
		}
		if found {
			cached = persisted
			ok = true
			f.mu.Lock()
			f.cache[normalizedURL] = persisted
			f.mu.Unlock()
		}
	}
	if ok && len(cached.body) > 0 && now.Before(cached.expiresAt) {
		return FetchResult{URL: normalizedURL, Body: append([]byte(nil), cached.body...)}, nil
	}

	release, err := f.acquireHostSlot(ctx, normalizedURL)
	if err != nil {
		return FetchResult{}, err
	}
	defer release()

	var lastErr error
	for attempt := 0; attempt <= f.retries; attempt++ {
		result, retry, delay, err := f.fetchOnce(ctx, normalizedURL, cached, ok)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retry || attempt == f.retries {
			break
		}
		if delay <= 0 {
			delay = retryDelay(f.backoff, attempt)
		}
		if err := sleepWithContext(ctx, delay); err != nil {
			return FetchResult{}, err
		}
	}
	return FetchResult{}, lastErr
}

func (f *CachedFetcher) fetchOnce(ctx context.Context, normalizedURL string, cached cachedResource, hasCached bool) (FetchResult, bool, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalizedURL, nil)
	if err != nil {
		return FetchResult{}, false, 0, err
	}
	req.Header.Set(constants.HeaderUserAgent, f.userAgent)
	if hasCached {
		if cached.etag != "" {
			req.Header.Set(constants.HeaderIfNoneMatch, cached.etag)
		}
		if cached.lastModified != "" {
			req.Header.Set(constants.HeaderIfModifiedSince, cached.lastModified)
		}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{}, true, 0, err
	}
	defer resp.Body.Close()

	now := time.Now().UTC()
	switch resp.StatusCode {
	case http.StatusOK:
		limited := io.LimitReader(resp.Body, maxFetchSize+1)
		body, err := io.ReadAll(limited)
		if err != nil {
			return FetchResult{}, false, 0, err
		}
		if len(body) > maxFetchSize {
			return FetchResult{}, false, 0, fmt.Errorf("response body exceeds %d bytes limit", maxFetchSize)
		}
		resource := cachedResource{
			body:         append([]byte(nil), body...),
			etag:         resp.Header.Get(constants.HeaderETag),
			lastModified: resp.Header.Get(constants.HeaderLastModified),
			expiresAt:    expirationFromHeaders(resp.Header, now, f.defaultTTL),
		}
		f.mu.Lock()
		f.cache[normalizedURL] = resource
		f.mu.Unlock()
		if err := f.store.Put(ctx, normalizedURL, resource); err != nil {
			return FetchResult{}, false, 0, err
		}
		return FetchResult{URL: normalizedURL, Body: body}, false, 0, nil
	case http.StatusNotModified:
		if !hasCached || len(cached.body) == 0 {
			return FetchResult{}, false, 0, fmt.Errorf("conditional fetch without cached body: %s", normalizedURL)
		}
		cached.expiresAt = expirationFromHeaders(resp.Header, now, f.defaultTTL)
		if etag := resp.Header.Get(constants.HeaderETag); etag != "" {
			cached.etag = etag
		}
		if lastModified := resp.Header.Get(constants.HeaderLastModified); lastModified != "" {
			cached.lastModified = lastModified
		}
		f.mu.Lock()
		f.cache[normalizedURL] = cached
		f.mu.Unlock()
		if err := f.store.Put(ctx, normalizedURL, cached); err != nil {
			return FetchResult{}, false, 0, err
		}
		return FetchResult{URL: normalizedURL, Body: append([]byte(nil), cached.body...)}, false, 0, nil
	default:
		return FetchResult{}, isRetryableStatus(resp.StatusCode), retryAfterDelay(resp.Header), fmt.Errorf("fetch %s: status %d", normalizedURL, resp.StatusCode)
	}
}

func expirationFromHeaders(header http.Header, now time.Time, fallback time.Duration) time.Time {
	for _, directive := range strings.Split(header.Get(constants.HeaderCacheControl), ",") {
		directive = strings.TrimSpace(strings.ToLower(directive))
		if strings.HasPrefix(directive, "max-age=") {
			seconds, err := strconv.Atoi(strings.TrimPrefix(directive, "max-age="))
			if err == nil && seconds >= 0 {
				return now.Add(time.Duration(seconds) * time.Second)
			}
		}
	}

	if expires := header.Get(constants.HeaderExpires); expires != "" {
		if parsed, err := http.ParseTime(expires); err == nil {
			return parsed.UTC()
		}
	}

	return now.Add(fallback)
}

func (f *CachedFetcher) acquireHostSlot(ctx context.Context, rawURL string) (func(), error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(parsed.Host)

	f.mu.Lock()
	limiter, ok := f.hostLimiter[host]
	if !ok {
		limiter = make(chan struct{}, f.hostConcurrency)
		f.hostLimiter[host] = limiter
	}
	f.mu.Unlock()

	select {
	case limiter <- struct{}{}:
		return func() { <-limiter }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if attempt <= 0 {
		return base
	}
	return base * time.Duration(1<<attempt)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return statusCode >= 500
	}
}

func retryAfterDelay(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get(constants.HeaderRetryAfter))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
