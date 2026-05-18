package httptransport

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONAPIError(w, http.StatusNotFound, "route-not-found", "Route not found", "not found", "")
		return
	}
	dist := s.settings.FrontendDist
	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == "." || path == "" {
		path = "index.html"
	}
	target := filepath.Join(dist, path)
	if fileExists(target) {
		http.ServeFile(w, r, target)
		return
	}
	index := filepath.Join(dist, "index.html")
	if fileExists(index) {
		http.ServeFile(w, r, index)
		return
	}
	http.Error(w, "frontend dist not found", http.StatusNotFound)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
