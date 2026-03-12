package assets

import (
	"context"
	"testing"
)

func TestNewS3StoreRequiresBucket(t *testing.T) {
	if _, err := NewS3Store(context.Background(), S3Config{}); err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestNewStoreFromSettingsRequiresS3Backend(t *testing.T) {
	// Empty backend should be rejected
	_, err := NewStoreFromSettings(context.Background(), Settings{})
	if err == nil {
		t.Fatal("expected error for empty backend")
	}

	// Unknown backend should be rejected
	_, err = NewStoreFromSettings(context.Background(), Settings{Backend: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestS3StorePublicURLUsesBase(t *testing.T) {
	store := &S3Store{bucket: "bucket", publicBase: "https://cdn.example.com", useBaseURL: true}
	url := store.publicURL("path/to/object.jpg")
	if url != "https://cdn.example.com/path/to/object.jpg" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestS3StorePublicURLDefault(t *testing.T) {
	store := &S3Store{bucket: "bucket"}
	url := store.publicURL("path/to/object.jpg")
	if url != "https://bucket.s3.amazonaws.com/path/to/object.jpg" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestS3StoreObjectKeyUsesPrefix(t *testing.T) {
	store := &S3Store{bucket: "bucket", prefix: "prefix"}
	key := store.objectKey("id-1", "photo.jpg", []byte("image-bytes"))
	if key != "prefix/id-1.jpg" {
		t.Fatalf("unexpected key: %s", key)
	}
}
