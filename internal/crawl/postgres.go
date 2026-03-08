package crawl

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("crawl store dsn is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	store := &PostgresStore{db: db}
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

func (s *PostgresStore) CreateSource(ctx context.Context, input CreateSourceInput) (Source, error) {
	now := time.Now().UTC()
	source := Source{
		ID:             uuid.NewString(),
		Kind:           input.Kind,
		SeedURL:        input.SeedURL,
		LocalPath:      input.LocalPath,
		Status:         SourceStatusActive,
		MaxDepth:       input.MaxDepth,
		RateLimitRPS:   input.RateLimitRPS,
		AllowedDomains: append([]string(nil), input.AllowedDomains...),
		CreatedAt:      now,
	}
	domainsJSON, err := json.Marshal(source.AllowedDomains)
	if err != nil {
		return Source{}, err
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO crawl_sources (id, kind, seed_url, local_path, status, max_depth, rate_limit_rps, allowed_domains, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		source.ID, string(source.Kind), source.SeedURL, source.LocalPath, string(source.Status), source.MaxDepth, source.RateLimitRPS, domainsJSON, source.CreatedAt,
	)
	if err != nil {
		return Source{}, fmt.Errorf("create source: %w", err)
	}
	return source, nil
}

func (s *PostgresStore) GetSource(ctx context.Context, id string) (Source, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, kind, seed_url, local_path, status, max_depth, rate_limit_rps, allowed_domains, created_at FROM crawl_sources WHERE id = $1`, id)
	return scanSource(row)
}

func (s *PostgresStore) CreateRun(ctx context.Context, sourceID, trigger string) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:        uuid.NewString(),
		SourceID:  sourceID,
		Trigger:   trigger,
		Status:    RunStatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO crawl_runs (id, source_id, trigger, status, discovered_count, indexed_count, failed_count, last_error, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,0,0,0,'',$5,$5)`,
		run.ID, run.SourceID, run.Trigger, string(run.Status), run.CreatedAt,
	)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, trigger, status, discovered_count, indexed_count, failed_count, last_error, created_at, updated_at FROM crawl_runs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *PostgresStore) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, source_id, trigger, status, discovered_count, indexed_count, failed_count, last_error, created_at, updated_at FROM crawl_runs WHERE id = $1`, id)
	return scanRun(row)
}

func (s *PostgresStore) SetRunDiscovered(ctx context.Context, id string, discovered int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET discovered_count = $2, updated_at = $3 WHERE id = $1`, id, discovered, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set run discovered: %w", err)
	}
	return nil
}

func (s *PostgresStore) IncrementRunIndexed(ctx context.Context, id string, delta int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET indexed_count = indexed_count + $2, updated_at = $3 WHERE id = $1`, id, delta, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("increment run indexed: %w", err)
	}
	return nil
}

func (s *PostgresStore) IncrementRunFailed(ctx context.Context, id string, delta int, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET failed_count = failed_count + $2, last_error = $3, updated_at = $4 WHERE id = $1`, id, delta, lastError, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("increment run failed: %w", err)
	}
	return nil
}

func (s *PostgresStore) MarkRunCompleted(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET status = $2, updated_at = $3 WHERE id = $1`, id, string(RunStatusCompleted), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark run completed: %w", err)
	}
	return nil
}

func (s *PostgresStore) MarkRunFailed(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET status = $2, last_error = $3, updated_at = $4 WHERE id = $1`, id, string(RunStatusFailed), message, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark run failed: %w", err)
	}
	return nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	const ddl = `
		CREATE TABLE IF NOT EXISTS crawl_sources (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			seed_url TEXT NOT NULL DEFAULT '',
			local_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			max_depth INTEGER NOT NULL DEFAULT 0,
			rate_limit_rps INTEGER NOT NULL DEFAULT 0,
			allowed_domains JSONB NOT NULL DEFAULT '[]'::jsonb,
			created_at TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS crawl_runs (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL REFERENCES crawl_sources(id),
			trigger TEXT NOT NULL,
			status TEXT NOT NULL,
			discovered_count INTEGER NOT NULL DEFAULT 0,
			indexed_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
	`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure crawl schema: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSource(row rowScanner) (Source, error) {
	var source Source
	var kind, status string
	var domainsJSON []byte
	if err := row.Scan(&source.ID, &kind, &source.SeedURL, &source.LocalPath, &status, &source.MaxDepth, &source.RateLimitRPS, &domainsJSON, &source.CreatedAt); err != nil {
		return Source{}, err
	}
	source.Kind = SourceKind(kind)
	source.Status = SourceStatus(status)
	_ = json.Unmarshal(domainsJSON, &source.AllowedDomains)
	return source, nil
}

func scanRun(row rowScanner) (Run, error) {
	var run Run
	var status string
	if err := row.Scan(&run.ID, &run.SourceID, &run.Trigger, &status, &run.DiscoveredCount, &run.IndexedCount, &run.FailedCount, &run.LastError, &run.CreatedAt, &run.UpdatedAt); err != nil {
		return Run{}, err
	}
	run.Status = RunStatus(status)
	return run, nil
}
