package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/posthut/sitepass/internal/archive"
	"github.com/posthut/sitepass/internal/health"
	"github.com/posthut/sitepass/internal/intake"
	"github.com/posthut/sitepass/internal/publish"
	"github.com/posthut/sitepass/internal/storage"
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.CFG.ReadOnly {
		s.writeAPIError(w, http.StatusServiceUnavailable, CodeServiceReadOnly,
			"Service is in read-only mode. Uploads are not accepted.", nil)
		return
	}
	if s.rejectIfOverloaded(w) {
		return
	}
	usage, _ := health.DiskUsagePercent(s.CFG.BuildsDir)
	if usage >= s.CFG.DiskCriticalPercent {
		w.Header().Set("Retry-After", "60")
		s.writeAPIError(w, http.StatusServiceUnavailable, CodeStorageCapacityExceeded,
			fmt.Sprintf("Disk usage is %d%% (critical). Uploads are paused until space is freed.", usage), nil)
		return
	}
	if !s.tryAcquireUploadSlot() {
		w.Header().Set("Retry-After", "15")
		s.writeAPIError(w, http.StatusServiceUnavailable, CodeServiceOverloaded,
			"Too many uploads in progress on this host. Retry shortly.", nil)
		return
	}
	defer s.releaseUploadSlot()

	hash, ok := s.bearerHash(r)
	if !ok {
		s.writeAPIError(w, http.StatusNotFound, CodeTokenNotFound, "Token was not found.", nil)
		return
	}

	ctx := r.Context()
	tx, err := s.Store.DB.Begin(ctx)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	defer tx.Rollback(ctx)

	t, err := s.Store.LockTokenForUpload(ctx, tx, hash[:])
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			s.writeAPIError(w, http.StatusConflict, CodeUploadInProgress, "Another upload is being processed for this token.", nil)
			return
		}
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

	plan, err := s.Store.GetPlanByID(ctx, t.PlanID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			plan, err = s.Store.GetAnonymousPlan(ctx)
		}
		if err != nil {
			s.writeInternal(w, err)
			return
		}
	}

	count, err := s.Store.IncrementRateLimit(ctx, "uploads:"+t.ID.String(), s.now().Truncate(time.Hour))
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if count > plan.MaxUploadsPerHour {
		w.Header().Set("Retry-After", "3600")
		s.writeAPIError(w, http.StatusTooManyRequests, CodeRateLimited, "Upload rate limit exceeded for this token.", nil)
		return
	}

	maxArchive := plan.MaxArchiveBytes
	if s.CFG.MaxArchiveBytes > 0 && s.CFG.MaxArchiveBytes < maxArchive {
		maxArchive = s.CFG.MaxArchiveBytes
	}

	tmpDir := filepath.Join(s.CFG.BuildsDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		s.writeInternal(w, err)
		return
	}

	body, expectedSHA, err := openUploadBody(r, maxArchive)
	if err != nil {
		s.mapIntakeError(w, err, maxArchive)
		return
	}
	defer body.Close()

	saved, err := intake.SaveStream(tmpDir, body, maxArchive)
	if err != nil {
		s.mapIntakeError(w, err, maxArchive)
		return
	}
	defer saved.Close()

	sum, err := hashFile(saved.Path)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if expectedSHA != "" && !strings.EqualFold(expectedSHA, hex.EncodeToString(sum)) {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeChecksumMismatch, "X-Content-SHA256 does not match the uploaded body.", nil)
		return
	}

	staging, err := os.MkdirTemp(tmpDir, "stage-*")
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	defer os.RemoveAll(staging)

	limits := archive.Limits{
		MaxUnpackedBytes: plan.MaxUnpackedBytes,
		MaxFiles:         plan.MaxFiles,
		MaxRatio:         200,
		MaxPathDepth:     32,
		MaxSegmentBytes:  255,
		MaxPathBytes:     4096,
	}
	result, err := archive.UnpackFile(saved.Path, staging, saved.Size, limits)
	if err != nil {
		s.mapArchiveError(w, err, plan)
		return
	}

	revision := t.Revision + 1
	if _, err := publish.SwitchAtomically(s.CFG.BuildsDir, t.Subdomain, result.SiteRoot, revision); err != nil {
		s.writeInternal(w, err)
		return
	}
	// SiteRoot may have been moved; avoid double-remove of staging parent when nested.
	if result.SiteRoot != staging {
		_ = os.RemoveAll(staging)
	}

	now := s.now()
	if err := s.Store.InsertBuild(ctx, tx, t.ID, revision, sum, saved.Size, result.FileCount, now); err != nil {
		s.writeInternal(w, err)
		return
	}
	if err := s.Store.MarkUploadAccepted(ctx, tx, t.ID, revision, now); err != nil {
		s.writeInternal(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.writeInternal(w, err)
		return
	}

	warnings := make([]map[string]any, 0, len(result.Warnings))
	for _, wn := range result.Warnings {
		warnings = append(warnings, map[string]any{
			"code":    wn.Code,
			"message": wn.Message,
			"details": map[string]any{"paths": wn.Paths},
		})
	}
	writeOK(w, http.StatusOK, map[string]any{
		"preview_url":        fmt.Sprintf("https://%s.%s", t.Subdomain, s.CFG.PreviewDomain),
		"revision":           revision,
		"expires_at":         t.ExpiresAt.Format(time.RFC3339),
		"expires_in_seconds": int(t.ExpiresAt.Sub(now).Seconds()),
		"file_count":         result.FileCount,
		"size_bytes":         saved.Size,
		"warnings":           warnings,
	})
}

func openUploadBody(r *http.Request, maxBytes int64) (io.ReadCloser, string, error) {
	expected := strings.TrimSpace(r.Header.Get("X-Content-SHA256"))
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		media, params, err := mime.ParseMediaType(ct)
		if err != nil || media != "multipart/form-data" {
			return nil, "", fmt.Errorf("%w: bad multipart type", archive.ErrMalformed)
		}
		mr := multipart.NewReader(io.LimitReader(r.Body, maxBytes+1024), params["boundary"])
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return nil, "", fmt.Errorf("%w: missing archive part", archive.ErrMalformed)
			}
			if err != nil {
				return nil, "", fmt.Errorf("%w: %v", archive.ErrMalformed, err)
			}
			if part.FormName() == "archive" {
				return part, expected, nil
			}
			_ = part.Close()
		}
	}
	return http.MaxBytesReader(nil, r.Body, maxBytes+1), expected, nil
}

func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func (s *Server) mapIntakeError(w http.ResponseWriter, err error, maxArchive int64) {
	if errors.Is(err, intake.ErrTooLarge) {
		s.writeAPIError(w, http.StatusRequestEntityTooLarge, CodeArchiveTooLarge,
			fmt.Sprintf("Archive exceeds the limit of %d bytes.", maxArchive), nil)
		return
	}
	s.writeAPIError(w, http.StatusUnprocessableEntity, CodeArchiveMalformed, "Archive could not be read.", nil)
}

func (s *Server) mapArchiveError(w http.ResponseWriter, err error, plan storage.Plan) {
	switch {
	case errors.Is(err, archive.ErrUnsupportedFormat):
		s.writeAPIError(w, http.StatusUnsupportedMediaType, CodeUnsupportedFormat, "Format is not tar.gz, zip, tar.zst or html.", nil)
	case errors.Is(err, archive.ErrUnpackedTooLarge), errors.Is(err, archive.ErrCompressionBomb):
		s.writeAPIError(w, http.StatusRequestEntityTooLarge, CodeArchiveUnpackedTooLarge,
			fmt.Sprintf("Unpacked size exceeds the limit of %d bytes.", plan.MaxUnpackedBytes), nil)
	case errors.Is(err, archive.ErrTooManyFiles):
		s.writeAPIError(w, http.StatusRequestEntityTooLarge, CodeArchiveTooManyFiles,
			fmt.Sprintf("Archive exceeds the limit of %d files.", plan.MaxFiles), nil)
	case errors.Is(err, archive.ErrUnsafeEntry):
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeArchiveUnsafeEntry, "Archive contains an unsafe entry.", nil)
	case errors.Is(err, archive.ErrEntrypointMissing):
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeEntrypointNotFound, "No index.html at the root or one level down.", nil)
	case errors.Is(err, archive.ErrMalformed):
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeArchiveMalformed, "Archive could not be read as the detected format.", nil)
	default:
		s.writeInternal(w, err)
	}
}
