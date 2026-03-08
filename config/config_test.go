package config

import "testing"

func TestLoadServerDefaults(t *testing.T) {
	t.Setenv("CLIP_ADDR", "")
	t.Setenv("QDRANT_ADDR", "")
	t.Setenv("CLIP_DIM", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("ASSET_DIR", "")

	cfg := LoadServer()

	if cfg.ClipAddr != defaultClipAddr {
		t.Fatalf("expected default clip addr, got %q", cfg.ClipAddr)
	}
	if cfg.QdrantAddr != defaultQdrantAddr {
		t.Fatalf("expected default qdrant addr, got %q", cfg.QdrantAddr)
	}
	if cfg.ClipDim != defaultClipDim {
		t.Fatalf("expected default clip dim, got %d", cfg.ClipDim)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("expected default http addr, got %q", cfg.HTTPAddr)
	}
	if cfg.AssetDir != defaultAssetDir {
		t.Fatalf("expected default asset dir, got %q", cfg.AssetDir)
	}
}

func TestLoadIndexerOverrides(t *testing.T) {
	t.Setenv("CLIP_ADDR", "http://clip:9000")
	t.Setenv("QDRANT_ADDR", "qdrant:7334")
	t.Setenv("CLIP_DIM", "768")
	t.Setenv("CONCURRENCY", "12")
	t.Setenv("ASSET_DIR", "/tmp/assets")

	cfg := LoadIndexer()

	if cfg.ClipAddr != "http://clip:9000" {
		t.Fatalf("unexpected clip addr: %q", cfg.ClipAddr)
	}
	if cfg.QdrantAddr != "qdrant:7334" {
		t.Fatalf("unexpected qdrant addr: %q", cfg.QdrantAddr)
	}
	if cfg.ClipDim != 768 {
		t.Fatalf("unexpected clip dim: %d", cfg.ClipDim)
	}
	if cfg.Concurrency != 12 {
		t.Fatalf("unexpected concurrency: %d", cfg.Concurrency)
	}
	if cfg.AssetDir != "/tmp/assets" {
		t.Fatalf("unexpected asset dir: %q", cfg.AssetDir)
	}
}

func TestLoadIndexerInvalidIntFallbacks(t *testing.T) {
	t.Setenv("CLIP_DIM", "bad")
	t.Setenv("CONCURRENCY", "bad")

	cfg := LoadIndexer()

	if cfg.ClipDim != defaultClipDim {
		t.Fatalf("expected default clip dim fallback, got %d", cfg.ClipDim)
	}
	if cfg.Concurrency != defaultConcurrency {
		t.Fatalf("expected default concurrency fallback, got %d", cfg.Concurrency)
	}
}
