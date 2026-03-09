package crawl

import (
	"context"
	"fmt"
	"iris/internal/ssrf"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testFetcherOptions() FetcherOptions {
	return FetcherOptions{
		DefaultTTL:      time.Second,
		HostConcurrency: 2,
		SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
	}
}

type memoryCacheStore struct {
	resource cachedResource
	ok       bool
	putCalls int
}

func (s *memoryCacheStore) Get(ctx context.Context, rawURL string) (cachedResource, bool, error) {
	return s.resource, s.ok, nil
}

func (s *memoryCacheStore) Put(ctx context.Context, rawURL string, resource cachedResource) error {
	s.resource = resource
	s.ok = true
	s.putCalls++
	return nil
}

func (s *memoryCacheStore) PruneExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	return 0, nil
}

func (s *memoryCacheStore) Close() error {
	return nil
}

func TestCachedFetcherUsesConditionalRequests(t *testing.T) {
	var (
		mu           sync.Mutex
		requestCount int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		current := requestCount
		mu.Unlock()

		switch current {
		case 1:
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Cache-Control", "max-age=0")
			_, _ = w.Write([]byte("first"))
		default:
			if r.Header.Get("If-None-Match") != `"v1"` {
				http.Error(w, "missing conditional header", http.StatusBadRequest)
				return
			}
			w.Header().Set("Cache-Control", "max-age=60")
			w.WriteHeader(http.StatusNotModified)
		}
	}))
	defer server.Close()

	fetcher := NewCachedFetcher(server.Client(), "iris", testFetcherOptions())

	first, err := fetcher.Fetch(context.Background(), server.URL+"/page?utm_source=test")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if string(first.Body) != "first" {
		t.Fatalf("unexpected first body: %q", first.Body)
	}

	second, err := fetcher.Fetch(context.Background(), server.URL+"/page?utm_source=other")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if string(second.Body) != "first" {
		t.Fatalf("expected cached body after 304, got %q", second.Body)
	}
}

func TestCachedFetcherRetriesTransientStatus(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	fetcher := NewCachedFetcher(server.Client(), "iris", FetcherOptions{
		DefaultTTL:      time.Second,
		Retries:         1,
		RetryBackoff:    time.Millisecond,
		HostConcurrency: 2,
		SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
	})

	result, err := fetcher.Fetch(context.Background(), server.URL+"/retry")
	if err != nil {
		t.Fatalf("fetch with retry: %v", err)
	}
	if string(result.Body) != "ok" {
		t.Fatalf("unexpected body after retry: %q", result.Body)
	}
	if requests.Load() != 2 {
		t.Fatalf("expected 2 requests, got %d", requests.Load())
	}
}

func TestCachedFetcherHydratesFromPersistentStore(t *testing.T) {
	store := &memoryCacheStore{
		resource: cachedResource{
			body:      []byte("persisted"),
			expiresAt: time.Now().Add(time.Minute),
		},
		ok: true,
	}
	fetcher := NewCachedFetcher(nil, "iris", FetcherOptions{
		DefaultTTL:      time.Second,
		HostConcurrency: 1,
		Store:           store,
		SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
	})

	result, err := fetcher.Fetch(context.Background(), "https://example.com/page")
	if err != nil {
		t.Fatalf("fetch from persistent store: %v", err)
	}
	if string(result.Body) != "persisted" {
		t.Fatalf("unexpected body from persistent store: %q", result.Body)
	}
	if store.putCalls != 0 {
		t.Fatalf("expected no write-back for fresh persisted resource, got %d", store.putCalls)
	}
}

func TestCachedFetcherLimitsHostConcurrency(t *testing.T) {
	var current atomic.Int32
	var maxSeen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active := current.Add(1)
		for {
			seen := maxSeen.Load()
			if active <= seen || maxSeen.CompareAndSwap(seen, active) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		current.Add(-1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	fetcher := NewCachedFetcher(server.Client(), "iris", FetcherOptions{
		DefaultTTL:      time.Millisecond,
		HostConcurrency: 1,
		SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
	})

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host := u.Host
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		pathURL := (&url.URL{Scheme: u.Scheme, Host: host, Path: fmt.Sprintf("/page-%d", i)}).String()
		wg.Add(1)
		go func(raw string) {
			defer wg.Done()
			_, _ = fetcher.Fetch(context.Background(), raw)
		}(pathURL)
	}
	wg.Wait()
	if maxSeen.Load() != 1 {
		t.Fatalf("expected max host concurrency 1, got %d", maxSeen.Load())
	}
}

func TestNormalizeURL(t *testing.T) {
	normalized, err := NormalizeURL("HTTPS://Example.com:443/a/../gallery/?b=2&utm_source=x&a=1&a=0#frag")
	if err != nil {
		t.Fatalf("normalize url: %v", err)
	}

	want := "https://example.com/gallery/?a=0&a=1&b=2"
	if normalized != want {
		t.Fatalf("unexpected normalized url\nwant: %s\ngot:  %s", want, normalized)
	}
}

func TestNormalizeURLDefaultPortAndHostCase(t *testing.T) {
	normalized, err := NormalizeURL("http://EXAMPLE.com:80/path")
	if err != nil {
		t.Fatalf("normalize url: %v", err)
	}
	if normalized != "http://example.com/path" {
		t.Fatalf("unexpected normalized url: %s", normalized)
	}
}

func TestNormalizeURLRequiresAbsoluteHTTPURL(t *testing.T) {
	cases := []string{
		"/relative/path",
		"mailto:test@example.com",
	}
	for _, raw := range cases {
		t.Run(fmt.Sprintf("reject_%s", raw), func(t *testing.T) {
			if _, err := NormalizeURL(raw); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}
