package jobs

import (
	"context"
	"testing"
	"time"

	"iris/config"
)

func TestNewPostgresStoreRequiresDSN(t *testing.T) {
	store, err := NewPostgresStore(context.Background(), "", config.PostgresPool{})
	if err == nil {
		t.Fatalf("expected error for empty dsn")
	}
	if store != nil {
		t.Fatalf("expected nil store on error")
	}
}

func TestBuildLeaseQueryWithTypes(t *testing.T) {
	query, args := buildLeaseQuery(time.Unix(0, 0), []Type{TypeFetchImage, TypeIndexLocalFile})
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
	if query == "" {
		t.Fatal("expected query")
	}
}
