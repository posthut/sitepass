package httpapi

import (
	"net/http"
	"strings"
)

// handleTLSAsk is used by Caddy on-demand TLS to decide whether a hostname
// may receive a certificate. Only live preview subdomains are allowed.
func (s *Server) handleTLSAsk(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		http.Error(w, "missing domain", http.StatusBadRequest)
		return
	}
	suffix := "." + s.CFG.PreviewDomain
	if domain == s.CFG.ControlDomain || domain == "www."+s.CFG.ControlDomain {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !strings.HasSuffix(domain, suffix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	label := strings.TrimSuffix(domain, suffix)
	if label == "" || strings.Contains(label, ".") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var ok bool
	err := s.Store.DB.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM tokens
			WHERE subdomain = $1
			  AND purged_at IS NULL
			  AND revoked_at IS NULL
			  AND expires_at > now()
		)`, label).Scan(&ok)
	if err != nil || !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}
