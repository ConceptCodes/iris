package metadata

import (
	"context"
	"strings"

	"iris/pkg/models"
)

type Result struct {
	Tags []string
	Meta map[string]string
}

type Enricher interface {
	Enrich(ctx context.Context, imageBytes []byte, record models.ImageRecord) (Result, error)
}

type Composite struct {
	enrichers []Enricher
}

func NewComposite(enrichers ...Enricher) *Composite {
	filtered := make([]Enricher, 0, len(enrichers))
	for _, enricher := range enrichers {
		if enricher != nil {
			filtered = append(filtered, enricher)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &Composite{enrichers: filtered}
}

func (c *Composite) Enrich(ctx context.Context, imageBytes []byte, record models.ImageRecord) (Result, error) {
	if c == nil {
		return Result{}, nil
	}
	combined := Result{
		Meta: make(map[string]string),
	}
	for _, enricher := range c.enrichers {
		result, err := enricher.Enrich(ctx, imageBytes, record)
		if err != nil {
			return Result{}, err
		}
		combined.Tags = MergeTags(combined.Tags, result.Tags)
		for key, value := range result.Meta {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			combined.Meta[key] = value
		}
	}
	return combined, nil
}

func MergeTags(existing, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, tag := range existing {
		normalized := normalizeTag(tag)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		merged = append(merged, normalized)
	}
	for _, tag := range incoming {
		normalized := normalizeTag(tag)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		merged = append(merged, normalized)
	}
	return merged
}

func normalizeTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.Join(strings.Fields(tag), "-")
	return tag
}
