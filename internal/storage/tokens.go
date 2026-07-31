package storage

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Plan holds per-plan limits loaded from the plans table.
type Plan struct {
	ID                   int16
	Name                 string
	TokenTTLSeconds      int
	MaxArchiveBytes      int64
	MaxUnpackedBytes     int64
	MaxFiles             int
	MaxTokensPerIPHour   int
	MaxUploadsPerHour    int
	HistorySize          int16
}

// Token is a persisted upload token row.
type Token struct {
	ID           uuid.UUID
	TokenHash    []byte
	TokenPrefix  string
	PlanID       int16
	ProjectName  *string
	Subdomain    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastUploadAt *time.Time
	UploadCount  int
	Revision     int
	CreatedIP    netip.Addr
	RevokedAt    *time.Time
	PurgedAt     *time.Time
}

// ErrNotFound means no matching row.
var ErrNotFound = errors.New("not found")

// ErrConflict means a uniqueness or lock conflict.
var ErrConflict = errors.New("conflict")

// GetAnonymousPlan loads the seeded anonymous plan.
func (p *Pool) GetAnonymousPlan(ctx context.Context) (Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var plan Plan
	err := p.DB.QueryRow(ctx, `
		SELECT id, name, token_ttl_seconds, max_archive_bytes, max_unpacked_bytes,
		       max_files, max_tokens_per_ip_hour, max_uploads_per_hour, history_size
		FROM plans WHERE name = 'anonymous'`).Scan(
		&plan.ID, &plan.Name, &plan.TokenTTLSeconds, &plan.MaxArchiveBytes,
		&plan.MaxUnpackedBytes, &plan.MaxFiles, &plan.MaxTokensPerIPHour,
		&plan.MaxUploadsPerHour, &plan.HistorySize,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("get anonymous plan: %w", err)
	}
	return plan, nil
}

// InsertToken persists a new token row.
func (p *Pool) InsertToken(ctx context.Context, t Token) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.DB.Exec(ctx, `
		INSERT INTO tokens (
			id, token_hash, token_prefix, plan_id, project_name, subdomain,
			created_at, expires_at, created_ip
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		t.ID, t.TokenHash, t.TokenPrefix, t.PlanID, t.ProjectName, t.Subdomain,
		t.CreatedAt, t.ExpiresAt, t.CreatedIP.String(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

// GetTokenByHash loads a token by SHA-256 hash.
func (p *Pool) GetTokenByHash(ctx context.Context, hash []byte) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return scanToken(p.DB.QueryRow(ctx, `
		SELECT id, token_hash, token_prefix, plan_id, project_name, subdomain,
		       created_at, expires_at, last_upload_at, upload_count, revision,
		       created_ip, revoked_at, purged_at
		FROM tokens WHERE token_hash = $1`, hash))
}

// LockTokenForUpload locks the token row for update without waiting.
func (p *Pool) LockTokenForUpload(ctx context.Context, tx pgx.Tx, hash []byte) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	t, err := scanToken(tx.QueryRow(ctx, `
		SELECT id, token_hash, token_prefix, plan_id, project_name, subdomain,
		       created_at, expires_at, last_upload_at, upload_count, revision,
		       created_ip, revoked_at, purged_at
		FROM tokens WHERE token_hash = $1 FOR UPDATE NOWAIT`, hash))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			return Token{}, ErrConflict
		}
		return Token{}, err
	}
	return t, nil
}

// MarkUploadAccepted updates counters after a successful publish.
func (p *Pool) MarkUploadAccepted(ctx context.Context, tx pgx.Tx, tokenID uuid.UUID, revision int, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := tx.Exec(ctx, `
		UPDATE tokens
		SET revision = $2, upload_count = upload_count + 1, last_upload_at = $3
		WHERE id = $1`, tokenID, revision, now)
	if err != nil {
		return fmt.Errorf("mark upload: %w", err)
	}
	return nil
}

// InsertBuild stores a build row.
func (p *Pool) InsertBuild(ctx context.Context, tx pgx.Tx, tokenID uuid.UUID, revision int, sha256 []byte, size int64, files int, uploadedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := tx.Exec(ctx, `
		INSERT INTO builds (token_id, revision, archive_sha256, size_bytes, file_count, uploaded_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		tokenID, revision, sha256, size, files, uploadedAt,
	)
	if err != nil {
		return fmt.Errorf("insert build: %w", err)
	}
	return nil
}

// RevokeToken sets revoked_at and purged_at for immediate removal tracking.
func (p *Pool) RevokeToken(ctx context.Context, hash []byte, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, err := p.DB.Exec(ctx, `
		UPDATE tokens
		SET revoked_at = COALESCE(revoked_at, $2), purged_at = COALESCE(purged_at, $2)
		WHERE token_hash = $1`, hash, at)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementRateLimit increments a fixed one-hour bucket and returns the new counter.
func (p *Pool) IncrementRateLimit(ctx context.Context, key string, windowStart time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var counter int
	err := p.DB.QueryRow(ctx, `
		INSERT INTO rate_limit_buckets (bucket_key, window_start, counter)
		VALUES ($1, $2, 1)
		ON CONFLICT (bucket_key) DO UPDATE
		SET counter = CASE
			WHEN rate_limit_buckets.window_start = EXCLUDED.window_start
			THEN rate_limit_buckets.counter + 1
			ELSE 1
		END,
		window_start = CASE
			WHEN rate_limit_buckets.window_start = EXCLUDED.window_start
			THEN rate_limit_buckets.window_start
			ELSE EXCLUDED.window_start
		END
		RETURNING counter`, key, windowStart).Scan(&counter)
	if err != nil {
		return 0, fmt.Errorf("rate limit: %w", err)
	}
	return counter, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanToken(row scannable) (Token, error) {
	var t Token
	err := row.Scan(
		&t.ID, &t.TokenHash, &t.TokenPrefix, &t.PlanID, &t.ProjectName, &t.Subdomain,
		&t.CreatedAt, &t.ExpiresAt, &t.LastUploadAt, &t.UploadCount, &t.Revision,
		&t.CreatedIP, &t.RevokedAt, &t.PurgedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, fmt.Errorf("scan token: %w", err)
	}
	return t, nil
}
