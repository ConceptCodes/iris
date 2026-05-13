package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"iris/internal/jobs"
)

func TestClassifyErrorStringBackedPermanentErrors(t *testing.T) {
	tests := []error{
		errors.New("unsupported content type for image"),
		errors.New("image exceeds maximum size"),
		errors.New("invalid URL format"),
		errors.New("field is required"),
		errors.New("image not found in store"),
	}
	for _, err := range tests {
		if got := ClassifyError(err); got != ErrorTypePermanent {
			t.Fatalf("expected %q to be permanent, got %v", err.Error(), got)
		}
	}
}

func TestEnqueueFetchImageBuildsPayloadAndDedupKey(t *testing.T) {
	var gotType string
	var gotDedup string
	var gotPayload json.RawMessage

	err := enqueueFetchImage(context.Background(), func(ctx context.Context, jobType, dedupKey string, payload json.RawMessage) error {
		gotType = jobType
		gotDedup = dedupKey
		gotPayload = payload
		return nil
	}, "https://example.com/image.jpg?utm_source=x", "run-1", "https://example.com/page", "Title", "source-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotType != string(jobs.TypeFetchImage) {
		t.Fatalf("expected fetch_image type, got %q", gotType)
	}
	if gotDedup != "fetch_image:run-1:https://example.com/image.jpg" {
		t.Fatalf("unexpected dedup key: %q", gotDedup)
	}
	var payload jobs.FetchImagePayload
	if err := json.Unmarshal(gotPayload, &payload); err != nil {
		t.Fatalf("payload did not unmarshal: %v", err)
	}
	if payload.URL != "https://example.com/image.jpg" || payload.RunID != "run-1" || payload.PageURL != "https://example.com/page" || payload.Title != "Title" || payload.CrawlSourceID != "source-1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
