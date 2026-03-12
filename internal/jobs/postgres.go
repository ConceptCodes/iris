package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"iris/config"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(ctx context.Context, dsn string, pool config.PostgresPool) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("job store dsn is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	configurePostgresPool(db, pool)

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

func configurePostgresPool(db *sql.DB, pool config.PostgresPool) {
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
}

func (s *PostgresStore) Enqueue(ctx context.Context, job Job) (Job, error) {
	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.Status == "" {
		job.Status = StatusPending
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = defaultMaxAttempts
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = now
	}

	const query = `
		INSERT INTO jobs (
			id, type, status, dedup_key, payload_json, attempts, max_attempts,
			available_at, leased_until, last_error, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 0, $6, $7, NULL, '', $8, $8)
		ON CONFLICT (dedup_key) WHERE dedup_key <> '' DO NOTHING
	`
	result, err := s.db.ExecContext(
		ctx,
		query,
		job.ID,
		string(job.Type),
		string(job.Status),
		job.DedupKey,
		[]byte(job.PayloadJSON),
		job.MaxAttempts,
		job.AvailableAt,
		now,
	)
	if err != nil {
		return Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows == 0 && job.DedupKey != "" {
		return job, nil
	}

	job.Attempts = 0
	job.CreatedAt = now
	job.UpdatedAt = now
	return job, nil
}

func (s *PostgresStore) LeaseNext(ctx context.Context, now time.Time, leaseDuration time.Duration, allowedTypes ...Type) (Job, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	query, args := buildLeaseQuery(now, allowedTypes)
	row := tx.QueryRowContext(ctx, query, args...)

	var job Job
	if err := scanJob(row, &job); err != nil {
		if err == sql.ErrNoRows {
			return Job{}, false, nil
		}
		return Job{}, false, fmt.Errorf("lease query: %w", err)
	}

	job.Status = StatusLeased
	job.Attempts++
	job.LeasedUntil = now.Add(leaseDuration)
	job.UpdatedAt = now

	const updateQuery = `
		UPDATE jobs
		SET status = $2, attempts = $3, leased_until = $4, updated_at = $5
		WHERE id = $1
	`
	if _, err := tx.ExecContext(
		ctx,
		updateQuery,
		job.ID,
		string(job.Status),
		job.Attempts,
		job.LeasedUntil,
		job.UpdatedAt,
	); err != nil {
		return Job{}, false, fmt.Errorf("lease update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("commit lease: %w", err)
	}
	return job, true, nil
}

func (s *PostgresStore) MarkSucceeded(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET status = $2, leased_until = NULL, updated_at = $3 WHERE id = $1`,
		id,
		string(StatusSucceeded),
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("mark succeeded: %w", err)
	}
	return nil
}

func (s *PostgresStore) MarkFailed(ctx context.Context, id string, failure error, retryAt time.Time) (Status, error) {
	var status string
	err := s.db.QueryRowContext(
		ctx,
		`UPDATE jobs
		 SET status = CASE WHEN attempts >= max_attempts THEN $2 ELSE $3 END,
		     last_error = $4,
		     available_at = $5,
		     leased_until = NULL,
		     updated_at = $6
		 WHERE id = $1
		 RETURNING status`,
		id,
		string(StatusDeadLetter),
		string(StatusPending),
		failure.Error(),
		retryAt,
		time.Now().UTC(),
	).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("mark failed: %w", err)
	}
	return Status(status), nil
}

func (s *PostgresStore) MarkDeadLetter(ctx context.Context, id string, failure error) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET status = $2, last_error = $3, leased_until = NULL, updated_at = $4
		 WHERE id = $1`,
		id,
		string(StatusDeadLetter),
		failure.Error(),
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("mark dead letter: %w", err)
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
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			dedup_key TEXT NOT NULL DEFAULT '',
			payload_json JSONB NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			available_at TIMESTAMPTZ NOT NULL,
			leased_until TIMESTAMPTZ NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS jobs_lease_idx
		ON jobs (status, available_at, leased_until, type);
		CREATE UNIQUE INDEX IF NOT EXISTS jobs_dedup_idx
		ON jobs (dedup_key)
		WHERE dedup_key <> '';
	`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure jobs schema: %w", err)
	}
	return nil
}

func buildLeaseQuery(now time.Time, allowedTypes []Type) (string, []any) {
	base := `
		SELECT id, type, status, dedup_key, payload_json, attempts, max_attempts,
		       available_at, leased_until, last_error, created_at, updated_at
		FROM jobs
		WHERE status = $1
		  AND available_at <= $2
	`
	args := []any{string(StatusPending), now}
	if len(allowedTypes) > 0 {
		placeholders := make([]string, 0, len(allowedTypes))
		for _, jobType := range allowedTypes {
			args = append(args, string(jobType))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		base += " AND type IN (" + strings.Join(placeholders, ", ") + ")"
	}
	base += " ORDER BY available_at ASC, created_at ASC FOR UPDATE SKIP LOCKED LIMIT 1"
	return base, args
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner, job *Job) error {
	var jobType, status string
	if err := row.Scan(
		&job.ID,
		&jobType,
		&status,
		&job.DedupKey,
		&job.PayloadJSON,
		&job.Attempts,
		&job.MaxAttempts,
		&job.AvailableAt,
		&job.LeasedUntil,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	); err != nil {
		return err
	}
	job.Type = Type(jobType)
	job.Status = Status(status)
	return nil
}
