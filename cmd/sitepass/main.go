// Command sitepass wires configuration, storage, HTTP API and lifecycle
// workers, then runs until shutdown. It contains no business rules.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/posthut/sitepass/internal/config"
	"github.com/posthut/sitepass/internal/httpapi"
	"github.com/posthut/sitepass/internal/lifecycle"
	"github.com/posthut/sitepass/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sitepass: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.BuildsDir, 0o755); err != nil {
		return fmt.Errorf("builds dir: %w", err)
	}

	store, err := storage.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer store.Close()

	migrationsDir := os.Getenv("SITEPASS_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if err := store.Migrate(ctx, migrationsDir); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	llmsPath := os.Getenv("SITEPASS_LLMS_PATH")
	if llmsPath == "" {
		llmsPath = "llms.txt"
	}
	if !filepath.IsAbs(llmsPath) {
		if abs, err := filepath.Abs(llmsPath); err == nil {
			llmsPath = abs
		}
	}

	api := &httpapi.Server{CFG: cfg, Store: store, LLMsPath: llmsPath}
	webDist := os.Getenv("SITEPASS_WEB_DIST")
	if webDist == "" {
		webDist = "web/dist"
	}
	var handler http.Handler = api.Handler()
	if st, err := os.Stat(webDist); err == nil && st.IsDir() {
		handler = api.WithStatic(webDist)
		logger.Info("serving control ui", "dir", webDist)
	}

	reaper := &lifecycle.Reaper{DB: store.DB, BuildsDir: cfg.BuildsDir, Logger: logger}
	reconciler := &lifecycle.Reconciler{DB: store.DB, BuildsDir: cfg.BuildsDir, Logger: logger}
	go reaper.Run(ctx)
	go reconciler.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		logger.Info("shutting down")
		return nil
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
