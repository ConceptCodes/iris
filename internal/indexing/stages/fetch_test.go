package stages

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchImageBytesUsesInjectedClient(t *testing.T) {
	called := false
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			if req.Header.Get("User-Agent") != "test-agent" {
				t.Fatalf("expected injected user agent, got %q", req.Header.Get("User-Agent"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/gif"}},
				Body:       io.NopCloser(bytes.NewReader([]byte("GIF89a"))),
				Request:    req,
			}, nil
		}),
	}

	result, err := FetchImageBytes(context.Background(), "https://example.com/image.gif", FetchConfig{
		Client:    client,
		MaxBytes:  1024,
		UserAgent: "test-agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected injected client to be called")
	}
	if result.MIMEType != "image/gif" {
		t.Fatalf("expected image/gif, got %q", result.MIMEType)
	}
}
