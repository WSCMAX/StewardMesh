package httpapi

// Requirements: REQ-FOUNDATION-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

const (
	csrfHeader                = "X-CSRF-Token"
	localSessionName          = "stewardmesh_session"
	secureSessionName         = "__Host-stewardmesh_session"
	localOIDCTransactionName  = "stewardmesh_oidc_transaction"
	secureOIDCTransactionName = "__Host-stewardmesh_oidc_transaction"
)

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Dependencies struct {
	Assets              repository.AssetRepository
	People              *people.Service
	Tags                repository.TagRepository
	Goals               repository.GoalRepository
	Blobs               storage.BlobStore
	Guard               *guard.Service
	OIDC                *identity.OIDCFlow
	Graph               directoryexpansion.GraphStore
	SessionCookieSecure bool
}

type Server struct {
	assets              repository.AssetRepository
	people              *people.Service
	tagsRepo            repository.TagRepository
	goalsRepo           repository.GoalRepository
	guard               *guard.Service
	oidc                *identity.OIDCFlow
	graph               directoryexpansion.GraphStore
	allowedOrigin       string
	organization        bootstrap.Organization
	sessionCookieSecure bool
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, guard.Authentication)

type guardAccountResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type guardRoleResponse struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Permissions     []guard.Permission `json:"permissions"`
	PolicyBundleIDs []string           `json:"policyBundleIds"`
}

type guardScopeResponse struct {
	Kind       guard.ScopeKind `json:"kind"`
	ResourceID string          `json:"resourceId"`
}

type guardRoleAssignmentResponse struct {
	ID        string             `json:"id"`
	AccountID string             `json:"accountId"`
	RoleID    string             `json:"roleId"`
	Scope     guardScopeResponse `json:"scope"`
	Source    string             `json:"source"`
	Managed   bool               `json:"managed"`
	CreatedAt time.Time          `json:"createdAt"`
}

func NewServer(deps Dependencies, allowedOrigin string, organizations ...bootstrap.Organization) http.Handler {
	organization := bootstrap.Organization{ID: "local-organization", Name: "StewardMesh Local Organization"}
	if len(organizations) > 0 {
		organization = organizations[0]
	}
	server := &Server{
		assets:              deps.Assets,
		people:              deps.People,
		tagsRepo:            deps.Tags,
		goalsRepo:           deps.Goals,
		guard:               deps.Guard,
		oidc:                deps.OIDC,
		graph:               deps.Graph,
		allowedOrigin:       allowedOrigin,
		organization:        organization,
		sessionCookieSecure: deps.SessionCookieSecure,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/auth/bootstrap", server.bootstrapStatus)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", server.bootstrapAdministrator)
	mux.HandleFunc("POST /api/v1/auth/login", server.login)
	mux.HandleFunc("GET /api/v1/auth/oidc/start", server.oidcStart)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", server.oidcCallback)
	mux.Handle("GET /api/v1/auth/session", server.protected("", false, server.getSession))
	mux.Handle("POST /api/v1/auth/logout", server.protected("", true, server.logout))
	mux.Handle("GET /api/v1/guard/access", server.protected(guard.PermissionGuardManage, false, server.listGuardAccess))
	mux.Handle("POST /api/v1/guard/role-assignments", server.protected(guard.PermissionGuardManage, true, server.createGuardRoleAssignment))
	mux.Handle("DELETE /api/v1/guard/role-assignments/{assignmentID}", server.protected(guard.PermissionGuardManage, true, server.deleteGuardRoleAssignment))
	mux.Handle("GET /api/v1/organization", server.protected(guard.PermissionOrganizationRead, false, server.getOrganization))
	mux.Handle("GET /api/v1/assets", server.protected(guard.PermissionAssetsRead, false, server.listAssets))
	mux.Handle("POST /api/v1/assets", server.protected(guard.PermissionAssetsWrite, true, server.createAsset))
	mux.Handle("GET /api/v1/sites", server.protected("", false, server.listSites))
	mux.Handle("POST /api/v1/sites", server.protected(guard.PermissionDirectoryWrite, true, server.createSite))
	mux.Handle("GET /api/v1/buildings", server.protected("", false, server.listBuildings))
	mux.Handle("POST /api/v1/buildings", server.protected(guard.PermissionDirectoryWrite, true, server.createBuilding))
	mux.Handle("GET /api/v1/rooms", server.protected("", false, server.listRooms))
	mux.Handle("POST /api/v1/rooms", server.protected(guard.PermissionDirectoryWrite, true, server.createRoom))
	mux.Handle("GET /api/v1/departments", server.protected("", false, server.listDepartments))
	mux.Handle("POST /api/v1/departments", server.protected(guard.PermissionDirectoryWrite, true, server.createDepartment))
	mux.Handle("GET /api/v1/identities", server.protected("", false, server.listIdentities))
	mux.Handle("POST /api/v1/identities", server.protected(guard.PermissionDirectoryWrite, true, server.createIdentity))
	mux.Handle("GET /api/v1/users", server.protected("", false, server.listUsers))
	mux.Handle("GET /api/v1/assets/{assetID}/assignments", server.protected(guard.PermissionAssetsRead, false, server.listAssetAssignments))
	mux.Handle("POST /api/v1/assets/{assetID}/assignments", server.protected(guard.PermissionDirectoryWrite, true, server.createAssetAssignment))
	mux.Handle("PATCH /api/v1/assets/{assetID}/assignments/{assignmentID}", server.protected(guard.PermissionDirectoryWrite, true, server.endAssetAssignment))
	mux.Handle("GET /api/v1/tags", server.protected(guard.PermissionGoalsRead, false, server.tags))
	mux.Handle("GET /api/v1/goals", server.protected(guard.PermissionGoalsRead, false, server.goals))
	mux.Handle("GET /api/v1/graph", server.protected("", false, server.graphView))
	return server.correlation(server.securityHeaders(server.cors(mux)))
}

func (s *Server) graphView(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.graph == nil {
		writeError(w, r, http.StatusServiceUnavailable, "graph_unavailable", "relationship graph unavailable")
		return
	}
	if _, ok := s.directoryVisibility(w, r, authentication); !ok {
		return
	}
	query := directoryexpansion.GraphQuery{
		Search:       r.URL.Query().Get("search"),
		Kind:         r.URL.Query().Get("kind"),
		Relationship: r.URL.Query().Get("relationship"),
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			query.Limit = parsed
		}
	}
	graph, err := s.graph.Graph(r.Context(), query)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "graph_error", "unable to load relationship graph")
		return
	}
	writeJSON(w, http.StatusOK, graph)
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
		"oidcEnabled":               s.oidc != nil,
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
	case errors.Is(err, guard.ErrLoginProtectionUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "authentication_unavailable", "login protection is temporarily unavailable")
		return
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

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.oidc == nil {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	if !s.trustedOIDCStart(r) {
		writeError(w, r, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	required, _, err := s.guard.BootstrapStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authentication_error", "unable to read administrator setup status")
		return
	}
	if required {
		writeError(w, r, http.StatusConflict, "bootstrap_required", "create the first administrator before using OpenID Connect")
		return
	}
	authorizationURL, transactionValue, expiresAt, err := s.oidc.Start()
	if err != nil {
		s.guard.RecordOIDCFailure(r.Context())
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.oidcTransactionCookieName(),
		Value:    transactionValue,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.guard == nil || s.oidc == nil {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_unavailable", "OpenID Connect sign-in is unavailable")
		return
	}
	cookie, cookieErr := r.Cookie(s.oidcTransactionCookieName())
	s.clearOIDCTransactionCookie(w)
	state := r.URL.Query().Get("state")
	if cookieErr != nil || cookie.Value == "" {
		s.oidcFailure(w, r)
		return
	}
	if r.URL.Query().Get("error") != "" {
		if err := s.oidc.Validate(cookie.Value, state); err != nil {
			s.oidcFailure(w, r)
			return
		}
		s.oidcFailure(w, r)
		return
	}
	principal, err := s.oidc.Complete(r.Context(), cookie.Value, state, r.URL.Query().Get("code"))
	if err != nil {
		s.oidcFailure(w, r)
		return
	}
	credentials, err := s.guard.LoginOIDC(r.Context(), principal)
	if err != nil {
		s.oidcFailureRedirect(w, r)
		return
	}
	s.setSessionCookie(w, credentials)
	http.Redirect(w, r, s.allowedOrigin, http.StatusSeeOther)
}

func (s *Server) oidcFailure(w http.ResponseWriter, r *http.Request) {
	s.guard.RecordOIDCFailure(r.Context())
	s.oidcFailureRedirect(w, r)
}

func (s *Server) oidcFailureRedirect(w http.ResponseWriter, r *http.Request) {
	if s.allowedOrigin == "" {
		writeError(w, r, http.StatusUnauthorized, "oidc_failed", "OpenID Connect sign-in could not be completed")
		return
	}
	http.Redirect(w, r, s.allowedOrigin+"?auth=oidc_error", http.StatusSeeOther)
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

func (s *Server) listGuardAccess(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	directory, err := s.guard.ListAuthorization(r.Context(), authentication)
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	accounts := make([]guardAccountResponse, 0, len(directory.Accounts))
	for _, account := range directory.Accounts {
		accounts = append(accounts, guardAccountResponse{
			ID: account.ID, Username: account.Username, Email: account.Email,
			DisplayName: account.DisplayName, Status: account.Status,
		})
	}
	roles := make([]guardRoleResponse, 0, len(directory.Roles))
	for _, role := range directory.Roles {
		roles = append(roles, guardRoleResponse{
			ID: role.ID, Name: role.Name, Description: role.Description,
			Permissions:     append([]guard.Permission(nil), role.Permissions...),
			PolicyBundleIDs: append([]string(nil), role.PolicyBundleIDs...),
		})
	}
	assignments := make([]guardRoleAssignmentResponse, 0, len(directory.Assignments))
	for _, assignment := range directory.Assignments {
		assignments = append(assignments, guardAssignmentResponse(assignment))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts, "roles": roles, "assignments": assignments,
	})
}

func (s *Server) createGuardRoleAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	var input struct {
		AccountID string `json:"accountId"`
		RoleID    string `json:"roleId"`
		Scope     struct {
			Kind       string `json:"kind"`
			ResourceID string `json:"resourceId"`
		} `json:"scope"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid role assignment payload")
		return
	}
	assignment, err := s.guard.AssignRole(r.Context(), authentication, guard.RoleAssignmentInput{
		AccountID: input.AccountID, RoleID: input.RoleID,
		ScopeKind: guard.ScopeKind(input.Scope.Kind), ResourceID: input.Scope.ResourceID,
	})
	if err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, guardAssignmentResponse(assignment))
}

func (s *Server) deleteGuardRoleAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.noStore(w)
	if _, err := s.guard.RevokeRoleAssignment(r.Context(), authentication, r.PathValue("assignmentID")); err != nil {
		s.writeGuardManagementError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func guardAssignmentResponse(assignment guard.RoleAssignment) guardRoleAssignmentResponse {
	return guardRoleAssignmentResponse{
		ID: assignment.ID, AccountID: assignment.AccountID, RoleID: assignment.RoleID,
		Scope:  guardScopeResponse{Kind: assignment.Scope.Kind, ResourceID: assignment.Scope.ResourceID},
		Source: assignment.Source, Managed: assignment.Source != guard.LocalAssignmentSource, CreatedAt: assignment.CreatedAt,
	}
}

func (s *Server) writeGuardManagementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, guard.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "role assignment details are invalid")
	case errors.Is(err, guard.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested Guard account, role, or assignment was not found")
	case errors.Is(err, guard.ErrManagedAssignment):
		writeError(w, r, http.StatusConflict, "managed_assignment", "this role assignment is managed by the identity provider")
	case errors.Is(err, guard.ErrLastAdministrator):
		writeError(w, r, http.StatusConflict, "last_administrator", "Assign another organization administrator before removing this assignment")
	case errors.Is(err, guard.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "this scoped role assignment already exists")
	case errors.Is(err, guard.ErrPermissionDenied):
		writeError(w, r, http.StatusForbidden, "permission_denied", "organization-level Guard management permission is required")
	default:
		writeError(w, r, http.StatusInternalServerError, "guard_error", "the Guard role assignment operation could not be completed")
	}
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

func (s *Server) listSites(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListSites(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Name    string              `json:"name"`
		Address people.Address      `json:"address"`
		Status  people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid site payload")
		return
	}
	created, err := s.people.CreateSite(r.Context(), people.CreateSiteInput{
		Name: input.Name, Address: input.Address, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listBuildings(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListBuildings(r.Context(), r.URL.Query().Get("siteId"), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createBuilding(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		SiteID string              `json:"siteId"`
		Name   string              `json:"name"`
		Status people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid building payload")
		return
	}
	created, err := s.people.CreateBuilding(r.Context(), people.CreateBuildingInput{
		SiteID: input.SiteID, Name: input.Name, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListRooms(
		r.Context(),
		r.URL.Query().Get("siteId"),
		r.URL.Query().Get("buildingId"),
		visibility,
	)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		SiteID     string              `json:"siteId"`
		BuildingID string              `json:"buildingId"`
		Number     string              `json:"number"`
		Name       string              `json:"name"`
		Status     people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid room payload")
		return
	}
	created, err := s.people.CreateRoom(r.Context(), people.CreateRoomInput{
		SiteID: input.SiteID, BuildingID: input.BuildingID, Number: input.Number,
		Name: input.Name, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listDepartments(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListDepartments(r.Context(), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createDepartment(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Name   string              `json:"name"`
		SiteID string              `json:"siteId"`
		Status people.RecordStatus `json:"status"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid department payload")
		return
	}
	created, err := s.people.CreateDepartment(r.Context(), people.CreateDepartmentInput{
		Name: input.Name, SiteID: input.SiteID, Status: input.Status,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listIdentities(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.listIdentityCollection(w, r, authentication, "")
}

// listUsers preserves the initial REST collection as a person-only alias.
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	s.listIdentityCollection(w, r, authentication, people.IdentityPerson)
}

func (s *Server) listIdentityCollection(w http.ResponseWriter, r *http.Request, authentication guard.Authentication, forcedKind people.IdentityKind) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	query, err := identityQueryFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "directory filters are invalid")
		return
	}
	if forcedKind != "" {
		query.Kind = forcedKind
	}
	items, err := s.people.SearchIdentities(r.Context(), query, visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createIdentity(w http.ResponseWriter, r *http.Request, _ guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	var input struct {
		Kind            people.IdentityKind `json:"kind"`
		DisplayName     string              `json:"displayName"`
		Email           string              `json:"email"`
		DepartmentID    string              `json:"departmentId"`
		SiteID          string              `json:"siteId"`
		Status          people.RecordStatus `json:"status"`
		Provider        string              `json:"provider"`
		ProviderSubject string              `json:"providerSubject"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid identity payload")
		return
	}
	created, err := s.people.CreateIdentity(r.Context(), people.CreateIdentityInput{
		Kind: input.Kind, DisplayName: input.DisplayName, Email: input.Email,
		DepartmentID: input.DepartmentID, SiteID: input.SiteID, Status: input.Status,
		Provider: input.Provider, ProviderSubject: input.ProviderSubject,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listAssetAssignments(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	visibility, ok := s.directoryVisibility(w, r, authentication)
	if !ok {
		return
	}
	items, err := s.people.ListAssetAssignments(r.Context(), r.PathValue("assetID"), visibility)
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAssetAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsWrite) {
		return
	}
	var input struct {
		AssigneeKind  people.AssigneeKind   `json:"assigneeKind"`
		AssigneeID    string                `json:"assigneeId"`
		Role          people.AssignmentRole `json:"role"`
		EffectiveFrom *time.Time            `json:"effectiveFrom"`
	}
	if err := decodeJSON(w, r, 32<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid asset assignment payload")
		return
	}
	effectiveFrom := time.Time{}
	if input.EffectiveFrom != nil {
		effectiveFrom = input.EffectiveFrom.UTC()
	}
	created, err := s.people.CreateAssetAssignment(r.Context(), people.CreateAssetAssignmentInput{
		AssetID: r.PathValue("assetID"), AssigneeKind: input.AssigneeKind,
		AssigneeID: input.AssigneeID, Role: input.Role, EffectiveFrom: effectiveFrom,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) endAssetAssignment(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	if s.people == nil {
		writeError(w, r, http.StatusServiceUnavailable, "repository_unavailable", "people directory unavailable")
		return
	}
	if !s.requireOrganizationPermission(w, r, authentication, guard.PermissionAssetsWrite) {
		return
	}
	var input struct {
		EffectiveTo *time.Time `json:"effectiveTo"`
	}
	if err := decodeJSON(w, r, 16<<10, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid assignment end payload")
		return
	}
	effectiveTo := time.Time{}
	if input.EffectiveTo != nil {
		effectiveTo = input.EffectiveTo.UTC()
	}
	ended, err := s.people.EndAssetAssignment(r.Context(), people.EndAssetAssignmentInput{
		AssetID: r.PathValue("assetID"), AssignmentID: r.PathValue("assignmentID"), EffectiveTo: effectiveTo,
	})
	if err != nil {
		writePeopleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ended)
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

func identityQueryFromRequest(r *http.Request) (people.IdentityQuery, error) {
	values := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			return people.IdentityQuery{}, err
		}
		limit = parsed
	}
	return people.IdentityQuery{
		Search:       values.Get("q"),
		Kind:         people.IdentityKind(values.Get("kind")),
		Status:       people.RecordStatus(values.Get("status")),
		DepartmentID: values.Get("departmentId"),
		SiteID:       values.Get("siteId"),
		Limit:        limit,
	}, nil
}

func (s *Server) directoryVisibility(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) (people.Visibility, bool) {
	visibility := people.Visibility{}
	for _, grant := range authentication.Grants {
		if grant.Permission != guard.PermissionDirectoryRead || grant.Scope.OrganizationID != s.organization.ID {
			continue
		}
		switch grant.Scope.Kind {
		case guard.ScopeOrganization:
			return people.Visibility{All: true}, true
		case guard.ScopeDepartment:
			visibility.DepartmentIDs = append(visibility.DepartmentIDs, grant.Scope.ResourceID)
		case guard.ScopeSite:
			visibility.SiteIDs = append(visibility.SiteIDs, grant.Scope.ResourceID)
		}
	}
	if visibility.Empty() {
		_ = s.guard.CheckPermission(r.Context(), authentication, guard.PermissionDirectoryRead, guard.Scope{
			Kind: guard.ScopeOrganization, OrganizationID: s.organization.ID, ResourceID: s.organization.ID,
		})
		writeError(w, r, http.StatusForbidden, "permission_denied", "directory permission is required for this operation")
		return people.Visibility{}, false
	}
	return visibility, true
}

func (s *Server) requireOrganizationPermission(w http.ResponseWriter, r *http.Request, authentication guard.Authentication, permission guard.Permission) bool {
	err := s.guard.CheckPermission(r.Context(), authentication, permission, guard.Scope{
		Kind: guard.ScopeOrganization, OrganizationID: s.organization.ID, ResourceID: s.organization.ID,
	})
	if err != nil {
		writeError(w, r, http.StatusForbidden, "permission_denied", "permission is required for this operation")
		return false
	}
	return true
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
	s.setSessionCookie(w, credentials)
	writeJSON(w, status, sessionResponse(credentials.Authentication, credentials.CSRFToken))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, credentials guard.SessionCredentials) {
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
}

func sessionResponse(authentication guard.Authentication, csrfToken string) map[string]any {
	return map[string]any{
		"principal":   authentication.Principal,
		"permissions": organizationPermissions(authentication),
		"csrfToken":   csrfToken,
		"expiresAt":   authentication.Session.ExpiresAt,
	}
}

func organizationPermissions(authentication guard.Authentication) []string {
	seen := make(map[guard.Permission]struct{})
	for _, grant := range authentication.Grants {
		if grant.Scope.Kind == guard.ScopeOrganization && grant.Scope.OrganizationID == authentication.Principal.OrganizationID {
			seen[grant.Permission] = struct{}{}
		}
	}
	permissions := make([]string, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, string(permission))
	}
	sort.Strings(permissions)
	return permissions
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

func (s *Server) oidcTransactionCookieName() string {
	if s.sessionCookieSecure {
		return secureOIDCTransactionName
	}
	return localOIDCTransactionName
}

func (s *Server) clearOIDCTransactionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.oidcTransactionCookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.sessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) trustedOIDCStart(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.allowedOrigin != "" && origin == s.allowedOrigin
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "none":
		return true
	case "same-site", "cross-site":
		return false
	}
	return isLoopbackOrigin(s.allowedOrigin) && isLoopbackRemote(r.RemoteAddr)
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
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

func isLoopbackOrigin(rawOrigin string) bool {
	origin, err := url.Parse(rawOrigin)
	if err != nil {
		return false
	}
	host := origin.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
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

func writePeopleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, people.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "people directory input is invalid")
	case errors.Is(err, people.ErrNotFound), errors.Is(err, people.ErrReferenceMissing):
		writeError(w, r, http.StatusNotFound, "not_found", "the requested directory reference was not found")
	case errors.Is(err, people.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "the directory operation conflicts with existing data")
	case errors.Is(err, people.ErrScopeRequired):
		writeError(w, r, http.StatusForbidden, "permission_denied", "directory scope is required for this operation")
	default:
		writeError(w, r, http.StatusInternalServerError, "repository_error", "the people directory operation could not be completed")
	}
}
