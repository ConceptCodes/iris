package crawl

import (
	"context"
	"testing"

	"iris/config"
)

func TestNewPostgresStoreRequiresDSN(t *testing.T) {
	store, err := NewPostgresStore(context.Background(), "", config.PostgresPool{})
	if err == nil {
		t.Fatal("expected error for empty dsn")
	}
	if store != nil {
		t.Fatal("expected nil store")
	}
}
