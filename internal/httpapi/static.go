package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// WithStatic serves the control SPA from distDir for non-API paths.
func (s *Server) WithStatic(distDir string) http.Handler {
	api := s.Handler()
	fileServer := http.FileServer(http.Dir(distDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/llms.txt" {
			api.ServeHTTP(w, r)
			return
		}
		path := filepath.Join(distDir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}
