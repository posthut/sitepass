package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reaper deletes expired token content and marks rows purged.
type Reaper struct {
	DB        *pgxpool.Pool
	BuildsDir string
	Interval  time.Duration
	Now       func() time.Time
	Logger    *slog.Logger
}

// Run periodically until ctx is cancelled.
func (r *Reaper) Run(ctx context.Context) {
	if r.Interval <= 0 {
		r.Interval = time.Minute
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	r.once(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.once(ctx)
		}
	}
}

func (r *Reaper) once(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	now := r.Now().UTC()
	rows, err := r.DB.Query(ctx, `
		SELECT id, subdomain FROM tokens
		WHERE purged_at IS NULL
		  AND (expires_at < $1 OR revoked_at IS NOT NULL)
		ORDER BY expires_at ASC
		LIMIT 100`, now)
	if err != nil {
		r.Logger.Error("reaper query", "err", err)
		return
	}
	defer rows.Close()

	type item struct {
		ID        uuid.UUID
		Subdomain string
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Subdomain); err != nil {
			r.Logger.Error("reaper scan", "err", err)
			return
		}
		batch = append(batch, it)
	}
	for _, it := range batch {
		dir := filepath.Join(r.BuildsDir, it.Subdomain)
		if err := os.RemoveAll(dir); err != nil {
			r.Logger.Error("reaper remove", "subdomain", it.Subdomain, "err", err)
			continue
		}
		if _, err := r.DB.Exec(ctx, `
			UPDATE tokens SET purged_at = $2
			WHERE id = $1 AND purged_at IS NULL`, it.ID, now); err != nil {
			r.Logger.Error("reaper mark purged", "id", it.ID, "err", err)
			continue
		}
		if _, err := r.DB.Exec(ctx, `
			INSERT INTO events (event_type, token_id, properties)
			VALUES ('token_expired', $1, jsonb_build_object('had_build', true))`, it.ID); err != nil {
			r.Logger.Warn("reaper event", "err", err)
		}
		r.Logger.Info("token purged", "subdomain", it.Subdomain)
	}
}

// Reconciler removes build directories that have no live token row.
type Reconciler struct {
	DB        *pgxpool.Pool
	BuildsDir string
	Interval  time.Duration
	Logger    *slog.Logger
}

// Run starts hourly reconciliation (and once at start).
func (c *Reconciler) Run(ctx context.Context) {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	c.once(ctx)
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.once(ctx)
		}
	}
}

func (c *Reconciler) once(ctx context.Context) {
	entries, err := os.ReadDir(c.BuildsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		c.Logger.Error("reconciler readdir", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".tmp" {
			continue
		}
		var live bool
		err := c.DB.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM tokens
				WHERE subdomain = $1
				  AND purged_at IS NULL
				  AND revoked_at IS NULL
				  AND expires_at > now()
			)`, e.Name()).Scan(&live)
		if err != nil {
			c.Logger.Error("reconciler exists", "dir", e.Name(), "err", err)
			continue
		}
		if live {
			continue
		}
		path := filepath.Join(c.BuildsDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			c.Logger.Error("reconciler remove", "dir", e.Name(), "err", err)
			continue
		}
		c.Logger.Info("reconciler removed orphan", "dir", e.Name())
	}
}
