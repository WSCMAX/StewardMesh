package entra

// Requirements: REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-009.
// Features: identity.directory, experience.help.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

const (
	testTenantID = "11111111-1111-4111-8111-111111111111"
	testClientID = "22222222-2222-4222-8222-222222222222"
	testUserOne  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testUserTwo  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testGroup    = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func TestConnectorPullsBoundedPaginatedUsersGroupsMembershipsAndAttributes(t *testing.T) {
	var mu sync.Mutex
	methods := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/token" {
			if r.Method != http.MethodPost {
				t.Fatalf("token endpoint used %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != testClientID ||
				r.Form.Get("client_secret") != "0123456789abcdef" || r.Form.Get("scope") != "https://graph.microsoft.com/.default" {
				t.Fatalf("unexpected token request fields %v", r.Form)
			}
			writeJSON(t, w, map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("Microsoft Graph operation used non-read method %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("missing bounded Graph request headers: %v", r.Header)
		}
		switch r.URL.Path {
		case "/v1.0/users":
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, map[string]any{"value": []any{map[string]any{
					"id": testUserTwo, "displayName": "Inactive User", "mail": "", "userPrincipalName": "inactive@example.test",
					"accountEnabled": false, "department": "Finance", "jobTitle": "Analyst", "officeLocation": "Room 2", "userType": "Member",
				}}})
				return
			}
			writeJSON(t, w, map[string]any{
				"value": []any{map[string]any{
					"id": testUserOne, "displayName": "Active User", "mail": "active@example.test", "userPrincipalName": "active@example.test",
					"accountEnabled": true, "department": "Information Technology", "jobTitle": "Engineer", "officeLocation": "Room 1", "userType": "Member",
				}},
				"@odata.nextLink": serverURL(r) + "/v1.0/users?page=2",
			})
		case "/v1.0/groups":
			writeJSON(t, w, map[string]any{"value": []any{map[string]any{
				"id": testGroup, "displayName": "Platform Operators",
				"mail": "platform@example.test", "mailEnabled": true, "securityEnabled": true,
			}}})
		case "/v1.0/groups/" + testGroup + "/members":
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, map[string]any{"value": []any{map[string]string{"id": testUserTwo}}})
				return
			}
			writeJSON(t, w, map[string]any{
				"value":           []any{map[string]string{"id": testUserOne}},
				"@odata.nextLink": serverURL(r) + "/v1.0/groups/" + testGroup + "/members?page=2",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	connector := newTestConnector(t, server, Options{Sleep: noSleep})
	page, err := connector.PullPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !page.CompleteSnapshot || page.NextCursor != "" || len(page.Records) != 3 {
		t.Fatalf("unexpected snapshot %#v", page)
	}
	byID := make(map[string]directoryexpansion.Record, len(page.Records))
	for _, record := range page.Records {
		byID[record.SourceRecordID] = record
	}
	active := byID["user:"+testUserOne]
	if active.Status != "active" || active.Department != "Information Technology" || active.DirectoryAttributes["job-title"] != "Engineer" ||
		len(active.GroupSourceIDs) != 1 || active.GroupSourceIDs[0] != "group:"+testGroup {
		t.Fatalf("active user normalization lost directory data %#v", active)
	}
	inactive := byID["user:"+testUserTwo]
	if inactive.Status != "inactive" || inactive.Email != "inactive@example.test" || inactive.Department != "Finance" ||
		len(inactive.GroupSourceIDs) != 1 {
		t.Fatalf("inactive user was not retained %#v", inactive)
	}
	group := byID["group:"+testGroup]
	if group.IdentityKind != "shared" || group.DirectoryAttributes["security-enabled"] != "true" || group.Email != "platform@example.test" {
		t.Fatalf("group normalization lost safe attributes %#v", group)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, method := range methods {
		if !strings.HasPrefix(method, "GET ") && method != "POST /token" {
			t.Fatalf("unexpected external operation %q", method)
		}
	}
}

func TestConnectorRetriesThrottledGraphReadsWithoutRepeatingWrites(t *testing.T) {
	var userCalls, sleeps int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected Graph method %s", r.Method)
		}
		switch {
		case r.URL.Path == "/v1.0/users":
			userCalls++
			if userCalls == 1 {
				w.Header().Set("Retry-After", "99")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writeJSON(t, w, map[string]any{"value": []any{validGraphUser(testUserOne)}})
		case r.URL.Path == "/v1.0/groups":
			writeJSON(t, w, map[string]any{"value": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	connector := newTestConnector(t, server, Options{Sleep: func(_ context.Context, delay time.Duration) error {
		sleeps++
		if delay != maximumRetryDelay {
			t.Fatalf("Retry-After was not bounded: %s", delay)
		}
		return nil
	}})
	page, err := connector.PullPage(context.Background(), "")
	if err != nil || len(page.Records) != 1 || userCalls != 2 || sleeps != 1 {
		t.Fatalf("unexpected retry result records=%d calls=%d sleeps=%d err=%v", len(page.Records), userCalls, sleeps, err)
	}
}

func TestConnectorRejectsDuplicatesMalformedResponsesUnsafeLinksAndBounds(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		handler func(*testing.T, http.ResponseWriter, *http.Request)
	}{
		{name: "duplicate users", handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1.0/users":
				writeJSON(t, w, map[string]any{"value": []any{validGraphUser(testUserOne), validGraphUser(testUserOne)}})
			case "/v1.0/groups":
				writeJSON(t, w, map[string]any{"value": []any{}})
			}
		}},
		{name: "duplicate memberships", handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/v1.0/users":
				writeJSON(t, w, map[string]any{"value": []any{validGraphUser(testUserOne)}})
			case r.URL.Path == "/v1.0/groups":
				writeJSON(t, w, map[string]any{"value": []any{validGraphGroup(testGroup)}})
			case strings.HasSuffix(r.URL.Path, "/members"):
				writeJSON(t, w, map[string]any{"value": []any{map[string]string{"id": testUserOne}, map[string]string{"id": testUserOne}}})
			}
		}},
		{name: "malformed JSON", handler: func(_ *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				_, _ = w.Write([]byte(`{"value":[`))
			}
		}},
		{name: "oversized response", handler: func(_ *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				w.Header().Set("Content-Length", strconv.Itoa(maximumResponseBytes+1))
				_, _ = w.Write([]byte(`{"value":[]}`))
			}
		}},
		{name: "unsafe next link", handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				writeJSON(t, w, map[string]any{"value": []any{}, "@odata.nextLink": "http://169.254.169.254/latest/meta-data"})
			}
		}},
		{name: "same host unexpected endpoint", handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				writeJSON(t, w, map[string]any{"value": []any{}, "@odata.nextLink": serverURL(r) + "/v1.0/applications"})
			}
		}},
		{name: "pagination loop", handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				query := url.Values{"$select": {userSelect}, "$top": {"999"}}
				writeJSON(t, w, map[string]any{"value": []any{}, "@odata.nextLink": serverURL(r) + "/v1.0/users?" + query.Encode()})
			}
		}},
		{name: "page bound", options: Options{MaximumPages: 1}, handler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1.0/users" {
				writeJSON(t, w, map[string]any{"value": []any{}, "@odata.nextLink": serverURL(r) + "/v1.0/users?page=2"})
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "Bearer", "expires_in": 3600})
					return
				}
				test.handler(t, w, r)
			}))
			defer server.Close()
			test.options.Sleep = noSleep
			connector := newTestConnector(t, server, test.options)
			_, err := connector.PullPage(context.Background(), "")
			var classified *directoryexpansion.ClassifiedError
			if !errors.As(err, &classified) || classified.Class != directoryexpansion.FailurePermanent || classified.Retryable {
				t.Fatalf("expected safe permanent failure, got %T %v", err, err)
			}
			if strings.Contains(err.Error(), testUserOne) || strings.Contains(err.Error(), "169.254") {
				t.Fatalf("safe error leaked provider data: %v", err)
			}
		})
	}
}

func TestConnectorValidatesTenantCredentialsAndDoesNotLeakSecrets(t *testing.T) {
	secret := "super-secret-credential"
	tests := []struct {
		configuration Config
		options       Options
	}{
		{configuration: Config{TenantID: "common", ClientID: testClientID, ClientSecret: secret}},
		{configuration: Config{TenantID: testTenantID, ClientID: "not-a-uuid", ClientSecret: secret}},
		{configuration: Config{TenantID: testTenantID, ClientID: testClientID, ClientSecret: "short"}},
		{configuration: Config{SourceSystemID: "bad source", TenantID: testTenantID, ClientID: testClientID, ClientSecret: secret}},
		{configuration: Config{TenantID: testTenantID, ClientID: testClientID, ClientSecret: secret}, options: Options{graphBaseURL: "https://example.test/v1.0"}},
	}
	for index, test := range tests {
		if connector, err := NewConnector(test.configuration, test.options); err == nil || connector != nil {
			t.Fatalf("case %d expected invalid configuration", index)
		} else if strings.Contains(err.Error(), secret) {
			t.Fatalf("configuration error leaked a credential: %v", err)
		}
	}
}

func TestConnectorProductionTransportDisablesAmbientProxy(t *testing.T) {
	connector, err := NewConnector(Config{
		TenantID: testTenantID, ClientID: testClientID, ClientSecret: "0123456789abcdef",
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	bounded, ok := connector.httpClient.Transport.(boundedResponseTransport)
	if !ok {
		t.Fatalf("unexpected bounded transport %T", connector.httpClient.Transport)
	}
	transport, ok := bounded.base.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected production transport %T", bounded.base)
	}
	if transport.Proxy != nil {
		t.Fatal("production connector inherited an ambient proxy resolver")
	}
}

func TestConnectorClassifiesCredentialAndTransientFailuresSafely(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		class     directoryexpansion.FailureClass
		retryable bool
	}{
		{name: "forbidden", status: http.StatusForbidden, class: directoryexpansion.FailurePermanent},
		{name: "unavailable", status: http.StatusServiceUnavailable, class: directoryexpansion.FailureTransient, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "Bearer", "expires_in": 3600})
					return
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":{"message":"private.user@example.test"}}`))
			}))
			defer server.Close()
			connector := newTestConnector(t, server, Options{Sleep: noSleep})
			_, err := connector.PullPage(context.Background(), "")
			var classified *directoryexpansion.ClassifiedError
			if !errors.As(err, &classified) || classified.Class != test.class || classified.Retryable != test.retryable {
				t.Fatalf("unexpected classification %#v err=%v", classified, err)
			}
			if strings.Contains(err.Error(), "private.user") {
				t.Fatalf("provider error body leaked: %v", err)
			}
		})
	}
}

func newTestConnector(t *testing.T, server *httptest.Server, options Options) *Connector {
	t.Helper()
	options.graphBaseURL = server.URL + "/v1.0"
	options.tokenURL = server.URL + "/token"
	connector, err := NewConnector(Config{
		SourceSystemID: "entra-primary", TenantID: testTenantID, ClientID: testClientID,
		ClientSecret: "0123456789abcdef",
	}, options)
	if err != nil {
		t.Fatal(err)
	}
	if connector.SourceSystem().ID != "entra-primary" || connector.SourceSystem().Provider != "entra" ||
		!strings.HasPrefix(connector.SourceSystem().ConfigRevision, "entra-") {
		t.Fatalf("unexpected safe source identity %#v", connector.SourceSystem())
	}
	return connector
}

func validGraphUser(id string) map[string]any {
	return map[string]any{
		"id": id, "displayName": "Directory User", "mail": "user@example.test", "userPrincipalName": "user@example.test",
		"accountEnabled": true, "department": "Technology", "jobTitle": "Engineer", "officeLocation": "Office", "userType": "Member",
	}
}

func validGraphGroup(id string) map[string]any {
	return map[string]any{
		"id": id, "displayName": "Directory Group", "mail": "",
		"mailEnabled": false, "securityEnabled": true,
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
