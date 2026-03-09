package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStoreSaveWritesFileAndReturnsURL(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	url, err := store.Save("asset-1", "photo.jpg", []byte("image-bytes"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty asset URL")
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 stored file, got %d", len(files))
	}
}

func TestLocalStoreSaveRequiresID(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Save("", "photo.jpg", []byte("image-bytes")); err == nil {
		t.Fatal("expected error when id is empty")
	}
}

func TestLocalStoreUsesContentSniffedExtension(t *testing.T) {
	store := NewStore(t.TempDir())
	url, err := store.Save("asset-1", "file.bin", []byte{0xFF, 0xD8, 0xFF, 0xE0})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if filepath.Ext(url) != ".jpg" && filepath.Ext(url) != ".jpeg" {
		t.Fatalf("expected jpg/jpeg extension, got %s", filepath.Ext(url))
	}
}

func TestAssetExtensionUsesDetectedType(t *testing.T) {
	ext := assetExtension("photo.png", []byte("this is not an image"))
	if ext != ".conf" {
		t.Fatalf("expected .conf, got %s", ext)
	}
}

func TestAssetExtensionUsesDetectedTypeWithoutFilename(t *testing.T) {
	ext := assetExtension("", []byte("this is not an image"))
	if ext != ".conf" {
		t.Fatalf("expected .conf, got %s", ext)
	}
}
