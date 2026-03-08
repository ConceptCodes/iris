package crawl

import (
	"context"
	"time"
)

type CacheStore interface {
	Get(ctx context.Context, rawURL string) (cachedResource, bool, error)
	Put(ctx context.Context, rawURL string, resource cachedResource) error
	PruneExpired(ctx context.Context, now time.Time, limit int) (int, error)
	Close() error
}

type noopCacheStore struct{}

func NewNoopCacheStore() CacheStore {
	return noopCacheStore{}
}

func (noopCacheStore) Get(ctx context.Context, rawURL string) (cachedResource, bool, error) {
	return cachedResource{}, false, nil
}

func (noopCacheStore) Put(ctx context.Context, rawURL string, resource cachedResource) error {
	return nil
}

func (noopCacheStore) PruneExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	return 0, nil
}

func (noopCacheStore) Close() error {
	return nil
}
