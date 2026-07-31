package lifecycle

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/posthut/sitepass/internal/health"
	"github.com/posthut/sitepass/internal/publish"
)

// Reaper deletes expired token content and marks rows purged.
// At critical disk usage it also revokes the oldest live tokens until
// usage falls below the high-water mark.
type Reaper struct {
	DB                   *pgxpool.Pool
	BuildsDir            string
	DiskHighWaterPercent int
	DiskCriticalPercent  int
	Interval             time.Duration
	Now                  func() time.Time
	Logger               *slog.Logger
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
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r.purgeExpired(ctx)
	r.relieveCriticalDisk(ctx)
}

func (r *Reaper) purgeExpired(ctx context.Context) {
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
		if err := r.removeAndPurge(ctx, it.ID, it.Subdomain, now, "token_expired", false); err != nil {
			r.Logger.Error("reaper purge", "subdomain", it.Subdomain, "err", err)
		}
	}
}

func (r *Reaper) relieveCriticalDisk(ctx context.Context) {
	if r.DiskCriticalPercent <= 0 || r.DiskHighWaterPercent <= 0 {
		return
	}
	usage, err := health.DiskUsagePercent(r.BuildsDir)
	if err != nil {
		r.Logger.Error("reaper disk usage", "err", err)
		return
	}
	if usage < r.DiskCriticalPercent {
		return
	}
	r.Logger.Warn("disk critical; revoking oldest live tokens",
		"usage_percent", usage,
		"critical", r.DiskCriticalPercent,
		"high_water", r.DiskHighWaterPercent,
	)
	now := r.Now().UTC()
	for i := 0; i < 50; i++ {
		usage, err = health.DiskUsagePercent(r.BuildsDir)
		if err != nil {
			r.Logger.Error("reaper disk usage", "err", err)
			return
		}
		if usage < r.DiskHighWaterPercent {
			r.Logger.Info("disk below high-water after capacity eviction", "usage_percent", usage)
			return
		}
		var id uuid.UUID
		var subdomain string
		err = r.DB.QueryRow(ctx, `
			SELECT id, subdomain FROM tokens
			WHERE purged_at IS NULL
			  AND revoked_at IS NULL
			  AND expires_at > $1
			ORDER BY created_at ASC
			LIMIT 1`, now).Scan(&id, &subdomain)
		if err != nil {
			r.Logger.Warn("no more live tokens to revoke for capacity", "err", err)
			return
		}
		if _, err := r.DB.Exec(ctx, `
			UPDATE tokens
			SET revoked_at = COALESCE(revoked_at, $2)
			WHERE id = $1`, id, now); err != nil {
			r.Logger.Error("capacity revoke", "id", id, "err", err)
			return
		}
		if err := r.removeAndPurge(ctx, id, subdomain, now, "token_revoked", true); err != nil {
			r.Logger.Error("capacity purge", "subdomain", subdomain, "err", err)
			return
		}
		r.Logger.Info("token revoked for capacity", "subdomain", subdomain, "usage_percent", usage)
	}
}

func (r *Reaper) removeAndPurge(ctx context.Context, id uuid.UUID, subdomain string, now time.Time, eventType string, capacity bool) error {
	dir := filepath.Join(r.BuildsDir, subdomain)
	if err := publish.ForceRemoveAll(dir); err != nil {
		return err
	}
	if _, err := r.DB.Exec(ctx, `
		UPDATE tokens SET purged_at = COALESCE(purged_at, $2)
		WHERE id = $1 AND purged_at IS NULL`, id, now); err != nil {
		return err
	}
	var props map[string]any
	if capacity {
		props = map[string]any{"by": "operator", "reason": "disk_capacity"}
	} else {
		props = map[string]any{"had_build": true}
	}
	raw, err := json.Marshal(props)
	if err != nil {
		r.Logger.Warn("reaper event marshal", "err", err)
	} else if _, err := r.DB.Exec(ctx, `
		INSERT INTO events (event_type, token_id, properties)
		VALUES ($1, $2, $3::jsonb)`, eventType, id, raw); err != nil {
		r.Logger.Warn("reaper event", "err", err)
	}
	if !capacity {
		r.Logger.Info("token purged", "subdomain", subdomain)
	}
	return nil
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
		if err := publish.ForceRemoveAll(path); err != nil {
			c.Logger.Error("reconciler remove", "dir", e.Name(), "err", err)
			continue
		}
		c.Logger.Info("reconciler removed orphan", "dir", e.Name())
	}
}
