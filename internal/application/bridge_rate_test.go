package application

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type observedBody struct {
	reader io.Reader
	reads  atomic.Int64
}

func (b *observedBody) Read(buffer []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(buffer)
}
func (*observedBody) Close() error { return nil }

func TestBridgeMCPRateLimitsIPBeforeAuthenticationAndBodyRead(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	for attempt := 1; attempt <= 121; attempt++ {
		body := &observedBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)}
		request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		request.Body = body
		request.RemoteAddr = "192.0.2.10:4567"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("MCP-Protocol-Version", "2026-07-28")
		request.Header.Set("MCP-Method", "server/discover")
		request.Header.Set("Authorization", "Bearer invalid")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if body.reads.Load() != 0 {
			t.Fatalf("attempt %d read the body before bearer authentication", attempt)
		}
		if attempt <= 120 && response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if attempt == 121 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60") {
			t.Fatalf("rate limit status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
		}
	}
}

func TestBridgeMCPRateLimitsActorAndClientBeforeJSONDecode(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	bootstrap := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(`{"username":"rate-admin","email":"rate-admin@example.test","displayName":"Rate Administrator","password":"correct horse battery staple"}`))
	bootstrap.Header.Set("Content-Type", "application/json")
	bootstrap.Header.Set("Origin", cfg.AllowedOrigin)
	bootstrapResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(bootstrapResponse, bootstrap)
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
	clientResponse := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, http.MethodPost, "/api/v1/bridge/clients", "application/json", strings.NewReader(`{"name":"Rate client","redirectUris":["http://127.0.0.1:7777/callback"],"allowedScopes":["mcp:resources","assets:read"]}`))
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	var registered struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(clientResponse.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	_, accessToken := authorizeBridgeClient(t, app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, registered.ID)
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"rate","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	for attempt := 1; attempt <= 121; attempt++ {
		body := &observedBody{reader: strings.NewReader(discover)}
		request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		request.Body = body
		request.RemoteAddr = "192.0.2." + strconv.Itoa(attempt) + ":4567"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", "2026-07-28")
		request.Header.Set("MCP-Method", "server/discover")
		request.Header.Set("Authorization", "Bearer "+accessToken)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if attempt <= 120 && response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if attempt == 121 {
			if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
				t.Fatalf("actor/client rate status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
			}
			if body.reads.Load() != 0 {
				t.Fatal("actor/client limited request decoded its JSON body")
			}
		}
	}
}
