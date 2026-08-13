package directoryexpansion_test

// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/grouperfixture"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type grouperSnapshotSafetyTarget struct{}

func (grouperSnapshotSafetyTarget) Preview(_ context.Context, _ string, _ SourceSystem, record Record, _ *Mapping) (TargetPlan, error) {
	digest := sha256.Sum256([]byte(record.SourceRecordID + "\x00" + record.Status))
	encoded := fmt.Sprintf("%x", digest)
	return TargetPlan{TargetID: encoded[:32], DesiredDigest: encoded}, nil
}

func (grouperSnapshotSafetyTarget) Apply(context.Context, guard.Authentication, SourceSystem, Item) (TargetResult, error) {
	return TargetResult{}, errors.New("snapshot safety target apply is not supported")
}

func (grouperSnapshotSafetyTarget) Compensate(context.Context, guard.Authentication, SourceSystem, Item, TargetResult) error {
	return nil
}

func TestGrouperConnectorPullsPaginatedGroupsNestedMembershipsAndMetadataReadOnly(t *testing.T) {
	var mu sync.Mutex
	methods, starts, authorizations := []string{}, []string{}, []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods, starts, authorizations = append(methods, r.Method), append(starts, r.URL.Query().Get("startIndex")), append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/grouper-ws/scim/v2/Groups" || r.URL.Query().Get("count") != "1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		start := r.URL.Query().Get("startIndex")
		resource := map[string]any{"id": "researchers", "name": "app:researchers", "displayName": "Researchers", "active": true,
			"description": "Research access", "stewardMetadata": map[string]string{"classification": "internal"},
			"members": []map[string]any{{"value": "nested-team", "display": "Nested team", "type": "group", "active": true}}}
		if start == "2" {
			resource = map[string]any{"id": "nested-team", "name": "app:nested-team", "displayName": "Nested team", "active": true,
				"members": []map[string]any{{"value": "subject-1", "display": "Ada", "type": "subject", "active": true}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 2, "startIndex": mustAtoi(start), "itemsPerPage": 1, "Resources": []any{resource}})
	}))
	defer server.Close()
	connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-test", PageSize: 1,
		BearerToken: "redacted-test-token", HTTPClient: &http.Client{Timeout: time.Second}})
	first, err := connector.PullPage(context.Background(), "")
	if err != nil || first.NextCursor != "groups:2" || first.CompleteSnapshot || len(first.Records) != 2 ||
		first.Records[0].Kind != RecordGroup || first.Records[1].MemberKind != MemberGroup ||
		first.Records[0].NormalizedMetadata["classification"] != "internal" {
		t.Fatalf("unexpected first page %#v err=%v", first, err)
	}
	second, err := connector.PullPage(context.Background(), first.NextCursor)
	if err != nil || second.NextCursor != "" || !second.CompleteSnapshot || len(second.Records) != 2 || second.Records[1].MemberKind != MemberSubject {
		t.Fatalf("unexpected second page %#v err=%v", second, err)
	}
	if fmt.Sprint(methods) != "[GET GET]" || fmt.Sprint(starts) != "[1 2]" || authorizations[0] != "Bearer redacted-test-token" {
		t.Fatalf("connector was not GET-only or pagination/auth was wrong: %#v %#v %#v", methods, starts, authorizations)
	}
}

func TestGrouperImporterDoesNotConfirmSnapshotWhenSameTotalPagesInsertDeleteAndReorder(t *testing.T) {
	group := func(id string, active bool) map[string]any {
		return map[string]any{"id": id, "name": "app:" + id, "displayName": strings.ToUpper(id), "active": active}
	}
	initial := []map[string]any{group("first", true), group("still-valid", true), group("third", true)}
	stableAfterMutation := []map[string]any{group("still-valid", false), group("third", true), group("inserted", true)}
	var mu sync.Mutex
	traversal := 0
	starts := make([]int, 0, 6)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := mustAtoi(r.URL.Query().Get("startIndex"))
		mu.Lock()
		if start == 1 {
			traversal++
		}
		currentTraversal := traversal
		starts = append(starts, start)
		mu.Unlock()
		// The first page is A from A,B,C. Before page two, A is deleted,
		// D is inserted, and the same-size window becomes B,C,D. The first
		// traversal sees A,C,D; the second independently sees B,C,D.
		values := stableAfterMutation
		if currentTraversal == 1 && start == 1 {
			values = initial
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": len(values), "startIndex": start, "itemsPerPage": 1,
			"Resources": []any{values[start-1]},
		})
	}))
	defer server.Close()

	connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-snapshot-safety", PageSize: 1,
		HTTPClient: &http.Client{Timeout: time.Second}})
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewMemoryDirectoryImportStore()
	service, err := NewService(store, grouperSnapshotSafetyTarget{}, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "grouper-snapshot-safety"}, "grouper-snapshot-safety")
	if err != nil || preview.Batch.CompleteSnapshot || preview.Batch.Counts.Deactivated != 0 {
		t.Fatalf("changing Grouper traversal was trusted as complete: %#v err=%v", preview, err)
	}
	detail, err := service.Get(context.Background(), preview.Batch.ID)
	if err != nil || len(detail.Items) != 3 {
		t.Fatalf("latest safe Grouper traversal was not planned: %#v err=%v", detail, err)
	}
	byID := make(map[string]Item, len(detail.Items))
	for _, item := range detail.Items {
		byID[item.Record.SourceRecordID] = item
	}
	if item := byID["still-valid"]; item.Record.Status != "inactive" || item.Action != ActionCreate {
		t.Fatalf("explicit inactive Grouper row was weakened in a partial snapshot: %#v", item)
	}
	if _, present := byID["first"]; present {
		t.Fatalf("plan retained the stale first traversal instead of the confirmation traversal: %#v", detail.Items)
	}
	mu.Lock()
	requestStarts := fmt.Sprint(starts)
	mu.Unlock()
	if requestStarts != "[1 2 3 1 2 3]" {
		t.Fatalf("Grouper snapshot was not independently traversed twice: %s", requestStarts)
	}
}

func TestGrouperConnectorRetriesTransientFailureAndRedactsProviderPayload(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"secret":"must-not-escape"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "startIndex": 1, "itemsPerPage": 0, "Resources": []any{}})
	}))
	defer server.Close()
	connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-retry", HTTPClient: &http.Client{Timeout: time.Second},
		RetryDelay: func(context.Context, int) error { return nil }})
	page, err := connector.PullPage(context.Background(), "")
	if err != nil || calls != 3 || !page.CompleteSnapshot {
		t.Fatalf("expected bounded retry recovery, page=%#v calls=%d err=%v", page, calls, err)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"password":"must-not-escape"}`))
	})
	_, err = connector.PullPage(context.Background(), "")
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "must-not-escape") {
		t.Fatalf("provider payload escaped sanitized error: %v", err)
	}
}

func TestGrouperConnectorRejectsUnsafeEndpointsAndRedirects(t *testing.T) {
	for _, candidate := range []GrouperConnectorConfig{
		{SourceSystemID: "grouper", BaseURL: "http://grouper.example.test/grouper-ws/scim/v2", AllowPrivateNetwork: true},
		{SourceSystemID: "grouper", BaseURL: "https://user:secret@grouper.example.test/grouper-ws/scim/v2"},
		{SourceSystemID: "grouper", BaseURL: "https://127.0.0.1/grouper-ws/scim/v2"},
		{SourceSystemID: "grouper", BaseURL: "https://0.0.0.0/grouper-ws/scim/v2", AllowPrivateNetwork: true},
		{SourceSystemID: "grouper", BaseURL: "https://169.254.169.254/grouper-ws/scim/v2", AllowPrivateNetwork: true},
		{SourceSystemID: "grouper", BaseURL: "https://224.0.0.1/grouper-ws/scim/v2", AllowPrivateNetwork: true},
		{SourceSystemID: "grouper", BaseURL: "https://grouper.example.test/arbitrary"},
		{SourceSystemID: "grouper", BaseURL: "https://grouper.example.test/grouper-ws/scim/v2", BearerToken: "line-one\nline-two"},
		{SourceSystemID: "grouper", BaseURL: "https://grouper.example.test/grouper-ws/scim/v2", BearerToken: strings.Repeat("x", 4097)},
		{SourceSystemID: "grouper", BaseURL: "https://grouper.example.test/grouper-ws/scim/v2", ConfigRevision: "invalid/revision"},
	} {
		if _, err := NewGrouperConnector(candidate); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe endpoint was accepted or leaked credentials: %v", err)
		}
	}
	redirectedCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectedCalls++ }))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirector.Close()
	connector := newTestGrouperConnector(t, redirector.URL, GrouperConnectorConfig{SourceSystemID: "grouper-redirect", HTTPClient: &http.Client{Timeout: time.Second}})
	if _, err := connector.PullPage(context.Background(), ""); err == nil || redirectedCalls != 0 {
		t.Fatalf("Grouper redirect was followed: calls=%d err=%v", redirectedCalls, err)
	}
}

func TestGrouperConnectorRejectsMalformedDuplicateAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{not-json`},
		{name: "inconsistent page", body: `{"totalResults":2,"startIndex":1,"itemsPerPage":0,"Resources":[]}`},
		{name: "duplicate group", body: `{"totalResults":2,"startIndex":1,"itemsPerPage":2,"Resources":[{"id":"same","name":"a","displayName":"A"},{"id":"same","name":"a","displayName":"A"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }))
			defer server.Close()
			connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-malformed", HTTPClient: &http.Client{Timeout: time.Second}})
			registry, _ := NewRegistry(connector)
			service := newGrouperTestService(t, registry, repository.NewMemoryDirectoryImportStore(), foundation.NopAuditor{})
			_, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "grouper-malformed"}, "malformed-key")
			if test.name == "duplicate group" {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("duplicate normalized source was not rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("failed previews persist a sanitized result rather than return transport details: %v", err)
			}
			detail, _ := service.List(context.Background(), ListQuery{Limit: 10})
			if len(detail.Batches) != 1 || detail.Batches[0].Status != BatchFailed {
				t.Fatalf("malformed response was not persisted as failed preview: %#v", detail)
			}
		})
	}

	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 2048))) }))
		defer server.Close()
		connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-large", MaximumResponseBytes: 1024, HTTPClient: &http.Client{Timeout: time.Second}})
		if _, err := connector.PullPage(context.Background(), ""); err == nil || strings.Contains(err.Error(), strings.Repeat("x", 20)) {
			t.Fatalf("oversized response was not rejected safely: %v", err)
		}
	})
}

func TestGrouperPreviewApplyNestedGraphDuplicateReplayAndRemoval(t *testing.T) {
	token := "fixture-token-value"
	fixture, err := grouperfixture.New(token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(fixture.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	for _, group := range []map[string]any{
		{"id": "parent", "name": "app:parent", "displayName": "Parent", "description": "Top level", "active": true, "metadata": map[string]string{"classification": "internal"}},
		{"id": "child", "name": "app:child", "displayName": "Child", "active": true},
	} {
		fixtureRequest(t, client, token, http.MethodPost, server.URL+"/fixture/groups", group, http.StatusCreated)
	}
	fixtureRequest(t, client, token, http.MethodPost, server.URL+"/fixture/memberships", map[string]any{"id": "nested", "groupId": "parent", "memberId": "child", "memberKind": "group", "displayName": "Child", "active": true}, http.StatusCreated)
	fixtureRequest(t, client, token, http.MethodPost, server.URL+"/fixture/memberships", map[string]any{"id": "person", "groupId": "child", "memberId": "subject-1", "memberKind": "subject", "displayName": "Ada", "active": true}, http.StatusCreated)

	connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-fixture", BearerToken: token, PageSize: 1, HTTPClient: client})
	registry, _ := NewRegistry(connector)
	store := repository.NewMemoryDirectoryImportStore()
	auditor := &contractAuditor{}
	service := newGrouperTestService(t, registry, store, auditor)
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "grouper-fixture"}, "grouper-preview-key")
	if err != nil || preview.Batch.Counts.Created != 4 || !preview.Batch.CompleteSnapshot {
		t.Fatalf("unexpected Grouper preview %#v err=%v", preview, err)
	}
	replay, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "grouper-fixture"}, "grouper-preview-key")
	if err != nil || !replay.Replay || replay.Batch.ID != preview.Batch.ID {
		t.Fatalf("duplicate import was not idempotent %#v err=%v", replay, err)
	}
	applied, err := service.Apply(context.Background(), auth, preview.Batch.ID, "grouper-apply-key")
	if err != nil || applied.Batch.Status != BatchApplied {
		t.Fatalf("Grouper apply failed %#v err=%v", applied, err)
	}
	graphStore, _ := NewManagedGraphStore(store, "example-org")
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || len(graph.Nodes) != 3 || len(graph.Edges) != 2 {
		t.Fatalf("nested group graph is incomplete %#v err=%v", graph, err)
	}
	fixtureRequest(t, client, token, http.MethodDelete, server.URL+"/fixture/memberships/person", nil, http.StatusNoContent)
	secondPreview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "grouper-fixture"}, "grouper-removal-preview")
	if err != nil || secondPreview.Batch.Counts.Deactivated != 1 {
		t.Fatalf("removed membership was not planned for deactivation %#v err=%v", secondPreview, err)
	}
	if _, err := service.Apply(context.Background(), auth, secondPreview.Batch.ID, "grouper-removal-apply"); err != nil {
		t.Fatal(err)
	}
	graph, _ = graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("removed membership remained active in graph: %#v", graph)
	}
	if len(auditor.events) < 4 {
		t.Fatalf("preview/apply/removal audits were not emitted: %#v", auditor.events)
	}
	for _, event := range auditor.events {
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), "Ada") || strings.Contains(string(encoded), token) {
			t.Fatalf("audit leaked provider data or credential: %s", encoded)
		}
		if event.Metadata["requirementId"] != GrouperRequirementID {
			t.Fatalf("Grouper audit used the wrong requirement: %#v", event.Metadata)
		}
	}
}

func TestGrouperPreviewRequiresAuthenticatedActorBeforeSourceRead(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "startIndex": 1, "itemsPerPage": 0, "Resources": []any{}})
	}))
	defer server.Close()
	connector := newTestGrouperConnector(t, server.URL, GrouperConnectorConfig{SourceSystemID: "grouper-auth", HTTPClient: &http.Client{Timeout: time.Second}})
	registry, _ := NewRegistry(connector)
	service := newGrouperTestService(t, registry, repository.NewMemoryDirectoryImportStore(), foundation.NopAuditor{})
	_, err := service.Preview(context.Background(), guard.Authentication{}, PreviewRequest{SourceSystemID: "grouper-auth"}, "grouper-auth-key")
	if !errors.Is(err, ErrInvalidInput) || calls != 0 {
		t.Fatalf("unauthenticated preview reached Grouper: calls=%d err=%v", calls, err)
	}
}

func newTestGrouperConnector(t *testing.T, base string, config GrouperConnectorConfig) *GrouperConnector {
	t.Helper()
	config.BaseURL = base + "/grouper-ws/scim/v2"
	config.AllowPrivateNetwork = true
	if config.ConfigRevision == "" {
		config.ConfigRevision = "test-v1"
	}
	connector, err := NewGrouperConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func newGrouperTestService(t *testing.T, registry *Registry, store *repository.MemoryDirectoryImportStore, auditor foundation.Auditor) *Service {
	t.Helper()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), contractPasswordHasher{}, foundation.NopAuditor{}, nil, guard.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	peopleTarget, err := NewPeopleTarget(repository.NewMemoryPeopleStore(), guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	groupTarget, err := NewGroupTarget(store, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewDirectoryTarget(peopleTarget, groupTarget)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, target, auditor, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func fixtureRequest(t *testing.T, client *http.Client, token, method, endpoint string, body any, expected int) {
	t.Helper()
	var encoded string
	if body != nil {
		data, _ := json.Marshal(body)
		encoded = string(data)
	}
	request, _ := http.NewRequest(method, endpoint, strings.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		t.Fatalf("fixture %s %s returned %d", method, endpoint, response.StatusCode)
	}
}

func mustAtoi(value string) int { result, _ := strconv.Atoi(value); return result }
