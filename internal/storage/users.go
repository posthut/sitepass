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
	"golang.org/x/crypto/bcrypt"
)

// User is an authenticated account.
type User struct {
	ID           int64
	Username     string
	Email        *string
	PasswordHash string
	PlanID       int16
	CreatedAt    time.Time
}

// Session is a server-side login session.
type Session struct {
	ID        uuid.UUID
	UserID    int64
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

const (
	PlanAnonymous  int16 = 1
	PlanRegistered int16 = 2
	bcryptCost           = 12
)

// HashPassword returns a bcrypt hash for storage.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// CheckPassword compares plaintext with a stored bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// CreateUser inserts a registered user on the registered plan.
func (p *Pool) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var u User
	err := p.DB.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, plan_id)
		VALUES ($1, $2, $3)
		RETURNING id, username, email, password_hash, plan_id, created_at`,
		username, passwordHash, PlanRegistered,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.PlanID, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrConflict
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUserByUsername loads a user by username.
func (p *Pool) GetUserByUsername(ctx context.Context, username string) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var u User
	err := p.DB.QueryRow(ctx, `
		SELECT id, username, email, password_hash, plan_id, created_at
		FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.PlanID, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// GetUserByID loads a user by id.
func (p *Pool) GetUserByID(ctx context.Context, id int64) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var u User
	err := p.DB.QueryRow(ctx, `
		SELECT id, username, email, password_hash, plan_id, created_at
		FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.PlanID, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

// CreateSession stores a new session row.
func (p *Pool) CreateSession(ctx context.Context, userID int64, tokenHash []byte, expiresAt time.Time, ip netip.Addr) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var s Session
	err := p.DB.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at, created_ip)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, token_hash, created_at, expires_at, revoked_at`,
		userID, tokenHash, expiresAt, ip.String(),
	).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return s, nil
}

// GetSessionByHash returns a live session.
func (p *Pool) GetSessionByHash(ctx context.Context, hash []byte, now time.Time) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var s Session
	err := p.DB.QueryRow(ctx, `
		SELECT id, user_id, token_hash, created_at, expires_at, revoked_at
		FROM sessions
		WHERE token_hash = $1`, hash,
	).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	if s.RevokedAt != nil || !s.ExpiresAt.After(now) {
		return Session{}, ErrNotFound
	}
	return s, nil
}

// RevokeSession marks a session revoked.
func (p *Pool) RevokeSession(ctx context.Context, hash []byte, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.DB.Exec(ctx, `
		UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2)
		WHERE token_hash = $1`, hash, at)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// ListUserTokens returns non-purged tokens for a user, newest first.
func (p *Pool) ListUserTokens(ctx context.Context, userID int64) ([]Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := p.DB.Query(ctx, `
		SELECT id, token_hash, token_prefix, plan_id, project_name, subdomain,
		       created_at, expires_at, last_upload_at, upload_count, revision,
		       created_ip, revoked_at, purged_at
		FROM tokens
		WHERE user_id = $1 AND purged_at IS NULL
		ORDER BY created_at DESC
		LIMIT 50`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user tokens: %w", err)
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetPlanByID loads a plan row.
func (p *Pool) GetPlanByID(ctx context.Context, id int16) (Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var plan Plan
	err := p.DB.QueryRow(ctx, `
		SELECT id, name, token_ttl_seconds, max_archive_bytes, max_unpacked_bytes,
		       max_files, max_tokens_per_ip_hour, max_uploads_per_hour, history_size
		FROM plans WHERE id = $1`, id).Scan(
		&plan.ID, &plan.Name, &plan.TokenTTLSeconds, &plan.MaxArchiveBytes,
		&plan.MaxUnpackedBytes, &plan.MaxFiles, &plan.MaxTokensPerIPHour,
		&plan.MaxUploadsPerHour, &plan.HistorySize,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("get plan: %w", err)
	}
	return plan, nil
}
