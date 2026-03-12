package assets

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// Store is the interface for saving image assets to a remote backend.
// The only supported implementation is S3Store (backed by AWS S3 or MinIO).
type Store interface {
	Save(id, filename string, data []byte) (string, error)
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
		for _, e := range exts {
			if e == ".jpg" || e == ".png" || e == ".webp" || e == ".gif" {
				return e
			}
		}
		return exts[0]
	}
	return ".bin"
}
