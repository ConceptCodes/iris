package assets

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const defaultFileMode = 0o644

type Store interface {
	Save(id, filename string, data []byte) (string, error)
	LocalDir() (string, bool)
}

type LocalStore struct {
	dir string
}

func NewStore(dir string) Store {
	return &LocalStore{dir: dir}
}

func (s *LocalStore) LocalDir() (string, bool) {
	return s.dir, true
}

func (s *LocalStore) Save(id, filename string, data []byte) (string, error) {
	if id == "" {
		return "", fmt.Errorf("asset id is required")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("create asset dir: %w", err)
	}

	ext := assetExtension(filename, data)
	name := id + ext
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, data, defaultFileMode); err != nil {
		return "", fmt.Errorf("write asset: %w", err)
	}
	return "/assets/" + name, nil
}

func assetExtension(filename string, data []byte) string {
	contentType := http.DetectContentType(data)
	if contentType != "application/octet-stream" && contentType != "text/plain; charset=utf-8" {
		exts, err := mime.ExtensionsByType(contentType)
		if err == nil && len(exts) > 0 {
			// Prefer common extensions
			for _, e := range exts {
				if e == ".jpg" || e == ".png" || e == ".webp" || e == ".gif" {
					return e
				}
			}
			return exts[0]
		}
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		return ext
	}
	if contentType == "application/octet-stream" || contentType == "text/plain; charset=utf-8" {
		return ".bin"
	}
	exts, err := mime.ExtensionsByType(contentType)
	if err == nil && len(exts) > 0 {
		// Prefer common extensions
		for _, e := range exts {
			if e == ".jpg" || e == ".png" || e == ".webp" || e == ".gif" {
				return e
			}
		}
		return exts[0]
	}
	return ".bin"
}
