package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Dependencies struct {
	Assets      repository.AssetRepository
	Departments repository.DepartmentRepository
	Users       repository.UserRepository
	Tags        repository.TagRepository
	Goals       repository.GoalRepository
	Blobs       storage.BlobStore
}

type Server struct {
	assets          repository.AssetRepository
	departmentsRepo repository.DepartmentRepository
	usersRepo       repository.UserRepository
	tagsRepo        repository.TagRepository
	goalsRepo       repository.GoalRepository
	allowedOrigin   string
	organization    bootstrap.Organization
}

func NewServer(deps Dependencies, allowedOrigin string, organizations ...bootstrap.Organization) http.Handler {
	organization := bootstrap.Organization{ID: "local-organization", Name: "StewardMesh Local Organization"}
	if len(organizations) > 0 {
		organization = organizations[0]
	}
	server := &Server{
		assets:          deps.Assets,
		departmentsRepo: deps.Departments,
		usersRepo:       deps.Users,
		tagsRepo:        deps.Tags,
		goalsRepo:       deps.Goals,
		allowedOrigin:   allowedOrigin,
		organization:    organization,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/organization", server.getOrganization)
	mux.HandleFunc("GET /api/v1/assets", server.listAssets)
	mux.HandleFunc("POST /api/v1/assets", server.createAsset)
	mux.HandleFunc("GET /api/v1/departments", server.departments)
	mux.HandleFunc("GET /api/v1/users", server.users)
	mux.HandleFunc("GET /api/v1/tags", server.tags)
	mux.HandleFunc("GET /api/v1/goals", server.goals)
	return server.correlation(server.securityHeaders(server.cors(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "product": "StewardMesh", "organizationId": s.organization.ID})
}

// getOrganization exposes the configured ownership boundary to the application
// shell. Requirement: REQ-FOUNDATION-001.
func (s *Server) getOrganization(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.organization)
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.assets.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": assets})
}

func (s *Server) departments(w http.ResponseWriter, r *http.Request) {
	if s.departmentsRepo == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "department repository unavailable")
		return
	}
	items, err := s.departmentsRepo.ListDepartments(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list departments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	if s.usersRepo == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "user repository unavailable")
		return
	}
	items, err := s.usersRepo.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	if s.tagsRepo == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "tag repository unavailable")
		return
	}
	items, err := s.tagsRepo.ListTags(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list tags")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) goals(w http.ResponseWriter, r *http.Request) {
	if s.goalsRepo == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "goal repository unavailable")
		return
	}
	items, err := s.goalsRepo.ListGoals(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list goals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAsset(w http.ResponseWriter, r *http.Request) {
	var asset domain.Asset
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&asset); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid asset payload")
		return
	}
	asset.ID = strings.TrimSpace(asset.ID)
	asset.Name = strings.TrimSpace(asset.Name)
	asset.Kind = strings.TrimSpace(asset.Kind)
	if asset.ID == "" || asset.Name == "" || asset.Kind == "" {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "id, name, and kind are required")
		return
	}
	now := time.Now().UTC()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	if asset.Status == "" {
		asset.Status = "draft"
	}
	created, err := s.assets.Create(r.Context(), asset)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "asset not found")
			return
		}
		writeError(w, r, http.StatusConflict, "conflict", "asset could not be created because it conflicts with an existing record")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if !correlationIDPattern.MatchString(correlationID) {
			generated, err := foundation.NewCorrelationID()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"code": "correlation_failed", "message": "unable to initialize request context"},
				})
				return
			}
			correlationID = generated
		}
		scope := foundation.Scope{
			OrganizationID: s.organization.ID,
			ActorID:        "anonymous",
			CorrelationID:  correlationID,
		}
		w.Header().Set("X-Correlation-ID", correlationID)
		next.ServeHTTP(w, r.WithContext(foundation.WithScope(r.Context(), scope)))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.allowedOrigin != "" && r.Header.Get("Origin") == s.allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", s.allowedOrigin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-ID")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Correlation-ID")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	correlationID := ""
	if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
		correlationID = scope.CorrelationID
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":          code,
			"message":       message,
			"correlationId": correlationID,
		},
	})
}
