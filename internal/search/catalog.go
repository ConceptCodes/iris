package search

import (
	"context"

	"iris/pkg/models"
)

type Catalog struct {
	store VectorStore
}

func NewCatalog(store VectorStore) *Catalog {
	return &Catalog{store: store}
}

func (c *Catalog) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	if c.store == nil {
		return nil, errSearchUnavailable
	}
	return c.store.ListImages(ctx, filters, limit, offset)
}
