package templates

import (
	"iris/internal/constants"
	"iris/pkg/models"
)

func imageURL(record models.ImageRecord) string {
	if record.URL != "" {
		return record.URL
	}
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
