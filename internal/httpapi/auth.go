package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/posthut/sitepass/internal/storage"
	"github.com/posthut/sitepass/internal/token"
)

const (
	sessionCookieName = "sitepass_session"
	sessionTTL        = 30 * 24 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

type authCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req authCredentials
	if err := decodeAuthBody(r, &req); err != nil {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeAuthValidation, "Request body is not valid JSON.", nil)
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(username) {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeAuthValidation,
			"Username must be 3–32 characters: letters, digits, underscore.", nil)
		return
	}
	if utf8.RuneCountInString(req.Password) < 8 {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeAuthValidation,
			"Password must be at least 8 characters.", nil)
		return
	}
	hash, err := storage.HashPassword(req.Password)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	user, err := s.Store.CreateUser(r.Context(), username, hash)
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			s.writeAPIError(w, http.StatusConflict, CodeAuthConflict, "Username is already taken.", nil)
			return
		}
		s.writeInternal(w, err)
		return
	}
	if err := s.issueSession(w, r, user); err != nil {
		s.writeInternal(w, err)
		return
	}
	writeOK(w, http.StatusCreated, map[string]any{
		"user": map[string]any{"id": user.ID, "username": user.Username},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authCredentials
	if err := decodeAuthBody(r, &req); err != nil {
		s.writeAPIError(w, http.StatusUnprocessableEntity, CodeAuthValidation, "Request body is not valid JSON.", nil)
		return
	}
	user, err := s.Store.GetUserByUsername(r.Context(), strings.TrimSpace(req.Username))
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			s.writeInternal(w, err)
			return
		}
		s.writeAPIError(w, http.StatusUnauthorized, CodeAuthInvalid, "Username or password is incorrect.", nil)
		return
	}
	if !storage.CheckPassword(user.PasswordHash, req.Password) {
		s.writeAPIError(w, http.StatusUnauthorized, CodeAuthInvalid, "Username or password is incorrect.", nil)
		return
	}
	if err := s.issueSession(w, r, user); err != nil {
		s.writeInternal(w, err)
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": user.ID, "username": user.Username},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if raw, ok := sessionPlaintext(r); ok {
		hash := token.HashPlaintext(raw)
		_ = s.Store.RevokeSession(r.Context(), hash[:], s.now())
	}
	clearSessionCookie(w, s.CFG.ControlDomain)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		s.writeAPIError(w, http.StatusUnauthorized, CodeAuthUnauthorized, "Not signed in.", nil)
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": user.ID, "username": user.Username},
	})
}

func (s *Server) handleMyTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		s.writeAPIError(w, http.StatusUnauthorized, CodeAuthUnauthorized, "Not signed in.", nil)
		return
	}
	rows, err := s.Store.ListUserTokens(r.Context(), user.ID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	now := s.now()
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		live := t.RevokedAt == nil && t.ExpiresAt.After(now)
		name := ""
		if t.ProjectName != nil {
			name = *t.ProjectName
		}
		expSec := int(t.ExpiresAt.Sub(now).Seconds())
		if expSec < 0 {
			expSec = 0
		}
		items = append(items, map[string]any{
			"token_id":           t.ID.String(),
			"project_name":       name,
			"preview_url":        "https://" + t.Subdomain + "." + s.CFG.PreviewDomain,
			"subdomain":          t.Subdomain,
			"expires_at":         t.ExpiresAt.Format(time.RFC3339),
			"expires_in_seconds": expSec,
			"revision":           t.Revision,
			"has_build":          t.Revision > 0,
			"live":               live,
		})
	}
	writeOK(w, http.StatusOK, map[string]any{"tokens": items})
}

func decodeAuthBody(r *http.Request, dst *authCredentials) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4096))
	return dec.Decode(dst)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user storage.User) error {
	raw, err := randomSessionToken()
	if err != nil {
		return err
	}
	hash := token.HashPlaintext(raw)
	addr, err := netip.ParseAddr(clientIP(r))
	if err != nil {
		addr = netip.MustParseAddr("127.0.0.1")
	}
	expires := s.now().Add(sessionTTL)
	if _, err := s.Store.CreateSession(r.Context(), user.ID, hash[:], expires, addr); err != nil {
		return err
	}
	// Host-only cookie so it is not sent to preview subdomains.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, _ string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionPlaintext(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func (s *Server) currentUser(r *http.Request) (storage.User, bool) {
	raw, ok := sessionPlaintext(r)
	if !ok {
		return storage.User{}, false
	}
	hash := token.HashPlaintext(raw)
	sess, err := s.Store.GetSessionByHash(r.Context(), hash[:], s.now())
	if err != nil {
		return storage.User{}, false
	}
	user, err := s.Store.GetUserByID(r.Context(), sess.UserID)
	if err != nil {
		return storage.User{}, false
	}
	return user, true
}

func randomSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
