package httpapi

// OAuth 2.1 and Bridge administration HTTP endpoints.
// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
)

func (s *Server) bridgeProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	if s.bridge == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": s.bridge.ResourceURI(), "authorization_servers": []string{s.bridge.Issuer()},
		"scopes_supported": scopeStrings(bridge.SupportedScopes()), "bearer_methods_supported": []string{"header"},
		"resource_name": "StewardMesh Bridge MCP",
	})
}

func (s *Server) bridgeAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	if s.bridge == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	issuer := s.bridge.Issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": issuer, "authorization_endpoint": issuer + "/oauth/authorize", "token_endpoint": issuer + "/oauth/token",
		"revocation_endpoint": issuer + "/oauth/revoke", "response_types_supported": []string{"code"},
		"grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": scopeStrings(bridge.SupportedScopes()),
		"authorization_response_iss_parameter_supported": true,
	})
}

func (s *Server) bridgeAuthorize(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	authentication, ok := s.browserAuthentication(w, r)
	if !ok {
		return
	}
	if err := s.bridge.AllowRate(r.Context(), []string{"oauth-authorize-actor:" + authentication.Principal.Subject, "oauth-authorize-ip:" + remoteHost(r.RemoteAddr)}, 30, time.Minute); err != nil {
		writeBridgeError(w, r, err)
		return
	}
	query := r.URL.Query()
	request, err := s.bridge.BeginAuthorization(r.Context(), authentication, bridge.AuthorizationInput{
		ResponseType: query.Get("response_type"), ClientID: query.Get("client_id"), RedirectURI: query.Get("redirect_uri"),
		ResourceURI: query.Get("resource"), Scopes: query.Get("scope"), State: query.Get("state"),
		CodeChallenge: query.Get("code_challenge"), CodeChallengeMethod: query.Get("code_challenge_method"),
	})
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	location := s.allowedOrigin + "/?consent=" + url.QueryEscape(request.ID) + "#workspace-bridge"
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (s *Server) bridgeToken(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.bridge == nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	if !parseBridgeForm(w, r) {
		return
	}
	clientID := r.PostForm.Get("client_id")
	if err := s.bridge.AllowRate(r.Context(), []string{"oauth-token-client:" + clientID, "oauth-token-ip:" + remoteHost(r.RemoteAddr)}, 60, time.Minute); err != nil {
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down")
		return
	}
	credentials, err := s.bridge.ExchangeToken(r.Context(), bridge.TokenInput{
		GrantType: r.PostForm.Get("grant_type"), Code: r.PostForm.Get("code"), RefreshToken: r.PostForm.Get("refresh_token"),
		ClientID: clientID, RedirectURI: r.PostForm.Get("redirect_uri"), ResourceURI: r.PostForm.Get("resource"), CodeVerifier: r.PostForm.Get("code_verifier"),
	})
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, oauthErrorCode(err))
		return
	}
	writeJSON(w, http.StatusOK, credentials)
}

func (s *Server) bridgeRevokeToken(w http.ResponseWriter, r *http.Request) {
	s.noStore(w)
	if s.bridge == nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	if !parseBridgeForm(w, r) {
		return
	}
	if err := s.bridge.AllowRate(r.Context(), []string{"oauth-revoke-ip:" + remoteHost(r.RemoteAddr)}, 60, time.Minute); err != nil {
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down")
		return
	}
	_ = s.bridge.RevokeToken(r.Context(), r.PostForm.Get("token"))
	w.WriteHeader(http.StatusOK)
}

func parseBridgeForm(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_request")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func (s *Server) browserAuthentication(w http.ResponseWriter, r *http.Request) (guard.Authentication, bool) {
	if s.bridge == nil || s.guard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "bridge_unavailable", "Bridge is unavailable")
		return guard.Authentication{}, false
	}
	cookie, err := r.Cookie(s.sessionCookieName())
	if err != nil || cookie.Value == "" {
		writeError(w, r, http.StatusUnauthorized, "authentication_required", "sign in is required")
		return guard.Authentication{}, false
	}
	authentication, err := s.guard.AuthenticateSession(r.Context(), cookie.Value)
	if err != nil {
		s.clearSessionCookie(w)
		writeError(w, r, http.StatusUnauthorized, "invalid_session", "the session is invalid or expired")
		return guard.Authentication{}, false
	}
	if scope, ok := foundation.ScopeFromContext(r.Context()); ok {
		scope.ActorID = authentication.Principal.Subject
		r = r.WithContext(foundation.WithScope(r.Context(), scope))
	}
	return authentication, true
}

func (s *Server) listBridgeClients(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	page, err := bridgeAdministrationPage(r)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	result, err := s.bridge.ListClients(r.Context(), authentication, page)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createBridgeClient(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	var input bridge.CreateClientInput
	if decodeJSON(w, r, 16<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid OAuth client payload")
		return
	}
	created, err := s.bridge.CreateClient(r.Context(), authentication, input)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) revokeBridgeClient(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	revoked, err := s.bridge.RevokeClient(r.Context(), authentication, r.PathValue("clientID"))
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, revoked)
}

func (s *Server) listBridgeGrants(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	page, err := bridgeAdministrationPage(r)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	result, err := s.bridge.ListGrants(r.Context(), authentication, page)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func bridgeAdministrationPage(r *http.Request) (bridge.PageRequest, error) {
	query := r.URL.Query()
	if len(query["cursor"]) > 1 || len(query["limit"]) > 1 {
		return bridge.PageRequest{}, bridge.ErrInvalidInput
	}
	page := bridge.PageRequest{Cursor: query.Get("cursor")}
	rawLimit := query.Get("limit")
	if rawLimit == "" {
		return page, nil
	}
	limit, err := strconv.Atoi(rawLimit)
	if err != nil || limit < 1 || limit > bridge.MaximumAdministrationPageSize {
		return bridge.PageRequest{}, bridge.ErrInvalidInput
	}
	page.Limit = limit
	return page, nil
}

func (s *Server) revokeBridgeGrant(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	revoked, err := s.bridge.RevokeGrant(r.Context(), authentication, r.PathValue("grantID"))
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, revoked)
}

func (s *Server) getBridgeConsent(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	consent, err := s.bridge.Consent(r.Context(), authentication, r.PathValue("requestID"))
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, consent)
}

func (s *Server) decideBridgeConsent(w http.ResponseWriter, r *http.Request, authentication guard.Authentication) {
	var input struct {
		Approved bool `json:"approved"`
	}
	if decodeJSON(w, r, 4<<10, &input) != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid consent decision")
		return
	}
	redirectTo, err := s.bridge.DecideConsent(r.Context(), authentication, r.PathValue("requestID"), input.Approved)
	if err != nil {
		writeBridgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirectTo": redirectTo})
}

func scopeStrings(scopes []bridge.Scope) []string {
	values := make([]string, len(scopes))
	for index, scope := range scopes {
		values[index] = string(scope)
	}
	return values
}

func oauthErrorCode(err error) string {
	switch {
	case errors.Is(err, bridge.ErrUnauthorized), errors.Is(err, bridge.ErrReplay), errors.Is(err, bridge.ErrExpired):
		return "invalid_grant"
	case errors.Is(err, bridge.ErrPermissionDenied):
		return "invalid_scope"
	default:
		return "invalid_request"
	}
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code})
}

func writeBridgeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, bridge.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "Bridge input is invalid")
	case errors.Is(err, bridge.ErrUnauthorized):
		writeError(w, r, http.StatusUnauthorized, "authentication_failed", "Bridge authentication failed")
	case errors.Is(err, bridge.ErrPermissionDenied):
		writeError(w, r, http.StatusForbidden, "permission_denied", "Bridge permission is required")
	case errors.Is(err, bridge.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "the Bridge record was not found")
	case errors.Is(err, bridge.ErrConflict), errors.Is(err, bridge.ErrReplay):
		writeError(w, r, http.StatusConflict, "conflict", "the Bridge operation conflicts with current state")
	case errors.Is(err, bridge.ErrExpired):
		writeError(w, r, http.StatusGone, "expired", "the Bridge request expired")
	case errors.Is(err, bridge.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "the Bridge rate limit was reached")
	default:
		writeError(w, r, http.StatusInternalServerError, "bridge_error", "the Bridge operation could not be completed")
	}
}

var _ = strings.TrimSpace
