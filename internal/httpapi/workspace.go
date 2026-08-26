package httpapi

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

import (
	"io/fs"
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
	rootPath := filepath.Clean(webDir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if workspaceReservedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer root.Close()
		relative := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if relative == "" {
			serveRootFile(w, r, root, "index.html")
			return
		}
		if strings.Contains(relative, "..") || !fs.ValidPath(relative) {
			http.NotFound(w, r)
			return
		}
		safeRelative := path.Clean(relative)
		if safeRelative == "." || strings.Contains(safeRelative, "..") || !fs.ValidPath(safeRelative) {
			http.NotFound(w, r)
			return
		}
		info, err := root.Stat(safeRelative)
		if err == nil && !info.IsDir() {
			serveRootFile(w, r, root, safeRelative)
			return
		}
		if err == nil && info.IsDir() {
			index := path.Join(safeRelative, "index.html")
			if strings.Contains(index, "..") || !fs.ValidPath(index) {
				http.NotFound(w, r)
				return
			}
			if indexInfo, indexErr := root.Stat(index); indexErr == nil && !indexInfo.IsDir() {
				serveRootFile(w, r, root, index)
				return
			}
		}
		if _, err := root.Stat("index.html"); err == nil {
			serveRootFile(w, r, root, "index.html")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveRootFile(w http.ResponseWriter, r *http.Request, root *os.Root, name string) {
	if name != "index.html" && !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	file, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
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
