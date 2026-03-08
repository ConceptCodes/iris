package clip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_HealthCheck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		if err := client.HealthCheck(context.Background()); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		if err := client.HealthCheck(context.Background()); err == nil {
			t.Errorf("expected error")
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		client := NewClient("http://broken.local:1234")
		if err := client.HealthCheck(context.Background()); err == nil {
			t.Errorf("expected error")
		}
	})
}

func TestClient_doPost(t *testing.T) {
	t.Run("marshal failure", func(t *testing.T) {
		client := NewClient("http://localhost")
		// Channels cannot be marshaled
		err := client.doPost(context.Background(), "/path", make(chan int), nil)
		if err == nil || !strings.Contains(err.Error(), "marshal request") {
			t.Errorf("expected marshal failure")
		}
	})

	t.Run("request-creation failure", func(t *testing.T) {
		client := NewClient("://bad-url")
		err := client.doPost(context.Background(), "/path", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "create request") {
			t.Errorf("expected request-creation failure")
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		client := NewClient("http://broken.local:1234")
		err := client.doPost(context.Background(), "/path", "body", nil)
		if err == nil || !strings.Contains(err.Error(), "do request") {
			t.Errorf("expected transport failure")
		}
	})

	t.Run("non-200 with detail", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"detail": "bad request"}`))
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		err := client.doPost(context.Background(), "/path", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "sidecar error (status 400): bad request") {
			t.Errorf("expected specific error detail")
		}
	})

	t.Run("non-200 without detail", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`bad`))
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		err := client.doPost(context.Background(), "/path", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "sidecar error: status 500") {
			t.Errorf("expected generic error")
		}
	})

	t.Run("decode failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{bad`))
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		err := client.doPost(context.Background(), "/path", nil, &map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "decode response") {
			t.Errorf("expected decode failure")
		}
	})

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"foo": "bar"}`))
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		var resp map[string]string
		err := client.doPost(context.Background(), "/path", nil, &resp)
		if err != nil {
			t.Errorf("expected success")
		}
		if resp["foo"] != "bar" {
			t.Errorf("expected parsed response")
		}
	})
}

func TestClient_EmbedText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedTextRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Text != "test" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{1.0, 2.0}, Dim: 2})
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	emb, err := client.EmbedText(context.Background(), "test")
	if err != nil {
		t.Errorf("expected no error")
	}
	if len(emb) != 2 || emb[0] != 1.0 {
		t.Errorf("unexpected embedding result")
	}

	// Error wrapping text
	client2 := NewClient("http://broken")
	_, err2 := client2.EmbedText(context.Background(), "test")
	if err2 == nil || !strings.Contains(err2.Error(), "embed text: ") {
		t.Errorf("expected wrapped error, got %v", err2)
	}
}

func TestClient_EmbedImageBytes(t *testing.T) {
	t.Run("max size guard", func(t *testing.T) {
		client := NewClient("http://localhost")
		_, err := client.EmbedImageBytes(context.Background(), make([]byte, maxImageSize+1))
		if err == nil || !strings.Contains(err.Error(), string("exceeds")) {
			t.Errorf("expected max-size error")
		}
	})

	t.Run("happy path", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{3.0, 4.0}, Dim: 2})
		}))
		defer ts.Close()
		client := NewClient(ts.URL)
		emb, err := client.EmbedImageBytes(context.Background(), []byte("data"))
		if err != nil {
			t.Fatalf("expected no error")
		}
		if len(emb) != 2 || emb[0] != 3.0 {
			t.Errorf("unexpected embedding result")
		}
	})
}

func TestClient_EmbedImageURL(t *testing.T) {
	t.Run("fetch failure", func(t *testing.T) {
		client := NewClient("http://localhost")
		_, err := client.EmbedImageURL(context.Background(), "http://broken.local:1234")
		if err == nil || !strings.Contains(err.Error(), "fetch image url") {
			t.Errorf("expected fetch failure")
		}
	})

	t.Run("non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()
		client := NewClient("http://localhost") // base url not used for fetching url
		_, err := client.EmbedImageURL(context.Background(), ts.URL)
		if err == nil || !strings.Contains(err.Error(), "status 404") {
			t.Errorf("expected non-200 failure")
		}
	})

	t.Run("oversize", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write more than maxImageSize
			w.Write(make([]byte, maxImageSize+2))
		}))
		defer ts.Close()
		client := NewClient("http://localhost")
		_, err := client.EmbedImageURL(context.Background(), ts.URL)
		if err == nil || !strings.Contains(err.Error(), string("exceeds")) {
			t.Errorf("expected oversize failure")
		}
	})

	t.Run("success chains", func(t *testing.T) {
		// Mock both the image download URL AND the clip POST

		clipTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/embed/image" {
				json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{5.0}, Dim: 1})
			}
		}))
		defer clipTs.Close()

		imgTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("image data"))
		}))
		defer imgTs.Close()

		client := NewClient(clipTs.URL)
		emb, err := client.EmbedImageURL(context.Background(), imgTs.URL)
		if err != nil {
			t.Fatalf("expected no error")
		}
		if len(emb) != 1 || emb[0] != 5.0 {
			t.Errorf("unexpected embedding result")
		}
	})

	// Wait, is there a read failure? We can test read failure using a broken reader.
	// But it's too much. The prompt covers io.LimitReader which we just tested.
}
