package sailpoint

// Requirement: REQ-DIRECTORY-EXPANSION-004. Feature: identity.directory.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

const (
	testIdentityOne = "2c9180835d2e5168015d32f890ca1581"
	testIdentityTwo = "2c9180835d2e5168015d32f890ca1582"
	testAccountOne  = "2c9180835d2e5168015d32f890ca1583"
	testAccountTwo  = "2c9180835d2e5168015d32f890ca1584"
	testSource      = "2c9180835d2e5168015d32f890ca1585"
	testWorkgroup   = "2c9180835d2e5168015d32f890ca1586"
	testRole        = "2c9180835d2e5168015d32f890ca1587"
)

func TestConnectorNormalizesPaginatedIdentitiesAccountsDepartmentsGroupsRolesAndMembershipsReadOnly(t *testing.T) {
	var mu sync.Mutex
	methods := make([]string, 0)
	updated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/oauth/token" {
			if r.Method != http.MethodPost || r.URL.RawQuery != "" {
				t.Fatalf("unsafe token request %s %s", r.Method, r.URL.String())
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "0123456789abcdef" ||
				r.Form.Get("client_secret") != "abcdef0123456789" {
				t.Fatalf("unexpected token form %v", r.Form)
			}
			writeJSON(t, w, map[string]any{"access_token": "test-access-token", "token_type": "bearer", "expires_in": 600})
			return
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-access-token" ||
			r.Header.Get("X-SailPoint-Experimental") != "true" {
			t.Fatalf("SailPoint connector attempted a non-read or unauthenticated operation: %s %v", r.Method, r.Header)
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("count") != "true" {
			t.Fatalf("collection request was not bounded: %s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/v2025/identities":
			identities := []any{
				map[string]any{"id": testIdentityOne, "name": map[bool]string{false: "Ada Example", true: "Ada Updated"}[updated],
					"emailAddress": "ADA@EXAMPLE.TEST", "identityStatus": map[bool]string{false: "ACTIVE", true: "DEACTIVATED"}[updated],
					"attributes": map[string]any{"department": "Engineering", "lifecycleState": "active"}},
				map[string]any{"id": testIdentityTwo, "name": "Inactive Example", "emailAddress": "inactive@example.test", "identityStatus": "DISABLED",
					"attributes": map[string]any{"department": "Finance"}},
			}
			writeCollection(t, w, identities, offset)
		case "/v2025/accounts":
			accounts := []any{
				map[string]any{"id": testAccountOne, "name": "Ada workstation account", "nativeIdentity": "ada", "sourceId": testSource,
					"sourceName": "Workstations", "identityId": testIdentityOne, "disabled": false, "locked": false, "uncorrelated": false},
				map[string]any{"id": testAccountTwo, "name": "Ada legacy account", "nativeIdentity": "ada-old", "sourceId": testSource,
					"sourceName": "Workstations", "identity": map[string]any{"id": testIdentityOne}, "disabled": true, "locked": true, "uncorrelated": false},
			}
			writeCollection(t, w, accounts, offset)
		case "/v2025/workgroups":
			writeCollection(t, w, []any{map[string]any{"id": testWorkgroup, "name": "Application Owners", "description": "Governance group",
				"owner": map[string]any{"id": testIdentityOne}}}, offset)
		case "/v2025/workgroups/" + testWorkgroup + "/members":
			writeCollection(t, w, []any{
				map[string]any{"id": testIdentityOne, "displayName": "Ada Example"},
				map[string]any{"id": testIdentityTwo, "displayName": "Inactive Example"},
			}, offset)
		case "/v2025/roles":
			writeCollection(t, w, []any{map[string]any{"id": testRole, "name": "Finance Reader", "description": "Read finance records",
				"enabled": false, "requestable": true, "owner": map[string]any{"id": testIdentityOne}}}, offset)
		case "/v2025/roles/" + testRole + "/assigned-identities":
			writeCollection(t, w, []any{map[string]any{"id": testIdentityOne, "name": "Ada Example"}}, offset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	connector := newTestConnector(t, server, Options{Sleep: noSleep, pageSize: 1})
	page, err := connector.PullPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !page.CompleteSnapshot || page.NextCursor != "" || len(page.Records) != 16 {
		t.Fatalf("unexpected normalized snapshot records=%d complete=%v cursor=%q", len(page.Records), page.CompleteSnapshot, page.NextCursor)
	}
	byID := recordsByID(page.Records)
	identity := byID["identity:"+testIdentityOne]
	if identity.DisplayName != "Ada Example" || identity.Email != "ada@example.test" || identity.Department != "Engineering" ||
		identity.DirectoryAttributes["account-count"] != "2" || identity.DirectoryAttributes["inactive-account-count"] != "1" ||
		len(identity.GroupSourceIDs) != 3 {
		t.Fatalf("identity normalization lost provider data %#v", identity)
	}
	inactive := byID["identity:"+testIdentityTwo]
	if inactive.Status != "inactive" || inactive.Department != "Finance" {
		t.Fatalf("inactive identity was not retained %#v", inactive)
	}
	account := byID["account:"+testAccountTwo]
	if account.IdentityKind != "shared" || account.Status != "inactive" || account.DirectoryAttributes["source-name"] != "Workstations" ||
		account.DirectoryAttributes["owner-identity-id"] != testIdentityOne || account.DirectoryAttributes["locked"] != "true" ||
		len(account.GroupSourceIDs) != 1 {
		t.Fatalf("account normalization lost source ownership %#v", account)
	}
	role := byID["role:"+testRole]
	if role.Kind != directoryexpansion.RecordGroup || role.Status != "inactive" || role.NormalizedMetadata["requestable"] != "true" {
		t.Fatalf("role normalization lost governance data %#v", role)
	}
	if byID["workgroup:"+testWorkgroup].NormalizedMetadata["directory-object-kind"] != "governance-group" {
		t.Fatal("governance group was not normalized")
	}

	updated = true
	second, err := connector.PullPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	updatedIdentity := recordsByID(second.Records)["identity:"+testIdentityOne]
	if updatedIdentity.DisplayName != "Ada Updated" || updatedIdentity.Status != "inactive" {
		t.Fatalf("identity update and inactive state were not reflected %#v", updatedIdentity)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, method := range methods {
		if method != "POST /oauth/token" && !strings.HasPrefix(method, "GET ") {
			t.Fatalf("unexpected SailPoint write operation %q", method)
		}
	}
}

func TestConnectorRetriesOnlyBoundedSailPointReads(t *testing.T) {
	var calls, sleeps int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "Bearer", "expires_in": 60})
			return
		}
		if r.URL.Path == "/v2025/identities" {
			calls++
			if calls == 1 {
				w.Header().Set("Retry-After", "99")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writeCollection(t, w, []any{validIdentity(testIdentityOne)}, 0)
			return
		}
		writeCollection(t, w, []any{}, 0)
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
	if err != nil || len(page.Records) != 1 || calls != 2 || sleeps != 1 {
		t.Fatalf("unexpected retry result records=%d calls=%d sleeps=%d err=%v", len(page.Records), calls, sleeps, err)
	}
}

func TestConnectorChoosesDeterministicDepartmentDisplayName(t *testing.T) {
	reversed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "bearer", "expires_in": 60})
			return
		}
		if r.URL.Path == "/v2025/identities" {
			identities := []any{
				map[string]any{"id": testIdentityOne, "name": "Ada", "emailAddress": "ada@example.test", "identityStatus": "ACTIVE", "attributes": map[string]any{"department": "engineering"}},
				map[string]any{"id": testIdentityTwo, "name": "Grace", "emailAddress": "grace@example.test", "identityStatus": "ACTIVE", "attributes": map[string]any{"department": "Engineering"}},
			}
			if reversed {
				identities[0], identities[1] = identities[1], identities[0]
			}
			writeCollection(t, w, identities, mustOffset(t, r))
			return
		}
		writeCollection(t, w, []any{}, 0)
	}))
	defer server.Close()
	connector := newTestConnector(t, server, Options{Sleep: noSleep, pageSize: 1})
	for pull := 0; pull < 2; pull++ {
		page, err := connector.PullPage(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		var department directoryexpansion.Record
		for _, record := range page.Records {
			if record.NormalizedMetadata["directory-object-kind"] == "department" {
				department = record
			}
		}
		if department.DisplayName != "Engineering" {
			t.Fatalf("department casing depended on provider order: %#v", department)
		}
		reversed = true
	}
}

func TestConnectorFeedsExactPlanIntoPeopleGraphMappingsAndSailPointAudit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			writeJSON(t, w, map[string]any{"access_token": "integration-token", "token_type": "bearer", "expires_in": 60})
			return
		}
		if r.URL.Path == "/v2025/identities" {
			writeCollection(t, w, []any{map[string]any{"id": testIdentityOne, "name": "Ada Integrated", "emailAddress": "ada@example.test",
				"identityStatus": "ACTIVE", "lifecycleState": map[string]any{"stateName": "active"},
				"attributes": map[string]any{"department": "Engineering"}}}, 0)
			return
		}
		if r.URL.Path == "/v2025/workgroups" {
			writeCollection(t, w, []any{map[string]any{"id": testWorkgroup, "name": "Application Owners"}}, 0)
			return
		}
		if r.URL.Path == "/v2025/workgroups/"+testWorkgroup+"/members" {
			writeCollection(t, w, []any{map[string]any{"id": testIdentityOne, "displayName": "Ada Integrated"}}, 0)
			return
		}
		if r.URL.Path == "/v2025/roles" {
			writeCollection(t, w, []any{map[string]any{"id": testRole, "name": "Directory Reader", "enabled": true,
				"owner": map[string]any{"id": testIdentityOne}}}, 0)
			return
		}
		if r.URL.Path == "/v2025/roles/"+testRole+"/assigned-identities" {
			writeCollection(t, w, []any{map[string]any{"id": testIdentityOne, "displayName": "Ada Integrated"}}, 0)
			return
		}
		writeCollection(t, w, []any{}, 0)
	}))
	defer server.Close()
	connector := newTestConnector(t, server, Options{Sleep: noSleep})
	registry, err := directoryexpansion.NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	importStore := repository.NewMemoryDirectoryImportStore()
	peopleStore := repository.NewMemoryPeopleStore()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), guard.NewArgon2idHasher(), foundation.NopAuditor{}, nil,
		guard.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	peopleTarget, err := directoryexpansion.NewPeopleTarget(peopleStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	groupTarget, err := directoryexpansion.NewGroupTarget(importStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := directoryexpansion.NewDirectoryTarget(peopleTarget, groupTarget)
	if err != nil {
		t.Fatal(err)
	}
	auditor := &captureAuditor{}
	service, err := directoryexpansion.NewService(importStore, target, auditor, registry,
		directoryexpansion.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	authentication := guard.Authentication{Principal: guard.Principal{Subject: "account:test-administrator"}}
	preview, err := service.Preview(context.Background(), authentication,
		directoryexpansion.PreviewRequest{SourceSystemID: "sailpoint-primary"}, "sailpoint-integration-preview")
	if err != nil || preview.Batch.Provider != directoryexpansion.SailPointProvider || !preview.Batch.CompleteSnapshot ||
		preview.Batch.Counts.Created != 7 {
		t.Fatalf("unexpected SailPoint preview %#v err=%v", preview, err)
	}
	applied, err := service.Apply(context.Background(), authentication, preview.Batch.ID, "sailpoint-integration-apply")
	if err != nil || applied.Batch.Status != directoryexpansion.BatchApplied {
		t.Fatalf("SailPoint exact plan was not applied %#v err=%v", applied, err)
	}
	identities, err := peopleStore.SearchIdentities(context.Background(), "example-org", people.IdentityQuery{Limit: 100}, people.Visibility{All: true})
	if err != nil || len(identities) != 1 || identities[0].DisplayName != "Ada Integrated" ||
		identities[0].ProviderSubject != "identity:"+testIdentityOne {
		t.Fatalf("SailPoint identity did not reconcile into People %#v err=%v", identities, err)
	}
	mappings, err := importStore.ListMappings(context.Background(), "example-org", "sailpoint-primary")
	if err != nil || len(mappings) != 7 {
		t.Fatalf("SailPoint source mappings were not durable %#v err=%v", mappings, err)
	}
	graphStore, err := directoryexpansion.NewManagedGraphStore(importStore, "example-org")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), directoryexpansion.GraphQuery{Scope: directoryexpansion.GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || len(graph.Nodes) != 4 || len(graph.Edges) != 3 {
		t.Fatalf("SailPoint memberships did not reconcile into the directory graph %#v err=%v", graph, err)
	}
	for _, edge := range graph.Edges {
		if edge.Kind != "member_of" {
			t.Fatalf("SailPoint graph contained an unexpected relationship %#v", edge)
		}
	}
	if len(auditor.events) < 2 {
		t.Fatalf("SailPoint preview/apply audits were not emitted %#v", auditor.events)
	}
	for _, event := range auditor.events {
		encoded, _ := json.Marshal(event)
		if event.Metadata["requirementId"] != directoryexpansion.SailPointRequirementID || strings.Contains(string(encoded), "Ada") ||
			strings.Contains(string(encoded), "integration-token") {
			t.Fatalf("SailPoint audit lost provenance or leaked provider data: %s", encoded)
		}
	}
}

func TestConnectorRejectsMalformedDuplicateAndUnboundedProviderResponsesSafely(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "duplicate identities", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				writeCollection(t, w, []any{validIdentity(testIdentityOne), validIdentity(testIdentityOne)}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "duplicate governance memberships", handler: func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2025/identities":
				writeCollection(t, w, []any{validIdentity(testIdentityOne)}, 0)
			case "/v2025/workgroups":
				writeCollection(t, w, []any{map[string]any{"id": testWorkgroup, "name": "Owners"}}, 0)
			case "/v2025/workgroups/" + testWorkgroup + "/members":
				writeCollection(t, w, []any{map[string]any{"id": testIdentityOne, "name": "Ada"}, map[string]any{"id": testIdentityOne, "name": "Ada"}}, 0)
			default:
				writeCollection(t, w, []any{}, 0)
			}
		}},
		{name: "malformed json", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				_, _ = w.Write([]byte(`[{`))
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "oversized response", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				w.Header().Set("Content-Length", strconv.Itoa(maximumResponseBytes+1))
				_, _ = w.Write([]byte(`[]`))
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "redirect rejected", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "malformed attribute", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				value := validIdentity(testIdentityOne)
				value["attributes"] = map[string]any{"department": map[string]any{"private": "value"}}
				writeCollection(t, w, []any{value}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "ambiguous case-insensitive attribute", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				value := validIdentity(testIdentityOne)
				value["attributes"] = map[string]any{"department": "Engineering", "Department": "Finance"}
				writeCollection(t, w, []any{value}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "missing required account status", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/accounts" {
				writeCollection(t, w, []any{map[string]any{"id": testAccountOne, "name": "Account", "nativeIdentity": "ada",
					"sourceId": testSource, "sourceName": "Workstations", "locked": false, "uncorrelated": false}}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "missing required role owner", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/roles" {
				writeCollection(t, w, []any{map[string]any{"id": testRole, "name": "Ownerless role", "enabled": true}}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "inconsistent total", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				w.Header().Set("X-Total-Count", "0")
				writeJSON(t, w, []any{validIdentity(testIdentityOne)})
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
		{name: "record bound", options: Options{MaximumRecords: 1}, handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2025/identities" {
				writeCollection(t, w, []any{validIdentity(testIdentityOne), validIdentity(testIdentityTwo)}, 0)
				return
			}
			writeCollection(t, w, []any{}, 0)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/token" {
					writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "bearer", "expires_in": 60})
					return
				}
				test.handler(w, r)
			}))
			defer server.Close()
			test.options.Sleep = noSleep
			if test.options.pageSize == 0 {
				test.options.pageSize = 1
			}
			connector := newTestConnector(t, server, test.options)
			_, err := connector.PullPage(context.Background(), "")
			var classified *directoryexpansion.ClassifiedError
			if !errors.As(err, &classified) || classified.Class != directoryexpansion.FailurePermanent || classified.Retryable {
				t.Fatalf("expected safe permanent failure, got %T %v", err, err)
			}
			if strings.Contains(err.Error(), testIdentityOne) || strings.Contains(err.Error(), "private") {
				t.Fatalf("safe error leaked provider data: %v", err)
			}
		})
	}
}

func TestConnectorValidatesTenantEndpointAndCredentialsWithoutLeakingSecrets(t *testing.T) {
	secret := "super-secret-credential"
	tests := []Config{
		{BaseURL: "http://tenant.api.identitynow.com", ClientID: "0123456789abcdef", ClientSecret: secret},
		{BaseURL: "https://identitynow.example.test", ClientID: "0123456789abcdef", ClientSecret: secret},
		{BaseURL: "https://user:private@tenant.api.identitynow.com", ClientID: "0123456789abcdef", ClientSecret: secret},
		{BaseURL: "https://tenant.api.identitynow.com/arbitrary", ClientID: "0123456789abcdef", ClientSecret: secret},
		{BaseURL: "https://tenant.api.identitynow.com?", ClientID: "0123456789abcdef", ClientSecret: secret},
		{BaseURL: "https://tenant.api.identitynow.com", ClientID: "short", ClientSecret: secret},
		{BaseURL: "https://tenant.api.identitynow.com", ClientID: "0123456789abcdef", ClientSecret: "short"},
		{SourceSystemID: "bad source", BaseURL: "https://tenant.api.identitynow.com", ClientID: "0123456789abcdef", ClientSecret: secret},
	}
	for index, configuration := range tests {
		if connector, err := NewConnector(configuration, Options{}); err == nil || connector != nil {
			t.Fatalf("case %d expected invalid configuration", index)
		} else if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private") {
			t.Fatalf("configuration error leaked a credential: %v", err)
		}
	}
}

func TestConnectorRejectsMalformedTokenResponses(t *testing.T) {
	for _, test := range []struct {
		name  string
		token map[string]any
	}{
		{name: "invalid access token", token: map[string]any{"access_token": "private token with spaces", "token_type": "bearer", "expires_in": 60}},
		{name: "invalid token type", token: map[string]any{"access_token": "private-token", "token_type": "basic", "expires_in": 60}},
		{name: "missing expiry", token: map[string]any{"access_token": "private-token", "token_type": "bearer"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(t, w, test.token) }))
			defer server.Close()
			connector := newTestConnector(t, server, Options{Sleep: noSleep})
			_, err := connector.PullPage(context.Background(), "")
			var classified *directoryexpansion.ClassifiedError
			if !errors.As(err, &classified) || classified.Class != directoryexpansion.FailurePermanent ||
				strings.Contains(err.Error(), "private") {
				t.Fatalf("malformed token response was not rejected safely: %#v err=%v", classified, err)
			}
		})
	}
}

func TestConnectorClassifiesProviderFailuresWithoutReturningBodies(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		class     directoryexpansion.FailureClass
		retryable bool
	}{
		{name: "forbidden", status: http.StatusForbidden, class: directoryexpansion.FailurePermanent},
		{name: "unavailable", status: http.StatusServiceUnavailable, class: directoryexpansion.FailureTransient, retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/token" {
					writeJSON(t, w, map[string]any{"access_token": "token", "token_type": "bearer", "expires_in": 60})
					return
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"message":"private.user@example.test"}`))
			}))
			defer server.Close()
			connector := newTestConnector(t, server, Options{Sleep: noSleep})
			_, err := connector.PullPage(context.Background(), "")
			var classified *directoryexpansion.ClassifiedError
			if !errors.As(err, &classified) || classified.Class != test.class || classified.Retryable != test.retryable {
				t.Fatalf("unexpected classification %#v err=%v", classified, err)
			}
			if strings.Contains(err.Error(), "private.user") {
				t.Fatalf("provider response body leaked: %v", err)
			}
		})
	}
}

func newTestConnector(t *testing.T, server *httptest.Server, options Options) *Connector {
	t.Helper()
	options.baseURL = server.URL
	connector, err := NewConnector(Config{SourceSystemID: "sailpoint-primary", BaseURL: "https://tenant.api.identitynow.com",
		ClientID: "0123456789abcdef", ClientSecret: "abcdef0123456789"}, options)
	if err != nil {
		t.Fatal(err)
	}
	if connector.SourceSystem().ID != "sailpoint-primary" || connector.SourceSystem().Provider != directoryexpansion.SailPointProvider ||
		!strings.HasPrefix(connector.SourceSystem().ConfigRevision, "sailpoint-") {
		t.Fatalf("unexpected safe source identity %#v", connector.SourceSystem())
	}
	return connector
}

func validIdentity(id string) map[string]any {
	return map[string]any{"id": id, "name": "Directory User", "emailAddress": strings.ToLower(id[:4]) + "@example.test", "identityStatus": "ACTIVE",
		"attributes": map[string]any{}}
}

func recordsByID(records []directoryexpansion.Record) map[string]directoryexpansion.Record {
	result := make(map[string]directoryexpansion.Record, len(records))
	for _, record := range records {
		result[record.SourceRecordID] = record
	}
	return result
}

func writeCollection(t *testing.T, w http.ResponseWriter, values []any, offset int) {
	t.Helper()
	w.Header().Set("X-Total-Count", strconv.Itoa(len(values)))
	if offset >= len(values) {
		writeJSON(t, w, []any{})
		return
	}
	writeJSON(t, w, []any{values[offset]})
}

func mustOffset(t *testing.T, r *http.Request) int {
	t.Helper()
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil {
		t.Fatal(err)
	}
	return offset
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

type captureAuditor struct{ events []foundation.AuditEvent }

func (a *captureAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}
