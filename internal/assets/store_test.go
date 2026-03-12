package assets

import "testing"

func TestAssetExtensionUsesDetectedType(t *testing.T) {
	ext := assetExtension("photo.jpg", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	if ext != ".png" {
		t.Fatalf("expected .png, got %s", ext)
	}
}

func TestAssetExtensionUsesDetectedTypeWithoutFilename(t *testing.T) {
	ext := assetExtension("", []byte("this is not an image"))
	if ext != ".bin" {
		t.Fatalf("expected .bin, got %s", ext)
	}
}

func TestAssetExtensionFallsBackToFilenameExtension(t *testing.T) {
	ext := assetExtension("photo.webp", []byte{})
	if ext != ".webp" {
		t.Fatalf("expected .webp, got %s", ext)
	}
}

func TestAssetExtensionFallsBackToBinWithoutFilename(t *testing.T) {
	ext := assetExtension("", []byte{})
	if ext != ".bin" {
		t.Fatalf("expected .bin, got %s", ext)
	}
}
