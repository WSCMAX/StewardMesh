package application

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBridgeOAuthPKCEAndRemoteMCPSmoke(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	bootstrapPayload := `{"username":"administrator","email":"administrator@example.test","displayName":"Administrator","password":"correct horse battery staple"}`
	bootstrapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(bootstrapPayload))
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapRequest.Header.Set("Origin", cfg.AllowedOrigin)
	bootstrapResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(bootstrapResponse, bootstrapRequest)
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookie := bootstrapResponse.Result().Cookies()[0]

	clientPayload := `{"name":"MCP test client","redirectUris":["http://127.0.0.1:7777/callback"],"allowedScopes":["mcp:resources","assets:read"]}`
	clientResponse := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, http.MethodPost, "/api/v1/bridge/clients", "application/json", strings.NewReader(clientPayload))
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	var client struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(clientResponse.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/bridge/clients?limit=0",
		"/api/v1/bridge/clients?limit=101",
		"/api/v1/bridge/clients?limit=1&limit=2",
	} {
		invalidPage := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, "", http.MethodGet, path, "", nil)
		if invalidPage.Code != http.StatusBadRequest {
			t.Fatalf("invalid administration page %q: %d %s", path, invalidPage.Code, invalidPage.Body.String())
		}
	}

	verifier := strings.Repeat("A", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	parameters := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:7777/callback"}, "resource": {cfg.AllowedOrigin + "/mcp"}, "scope": {"mcp:resources assets:read"}, "state": {"opaque-state"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	authorize := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+parameters.Encode(), nil)
	authorize.AddCookie(cookie)
	authorizeResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(authorizeResponse, authorize)
	if authorizeResponse.Code != http.StatusSeeOther {
		t.Fatalf("authorize: %d %s", authorizeResponse.Code, authorizeResponse.Body.String())
	}
	consentLocation, err := url.Parse(authorizeResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	requestID := consentLocation.Query().Get("consent")
	if requestID == "" {
		t.Fatalf("missing consent id in %q", consentLocation.String())
	}

	decision := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, http.MethodPost, "/api/v1/bridge/consents/"+requestID+"/decision", "application/json", strings.NewReader(`{"approved":true}`))
	if decision.Code != http.StatusOK {
		t.Fatalf("consent: %d %s", decision.Code, decision.Body.String())
	}
	var decisionBody struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := json.Unmarshal(decision.Body.Bytes(), &decisionBody); err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(decisionBody.RedirectTo)
	if err != nil {
		t.Fatal(err)
	}
	if callback.Query().Get("state") != "opaque-state" || callback.Query().Get("iss") != cfg.AllowedOrigin {
		t.Fatalf("authorization response binding failed: %s", callback)
	}
	code := callback.Query().Get("code")

	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:7777/callback"}, "resource": {cfg.AllowedOrigin + "/mcp"}, "code_verifier": {verifier}}
	tokenResponse := bridgeRequest(app.Handler(), cfg.AllowedOrigin, nil, "", http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(tokenForm.Encode()))
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token: %d %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &token); err != nil || token.AccessToken == "" || token.RefreshToken == "" {
		t.Fatalf("unexpected token response %s err=%v", tokenResponse.Body.String(), err)
	}

	replay := bridgeRequest(app.Handler(), cfg.AllowedOrigin, nil, "", http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(tokenForm.Encode()))
	if replay.Code == http.StatusOK {
		t.Fatal("authorization code replay was accepted")
	}

	discoverBody := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"smoke","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	mcpRequest := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverBody))
	mcpRequest.Header.Set("Content-Type", "application/json")
	mcpRequest.Header.Set("Accept", "application/json, text/event-stream")
	mcpRequest.Header.Set("MCP-Protocol-Version", "2026-07-28")
	mcpRequest.Header.Set("MCP-Method", "server/discover")
	mcpRequest.Header.Set("Authorization", "Bearer "+token.AccessToken)
	mcpResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(mcpResponse, mcpRequest)
	if mcpResponse.Code != http.StatusOK || !strings.Contains(mcpResponse.Body.String(), `"supportedVersions":["2026-07-28"`) {
		t.Fatalf("MCP initialize: %d %s", mcpResponse.Code, mcpResponse.Body.String())
	}

	batch := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[]`))
	batch.Header = mcpRequest.Header.Clone()
	batchResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(batchResponse, batch)
	if batchResponse.Code != http.StatusBadRequest {
		t.Fatalf("MCP batch was not rejected: %d %s", batchResponse.Code, batchResponse.Body.String())
	}

	mismatchedMethod := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverBody))
	mismatchedMethod.Header = mcpRequest.Header.Clone()
	mismatchedMethod.Header.Set("MCP-Method", "tools/list")
	mismatchedResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(mismatchedResponse, mismatchedMethod)
	if mismatchedResponse.Code != http.StatusBadRequest {
		t.Fatalf("mismatched MCP method metadata was not rejected: %d %s", mismatchedResponse.Code, mismatchedResponse.Body.String())
	}
}

func TestBridgeLocalStdioMCPUsesExplicitGuardSessionAndScopes(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	bootstrapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(`{"username":"stdio-admin","email":"stdio-admin@example.test","displayName":"Stdio Administrator","password":"correct horse battery staple"}`))
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapRequest.Header.Set("Origin", cfg.AllowedOrigin)
	bootstrapResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(bootstrapResponse, bootstrapRequest)
	if bootstrapResponse.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	cookie := bootstrapResponse.Result().Cookies()[0]
	access, err := app.Bridge().AuthenticateLocalSession(t.Context(), cookie.Value, "mcp:resources assets:read")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Bridge().AuthenticateLocalSession(t.Context(), cookie.Value, "mcp:resources signals:acknowledge unknown:scope"); err == nil {
		t.Fatal("local stdio accepted an unknown requested scope")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	serverConnection, clientConnection := net.Pipe()
	defer serverConnection.Close()
	defer clientConnection.Close()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- app.Bridge().RunStdio(ctx, access, serverConnection, serverConnection)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-smoke", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientConnection, Writer: clientConnection}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "search_assets" {
		t.Fatalf("stdio scope reduction failed: %#v", tools.Tools)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	clientConnection.Close()
	select {
	case err := <-serverDone:
		if err != nil && !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("stdio server shutdown: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("stdio MCP server did not stop")
	}
}

func bridgeRequest(handler http.Handler, origin string, cookie *http.Cookie, csrf, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if csrf != "" {
		request.Header.Set("Origin", origin)
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
