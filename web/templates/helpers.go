package templates

import (
	"iris/internal/constants"
	"iris/pkg/models"
	"strings"
)

func imageURL(record models.ImageRecord) string {
	if record.Meta != nil {
		if sourceURL := record.Meta[constants.MetaKeySourceURL]; sourceURL != "" {
			return sourceURL
		}
		if originURL := record.Meta[constants.MetaKeyOriginURL]; originURL != "" {
			return originURL
		}
	}
	if record.URL != "" && !strings.HasPrefix(record.URL, "/assets/") {
		return record.URL
	}
	if record.URL != "" {
		return record.URL
	}
	return ""
}
