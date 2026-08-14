package application

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/grpcapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestBridgeGRPCAdministrationHasAuthenticatedRESTParity(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	bootstrap := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(`{"username":"grpc-admin","email":"grpc-admin@example.test","displayName":"gRPC Administrator","password":"correct horse battery staple"}`))
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

	listener := bufconn.Listen(1 << 20)
	adapter, err := grpcapi.New(app.Handler(), grpcapi.Options{
		AllowedOrigin: cfg.AllowedOrigin, SessionCookieSecure: cfg.SessionCookieSecure,
		OrganizationID: app.Organization().ID, Guard: app.Guard(), Vault: app.Vault(),
	})
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(grpcapi.MaximumMessageBytes), grpc.MaxSendMsgSize(grpcapi.MaximumMessageBytes),
		grpc.InTapHandle(adapter.TapHandle), grpc.ForceServerCodec(adapter.TransportCodec()),
	)
	if err := adapter.RegisterAll(grpcServer); err != nil {
		t.Fatal(err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///bridge", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := stewardmeshv1.NewBridgeServiceClient(connection)

	if _, err := client.ListClients(t.Context(), &stewardmeshv1.ListBridgeClientsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated gRPC list code=%s err=%v", status.Code(err), err)
	}
	grpcContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+cookie.Value))
	created, err := client.CreateClient(grpcContext, &stewardmeshv1.CreateBridgeClientRequest{
		Name: "A gRPC client", RedirectUris: []string{"http://127.0.0.1:7777/callback"},
		AllowedScopes: []stewardmeshv1.BridgeScope{stewardmeshv1.BridgeScope_BRIDGE_SCOPE_MCP_RESOURCES, stewardmeshv1.BridgeScope_BRIDGE_SCOPE_ASSETS_READ},
	})
	if err != nil || created.GetId() == "" {
		t.Fatalf("gRPC create: %#v err=%v", created, err)
	}
	if _, err := client.CreateClient(grpcContext, &stewardmeshv1.CreateBridgeClientRequest{Name: "B gRPC client", RedirectUris: []string{"https://b.example.test/callback"}, AllowedScopes: []stewardmeshv1.BridgeScope{stewardmeshv1.BridgeScope_BRIDGE_SCOPE_MCP_RESOURCES}}); err != nil {
		t.Fatal(err)
	}

	grpcFirst, err := client.ListClients(grpcContext, &stewardmeshv1.ListBridgeClientsRequest{Limit: 1})
	if err != nil || len(grpcFirst.GetItems()) != 1 || grpcFirst.GetNextCursor() == "" {
		t.Fatalf("gRPC first page %#v err=%v", grpcFirst, err)
	}
	grpcSecond, err := client.ListClients(grpcContext, &stewardmeshv1.ListBridgeClientsRequest{Limit: 1, Cursor: grpcFirst.GetNextCursor()})
	if err != nil || len(grpcSecond.GetItems()) != 1 || grpcSecond.GetItems()[0].GetId() == grpcFirst.GetItems()[0].GetId() {
		t.Fatalf("gRPC second page %#v err=%v", grpcSecond, err)
	}
	restFirst := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, "", http.MethodGet, "/api/v1/bridge/clients?limit=1", "", nil)
	if restFirst.Code != http.StatusOK {
		t.Fatalf("REST first page: %d %s", restFirst.Code, restFirst.Body.String())
	}
	var restPage struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(restFirst.Body.Bytes(), &restPage); err != nil || len(restPage.Items) != 1 || restPage.Items[0].ID != grpcFirst.GetItems()[0].GetId() || restPage.NextCursor != grpcFirst.GetNextCursor() {
		t.Fatalf("REST/gRPC list mismatch %#v / %#v err=%v", restPage, grpcFirst, err)
	}

	grantID, _ := authorizeBridgeClient(t, app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, created.GetId())
	grants, err := client.ListGrants(grpcContext, &stewardmeshv1.ListBridgeGrantsRequest{Limit: 1})
	if err != nil || len(grants.GetItems()) != 1 || grants.GetItems()[0].GetId() != grantID {
		t.Fatalf("gRPC grant list %#v err=%v", grants, err)
	}
	revokedGrant, err := client.RevokeGrant(grpcContext, &stewardmeshv1.RevokeBridgeGrantRequest{GrantId: grantID})
	if err != nil || revokedGrant.GetRevokedAt() == nil {
		t.Fatalf("gRPC grant revoke %#v err=%v", revokedGrant, err)
	}
	revokedClient, err := client.RevokeClient(grpcContext, &stewardmeshv1.RevokeBridgeClientRequest{ClientId: created.GetId()})
	if err != nil || revokedClient.GetRevokedAt() == nil {
		t.Fatalf("gRPC client revoke %#v err=%v", revokedClient, err)
	}
	restClients := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, "", http.MethodGet, "/api/v1/bridge/clients?limit=100", "", nil)
	restGrants := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, "", http.MethodGet, "/api/v1/bridge/grants?limit=100", "", nil)
	var restRevocations struct {
		Items []struct {
			ID        string `json:"id"`
			RevokedAt string `json:"revokedAt"`
		} `json:"items"`
	}
	if restClients.Code != http.StatusOK || json.Unmarshal(restClients.Body.Bytes(), &restRevocations) != nil || !containsBridgeRevocation(restRevocations.Items, created.GetId()) {
		t.Fatalf("gRPC client revocation missing from REST: %d %s", restClients.Code, restClients.Body.String())
	}
	restRevocations.Items = nil
	if restGrants.Code != http.StatusOK || json.Unmarshal(restGrants.Body.Bytes(), &restRevocations) != nil || !containsBridgeRevocation(restRevocations.Items, grantID) {
		t.Fatalf("gRPC grant revocation missing from REST: %d %s", restGrants.Code, restGrants.Body.String())
	}
	logout := bridgeRequest(app.Handler(), cfg.AllowedOrigin, cookie, session.CSRF, http.MethodPost, "/api/v1/auth/logout", "", nil)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
	if _, err := client.ListClients(grpcContext, &stewardmeshv1.ListBridgeClientsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked Guard session survived gRPC revalidation code=%s err=%v", status.Code(err), err)
	}
}

func containsBridgeRevocation(items []struct {
	ID        string `json:"id"`
	RevokedAt string `json:"revokedAt"`
}, expectedID string) bool {
	for _, item := range items {
		if item.ID == expectedID && item.RevokedAt != "" {
			return true
		}
	}
	return false
}

func authorizeBridgeClient(t *testing.T, handler http.Handler, issuer string, cookie *http.Cookie, csrf, clientID string) (string, string) {
	t.Helper()
	verifier := strings.Repeat("A", 43)
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:7777/callback"},
		"resource": {issuer + "/mcp"}, "scope": {"mcp:resources assets:read"}, "state": {"grpc-parity"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
	}
	authorize := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	authorize.AddCookie(cookie)
	authorizeResponse := httptest.NewRecorder()
	handler.ServeHTTP(authorizeResponse, authorize)
	if authorizeResponse.Code != http.StatusSeeOther {
		t.Fatalf("authorize: %d %s", authorizeResponse.Code, authorizeResponse.Body.String())
	}
	consentLocation, _ := url.Parse(authorizeResponse.Header().Get("Location"))
	decision := bridgeRequest(handler, issuer, cookie, csrf, http.MethodPost, "/api/v1/bridge/consents/"+consentLocation.Query().Get("consent")+"/decision", "application/json", strings.NewReader(`{"approved":true}`))
	if decision.Code != http.StatusOK {
		t.Fatalf("consent: %d %s", decision.Code, decision.Body.String())
	}
	var decisionBody struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := json.Unmarshal(decision.Body.Bytes(), &decisionBody); err != nil {
		t.Fatal(err)
	}
	callback, _ := url.Parse(decisionBody.RedirectTo)
	tokenForm := url.Values{
		"grant_type": {"authorization_code"}, "code": {callback.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {"http://127.0.0.1:7777/callback"}, "resource": {issuer + "/mcp"}, "code_verifier": {verifier},
	}
	token := bridgeRequest(handler, issuer, nil, "", http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(tokenForm.Encode()))
	if token.Code != http.StatusOK {
		t.Fatalf("token: %d %s", token.Code, token.Body.String())
	}
	var tokenBody struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(token.Body.Bytes(), &tokenBody); err != nil || tokenBody.AccessToken == "" {
		t.Fatalf("token payload: %s err=%v", token.Body.String(), err)
	}
	grants := bridgeRequest(handler, issuer, cookie, "", http.MethodGet, "/api/v1/bridge/grants?limit=1", "", nil)
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(grants.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("grant lookup: %d %s err=%v", grants.Code, grants.Body.String(), err)
	}
	return page.Items[0].ID, tokenBody.AccessToken
}
