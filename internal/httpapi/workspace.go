package httpapi

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) withWorkspace(next http.Handler) http.Handler {
	webDir := strings.TrimSpace(s.webDir)
	if webDir == "" {
		return next
	}
	root := filepath.Clean(webDir)
	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if workspaceReservedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		relative := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if relative == "" {
			http.ServeFile(w, r, filepath.Join(root, "index.html"))
			return
		}
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		if !isWithinDir(root, candidate) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if err == nil && info.IsDir() {
			index := filepath.Join(candidate, "index.html")
			if indexInfo, indexErr := os.Stat(index); indexErr == nil && !indexInfo.IsDir() {
				http.ServeFile(w, r, index)
				return
			}
		}
		if _, err := os.Stat(filepath.Join(root, "index.html")); err == nil {
			http.ServeFile(w, r, filepath.Join(root, "index.html"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func workspaceReservedPath(requestPath string) bool {
	cleaned := path.Clean("/" + requestPath)
	switch {
	case cleaned == "/healthz",
		strings.HasPrefix(cleaned, "/api/"),
		strings.HasPrefix(cleaned, "/oauth/"),
		strings.HasPrefix(cleaned, "/mcp"),
		strings.HasPrefix(cleaned, "/.well-known/"):
		return true
	default:
		return false
	}
}

func isWithinDir(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}
