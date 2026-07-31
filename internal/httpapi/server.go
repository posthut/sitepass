package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/posthut/sitepass/internal/config"
	"github.com/posthut/sitepass/internal/health"
	"github.com/posthut/sitepass/internal/publish"
	"github.com/posthut/sitepass/internal/storage"
	"github.com/posthut/sitepass/internal/token"
)

// Server serves the public HTTP API.
type Server struct {
	CFG      config.Config
	Store    *storage.Pool
	LLMsPath string
	DocsBase string
	Now      func() time.Time

	uploadSem     chan struct{}
	uploadSemOnce sync.Once
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Server) docsBase() string {
	if s.DocsBase != "" {
		return s.DocsBase
	}
	return "https://" + s.CFG.ControlDomain + "/llms.txt"
}

func (s *Server) writeAPIError(w http.ResponseWriter, status int, code ErrorCode, message string, details map[string]any) {
	writeErr(w, status, code, message, details, s.docsBase())
}

func (s *Server) writeInternal(w http.ResponseWriter, err error) {
	slog.Error("internal_error", "err", err)
	s.writeAPIError(w, http.StatusInternalServerError, CodeInternalError, "An unexpected error occurred.", nil)
}

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	mux.HandleFunc("GET /api/v1/me/tokens", s.handleMyTokens)
	mux.HandleFunc("POST /api/v1/tokens", s.handleCreateToken)
	mux.HandleFunc("POST /api/v1/upload", s.handleUpload)
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("DELETE /api/v1/token", s.handleDeleteToken)
	mux.HandleFunc("GET /llms.txt", s.handleLLMs)
	mux.HandleFunc("GET /api/v1/internal/tls-ask", s.handleTLSAsk)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	usage, err := health.DiskUsagePercent(s.CFG.BuildsDir)
	status := "healthy"
	accepting := true
	if err != nil {
		status = "degraded"
		usage = 0
	} else if s.CFG.ReadOnly {
		status = "read_only"
		accepting = false
	} else if usage >= s.CFG.DiskCriticalPercent {
		status = "degraded"
		accepting = false
	} else if usage >= s.CFG.DiskHighWaterPercent {
		status = "degraded"
	}
	if accepting && s.CFG.MinAvailableMemMB > 0 {
		if avail, err := health.MemAvailableMB(); err == nil && avail < s.CFG.MinAvailableMemMB {
			status = "degraded"
			accepting = false
		}
	}
	writeOK(w, http.StatusOK, map[string]any{
		"status":                   status,
		"accepting_uploads":        accepting,
		"disk_usage_percent":       usage,
		"abuse_contact":            s.CFG.AbuseContact,
		"max_concurrent_uploads":   s.CFG.MaxConcurrentUploads,
		"upload_slots_in_use":      s.uploadSlotsInUse(),
	})
}

func (s *Server) uploadSlots() chan struct{} {
	s.uploadSemOnce.Do(func() {
		n := s.CFG.MaxConcurrentUploads
		if n <= 0 {
			n = 2
		}
		s.uploadSem = make(chan struct{}, n)
	})
	return s.uploadSem
}

func (s *Server) tryAcquireUploadSlot() bool {
	select {
	case s.uploadSlots() <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseUploadSlot() {
	select {
	case <-s.uploadSlots():
	default:
	}
}

func (s *Server) uploadSlotsInUse() int {
	return len(s.uploadSlots())
}

func (s *Server) rejectIfOverloaded(w http.ResponseWriter) bool {
	if s.CFG.MinAvailableMemMB > 0 {
		if avail, err := health.MemAvailableMB(); err == nil && avail < s.CFG.MinAvailableMemMB {
			w.Header().Set("Retry-After", "30")
			s.writeAPIError(w, http.StatusServiceUnavailable, CodeServiceOverloaded,
				fmt.Sprintf("Host memory is low (%d MiB available). Retry shortly.", avail), nil)
			return true
		}
	}
	return false
}

func (s *Server) handleLLMs(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(s.LLMsPath)
	if err != nil {
		http.Error(w, "llms.txt unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

type createTokenRequest struct {
	ProjectName string `json:"project_name"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req createTokenRequest
	if r.Body != nil {
		defer r.Body.Close()
		dec := json.NewDecoder(io.LimitReader(r.Body, 4096))
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			s.writeAPIError(w, http.StatusUnprocessableEntity, CodeProjectNameInvalid, "Request body is not valid JSON.", nil)
			return
		}
	}
	if len([]rune(req.ProjectName)) > 48 {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeProjectNameInvalid, "Project name is longer than 48 characters.", nil)
		return
	}

	if s.CFG.ReadOnly {
		s.writeAPIError(w, http.StatusServiceUnavailable, CodeServiceReadOnly,
			"Service is in read-only mode. New tokens are not accepted.", nil)
		return
	}
	if s.rejectIfOverloaded(w) {
		return
	}

	usage, _ := health.DiskUsagePercent(s.CFG.BuildsDir)
	if usage >= s.CFG.DiskHighWaterPercent {
		s.writeAPIError(w, http.StatusServiceUnavailable, CodeStorageCapacityExceeded,
			fmt.Sprintf("Disk usage is %d%%. New tokens are not accepted above %d%%.", usage, s.CFG.DiskHighWaterPercent), nil)
		return
	}

	var userID *int64
	plan, err := s.Store.GetAnonymousPlan(ctx)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if user, ok := s.currentUser(r); ok {
		registered, err := s.Store.GetPlanByID(ctx, storage.PlanRegistered)
		if err != nil {
			s.writeInternal(w, err)
			return
		}
		plan = registered
		uid := user.ID
		userID = &uid
	}

	addr, err := netip.ParseAddr(clientIP(r))
	if err != nil {
		addr = netip.MustParseAddr("127.0.0.1")
	}
	count, err := s.Store.IncrementRateLimit(ctx, "tokens:"+addr.String(), s.now().Truncate(time.Hour))
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if count > plan.MaxTokensPerIPHour {
		w.Header().Set("Retry-After", "3600")
		s.writeAPIError(w, http.StatusTooManyRequests, CodeRateLimited, "Token creation rate limit exceeded for this IP.", nil)
		return
	}

	gen, err := token.Generate(req.ProjectName)
	if err != nil {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeProjectNameInvalid, "Project name produces an empty slug.", nil)
		return
	}

	now := s.now()
	ttl := plan.TokenTTLSeconds
	if userID == nil && s.CFG.TokenTTLSeconds > 0 {
		ttl = s.CFG.TokenTTLSeconds
	}
	var projectName *string
	if n := strings.TrimSpace(req.ProjectName); n != "" {
		projectName = &n
	}
	row := storage.Token{
		ID:          uuid.New(),
		TokenHash:   gen.Hash[:],
		TokenPrefix: gen.Prefix,
		UserID:      userID,
		PlanID:      plan.ID,
		ProjectName: projectName,
		Subdomain:   gen.Subdomain,
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Duration(ttl) * time.Second),
		CreatedIP:   addr,
	}
	if err := s.Store.InsertToken(ctx, row); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			s.writeAPIError(w, http.StatusUnprocessableEntity, CodeProjectNameInvalid, "Subdomain collision; try another project name.", nil)
			return
		}
		s.writeInternal(w, err)
		return
	}
	if err := seedWaitingPage(s.CFG.BuildsDir, gen.Subdomain); err != nil {
		s.writeInternal(w, err)
		return
	}

	writeOK(w, http.StatusCreated, map[string]any{
		"token":              gen.Plaintext,
		"token_id":           row.ID.String(),
		"preview_url":        fmt.Sprintf("https://%s.%s", gen.Subdomain, s.CFG.PreviewDomain),
		"expires_at":         row.ExpiresAt.Format(time.RFC3339),
		"expires_in_seconds": ttl,
		"limits": map[string]any{
			"max_archive_bytes":  plan.MaxArchiveBytes,
			"max_unpacked_bytes": plan.MaxUnpackedBytes,
			"max_files":          plan.MaxFiles,
		},
	})
}

func (s *Server) bearerHash(r *http.Request) ([32]byte, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return [32]byte{}, false
	}
	raw := strings.TrimSpace(h[len(prefix):])
	if raw == "" {
		return [32]byte{}, false
	}
	return token.HashPlaintext(raw), true
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	hash, ok := s.bearerHash(r)
	if !ok {
		s.writeAPIError(w, http.StatusNotFound, CodeTokenNotFound, "Token was not found.", nil)
		return
	}
	t, err := s.Store.GetTokenByHash(r.Context(), hash[:])
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.writeAPIError(w, http.StatusNotFound, CodeTokenNotFound, "Token was not found.", nil)
			return
		}
		s.writeInternal(w, err)
		return
	}
	if s.rejectInactiveToken(w, t) {
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"preview_url":        fmt.Sprintf("https://%s.%s", t.Subdomain, s.CFG.PreviewDomain),
		"expires_at":         t.ExpiresAt.Format(time.RFC3339),
		"expires_in_seconds": int(t.ExpiresAt.Sub(s.now()).Seconds()),
		"revision":           t.Revision,
		"has_build":          t.Revision > 0,
		"upload_count":       t.UploadCount,
	})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	hash, ok := s.bearerHash(r)
	if !ok {
		s.writeAPIError(w, http.StatusNotFound, CodeTokenNotFound, "Token was not found.", nil)
		return
	}
	t, err := s.Store.GetTokenByHash(r.Context(), hash[:])
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.writeAPIError(w, http.StatusNotFound, CodeTokenNotFound, "Token was not found.", nil)
			return
		}
		s.writeInternal(w, err)
		return
	}
	if err := s.Store.RevokeToken(r.Context(), hash[:], s.now()); err != nil {
		s.writeInternal(w, err)
		return
	}
	_ = publish.ForceRemoveAll(filepath.Join(s.CFG.BuildsDir, t.Subdomain))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rejectInactiveToken(w http.ResponseWriter, t storage.Token) bool {
	if t.RevokedAt != nil {
		s.writeAPIError(w, http.StatusGone, CodeTokenRevoked, "Token was revoked.", nil)
		return true
	}
	if !t.ExpiresAt.After(s.now()) || t.PurgedAt != nil {
		s.writeAPIError(w, http.StatusGone, CodeTokenExpired, "Token lifetime has elapsed.", nil)
		return true
	}
	return false
}
