package search

import (
	"context"

	"iris/internal/constants"
)

type Deduper struct {
	store VectorStore
}

func NewDeduper(store VectorStore) *Deduper {
	return &Deduper{store: store}
}

func (d *Deduper) FindExistingID(ctx context.Context, meta map[string]string, fallbackURL string) (string, bool, error) {
	if meta == nil {
		meta = map[string]string{}
	}
	if hash := meta[constants.MetaKeyContentSHA256]; hash != "" {
		if id, ok, err := d.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeyContentSHA256, hash); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if source := meta[constants.MetaKeySourceURL]; source != "" {
		if id, ok, err := d.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeySourceURL, source); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	if fallbackURL != "" {
		if id, ok, err := d.store.FindIDByMeta(ctx, constants.PayloadFieldMetaPrefix+constants.MetaKeySourceURL, fallbackURL); err != nil {
			return "", false, err
		} else if ok {
			return id, true, nil
		}
	}
	return "", false, nil
}
