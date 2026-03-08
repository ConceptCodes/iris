package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectDirJobs(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "cat.jpg"), []byte("jpg"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("txt"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "dog.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write nested image: %v", err)
	}

	jobs := collectDirJobs(dir)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 image jobs, got %d", len(jobs))
	}

	for _, job := range jobs {
		if filepath.Ext(job) == ".txt" {
			t.Fatalf("unexpected non-image job: %s", job)
		}
		if !filepath.IsAbs(job) {
			t.Fatalf("expected absolute path, got %s", job)
		}
	}
}

func TestCollectURLJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")
	err := os.WriteFile(path, []byte("http://test.com\n#comment\n\nhttp://t2.com"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	jobs := collectURLJobs(path)
	if len(jobs) != 2 || jobs[0] != "http://test.com" || jobs[1] != "http://t2.com" {
		t.Errorf("expected 2 URLs: got %v", jobs)
	}
}
