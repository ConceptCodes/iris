package crawl

import (
	"context"
	"testing"
)

func TestNewPostgresStoreRequiresDSN(t *testing.T) {
	store, err := NewPostgresStore(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty dsn")
	}
	if store != nil {
		t.Fatal("expected nil store")
	}
}
