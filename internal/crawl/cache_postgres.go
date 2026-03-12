package crawl

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"iris/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresCacheStore struct {
	db *sql.DB
}

func NewPostgresCacheStore(ctx context.Context, dsn string, pool config.PostgresPool) (*PostgresCacheStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("crawl cache dsn is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres cache: %w", err)
	}
	configurePostgresPool(db, pool)
	store := &PostgresCacheStore{db: db}
	if err := store.ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.ensureSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresCacheStore) Get(ctx context.Context, rawURL string) (cachedResource, bool, error) {
	var resource cachedResource
	row := s.db.QueryRowContext(
		ctx,
		`SELECT body, etag, last_modified, expires_at
		 FROM crawl_http_cache
		 WHERE url = $1`,
		rawURL,
	)
	if err := row.Scan(&resource.body, &resource.etag, &resource.lastModified, &resource.expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return cachedResource{}, false, nil
		}
		return cachedResource{}, false, fmt.Errorf("get crawl cache: %w", err)
	}
	return resource, true, nil
}

func (s *PostgresCacheStore) Put(ctx context.Context, rawURL string, resource cachedResource) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO crawl_http_cache (url, body, etag, last_modified, expires_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (url) DO UPDATE
		 SET body = EXCLUDED.body,
		     etag = EXCLUDED.etag,
		     last_modified = EXCLUDED.last_modified,
		     expires_at = EXCLUDED.expires_at,
		     updated_at = EXCLUDED.updated_at`,
		rawURL,
		resource.body,
		resource.etag,
		resource.lastModified,
		resource.expiresAt,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("put crawl cache: %w", err)
	}
	return nil
}

func (s *PostgresCacheStore) PruneExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM crawl_http_cache
		 WHERE url IN (
			SELECT url
			FROM crawl_http_cache
			WHERE expires_at < $1
			ORDER BY expires_at ASC
			LIMIT $2
		 )`,
		now,
		limit,
	)
	if err != nil {
		return 0, fmt.Errorf("prune crawl cache: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune crawl cache rows: %w", err)
	}
	return int(rows), nil
}

func (s *PostgresCacheStore) Close() error {
	return s.db.Close()
}

func (s *PostgresCacheStore) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres cache: %w", err)
	}
	return nil
}

func (s *PostgresCacheStore) ensureSchema(ctx context.Context) error {
	const ddl = `
		CREATE TABLE IF NOT EXISTS crawl_http_cache (
			url TEXT PRIMARY KEY,
			body BYTEA NOT NULL,
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS crawl_http_cache_expires_idx
		ON crawl_http_cache (expires_at);
	`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure crawl cache schema: %w", err)
	}
	return nil
}
