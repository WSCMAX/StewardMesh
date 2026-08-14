package directoryexpansion_test

// Requirement: REQ-DIRECTORY-EXPANSION-006. Feature: integrations.protocols.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestPeopleSoftConnectorPullsMappedOrganizationLocationBuildingAndDepartmentQueriesReadOnly(t *testing.T) {
	fixture := &peopleSoftFixture{}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, nil)

	page, err := connector.PullPage(context.Background(), "")
	if err != nil || page.CompleteSnapshot || page.NextCursor != "" || len(page.Records) != 8 {
		t.Fatalf("unexpected PeopleSoft snapshot %#v err=%v", page, err)
	}
	bySource := make(map[string]Record, len(page.Records))
	for _, record := range page.Records {
		bySource[record.SourceRecordID] = record
	}
	if bySource["organization:SHARE:CAMPUS"].DisplayName != "Example University" ||
		bySource["location:SHARE:MAIN"].NormalizedMetadata["city"] != "Chicago" ||
		bySource["location:SHARE:MAIN"].NormalizedMetadata["source-setid"] != "SHARE" ||
		bySource["building:SHARE:LIB"].Status != "active" ||
		bySource["department:SHARE:ARCHIVE"].Status != "inactive" {
		t.Fatalf("institution mappings were not normalized: %#v", bySource)
	}
	if findPeopleSoftMembership(t, page.Records, "organization:SHARE:CAMPUS", "location:SHARE:MAIN").MemberKind != MemberGroup ||
		findPeopleSoftMembership(t, page.Records, "location:SHARE:MAIN", "building:SHARE:LIB").GroupSourceID != "location:SHARE:MAIN" ||
		findPeopleSoftMembership(t, page.Records, "location:SHARE:MAIN", "department:SHARE:ARCHIVE").Status != "inactive" {
		t.Fatalf("PeopleSoft hierarchy relationships are incomplete: %#v", bySource)
	}
	system := connector.SourceSystem()
	if system.Provider != PeopleSoftProvider || system.ID != "peoplesoft-campus" || !strings.HasPrefix(system.ConfigRevision, "peoplesoft-") {
		t.Fatalf("unexpected credential-free source identity %#v", system)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fmt.Sprint(fixture.methods) != "[GET GET GET GET]" || len(fixture.authorizations) != 4 {
		t.Fatalf("PeopleSoft adapter was not GET-only: %#v", fixture.methods)
	}
	for _, authorization := range fixture.authorizations {
		if !strings.HasPrefix(authorization, "Basic ") {
			t.Fatalf("expected deployment-owned Basic credential, got %q", authorization)
		}
	}
	if fmt.Sprint(fixture.queries) != "[SM_ORGANIZATIONS SM_LOCATIONS SM_BUILDINGS SM_DEPARTMENTS]" {
		t.Fatalf("QAS query sequence was not deterministic: %#v", fixture.queries)
	}
	expectedFilters := []string{
		"A.DESCR,A.EFF_STATUS,A.ORG_ID,A.SETID",
		"A.CITY,A.DESCR,A.EFF_STATUS,A.LOCATION,A.ORG_ID,A.SETID",
		"A.BUILDING,A.DESCR,A.EFF_STATUS,A.LOCATION,A.SETID",
		"A.DEPTID,A.DESCR,A.EFF_STATUS,A.LOCATION,A.ORG_ID,A.SETID",
	}
	if fmt.Sprint(fixture.filterFields) != fmt.Sprint(expectedFilters) {
		t.Fatalf("QAS filterfields must use Oracle-qualified selectors: got %#v want %#v", fixture.filterFields, expectedFilters)
	}
}

func TestPeopleSoftConnectorUsesBearerAuthenticationWhenConfigured(t *testing.T) {
	fixture := &peopleSoftFixture{}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, func(config *PeopleSoftConnectorConfig) {
		config.Username, config.Password, config.BearerToken = "", "", "fixture-token_123.abc"
	})
	if _, err := connector.PullPage(context.Background(), ""); err != nil {
		t.Fatalf("pull with bearer authentication: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	for _, authorization := range fixture.authorizations {
		if authorization != "Bearer fixture-token_123.abc" {
			t.Fatalf("unexpected bearer authorization %q", authorization)
		}
	}
}

func TestPeopleSoftConnectorRevisionBindsAuthenticatedPrincipalWithoutExposingIt(t *testing.T) {
	base := validPeopleSoftConfig("https://peoplesoft.example.test")
	first, err := NewPeopleSoftConnector(base)
	if err != nil {
		t.Fatal(err)
	}
	rotatedPassword := base
	rotatedPassword.Password = "rotated-reader-password"
	second, err := NewPeopleSoftConnector(rotatedPassword)
	if err != nil {
		t.Fatal(err)
	}
	changedPrincipal := base
	changedPrincipal.Username = "security-filtered-reader"
	third, err := NewPeopleSoftConnector(changedPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := first.SourceSystem().ConfigRevision
	if firstRevision != second.SourceSystem().ConfigRevision || firstRevision == third.SourceSystem().ConfigRevision ||
		strings.Contains(firstRevision, base.Username) || len(firstRevision) != len("peoplesoft-")+64 {
		t.Fatalf("revision did not bind a nonreversible principal fingerprint: %q %q %q", firstRevision, second.SourceSystem().ConfigRevision, third.SourceSystem().ConfigRevision)
	}
}

func TestPeopleSoftConnectorScopesDuplicateRawIDsAndRelationshipsBySETID(t *testing.T) {
	fixture := &peopleSoftFixture{multipleSetIDs: true}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, nil)

	page, err := connector.PullPage(context.Background(), "")
	if err != nil || page.CompleteSnapshot || len(page.Records) != 16 {
		t.Fatalf("SETID-scoped PeopleSoft snapshot failed: records=%d complete=%v err=%v", len(page.Records), page.CompleteSnapshot, err)
	}
	for _, setID := range []string{"SHARE", "LOCAL"} {
		findPeopleSoftMembership(t, page.Records, "organization:"+setID+":CAMPUS", "location:"+setID+":MAIN")
		findPeopleSoftMembership(t, page.Records, "location:"+setID+":MAIN", "building:"+setID+":LIB")
		findPeopleSoftMembership(t, page.Records, "organization:"+setID+":CAMPUS", "department:"+setID+":ARCHIVE")
		findPeopleSoftMembership(t, page.Records, "location:"+setID+":MAIN", "department:"+setID+":ARCHIVE")
	}
}

func TestPeopleSoftConnectorRetriesTransientGETAndRedactsProviderResponses(t *testing.T) {
	fixture := &peopleSoftFixture{transientFailures: 2}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, func(config *PeopleSoftConnectorConfig) {
		config.RetryDelay = func(context.Context, int) error { return nil }
	})
	page, err := connector.PullPage(context.Background(), "")
	if err != nil || page.CompleteSnapshot || fixture.calls != 6 {
		t.Fatalf("bounded retry did not recover: page=%#v calls=%d err=%v", page, fixture.calls, err)
	}

	fixture.failureStatus = http.StatusUnauthorized
	fixture.providerBody = `{"password":"must-not-escape"}`
	_, err = connector.PullPage(context.Background(), "")
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "must-not-escape") {
		t.Fatalf("PeopleSoft provider response escaped the safe error: %v", err)
	}
}

func TestPeopleSoftConnectorRetriesMidBodyReadFailure(t *testing.T) {
	calls := 0
	config := validPeopleSoftConfig("https://peoplesoft.example.test")
	config.RetryDelay = func(context.Context, int) error { return nil }
	config.HTTPClient = &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: &failingReadCloser{content: []byte(`{"status":"success"}`)}}, nil
		}
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		queryName := parts[len(parts)-3]
		rows := (&peopleSoftFixture{}).rows(queryName)
		body, _ := json.Marshal(qasResponse(queryName, rows, len(rows)))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	connector, err := NewPeopleSoftConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	page, err := connector.PullPage(context.Background(), "")
	if err != nil || len(page.Records) != 8 || calls != 5 {
		t.Fatalf("retryable mid-body failure did not recover: records=%d calls=%d err=%v", len(page.Records), calls, err)
	}
}

func TestPeopleSoftConnectorClassifiesExhaustedMidBodyReadFailureAsRetryable(t *testing.T) {
	calls := 0
	config := validPeopleSoftConfig("https://peoplesoft.example.test")
	config.RetryDelay = func(context.Context, int) error { return nil }
	config.HTTPClient = &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: &failingReadCloser{content: []byte(`{"status":"success"}`)}}, nil
	})}
	connector, err := NewPeopleSoftConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connector.PullPage(context.Background(), "")
	var classified *ClassifiedError
	if !errors.As(err, &classified) || classified.Class != FailureTransient || !classified.Retryable || calls != MaximumPeopleSoftRetries {
		t.Fatalf("mid-body read failure was not safely retryable: calls=%d classified=%#v err=%v", calls, classified, err)
	}
}

func TestPeopleSoftConnectorRejectsUnsafeConfigurationAndRedirects(t *testing.T) {
	secret := "must-not-escape-secret"
	valid := validPeopleSoftConfig("https://peoplesoft.example.test")
	valid.Password = secret
	tests := []struct {
		name   string
		mutate func(*PeopleSoftConnectorConfig)
	}{
		{name: "plaintext remote", mutate: func(c *PeopleSoftConnectorConfig) {
			c.BaseURL = "http://peoplesoft.example.test/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1"
		}},
		{name: "credentials in endpoint", mutate: func(c *PeopleSoftConnectorConfig) {
			c.BaseURL = "https://user:secret@peoplesoft.example.test/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1"
		}},
		{name: "wrong service", mutate: func(c *PeopleSoftConnectorConfig) { c.BaseURL = "https://peoplesoft.example.test/arbitrary" }},
		{name: "loopback without opt in", mutate: func(c *PeopleSoftConnectorConfig) {
			c.BaseURL = "https://127.0.0.1/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1"
			c.AllowPrivateNetwork = false
		}},
		{name: "metadata endpoint", mutate: func(c *PeopleSoftConnectorConfig) {
			c.BaseURL = "https://169.254.169.254/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1"
			c.AllowPrivateNetwork = true
		}},
		{name: "mixed credentials", mutate: func(c *PeopleSoftConnectorConfig) { c.BearerToken = "token" }},
		{name: "malformed bearer", mutate: func(c *PeopleSoftConnectorConfig) {
			c.Username, c.Password, c.BearerToken = "", "", "token with spaces"
		}},
		{name: "missing password", mutate: func(c *PeopleSoftConnectorConfig) { c.Password = "" }},
		{name: "ambiguous basic username", mutate: func(c *PeopleSoftConnectorConfig) { c.Username = "integration:reader" }},
		{name: "header injection", mutate: func(c *PeopleSoftConnectorConfig) { c.Username = "reader\r\nX-Unsafe: yes" }},
		{name: "duplicate query", mutate: func(c *PeopleSoftConnectorConfig) { c.DepartmentQuery = c.BuildingQuery }},
		{name: "unsafe query name", mutate: func(c *PeopleSoftConnectorConfig) { c.LocationQuery = "../LOCATION" }},
		{name: "unknown mapping", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"name":{"selector":"A.DESCR","alias":"DESCR"}`, `"name":{"selector":"A.DESCR","alias":"DESCR"},"unknown":"FIELD"`, 1)
		}},
		{name: "missing parent mapping", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"organizationId":{"selector":"A.ORG_ID","alias":"ORG_ID"}`, `"organizationId":{"selector":"","alias":""}`, 1)
		}},
		{name: "unqualified QAS selector", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"selector":"A.SETID"`, `"selector":"SETID"`, 1)
		}},
		{name: "qualified JSON alias", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"alias":"SETID"`, `"alias":"A.SETID"`, 1)
		}},
		{name: "ambiguous JSON alias", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"name":{"selector":"A.DESCR","alias":"DESCR"}`, `"name":{"selector":"A.DESCR","alias":"ORG_ID"}`, 1)
		}},
		{name: "case-insensitive ambiguous JSON alias", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"name":{"selector":"A.DESCR","alias":"DESCR"}`, `"name":{"selector":"A.DESCR","alias":"org_id"}`, 1)
		}},
		{name: "case-insensitive ambiguous QAS selector", mutate: func(c *PeopleSoftConnectorConfig) {
			c.FieldMappingsJSON = strings.Replace(c.FieldMappingsJSON, `"name":{"selector":"A.DESCR","alias":"DESCR"}`, `"name":{"selector":"a.org_id","alias":"DESCR"}`, 1)
		}},
		{name: "overlarge query", mutate: func(c *PeopleSoftConnectorConfig) { c.MaximumRows = MaximumPeopleSoftRows + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			_, err := NewPeopleSoftConnector(candidate)
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe configuration was accepted or leaked its credential: %v", err)
			}
		})
	}

	redirectedCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectedCalls++ }))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirector.Close()
	connector := newTestPeopleSoftConnector(t, redirector.URL, nil)
	if _, err := connector.PullPage(context.Background(), ""); err == nil || redirectedCalls != 0 {
		t.Fatalf("PeopleSoft redirect was followed: calls=%d err=%v", redirectedCalls, err)
	}
}

func TestPeopleSoftConnectorRejectsMalformedDuplicateIncompleteAndOversizedData(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		check   func(error) bool
	}{
		{name: "malformed", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{not-json`))
		}},
		{name: "provider failure", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"status": "fail", "data": "private provider detail"})
		}},
		{name: "inconsistent count", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeQAS(t, w, "SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}}, 2)
		}},
		{name: "duplicate", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeQAS(t, w, "SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}, {"SETID": "SHARE", "ORG_ID": "A", "DESCR": "Again", "EFF_STATUS": "A"}}, 2)
		}, check: func(err error) bool { return errors.Is(err, ErrConflict) }},
		{name: "unknown status", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeQAS(t, w, "SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "UNKNOWN"}}, 1)
		}},
		{name: "non scalar field", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeQAS(t, w, "SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": map[string]string{"raw": "A"}, "DESCR": "A", "EFF_STATUS": "A"}}, 1)
		}},
		{name: "provider warning", handler: func(w http.ResponseWriter, _ *http.Request) {
			response := qasResponse("SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}}, 1)
			response["warnings"] = []string{"maximum rows fetched"}
			writeJSON(t, w, response)
		}},
		{name: "provider truncation", handler: func(w http.ResponseWriter, _ *http.Request) {
			response := qasResponse("SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}}, 1)
			response["truncated"] = true
			writeJSON(t, w, response)
		}},
		{name: "unknown envelope metadata", handler: func(w http.ResponseWriter, _ *http.Request) {
			response := qasResponse("SM_ORGANIZATIONS", []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}}, 1)
			response["maxRowsReached"] = true
			writeJSON(t, w, response)
		}},
		{name: "missing rows array", handler: func(w http.ResponseWriter, _ *http.Request) {
			response := qasResponse("SM_ORGANIZATIONS", []map[string]any{}, 0)
			delete(response["data"].(map[string]any)["query"].(map[string]any), "rows")
			writeJSON(t, w, response)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			connector := newTestPeopleSoftConnector(t, server.URL, nil)
			_, err := connector.PullPage(context.Background(), "")
			if err == nil || strings.Contains(err.Error(), "private provider detail") || test.check != nil && !test.check(err) {
				t.Fatalf("unsafe provider response was not rejected safely: %v", err)
			}
		})
	}

	t.Run("unknown hierarchy parent", func(t *testing.T) {
		fixture := &peopleSoftFixture{unknownLocationParent: true}
		server := httptest.NewServer(fixture)
		defer server.Close()
		connector := newTestPeopleSoftConnector(t, server.URL, nil)
		if _, err := connector.PullPage(context.Background(), ""); err == nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unknown hierarchy parent was accepted: %v", err)
		}
	})

	t.Run("cross organization department location", func(t *testing.T) {
		fixture := &peopleSoftFixture{crossOrganizationDepartment: true}
		server := httptest.NewServer(fixture)
		defer server.Close()
		connector := newTestPeopleSoftConnector(t, server.URL, nil)
		if _, err := connector.PullPage(context.Background(), ""); err == nil || !errors.Is(err, ErrConflict) {
			t.Fatalf("cross-organization department/location was accepted: %v", err)
		}
	})

	t.Run("too many rows", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			rows := []map[string]any{{"SETID": "SHARE", "ORG_ID": "A", "DESCR": "A", "EFF_STATUS": "A"}, {"SETID": "SHARE", "ORG_ID": "B", "DESCR": "B", "EFF_STATUS": "A"}}
			writeQAS(t, w, "SM_ORGANIZATIONS", rows, len(rows))
		}))
		defer server.Close()
		connector := newTestPeopleSoftConnector(t, server.URL, func(config *PeopleSoftConnectorConfig) { config.MaximumRows = 1 })
		if _, err := connector.PullPage(context.Background(), ""); err == nil {
			t.Fatal("over-limit QAS response was accepted")
		}
	})

	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
		}))
		defer server.Close()
		connector := newTestPeopleSoftConnector(t, server.URL, func(config *PeopleSoftConnectorConfig) { config.MaximumResponseBytes = 1024 })
		if _, err := connector.PullPage(context.Background(), ""); err == nil || strings.Contains(err.Error(), strings.Repeat("x", 20)) {
			t.Fatalf("oversized response was not rejected safely: %v", err)
		}
	})

	connector := newTestPeopleSoftConnector(t, "https://peoplesoft.example.test", func(config *PeopleSoftConnectorConfig) {
		config.AllowPrivateNetwork = false
		config.HTTPClient = &http.Client{Timeout: time.Second}
	})
	if _, err := connector.PullPage(context.Background(), "unexpected"); err == nil {
		t.Fatal("unexpected connector cursor was accepted")
	}
}

func TestPeopleSoftPreviewApplyUpdateInactiveConflictAndAuditHistory(t *testing.T) {
	fixture := &peopleSoftFixture{}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, nil)
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewMemoryDirectoryImportStore()
	auditor := &contractAuditor{}
	service, guardService, authentication := newPeopleSoftTestService(t, registry, store, auditor)

	preview, err := service.Preview(context.Background(), authentication, PreviewRequest{SourceSystemID: "peoplesoft-campus"}, "peoplesoft-preview-1")
	if err != nil || preview.Batch.CompleteSnapshot || preview.Batch.Counts.Created != 8 || preview.Batch.Counts.Deactivated != 0 {
		t.Fatalf("unexpected PeopleSoft preview %#v err=%v", preview, err)
	}
	detail, err := service.Get(context.Background(), preview.Batch.ID)
	if err != nil || len(detail.Items) != 8 {
		t.Fatalf("PeopleSoft preview detail was not durable: %#v err=%v", detail, err)
	}
	var organizationTarget string
	for _, item := range detail.Items {
		if item.Record.SourceRecordID == "organization:SHARE:CAMPUS" {
			organizationTarget = item.TargetID
		}
	}
	if organizationTarget == "" {
		t.Fatal("organization target was not planned")
	}
	if _, err := service.Apply(context.Background(), authentication, preview.Batch.ID, "peoplesoft-apply-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := guardService.ClaimResourceOwnership(context.Background(), authentication, "directory.group", organizationTarget); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.stage = 1
	fixture.mu.Unlock()
	second, err := service.Preview(context.Background(), authentication, PreviewRequest{SourceSystemID: "peoplesoft-campus"}, "peoplesoft-preview-2")
	if err != nil || second.Batch.Counts.Conflicts != 1 || second.Batch.Counts.Updated < 2 {
		t.Fatalf("updates, inactive rows, and local ownership conflict were not explicit: %#v err=%v", second, err)
	}
	if _, err := service.Apply(context.Background(), authentication, second.Batch.ID, "peoplesoft-apply-2"); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.omitBuilding = true
	fixture.mu.Unlock()
	partial, err := service.Preview(context.Background(), authentication, PreviewRequest{SourceSystemID: "peoplesoft-campus"}, "peoplesoft-preview-3")
	if err != nil || partial.Batch.CompleteSnapshot || partial.Batch.Counts.Deactivated != 0 {
		t.Fatalf("unproven QAS completeness planned a deactivation: %#v err=%v", partial, err)
	}
	partialDetail, err := service.Get(context.Background(), partial.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range partialDetail.Items {
		if item.Action == ActionDeactivate {
			t.Fatalf("partial PeopleSoft result contained a deactivation: %#v", item)
		}
	}
	graphStore, _ := NewManagedGraphStore(store, "example-org")
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("PeopleSoft hierarchy did not reach the provider-neutral graph: %#v err=%v", graph, err)
	}
	history, err := service.List(context.Background(), ListQuery{Limit: 10})
	if err != nil || len(history.Batches) != 3 {
		t.Fatalf("PeopleSoft import history was not retained: %#v err=%v", history, err)
	}
	if len(auditor.events) < 5 {
		t.Fatalf("PeopleSoft preview/apply/conflict audits are incomplete: %#v", auditor.events)
	}
	for _, event := range auditor.events {
		encoded, _ := json.Marshal(event)
		if event.Metadata["requirementId"] != PeopleSoftRequirementID || strings.Contains(string(encoded), "Example University") || strings.Contains(string(encoded), "reader-password") {
			t.Fatalf("PeopleSoft audit used the wrong requirement or leaked provider data: %s", encoded)
		}
	}
}

func TestPeopleSoftPreviewRequiresAuthenticatedActorBeforeProviderRead(t *testing.T) {
	fixture := &peopleSoftFixture{}
	server := httptest.NewServer(fixture)
	defer server.Close()
	connector := newTestPeopleSoftConnector(t, server.URL, nil)
	registry, _ := NewRegistry(connector)
	service, _, _ := newPeopleSoftTestService(t, registry, repository.NewMemoryDirectoryImportStore(), foundation.NopAuditor{})
	_, err := service.Preview(context.Background(), guard.Authentication{}, PreviewRequest{SourceSystemID: "peoplesoft-campus"}, "peoplesoft-auth-key")
	if !errors.Is(err, ErrInvalidInput) || fixture.calls != 0 {
		t.Fatalf("unauthenticated preview reached PeopleSoft: calls=%d err=%v", fixture.calls, err)
	}
}

type peopleSoftFixture struct {
	mu                          sync.Mutex
	stage                       int
	calls                       int
	transientFailures           int
	failureStatus               int
	providerBody                string
	unknownLocationParent       bool
	multipleSetIDs              bool
	crossOrganizationDepartment bool
	omitBuilding                bool
	methods                     []string
	queries                     []string
	filterFields                []string
	authorizations              []string
}

func (f *peopleSoftFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.methods = append(f.methods, r.Method)
	f.authorizations = append(f.authorizations, r.Header.Get("Authorization"))
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	queryName := ""
	if len(parts) >= 3 {
		queryName = parts[len(parts)-3]
	}
	f.queries = append(f.queries, queryName)
	f.filterFields = append(f.filterFields, r.URL.Query().Get("filterfields"))
	if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1/public/") ||
		r.URL.Query().Get("isconnectedquery") != "N" || r.URL.Query().Get("json_resp") != "true" ||
		r.URL.Query().Get("maxrows") != "3" || r.URL.Query().Get("filterfields") == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if f.transientFailures > 0 {
		f.transientFailures--
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"secret":"retry-body"}`))
		return
	}
	if f.failureStatus != 0 {
		w.WriteHeader(f.failureStatus)
		_, _ = w.Write([]byte(f.providerBody))
		return
	}
	rows := f.rows(queryName)
	writeQAS(nil, w, queryName, rows, len(rows))
}

func (f *peopleSoftFixture) rows(queryName string) []map[string]any {
	organizationName := "Example University"
	departmentStatus := "I"
	if f.stage > 0 {
		organizationName = "Local-conflicting provider change"
		departmentStatus = "A"
	}
	if f.multipleSetIDs {
		return f.rowsForSETIDs(queryName, []string{"SHARE", "LOCAL"})
	}
	if f.crossOrganizationDepartment {
		switch queryName {
		case "SM_ORGANIZATIONS":
			return []map[string]any{
				{"SETID": "SHARE", "ORG_ID": "CAMPUS", "DESCR": organizationName, "EFF_STATUS": "A"},
				{"SETID": "SHARE", "ORG_ID": "OTHER", "DESCR": "Other organization", "EFF_STATUS": "A"},
			}
		case "SM_DEPARTMENTS":
			return []map[string]any{{"SETID": "SHARE", "DEPTID": "ARCHIVE", "DESCR": "Archives", "EFF_STATUS": departmentStatus, "ORG_ID": "OTHER", "LOCATION": "MAIN"}}
		}
	}
	switch queryName {
	case "SM_ORGANIZATIONS":
		return []map[string]any{{"SETID": "SHARE", "ORG_ID": "CAMPUS", "DESCR": organizationName, "EFF_STATUS": "A"}}
	case "SM_LOCATIONS":
		organizationID := "CAMPUS"
		if f.unknownLocationParent {
			organizationID = "MISSING"
		}
		return []map[string]any{{"SETID": "SHARE", "LOCATION": "MAIN", "DESCR": "Main Campus", "EFF_STATUS": "A", "ORG_ID": organizationID, "CITY": "Chicago"}}
	case "SM_BUILDINGS":
		if f.omitBuilding {
			return []map[string]any{}
		}
		name := "Library"
		if f.stage > 0 {
			name = "University Library"
		}
		return []map[string]any{{"SETID": "SHARE", "BUILDING": "LIB", "DESCR": name, "EFF_STATUS": "A", "LOCATION": "MAIN"}}
	case "SM_DEPARTMENTS":
		return []map[string]any{{"SETID": "SHARE", "DEPTID": "ARCHIVE", "DESCR": "Archives", "EFF_STATUS": departmentStatus, "ORG_ID": "CAMPUS", "LOCATION": "MAIN"}}
	default:
		return nil
	}
}

func (f *peopleSoftFixture) rowsForSETIDs(queryName string, setIDs []string) []map[string]any {
	rows := make([]map[string]any, 0, len(setIDs))
	for _, setID := range setIDs {
		suffix := strings.ToLower(setID)
		switch queryName {
		case "SM_ORGANIZATIONS":
			rows = append(rows, map[string]any{"SETID": setID, "ORG_ID": "CAMPUS", "DESCR": "Campus " + suffix, "EFF_STATUS": "A"})
		case "SM_LOCATIONS":
			rows = append(rows, map[string]any{"SETID": setID, "LOCATION": "MAIN", "DESCR": "Main " + suffix, "EFF_STATUS": "A", "ORG_ID": "CAMPUS", "CITY": "Chicago"})
		case "SM_BUILDINGS":
			rows = append(rows, map[string]any{"SETID": setID, "BUILDING": "LIB", "DESCR": "Library " + suffix, "EFF_STATUS": "A", "LOCATION": "MAIN"})
		case "SM_DEPARTMENTS":
			rows = append(rows, map[string]any{"SETID": setID, "DEPTID": "ARCHIVE", "DESCR": "Archives " + suffix, "EFF_STATUS": "I", "ORG_ID": "CAMPUS", "LOCATION": "MAIN"})
		}
	}
	return rows
}

func validPeopleSoftConfig(serverBase string) PeopleSoftConnectorConfig {
	mappings := PeopleSoftFieldMappings{
		Organization: PeopleSoftFieldMapping{
			SetID: peopleSoftField("A.SETID", "SETID"), ID: peopleSoftField("A.ORG_ID", "ORG_ID"),
			Name: peopleSoftField("A.DESCR", "DESCR"), Status: peopleSoftField("A.EFF_STATUS", "EFF_STATUS"),
		},
		Location: PeopleSoftFieldMapping{
			SetID: peopleSoftField("A.SETID", "SETID"), ID: peopleSoftField("A.LOCATION", "LOCATION"),
			Name: peopleSoftField("A.DESCR", "DESCR"), Status: peopleSoftField("A.EFF_STATUS", "EFF_STATUS"),
			OrganizationID: peopleSoftField("A.ORG_ID", "ORG_ID"), City: peopleSoftField("A.CITY", "CITY"),
		},
		Building: PeopleSoftFieldMapping{
			SetID: peopleSoftField("A.SETID", "SETID"), ID: peopleSoftField("A.BUILDING", "BUILDING"),
			Name: peopleSoftField("A.DESCR", "DESCR"), Status: peopleSoftField("A.EFF_STATUS", "EFF_STATUS"),
			LocationID: peopleSoftField("A.LOCATION", "LOCATION"),
		},
		Department: PeopleSoftFieldMapping{
			SetID: peopleSoftField("A.SETID", "SETID"), ID: peopleSoftField("A.DEPTID", "DEPTID"),
			Name: peopleSoftField("A.DESCR", "DESCR"), Status: peopleSoftField("A.EFF_STATUS", "EFF_STATUS"),
			OrganizationID: peopleSoftField("A.ORG_ID", "ORG_ID"), LocationID: peopleSoftField("A.LOCATION", "LOCATION"),
		},
	}
	encoded, _ := json.Marshal(mappings)
	return PeopleSoftConnectorConfig{
		SourceSystemID: "peoplesoft-campus", BaseURL: serverBase + "/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1",
		Username: "integration-reader", Password: "reader-password", QueryOwner: "public",
		OrganizationQuery: "SM_ORGANIZATIONS", LocationQuery: "SM_LOCATIONS", BuildingQuery: "SM_BUILDINGS", DepartmentQuery: "SM_DEPARTMENTS",
		FieldMappingsJSON: string(encoded), MaximumRows: 2, MaximumResponseBytes: DefaultPeopleSoftResponseBytes,
		Timeout: time.Second, AllowPrivateNetwork: true, HTTPClient: &http.Client{Timeout: time.Second},
	}
}

func peopleSoftField(selector, alias string) PeopleSoftMappedField {
	return PeopleSoftMappedField{Selector: selector, Alias: alias}
}

func newTestPeopleSoftConnector(t *testing.T, serverBase string, mutate func(*PeopleSoftConnectorConfig)) *PeopleSoftConnector {
	t.Helper()
	config := validPeopleSoftConfig(serverBase)
	if mutate != nil {
		mutate(&config)
	}
	connector, err := NewPeopleSoftConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func newPeopleSoftTestService(t *testing.T, registry *Registry, store *repository.MemoryDirectoryImportStore, auditor foundation.Auditor) (*Service, *guard.Service, guard.Authentication) {
	t.Helper()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), contractPasswordHasher{}, foundation.NopAuditor{}, nil,
		guard.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := guardService.Bootstrap(context.Background(), guard.BootstrapInput{Username: "administrator", Email: "administrator@example.test",
		DisplayName: "Administrator", Password: "correct horse battery staple"}, true)
	if err != nil {
		t.Fatal(err)
	}
	peopleTarget, _ := NewPeopleTarget(repository.NewMemoryPeopleStore(), guardService, nil)
	groupTarget, _ := NewGroupTarget(store, guardService, nil)
	target, _ := NewDirectoryTarget(peopleTarget, groupTarget)
	service, err := NewService(store, target, auditor, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	return service, guardService, credentials.Authentication
}

func writeQAS(t *testing.T, w http.ResponseWriter, queryName string, rows []map[string]any, count int) {
	if t != nil {
		t.Helper()
	}
	writeJSON(t, w, qasResponse(queryName, rows, count))
}

func qasResponse(queryName string, rows []map[string]any, count int) map[string]any {
	return map[string]any{"status": "success", "data": map[string]any{"query": map[string]any{
		"numrows": count, "queryname": queryName, "rows": rows,
	}}}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	if t != nil {
		t.Helper()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil && t != nil {
		t.Fatal(err)
	}
}

func findPeopleSoftMembership(t *testing.T, records []Record, parent, child string) Record {
	t.Helper()
	for _, record := range records {
		if record.Kind == RecordMembership && record.GroupSourceID == parent && record.MemberSourceID == child {
			return record
		}
	}
	t.Fatalf("missing PeopleSoft hierarchy relationship %s -> %s", parent, child)
	return Record{}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type failingReadCloser struct {
	content []byte
}

func (reader *failingReadCloser) Read(destination []byte) (int, error) {
	if len(reader.content) > 0 {
		count := copy(destination, reader.content)
		reader.content = reader.content[count:]
		return count, nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (*failingReadCloser) Close() error { return nil }
