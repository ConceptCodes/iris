package templates

import (
	"iris/internal/constants"
	"iris/pkg/models"
	"strings"
)

func imageURL(record models.ImageRecord) string {
	// If it's a relative asset path, it's local
	if strings.HasPrefix(record.URL, "/assets/") {
		return record.URL
	}
	// If it's an S3 URL or other full URL, use it
	if record.URL != "" {
		return record.URL
	}
	// Fallback to metadata
	if record.Meta != nil {
		if sourceURL := record.Meta[constants.MetaKeySourceURL]; sourceURL != "" {
			return sourceURL
		}
		if originURL := record.Meta[constants.MetaKeyOriginURL]; originURL != "" {
			return originURL
		}
	}
	return ""
}

func thumbnailURL(record models.ImageRecord) string {
	if record.ThumbnailURL != "" {
		return record.ThumbnailURL
	}
	return imageURL(record)
}
