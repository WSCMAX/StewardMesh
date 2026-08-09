package httpapi

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, SEC-HTTP-001.

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

const (
	csrfHeader        = "X-CSRF-Token"
	localSessionName  = "stewardmesh_session"
	secureSessionName = "__Host-stewardmesh_session"
)

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Dependencies struct {
	Assets              repository.AssetRepository
	Departments         repository.DepartmentRepository
	Users               repository.UserRepository
	Tags                repository.TagRepository
	Goals               repository.GoalRepository
	Blobs               storage.BlobStore
	Guard               *guard.Service
	SessionCookieSecure bool
}

type Server struct {
	assets              repository.AssetRepository
	departmentsRepo     repository.DepartmentRepository
	usersRepo           repository.UserRepository
	tagsRepo            repository.TagRepository
	goalsRepo           repository.GoalRepository
	guard               *guard.Service
	allowedOrigin       string
	organization        bootstrap.Organization
	sessionCookieSecure bool
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, guard.Authentication)

func NewServer(deps Dependencies, allowedOrigin string, organizations ...bootstrap.Organization) http.Handler {
	organization := bootstrap.Organization{ID: "local-organization", Name: "StewardMesh Local Organization"}
	if len(organizations) > 0 {
		organization = organizations[0]
	}
	server := &Server{
		assets:              deps.Assets,
		departmentsRepo:     deps.Departments,
		usersRepo:           deps.Users,
		tagsRepo:            deps.Tags,
		goalsRepo:           deps.Goals,
		guard:               deps.Guard,
		allowedOrigin:       allowedOrigin,
		organization:        organization,
		sessionCookieSecure: deps.SessionCookieSecure,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/auth/bootstrap", server.bootstrapStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", server.bootstrapAdministrator)
	mux.HandleFunc("POST /api/v1/auth/login", server.login)
	mux.Handle("GET /api/v1/auth/session", server.protected("", false, server.getSession))
	mux.Handle("POST /api/v1/auth/logout", server.protected("", true, server.logout))
	mux.Handle("GET /api/v1/organization", server.protected(guard.PermissionOrganizationRead, false, server.getOrganization))
	mux.Handle("GET /api/v1/assets", server.protected(guard.PermissionAssetsRead, false, server.listAssets))
	mux.Handle("POST /api/v1/assets", server.protected(guard.PermissionAssetsWrite, true, server.createAsset))
	mux.Handle("GET /api/v1/departments", server.protected(guard.PermissionDirectoryRead, false, server.departments))
	mux.Handle("GET /api/v1/users", server.protected(guard.PermissionDirectoryRead, false, server.users))
	mux.Handle("GET /api/v1/tags", server.protected(guard.PermissionGoalsRead, false, server.tags))
	mux.Handle("GET /api/v1/goals", server.protected(guard.PermissionGoalsRead, false, server.goals))
	return server.correlation(server.securityHeaders(server.cors(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "product": "StewardMesh", "organizationId": s.organization.ID})
}

func (s *Server) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	required, tokenRequired, err := s.guard.BootstrapStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to read administrator setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"required":                  required,
		"tokenRequired":             tokenRequired,
		"minimumPasswordCharacters": guard.MinimumPasswordCharacters,
	})
}

func (s *Server) bootstrapAdministrator(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	var input struct {
		Username       string `json:"username"`
		Email          string `json:"email"`
		DisplayName    string `json:"displayName"`
		Password       string `json:"password"`
		BootstrapToken string `json:"bootstrapToken"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid administrator setup payload")
		return
	}
	credentials, err := s.guard.Bootstrap(r.Context(), guard.BootstrapInput{
		Username:       input.Username,
		Email:          input.Email,
		DisplayName:    input.DisplayName,
		Password:       input.Password,
		BootstrapToken: input.BootstrapToken,
	}, s.trustedBrowserRequest(r))
	switch {
	case errors.Is(err, guard.ErrBootstrapDenied):
		writeError(w, r, http.StatusForbidden, "bootstrap_denied", "administrator setup is not authorized")
		return
	case errors.Is(err, guard.ErrBootstrapComplete):
		writeError(w, r, http.StatusConflict, "bootstrap_complete", "administrator setup is already complete")
		return
	case errors.Is(err, guard.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "administrator details do not meet setup requirements")
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to create the administrator")
		return
	}
	s.writeAuthenticatedSession(w, http.StatusCreated, credentials)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	if !s.trustedBrowserRequest(r) {
		writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid login payload")
		return
	}
	credentials, err := s.guard.Login(r.Context(), guard.LoginInput{
		Username: input.Username,
		Password: input.Password,
		RateKey:  remoteHost(r.RemoteAddr),
	})
	switch {
	case errors.Is(err, guard.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many login attempts; try again later")
		return
	case errors.Is(err, guard.ErrInvalidCredential):
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to sign in")
		return
	}
	s.writeAuthenticatedSession(w, http.StatusOK, credentials)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	csrfToken, err := s.guard.RefreshCSRF(r.Context(), &authentication)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to refresh the session")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(authentication, csrfToken))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	if err := s.guard.Logout(r.Context(), authentication); err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to sign out")
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getOrganization(w http.ResponseWriter, _ *http.Request, _ guard.Authentication) {
	writeJSON(w, http.StatusOK, s.organization)
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.assets == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	assets, err := s.assets.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "repository_error", "unable to list assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": assets})
}

func (s *Server) departments(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
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

func (s *Server) users(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
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

func (s *Server) tags(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
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

func (s *Server) goals(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
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

func (s *Server) createAsset(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.assets == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "asset repository unavailable")
		return
	}
	var asset domain.Asset
	if err := decodeJSON(w, r, 1<<20, &asset); err != nil {
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

func (s *Server) protected(permission guard.Permission, requireCSRF bool, next authenticatedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.noStore(w)
		if s.guard == nil {
			writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
			return
		}
		cookie, err := r.Cookie(s.sessionCookieName())
		if err != nil || cookie.Value == "" {
			writeError(w, r, http.StatusUnauthorized, "authentication_required", "sign in is required")
			return
		}
		authentication, err := s.guard.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			s.clearSessionCookie(w)
			writeError(w, r, http.StatusUnauthorized, "invalid_session", "the session is invalid or expired")
			return
		}
		if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
			scope.ActorID = authentication.Principal.Subject
			r = r.WithContext(foundation.WithScope(r.Context(), scope))
		}
		if requireCSRF {
			if !s.trustedBrowserRequest(r) {
				writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			if err := s.guard.ValidateCSRF(authentication, r.Header.Get(csrfHeader)); err != nil {
				writeError(w, r, http.StatusForbidden, "csrf_failed", "request verification failed")
				return
			}
		}
		if permission != "" {
			err := s.guard.CheckPermission(r.Context(), authentication, permission, guard.Scope{
				Kind:           guard.ScopeOrganization,
				OrganizationID: s.organization.ID,
				ResourceID:     s.organization.ID,
			})
			if err != nil {
				writeError(w, r, http.StatusForbidden, "permission_denied", "permission is required for this operation")
				return
			}
		}
		next(w, r, authentication)
	})
}

func (s *Server) writeAuthenticatedSession(w http.ResponseWriter, status int, credentials guard.SessionCredentials) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    credentials.Token,
		Path:     "/",
		Expires:  credentials.Authentication.Session.ExpiresAt,
		MaxAge:   int(time.Until(credentials.Authentication.Session.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, status, sessionResponse(credentials.Authentication, credentials.CSRFToken))
}

func sessionResponse(authentication guard.Authentication, csrfToken string) map[string]any {
	return map[string]any{
		"principal": authentication.Principal,
		"csrfToken": csrfToken,
		"expiresAt": authentication.Session.ExpiresAt,
	}
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionCookieName() string {
	if s.sessionCookieSecure {
		return secureSessionName
	}
	return localSessionName
}

func (s *Server) trustedBrowserRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.allowedOrigin != "" && origin == s.allowedOrigin
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	return isLoopbackRemote(r.RemoteAddr)
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
		origin := r.Header.Get("Origin")
		originAllowed := s.allowedOrigin != "" && origin == s.allowedOrigin
		if s.allowedOrigin != "" {
			w.Header().Add("Vary", "Origin")
		}
		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", s.allowedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-ID")
		}
		if r.Method == http.MethodOptions {
			if !originAllowed {
				writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Correlation-ID")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maximumBytes int64, destination any) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maximumBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON document")
	}
	return nil
}

func remoteHost(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return "unknown"
	}
	return host
}

func isLoopbackRemote(remoteAddress string) bool {
	host := remoteHost(remoteAddress)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
