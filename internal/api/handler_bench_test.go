package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"iris/internal/indexing"
	"iris/internal/metrics"
	"iris/pkg/models"
)

var benchPNGBytes = mustDecodeBase64(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7Z0XQAAAAASUVORK5CYII=",
)

func mustDecodeBase64(input string) []byte {
	data, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		panic(err)
	}
	return data
}

type benchmarkEngine struct {
	searchResults []models.SearchResult
}

func (e *benchmarkEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return "unused", nil
}

func (e *benchmarkEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "unused", nil
}

func (e *benchmarkEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "unused", nil
}

func (e *benchmarkEngine) SearchByText(ctx context.Context, req models.TextSearchRequest) ([]models.SearchResult, error) {
	return e.searchResults, nil
}

func (e *benchmarkEngine) SearchByImageBytes(ctx context.Context, imageBytes []byte, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return e.searchResults, nil
}

func (e *benchmarkEngine) SearchByImageURL(ctx context.Context, imageURL string, topK int, filters map[string]string, enc models.Encoder) ([]models.SearchResult, error) {
	return e.searchResults, nil
}

func (e *benchmarkEngine) GetSimilar(ctx context.Context, id string, topK int, enc models.Encoder) ([]models.SearchResult, error) {
	return e.searchResults, nil
}

func (e *benchmarkEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, nil
}

func (e *benchmarkEngine) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	return nil, nil
}

type benchmarkIndexEngine struct{}

func (e *benchmarkIndexEngine) IndexFromURL(ctx context.Context, req models.IndexRequest) (string, error) {
	return "unused", nil
}

func (e *benchmarkIndexEngine) IndexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "img-1", nil
}

func (e *benchmarkIndexEngine) ReindexFromBytes(ctx context.Context, imageBytes []byte, record models.ImageRecord) (string, error) {
	return "img-1", nil
}

func (e *benchmarkIndexEngine) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	return "", false, nil
}

func benchmarkAPISearchResults(n int) []models.SearchResult {
	results := make([]models.SearchResult, n)
	for i := range results {
		results[i] = models.SearchResult{
			Record: models.ImageRecord{
				ID:       fmt.Sprintf("img-%d", i),
				URL:      fmt.Sprintf("https://example.com/%d.jpg", i),
				Filename: fmt.Sprintf("%d.jpg", i),
			},
			Score: 0.99,
		}
	}
	return results
}

func benchmarkAPIHandler() *Handler {
	return NewHandler(
		&benchmarkEngine{searchResults: benchmarkAPISearchResults(40)},
		indexing.NewPipeline(&benchmarkIndexEngine{}),
		nil,
		nil,
		metrics.NewCounters(),
	)
}

func BenchmarkHandlerSearchText(b *testing.B) {
	h := benchmarkAPIHandler()
	payload, err := json.Marshal(models.TextSearchRequest{
		Query: "mountain sunset wallpaper",
		TopK:  40,
		Filters: map[string]string{
			"meta_source_domain": "example.com",
		},
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/search/text", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		h.SearchText(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

func BenchmarkHandlerSearchImage(b *testing.B) {
	h := benchmarkAPIHandler()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("image", "query.png")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := part.Write(benchPNGBytes); err != nil {
			b.Fatal(err)
		}
		if err := writer.WriteField("top_k", "40"); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/search/image", bytes.NewReader(body.Bytes()))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res := httptest.NewRecorder()
		h.SearchImage(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

func BenchmarkHandlerIndexFromURL(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(benchPNGBytes)
	}))
	defer server.Close()

	h := NewHandler(
		&benchmarkEngine{searchResults: benchmarkAPISearchResults(40)},
		indexing.NewPipelineWithOptions(&benchmarkIndexEngine{}, indexing.PipelineOptions{
			SSRFAllowPrivateNetworks: true,
		}),
		nil,
		nil,
		metrics.NewCounters(),
	)
	payload := []byte(fmt.Sprintf(`{"url":"%s/photo.png","filename":"photo.png","tags":["bench"]}`, server.URL))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/index/url", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		h.IndexFromURL(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

func BenchmarkHandlerIndexFromUpload(b *testing.B) {
	h := benchmarkAPIHandler()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("image", "upload.png")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := part.Write(benchPNGBytes); err != nil {
			b.Fatal(err)
		}
		if err := writer.WriteField("tags", "bench,upload"); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/index/upload", bytes.NewReader(body.Bytes()))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res := httptest.NewRecorder()
		h.IndexFromUpload(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}
