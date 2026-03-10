package crawl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iris/internal/ssrf"
)

func BenchmarkCachedFetcher(b *testing.B) {
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.Run("hot_memory_cache", func(b *testing.B) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = w.Write(payload)
		}))
		defer server.Close()

		fetcher := NewCachedFetcher(server.Client(), "iris-bench", FetcherOptions{
			DefaultTTL:      time.Minute,
			HostConcurrency: 4,
			SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
		})
		if _, err := fetcher.Fetch(context.Background(), server.URL+"/warm"); err != nil {
			b.Fatal(err)
		}

		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := fetcher.Fetch(context.Background(), server.URL+"/warm"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("cold_fetch", func(b *testing.B) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "max-age=0")
			_, _ = w.Write(payload)
		}))
		defer server.Close()

		fetcher := NewCachedFetcher(server.Client(), "iris-bench", FetcherOptions{
			DefaultTTL:      time.Nanosecond,
			HostConcurrency: 4,
			SSRFValidator:   ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(true)),
		})

		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := fetcher.Fetch(context.Background(), server.URL+"/cold"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
