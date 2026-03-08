package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"iris/internal/jobs"
)

func TestEnqueueURLFile(t *testing.T) {
	store := jobs.NewMemoryStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(path, []byte("https://example.com/a.jpg\n# comment\n\nhttps://example.com/b.jpg\n"), 0o644); err != nil {
		t.Fatalf("write url file: %v", err)
	}

	if err := enqueueURLFile(context.Background(), store, path); err != nil {
		t.Fatalf("enqueue url file: %v", err)
	}

	if _, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, jobs.TypeFetchImage); err != nil || !ok {
		t.Fatalf("expected queued fetch_image job, err=%v", err)
	}
}

func TestEnqueueDir(t *testing.T) {
	store := jobs.NewMemoryStore()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cat.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	if err := enqueueDir(context.Background(), store, dir); err != nil {
		t.Fatalf("enqueue dir: %v", err)
	}

	if _, ok, err := store.LeaseNext(context.Background(), time.Now(), 30*time.Second, jobs.TypeIndexLocalFile); err != nil || !ok {
		t.Fatalf("expected queued index_local_file job, err=%v", err)
	}
}
