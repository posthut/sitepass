package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

type errorBody struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message"`
	Details       map[string]any `json:"details"`
	TokenConsumed bool           `json:"token_consumed"`
	Docs          string         `json:"docs"`
}

type failResponse struct {
	OK    bool      `json:"ok"`
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOK(w http.ResponseWriter, status int, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	writeJSON(w, status, payload)
}

func writeErr(w http.ResponseWriter, status int, code ErrorCode, message string, details map[string]any, docsBase string) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, failResponse{
		OK: false,
		Error: errorBody{
			Code:          code,
			Message:       message,
			Details:       details,
			TokenConsumed: false,
			Docs:          docsBase + "#" + string(code),
		},
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.Trim(r.RemoteAddr, "[]")
	}
	return host
}
