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
	scheduleEvery, _ := time.ParseDuration(input.ScheduleEvery)
	source := Source{
		ID:              uuid.NewString(),
		Kind:            input.Kind,
		SeedURL:         input.SeedURL,
		LocalPath:       input.LocalPath,
		Status:          SourceStatusActive,
		MaxDepth:        input.MaxDepth,
		RateLimitRPS:    input.RateLimitRPS,
		MaxPagesPerRun:  input.MaxPagesPerRun,
		MaxImagesPerRun: input.MaxImagesPerRun,
		AllowedDomains:  append([]string(nil), input.AllowedDomains...),
		ScheduleEvery:   scheduleEvery,
		NextRunAt:       nextRunTime(now, scheduleEvery),
		CreatedAt:       now,
	}
	domainsJSON, err := json.Marshal(source.AllowedDomains)
	if err != nil {
		return Source{}, err
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO crawl_sources (id, kind, seed_url, local_path, status, max_depth, rate_limit_rps, max_pages_per_run, max_images_per_run, allowed_domains, schedule_every_seconds, next_run_at, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		source.ID, string(source.Kind), source.SeedURL, source.LocalPath, string(source.Status), source.MaxDepth, source.RateLimitRPS, source.MaxPagesPerRun, source.MaxImagesPerRun, domainsJSON, int64(scheduleEvery.Seconds()), source.NextRunAt, source.CreatedAt,
	)
	if err != nil {
		return Source{}, fmt.Errorf("create source: %w", err)
	}
	return source, nil
}

func (s *PostgresStore) GetSource(ctx context.Context, id string) (Source, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, kind, seed_url, local_path, status, max_depth, rate_limit_rps, max_pages_per_run, max_images_per_run, allowed_domains, schedule_every_seconds, next_run_at, last_run_at, last_success_at, last_content_change_at, consecutive_failures, last_discovered_count, last_indexed_count, last_duplicate_count, last_failed_count, created_at FROM crawl_sources WHERE id = $1`, id)
	return scanSource(row)
}

func (s *PostgresStore) CreateRun(ctx context.Context, sourceID, trigger string, scheduledAt time.Time) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:          uuid.NewString(),
		SourceID:    sourceID,
		Trigger:     trigger,
		Status:      RunStatusRunning,
		ScheduledAt: scheduledAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO crawl_runs (id, source_id, trigger, status, discovered_count, indexed_count, duplicate_count, failed_count, last_error, scheduled_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,0,0,0,0,'',$5,$6,$6)`,
		run.ID, run.SourceID, run.Trigger, string(run.Status), run.ScheduledAt, run.CreatedAt,
	)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, trigger, status, discovered_count, indexed_count, duplicate_count, failed_count, last_error, scheduled_at, created_at, updated_at FROM crawl_runs ORDER BY created_at DESC`)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, source_id, trigger, status, discovered_count, indexed_count, duplicate_count, failed_count, last_error, scheduled_at, created_at, updated_at FROM crawl_runs WHERE id = $1`, id)
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

func (s *PostgresStore) IncrementRunDuplicate(ctx context.Context, id string, delta int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET duplicate_count = duplicate_count + $2, updated_at = $3 WHERE id = $1`, id, delta, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("increment run duplicate: %w", err)
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
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET status = $2, updated_at = $3 WHERE id = $1`, id, string(RunStatusCompleted), now); err != nil {
		return fmt.Errorf("mark run completed: %w", err)
	}
	return s.applySourceOutcome(ctx, id, true, now)
}

func (s *PostgresStore) MarkRunFailed(ctx context.Context, id, message string) error {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE crawl_runs SET status = $2, last_error = $3, updated_at = $4 WHERE id = $1`, id, string(RunStatusFailed), message, now); err != nil {
		return fmt.Errorf("mark run failed: %w", err)
	}
	return s.applySourceOutcome(ctx, id, false, now)
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
			max_pages_per_run INTEGER NOT NULL DEFAULT 0,
			max_images_per_run INTEGER NOT NULL DEFAULT 0,
			allowed_domains JSONB NOT NULL DEFAULT '[]'::jsonb,
			schedule_every_seconds BIGINT NOT NULL DEFAULT 0,
			next_run_at TIMESTAMPTZ NULL,
			last_run_at TIMESTAMPTZ NULL,
			last_success_at TIMESTAMPTZ NULL,
			last_content_change_at TIMESTAMPTZ NULL,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			last_discovered_count INTEGER NOT NULL DEFAULT 0,
			last_indexed_count INTEGER NOT NULL DEFAULT 0,
			last_duplicate_count INTEGER NOT NULL DEFAULT 0,
			last_failed_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS crawl_runs (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL REFERENCES crawl_sources(id),
			trigger TEXT NOT NULL,
			status TEXT NOT NULL,
			discovered_count INTEGER NOT NULL DEFAULT 0,
			indexed_count INTEGER NOT NULL DEFAULT 0,
			duplicate_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			scheduled_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS max_pages_per_run INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS max_images_per_run INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_run_at TIMESTAMPTZ NULL;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ NULL;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_content_change_at TIMESTAMPTZ NULL;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS consecutive_failures INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_discovered_count INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_indexed_count INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_duplicate_count INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_sources ADD COLUMN IF NOT EXISTS last_failed_count INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE crawl_runs ADD COLUMN IF NOT EXISTS duplicate_count INTEGER NOT NULL DEFAULT 0;
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
	var scheduleSeconds int64
	var nextRunAt sql.NullTime
	var lastRunAt sql.NullTime
	var lastSuccessAt sql.NullTime
	var lastContentChangeAt sql.NullTime
	if err := row.Scan(&source.ID, &kind, &source.SeedURL, &source.LocalPath, &status, &source.MaxDepth, &source.RateLimitRPS, &source.MaxPagesPerRun, &source.MaxImagesPerRun, &domainsJSON, &scheduleSeconds, &nextRunAt, &lastRunAt, &lastSuccessAt, &lastContentChangeAt, &source.ConsecutiveFailures, &source.LastDiscoveredCount, &source.LastIndexedCount, &source.LastDuplicateCount, &source.LastFailedCount, &source.CreatedAt); err != nil {
		return Source{}, err
	}
	source.Kind = SourceKind(kind)
	source.Status = SourceStatus(status)
	_ = json.Unmarshal(domainsJSON, &source.AllowedDomains)
	if scheduleSeconds > 0 {
		source.ScheduleEvery = time.Duration(scheduleSeconds) * time.Second
	}
	if nextRunAt.Valid {
		source.NextRunAt = nextRunAt.Time
	}
	if lastRunAt.Valid {
		source.LastRunAt = lastRunAt.Time
	}
	if lastSuccessAt.Valid {
		source.LastSuccessAt = lastSuccessAt.Time
	}
	if lastContentChangeAt.Valid {
		source.LastContentChangeAt = lastContentChangeAt.Time
	}
	return source, nil
}

func scanRun(row rowScanner) (Run, error) {
	var run Run
	var status string
	var scheduledAt sql.NullTime
	if err := row.Scan(&run.ID, &run.SourceID, &run.Trigger, &status, &run.DiscoveredCount, &run.IndexedCount, &run.DuplicateCount, &run.FailedCount, &run.LastError, &scheduledAt, &run.CreatedAt, &run.UpdatedAt); err != nil {
		return Run{}, err
	}
	run.Status = RunStatus(status)
	if scheduledAt.Valid {
		run.ScheduledAt = scheduledAt.Time
	}
	return run, nil
}

func (s *PostgresStore) ListSourcesDue(ctx context.Context, now time.Time) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, seed_url, local_path, status, max_depth, rate_limit_rps, max_pages_per_run, max_images_per_run, allowed_domains, schedule_every_seconds, next_run_at, last_run_at, last_success_at, last_content_change_at, consecutive_failures, last_discovered_count, last_indexed_count, last_duplicate_count, last_failed_count, created_at FROM crawl_sources WHERE status = $1 AND schedule_every_seconds > 0 AND (next_run_at IS NULL OR next_run_at <= $2)`, string(SourceStatusActive), now)
	if err != nil {
		return nil, fmt.Errorf("list due sources: %w", err)
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *PostgresStore) UpdateSourceNextRun(ctx context.Context, id string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_sources SET next_run_at = $2 WHERE id = $1`, id, next)
	if err != nil {
		return fmt.Errorf("update next run: %w", err)
	}
	return nil
}

func (s *PostgresStore) applySourceOutcome(ctx context.Context, runID string, success bool, now time.Time) error {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load run for source outcome: %w", err)
	}
	source, err := s.GetSource(ctx, run.SourceID)
	if err != nil {
		return fmt.Errorf("load source for run outcome: %w", err)
	}
	if success {
		source.ConsecutiveFailures = 0
		source.LastSuccessAt = now
		if run.IndexedCount > 0 {
			source.LastContentChangeAt = now
		}
	} else {
		source.ConsecutiveFailures++
	}
	source.LastRunAt = now
	source.LastDiscoveredCount = run.DiscoveredCount
	source.LastIndexedCount = run.IndexedCount
	source.LastDuplicateCount = run.DuplicateCount
	source.LastFailedCount = run.FailedCount
	source.NextRunAt = nextAdaptiveRunTime(now, source, run, success)
	_, err = s.db.ExecContext(ctx, `UPDATE crawl_sources
		SET next_run_at = $2,
			last_run_at = $3,
			last_success_at = $4,
			last_content_change_at = $5,
			consecutive_failures = $6,
			last_discovered_count = $7,
			last_indexed_count = $8,
			last_duplicate_count = $9,
			last_failed_count = $10
		WHERE id = $1`,
		source.ID, source.NextRunAt, source.LastRunAt, nullTime(source.LastSuccessAt), nullTime(source.LastContentChangeAt), source.ConsecutiveFailures, source.LastDiscoveredCount, source.LastIndexedCount, source.LastDuplicateCount, source.LastFailedCount)
	if err != nil {
		return fmt.Errorf("update source outcome: %w", err)
	}
	return nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
