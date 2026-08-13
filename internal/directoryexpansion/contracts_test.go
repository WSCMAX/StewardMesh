package directoryexpansion_test

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-007.
// Features: integrations.protocols, identity.directory.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

type contractConnector struct {
	system SourceSystem
	mu     sync.Mutex
	pages  map[string]Page
	errors map[string]error
	calls  []string
}

type contractPasswordHasher struct{}

func (contractPasswordHasher) Hash(string) (string, error)               { return "test-hash", nil }
func (contractPasswordHasher) Verify(string, string) (bool, bool, error) { return true, false, nil }

func (c *contractConnector) SourceSystem() SourceSystem { return c.system }
func (c *contractConnector) PullPage(_ context.Context, cursor string) (Page, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, cursor)
	if err := c.errors[cursor]; err != nil {
		return Page{}, err
	}
	return c.pages[cursor], nil
}

type contractTarget struct {
	mu            sync.Mutex
	current       map[string]TargetPlan
	results       map[string]TargetResult
	errors        map[string]error
	previewErrors map[string]error
	apply         []string
	compensations []string
}

type contractAuditor struct {
	mu     sync.Mutex
	events []foundation.AuditEvent
}

func (a *contractAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, existing := range a.events {
		if existing.ID == event.ID {
			if !reflect.DeepEqual(existing, event) {
				return errors.New("audit event identity changed during replay")
			}
			return nil
		}
	}
	a.events = append(a.events, event)
	return nil
}

func (t *contractTarget) Preview(_ context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	if err := t.previewErrors[record.SourceRecordID]; err != nil {
		return TargetPlan{}, err
	}
	if plan, ok := t.current[record.SourceRecordID]; ok {
		return plan, nil
	}
	return TargetPlan{TargetID: testDigest(organizationID, system.ID, record.SourceRecordID)[:32], DesiredDigest: testDigest(record.IdentityKind, record.DisplayName, record.Email, record.Status)}, nil
}
func (t *contractTarget) Apply(_ context.Context, _ guard.Authentication, _ SourceSystem, item Item) (TargetResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.apply = append(t.apply, item.Record.SourceRecordID)
	if err := t.errors[item.Record.SourceRecordID]; err != nil {
		return TargetResult{}, err
	}
	if result, ok := t.results[item.Record.SourceRecordID]; ok {
		t.current[item.Record.SourceRecordID] = TargetPlan{TargetID: result.TargetID, Revision: result.Revision, CurrentDigest: result.Digest, DesiredDigest: result.Digest, Found: true, SourceMatched: true}
		return result, nil
	}
	result := TargetResult{TargetID: item.TargetID, Revision: item.ExpectedRevision + 1, Digest: item.PlannedTargetDigest, Changed: item.Action != ActionUnchanged}
	t.current[item.Record.SourceRecordID] = TargetPlan{TargetID: result.TargetID, Revision: result.Revision, CurrentDigest: result.Digest, DesiredDigest: result.Digest, Found: true, SourceMatched: true}
	return result, nil
}

func (t *contractTarget) Compensate(_ context.Context, _ guard.Authentication, _ SourceSystem, item Item, _ TargetResult) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.compensations = append(t.compensations, item.Record.SourceRecordID)
	delete(t.current, item.Record.SourceRecordID)
	return nil
}

type leaseLosingStore struct{ Store }

func (s leaseLosingStore) SaveItem(context.Context, string, string, string, Item, *Mapping) error {
	return ErrLeaseLost
}

type finishFailingStore struct {
	Store
	failRetry bool
}

type uncertainCommitStore struct {
	Store
	failOnce bool
}

type saveRejectingStore struct{ Store }

func (s saveRejectingStore) SaveItem(context.Context, string, string, string, Item, *Mapping) error {
	return errors.New("simulated definitive mapping rejection")
}

func (s *uncertainCommitStore) SaveItem(ctx context.Context, organizationID, batchID, leaseToken string, item Item, mapping *Mapping) error {
	if err := s.Store.SaveItem(ctx, organizationID, batchID, leaseToken, item, mapping); err != nil {
		return err
	}
	if s.failOnce {
		s.failOnce = false
		return errors.New("simulated uncertain commit acknowledgement")
	}
	return nil
}

func (s *finishFailingStore) FinishOperation(ctx context.Context, organizationID, batchID, leaseToken string, attempt Attempt, result OperationResult) error {
	if s.failRetry && attempt.Operation == OperationRetry {
		s.failRetry = false
		return errors.New("simulated crash after recovered plan commit")
	}
	return s.Store.FinishOperation(ctx, organizationID, batchID, leaseToken, attempt, result)
}

type contractStore interface {
	Store
}

func TestRegistrySnapshotsNormalizedSourceSystemIdentity(t *testing.T) {
	connector := &contractConnector{system: SourceSystem{
		ID: " hr-primary ", Provider: " Example ", ConfigRevision: " v1 ",
	}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	// A registered adapter cannot drift the metadata used for persisted plans.
	connector.system = SourceSystem{ID: "changed", Provider: "changed", ConfigRevision: "v2"}
	registered, ok := registry.Connector("hr-primary")
	if !ok {
		t.Fatal("normalized source system was not registered")
	}
	if got := registered.SourceSystem(); got.ID != "hr-primary" || got.Provider != "example" || got.ConfigRevision != "v1" {
		t.Fatalf("registered source metadata was not normalized and pinned: %#v", got)
	}
}

func TestRegistryReturnsBoundedSortedCredentialFreeSources(t *testing.T) {
	connectors := make([]Connector, 0, MaximumSources)
	for index := MaximumSources - 1; index >= 0; index-- {
		connectors = append(connectors, &contractConnector{system: SourceSystem{
			ID: fmt.Sprintf("source-%03d", index), Provider: "example", ConfigRevision: "v1",
		}})
	}
	registry, err := NewRegistry(connectors...)
	if err != nil {
		t.Fatal(err)
	}
	sources := registry.SourceSystems()
	if len(sources) != MaximumSources || sources[0].ID != "source-000" || sources[len(sources)-1].ID != "source-099" {
		t.Fatalf("unexpected bounded source discovery: %#v", sources)
	}
	if err := registry.Register(&contractConnector{system: SourceSystem{ID: "overflow", Provider: "example", ConfigRevision: "v1"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected source bound rejection, got %v", err)
	}
}

func TestConnectorLifecycleIsProviderNeutralAcrossPlannedAdapters(t *testing.T) {
	for _, provider := range []Provider{"entra", "sailpoint", "grouper", "peoplesoft"} {
		t.Run(string(provider), func(t *testing.T) {
			connector := &contractConnector{system: SourceSystem{ID: string(provider) + "-primary", Provider: provider, ConfigRevision: "v1"}, pages: map[string]Page{
				"": {Records: []Record{{SourceRecordID: "record-1", Kind: RecordIdentity, IdentityKind: "shared", DisplayName: "Provider record", Status: "active"}}, CompleteSnapshot: true},
			}, errors: map[string]error{}}
			service := newContractService(t, newContractMemoryStore(), &contractTarget{current: map[string]TargetPlan{}}, connector)
			preview, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: string(provider) + "-primary"}, "provider-preview-key")
			if err != nil || preview.Batch.Provider != provider || preview.Batch.Status != BatchPreviewed {
				t.Fatalf("provider-neutral lifecycle failed: %#v %v", preview, err)
			}
		})
	}
}

func TestLostLeaseNeverCompensatesTargetOwnedByTakeoverWorker(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{
		SourceRecordID: "takeover", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Takeover", Email: "takeover@example.com", Status: "active",
	}})
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(leaseLosingStore{Store: newContractMemoryStore()}, target, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "takeover-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), contractAuthentication(), preview.Batch.ID, "takeover-apply-key"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expected lease loss, got %v", err)
	}
	if len(target.compensations) != 0 {
		t.Fatalf("lease-losing worker compensated target owned by takeover: %#v", target.compensations)
	}
}

func TestCommittedMappingIsVerifiedBeforeCompensation(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{
		SourceRecordID: "committed", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Committed", Email: "committed@example.com", Status: "active",
	}})
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	store := &uncertainCommitStore{Store: newContractMemoryStore(), failOnce: true}
	service := newContractService(t, store, target, connector)
	preview, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "commit-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	applied, err := service.Apply(context.Background(), contractAuthentication(), preview.Batch.ID, "commit-apply-key")
	if err != nil || applied.Batch.Status != BatchApplied {
		t.Fatalf("committed mapping acknowledgement was not recovered: %#v %v", applied, err)
	}
	if len(target.compensations) != 0 {
		t.Fatalf("committed target was compensated: %#v", target.compensations)
	}
}

func TestUncommittedCreateIsCompensated(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{
		SourceRecordID: "rejected", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Rejected", Email: "rejected@example.com", Status: "active",
	}})
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, saveRejectingStore{Store: newContractMemoryStore()}, target, connector)
	preview, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "reject-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), contractAuthentication(), preview.Batch.ID, "reject-apply-key"); err == nil {
		t.Fatal("expected mapping rejection")
	}
	if len(target.compensations) != 1 || target.compensations[0] != "rejected" {
		t.Fatalf("uncommitted target was not compensated: %#v", target.compensations)
	}
}

func TestPreviewPersistsExactBoundedPlanWithoutTargetMutation(t *testing.T) {
	connector := &contractConnector{system: SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "v7"}, pages: map[string]Page{
		"":     {Records: []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}}, NextCursor: "next"},
		"next": {Records: []Record{{SourceRecordID: "u-2", Kind: RecordIdentity, IdentityKind: "shared", DisplayName: "Support", Status: "active"}}, CompleteSnapshot: true},
	}}
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	result, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Status != BatchPreviewed || !result.Batch.CompleteSnapshot || result.Batch.Counts.Created != 2 || len(target.apply) != 0 {
		t.Fatalf("unexpected preview: %#v apply=%#v", result, target.apply)
	}
	detail, err := service.Get(context.Background(), result.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 2 || detail.Items[0].Record.SourceRecordID != "u-1" || detail.Items[0].SourceDigest == "" || detail.Items[0].PlannedTargetDigest == "" {
		t.Fatalf("exact preview plan was not retained: %#v", detail)
	}
	replay, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0001")
	if err != nil || !replay.Replay || replay.Batch.ID != result.Batch.ID || len(connector.calls) != 2 {
		t.Fatalf("exact preview did not replay: %#v %v calls=%#v", replay, err, connector.calls)
	}
	_, err = service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "other"}, "preview-key-0001")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected idempotency fingerprint conflict, got %v", err)
	}
}

func TestApplyUsesPersistedPlanAndExactReplay(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0002")
	if err != nil {
		t.Fatal(err)
	}
	connector.pages[""] = Page{Records: []Record{{SourceRecordID: "attacker", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Changed", Email: "changed@example.com", Status: "active"}}, CompleteSnapshot: true}
	applied, err := service.Apply(context.Background(), auth, preview.Batch.ID, "apply-key-00001")
	if err != nil || applied.Batch.Status != BatchApplied || len(target.apply) != 1 || target.apply[0] != "u-1" || len(connector.calls) != 1 {
		t.Fatalf("apply did not use persisted plan: %#v %v apply=%#v calls=%#v", applied, err, target.apply, connector.calls)
	}
	replay, err := service.Apply(context.Background(), auth, preview.Batch.ID, "apply-key-00001")
	if err != nil || !replay.Replay || replay.Batch.ID != applied.Batch.ID || replay.Batch.Status != applied.Batch.Status || replay.Batch.Counts != applied.Batch.Counts || len(target.apply) != 1 {
		t.Fatalf("apply replay mutated target: %#v %v apply=%#v", replay, err, target.apply)
	}
}

func TestConcurrentApplyLeaseAllowsOnlyOneWorker(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "concurrent-preview")
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	blocking := &blockingTarget{delegate: target, started: started, release: release}
	registry, _ := NewRegistry(connector)
	service, err = NewService(store, blocking, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, applyErr := service.Apply(context.Background(), auth, preview.Batch.ID, "concurrent-apply-one")
		firstDone <- applyErr
	}()
	<-started
	if _, err := service.Apply(context.Background(), auth, preview.Batch.ID, "concurrent-apply-two"); !errors.Is(err, ErrBusy) && !errors.Is(err, ErrConflict) {
		t.Fatalf("expected concurrent apply rejection, got %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
}

type blockingTarget struct {
	delegate Target
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (t *blockingTarget) Preview(ctx context.Context, org string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	return t.delegate.Preview(ctx, org, system, record, mapping)
}

func (t *blockingTarget) Apply(ctx context.Context, auth guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	t.once.Do(func() { close(t.started) })
	<-t.release
	return t.delegate.Apply(ctx, auth, system, item)
}

func (t *blockingTarget) Compensate(ctx context.Context, auth guard.Authentication, system SourceSystem, item Item, result TargetResult) error {
	return t.delegate.Compensate(ctx, auth, system, item, result)
}

func TestConflictAndTransientFailureRetryRemainExplicit(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{
		{SourceRecordID: "conflict", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Existing", Email: "existing@example.com", Status: "active"},
		{SourceRecordID: "retry", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Retry", Email: "retry@example.com", Status: "active"},
	})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{
		"conflict": {TargetID: "11111111111111111111111111111111", Revision: 1, CurrentDigest: testDigest("current"), Found: true, Conflict: true, ConflictReason: "target identity already exists", DesiredDigest: testDigest("desired")},
	}, results: map[string]TargetResult{}, errors: map[string]error{
		"retry": &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "provider target temporarily unavailable"},
	}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0003")
	if err != nil {
		t.Fatal(err)
	}
	applied, err := service.Apply(context.Background(), auth, preview.Batch.ID, "apply-key-00002")
	if err != nil || applied.Batch.Status != BatchPartial || applied.Batch.Counts.Conflicts != 1 || applied.Batch.Counts.Failed != 1 {
		t.Fatalf("explicit failures missing: %#v %v", applied, err)
	}
	delete(target.errors, "retry")
	retried, err := service.Retry(context.Background(), auth, preview.Batch.ID, "retry-key-0001")
	if err != nil || retried.Batch.Status != BatchPartial {
		t.Fatalf("transient retry did not complete: %#v %v", retried, err)
	}
	detail, _ := service.Get(context.Background(), preview.Batch.ID)
	if len(detail.Attempts) != 3 || detail.Items[0].Outcome != OutcomeConflict || detail.Items[1].Outcome != OutcomeApplied {
		t.Fatalf("attempt or item history was lost: %#v", detail)
	}
}

func TestCompleteSnapshotOnlyPlansDeactivation(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	first, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0004")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), auth, first.Batch.ID, "apply-key-00003"); err != nil {
		t.Fatal(err)
	}
	connector.pages[""] = Page{CompleteSnapshot: false}
	partial, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0005")
	if err != nil {
		t.Fatal(err)
	}
	partialDetail, _ := service.Get(context.Background(), partial.Batch.ID)
	if len(partialDetail.Items) != 0 || partial.Batch.Counts.Deactivated != 0 {
		t.Fatalf("partial snapshot deactivated records: %#v", partialDetail)
	}
	connector.pages[""] = Page{CompleteSnapshot: true}
	complete, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-0006")
	if err != nil {
		t.Fatal(err)
	}
	completeDetail, _ := service.Get(context.Background(), complete.Batch.ID)
	if len(completeDetail.Items) != 1 || completeDetail.Items[0].Action != ActionDeactivate {
		t.Fatalf("complete snapshot did not plan deactivation: %#v", completeDetail)
	}
}

func TestConnectorContractsRejectDuplicateRecordsAndCursorLoops(t *testing.T) {
	for name, connector := range map[string]*contractConnector{
		"duplicate": {system: SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "1"}, pages: map[string]Page{"": {Records: []Record{
			{SourceRecordID: "same", IdentityKind: "person", DisplayName: "A", Email: "a@example.com"},
			{SourceRecordID: "same", IdentityKind: "person", DisplayName: "B", Email: "b@example.com"},
		}, CompleteSnapshot: true}}},
		"cursor-loop": {system: SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "1"}, pages: map[string]Page{"": {NextCursor: "again"}, "again": {NextCursor: "again"}}},
	} {
		t.Run(name, func(t *testing.T) {
			service := newContractService(t, newContractMemoryStore(), &contractTarget{current: map[string]TargetPlan{}}, connector)
			_, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "preview-key-loop")
			if err == nil {
				t.Fatal("expected connector contract failure")
			}
		})
	}
}

func TestTransientPreviewFailureIsDurableAndRetryableWithoutStartingAnotherBatch(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	connector.errors[""] = &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "source temporarily unavailable"}
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	failed, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "failed-preview-key")
	if err != nil || failed.Batch.Status != BatchFailed || failed.Batch.Counts.Failed != 1 {
		t.Fatalf("preview failure was not persisted: %#v %v", failed, err)
	}
	replay, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "failed-preview-key")
	if err != nil || !replay.Replay || replay.Batch.ID != failed.Batch.ID || len(connector.calls) != 1 {
		t.Fatalf("failed preview replay was not exact: %#v %v calls=%#v", replay, err, connector.calls)
	}
	delete(connector.errors, "")
	recovered, err := service.Retry(context.Background(), auth, failed.Batch.ID, "failed-preview-retry")
	if err != nil || recovered.Batch.ID != failed.Batch.ID || recovered.Batch.Status != BatchPreviewed || recovered.Batch.Counts.Created != 1 || len(connector.calls) != 2 {
		t.Fatalf("preview retry did not recover the same batch: %#v %v calls=%#v", recovered, err, connector.calls)
	}
	detail, err := service.Get(context.Background(), failed.Batch.ID)
	if err != nil || len(detail.Items) != 1 || len(detail.Attempts) != 2 || detail.Attempts[0].Operation != OperationPreview || !detail.Attempts[0].Retryable || detail.Attempts[1].Operation != OperationRetry {
		t.Fatalf("preview retry history is incomplete: %#v %v", detail, err)
	}
	if _, err := service.Apply(context.Background(), auth, failed.Batch.ID, "failed-preview-apply"); err != nil {
		t.Fatalf("recovered preview could not be applied: %v", err)
	}
}

func TestTransientTargetReadFailureUsesDurablePreviewRetry(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}, previewErrors: map[string]error{
		"u-1": &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target temporarily unavailable"},
	}}
	service := newContractService(t, newContractMemoryStore(), target, connector)
	failed, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "target-preview-key")
	if err != nil || failed.Batch.Status != BatchFailed || failed.Batch.CompletedAt == nil {
		t.Fatalf("target read failure was not persisted: %#v %v", failed, err)
	}
	delete(target.previewErrors, "u-1")
	recovered, err := service.Retry(context.Background(), contractAuthentication(), failed.Batch.ID, "target-retry-key")
	if err != nil || recovered.Batch.Status != BatchPreviewed || recovered.Batch.Counts.Created != 1 || len(connector.calls) != 2 {
		t.Fatalf("target read retry did not recover preview: %#v %v calls=%#v", recovered, err, connector.calls)
	}
}

func TestRecoveredPreviewPlanSurvivesCrashBeforeBatchCompletion(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "u-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}})
	connector.errors[""] = &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "source temporarily unavailable"}
	store := &finishFailingStore{Store: newContractMemoryStore(), failRetry: true}
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	service, err := NewService(store, target, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	auth := contractAuthentication()
	failed, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "crash-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	delete(connector.errors, "")
	if _, err := service.Retry(context.Background(), auth, failed.Batch.ID, "crash-retry-key"); err == nil {
		t.Fatal("expected simulated post-plan crash")
	}
	interrupted, err := service.Get(context.Background(), failed.Batch.ID)
	if err != nil || len(interrupted.Items) != 1 || !interrupted.Batch.CompleteSnapshot || interrupted.Items[0].Outcome != OutcomePending {
		t.Fatalf("recovered plan was not durable before completion: %#v %v", interrupted, err)
	}
	now = now.Add(2*time.Minute + time.Second)
	recovered, err := service.Retry(context.Background(), auth, failed.Batch.ID, "crash-retry-key")
	if err != nil || recovered.Batch.Status != BatchPreviewed || recovered.Batch.Counts.Created != 1 || len(connector.calls) != 2 {
		t.Fatalf("takeover did not finalize stored plan exactly: %#v %v calls=%#v", recovered, err, connector.calls)
	}
	if len(target.apply) != 0 {
		t.Fatalf("preview recovery applied target mutations: %#v", target.apply)
	}
}

func TestPermanentPreviewFailureCannotBeRetried(t *testing.T) {
	connector := onePageConnector("hr-primary", true, nil)
	connector.errors[""] = &ClassifiedError{Class: FailurePermanent, Message: "source authorization is invalid"}
	service := newContractService(t, newContractMemoryStore(), &contractTarget{current: map[string]TargetPlan{}}, connector)
	failed, err := service.Preview(context.Background(), contractAuthentication(), PreviewRequest{SourceSystemID: "hr-primary"}, "permanent-preview-key")
	if err != nil || failed.Batch.Status != BatchFailed {
		t.Fatalf("permanent preview failure was not persisted: %#v %v", failed, err)
	}
	if _, err := service.Retry(context.Background(), contractAuthentication(), failed.Batch.ID, "permanent-retry-key"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("expected permanent failure to reject retry, got %v", err)
	}
}

func TestAuditHistoryIsDeterministicAndSanitized(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{
		SourceRecordID: "sensitive-source-id", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada",
		Email: "secret@example.com", Status: "active", Department: "Secret Department",
		DirectoryAttributes: map[string]string{"job-title": "Secret Job"}, GroupSourceIDs: []string{"group:sensitive-group"},
	}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	auditor := &contractAuditor{}
	service, err := NewService(store, target, auditor, registry, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "audit-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	replayContext := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "example-org", ActorID: "account:other", CorrelationID: "different-correlation"})
	otherAuth := guard.Authentication{Principal: guard.Principal{Subject: "account:other"}}
	if _, err := service.Preview(replayContext, otherAuth, PreviewRequest{SourceSystemID: "hr-primary"}, "audit-preview-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), auth, preview.Batch.ID, "audit-apply-key-1"); err != nil {
		t.Fatal(err)
	}
	if len(auditor.events) != 2 {
		t.Fatalf("expected idempotent preview and apply events, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		encoded, _ := json.Marshal(event.Metadata)
		metadata := string(encoded)
		if strings.Contains(metadata, "secret@example.com") || strings.Contains(metadata, "sensitive-source-id") ||
			strings.Contains(metadata, "Secret Department") || strings.Contains(metadata, "Secret Job") || strings.Contains(metadata, "sensitive-group") ||
			event.ResourceID != preview.Batch.ID || event.Metadata["requirementId"] != RequirementID {
			t.Fatalf("audit event exposed provider data or lost provenance: %#v", event)
		}
	}
}

func TestFailedRetriesProduceDistinctReplaySafeAuditEvents(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "retry", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Retry", Email: "retry@example.com", Status: "active"}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{
		"retry": &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "temporarily unavailable"},
	}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	auditor := &contractAuditor{}
	service, err := NewService(store, target, auditor, registry, ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "audit-failure-preview")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), auth, preview.Batch.ID, "audit-failure-apply"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Retry(context.Background(), auth, preview.Batch.ID, "audit-failure-retry"); err != nil {
		t.Fatal(err)
	}
	if len(auditor.events) != 3 {
		t.Fatalf("expected preview plus distinct apply and retry audits, got %#v", auditor.events)
	}
	if _, err := service.Retry(context.Background(), auth, preview.Batch.ID, "audit-failure-retry"); err != nil {
		t.Fatal(err)
	}
	if len(auditor.events) != 3 {
		t.Fatalf("exact retry replay duplicated audit event: %#v", auditor.events)
	}
}

func TestMaximumAttemptBoundStopsFurtherRetries(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{{SourceRecordID: "retry", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Retry", Email: "retry@example.com", Status: "active"}})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{
		"retry": &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "temporarily unavailable"},
	}}
	service := newContractService(t, store, target, connector)
	auth := contractAuthentication()
	preview, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "attempt-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), auth, preview.Batch.ID, "attempt-apply-key"); err != nil {
		t.Fatal(err)
	}
	for attempt := 2; attempt < MaximumAttempts; attempt++ {
		if _, err := service.Retry(context.Background(), auth, preview.Batch.ID, fmt.Sprintf("attempt-retry-%03d", attempt)); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err := service.Retry(context.Background(), auth, preview.Batch.ID, "attempt-retry-overflow"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("expected attempt bound, got %v", err)
	}
}

func TestPlanBoundIncludesHistoricalDeactivations(t *testing.T) {
	connector := onePageConnector("hr-primary", true, []Record{
		{SourceRecordID: "one", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "One", Email: "one@example.com", Status: "active"},
		{SourceRecordID: "two", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Two", Email: "two@example.com", Status: "active"},
	})
	store := newContractMemoryStore()
	target := &contractTarget{current: map[string]TargetPlan{}, results: map[string]TargetResult{}, errors: map[string]error{}}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, target, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org", MaxRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	auth := contractAuthentication()
	first, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "bound-preview-key-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), auth, first.Batch.ID, "bound-apply-key-01"); err != nil {
		t.Fatal(err)
	}
	connector.pages[""] = Page{Records: []Record{{SourceRecordID: "three", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Three", Email: "three@example.com", Status: "active"}}, CompleteSnapshot: true}
	if _, err := service.Preview(context.Background(), auth, PreviewRequest{SourceSystemID: "hr-primary"}, "bound-preview-key-2"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected historical deactivations to enforce plan bound, got %v", err)
	}
}

func TestPeopleTargetReconcilesWithCASAndPreservesClaimedOwnership(t *testing.T) {
	peopleStore := repository.NewMemoryPeopleStore()
	guardStore := repository.NewMemoryGuardStore()
	clock := func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	guardService, err := guard.NewService(guardStore, contractPasswordHasher{}, foundation.NopAuditor{}, nil, guard.ServiceConfig{OrganizationID: "example-org", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := guardService.Bootstrap(context.Background(), guard.BootstrapInput{Username: "administrator", Email: "administrator@example.test", DisplayName: "Administrator", Password: "correct horse battery staple"}, true)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewPeopleTarget(peopleStore, guardService, clock)
	if err != nil {
		t.Fatal(err)
	}
	system := SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "v1"}
	auth := credentials.Authentication
	record := Record{SourceRecordID: "employee-1", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "Ada", Email: "ada@example.test", Status: "active"}
	plan, err := target.Preview(context.Background(), "example-org", system, record, nil)
	if err != nil || plan.Found || plan.TargetID == "" {
		t.Fatalf("unexpected create plan %#v %v", plan, err)
	}
	item := Item{OrganizationID: "example-org", Record: record, TargetID: plan.TargetID, PlannedTargetDigest: plan.DesiredDigest, Action: ActionCreate}
	created, err := target.Apply(context.Background(), auth, system, item)
	if err != nil || !created.Changed || created.Revision != 1 {
		t.Fatalf("create target: %#v %v", created, err)
	}
	ownership, err := guardStore.GetResourceOwnership(context.Background(), "example-org", "people.identity", created.TargetID)
	if err != nil || !ownership.WriteLocked || ownership.SourceSystemID != system.ID {
		t.Fatalf("ownership was not registered: %#v %v", ownership, err)
	}
	mapping := &Mapping{TargetID: created.TargetID, SourceRecordID: record.SourceRecordID}
	updatedRecord := record
	updatedRecord.DisplayName = "Ada Updated"
	updatePlan, err := target.Preview(context.Background(), "example-org", system, updatedRecord, mapping)
	if err != nil || !updatePlan.Found || updatePlan.Conflict {
		t.Fatalf("unexpected update plan %#v %v", updatePlan, err)
	}
	updateItem := Item{OrganizationID: "example-org", Record: updatedRecord, TargetID: updatePlan.TargetID,
		ExpectedRevision: updatePlan.Revision, ObservedTargetDigest: updatePlan.CurrentDigest, PlannedTargetDigest: updatePlan.DesiredDigest, Action: ActionUpdate}
	updated, err := target.Apply(context.Background(), auth, system, updateItem)
	if err != nil || !updated.Changed || updated.Revision != 2 {
		t.Fatalf("update target: %#v %v", updated, err)
	}
	exact, err := target.Apply(context.Background(), auth, system, updateItem)
	if err != nil || exact.Changed || exact.Revision != 2 {
		t.Fatalf("exact target replay was not idempotent: %#v %v", exact, err)
	}
	claimedAt := time.Date(2026, 8, 13, 12, 1, 0, 0, time.UTC)
	if _, err := guardService.ClaimResourceOwnership(context.Background(), auth, "people.identity", created.TargetID); err != nil {
		t.Fatal(err)
	}
	ownership, err = guardStore.GetResourceOwnership(context.Background(), "example-org", "people.identity", created.TargetID)
	if err != nil || ownership.WriteLocked || ownership.ClaimedBy != auth.Principal.Subject {
		t.Fatalf("import re-locked claimed ownership: %#v %v", ownership, err)
	}

	thirdRecord := updatedRecord
	thirdRecord.DisplayName = "Provider Change"
	thirdPlan, err := target.Preview(context.Background(), "example-org", system, thirdRecord, mapping)
	if err != nil || !thirdPlan.Conflict {
		t.Fatalf("claimed target did not produce an explicit provider conflict: %#v %v", thirdPlan, err)
	}
	identity, err := peopleStore.GetIdentity(context.Background(), "example-org", created.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	identity.DisplayName, identity.NormalizedName, identity.Revision, identity.UpdatedAt = "Local Change", "local change", identity.Revision+1, claimedAt.Add(time.Minute)
	if _, err := peopleStore.ReconcileIdentity(context.Background(), identity, identity.Revision-1); err != nil {
		t.Fatal(err)
	}
	_, err = target.Apply(context.Background(), auth, system, Item{OrganizationID: "example-org", Record: thirdRecord, TargetID: thirdPlan.TargetID,
		ExpectedRevision: thirdPlan.Revision, ObservedTargetDigest: thirdPlan.CurrentDigest, PlannedTargetDigest: thirdPlan.DesiredDigest, Action: ActionUpdate})
	var classified *ClassifiedError
	if !errors.As(err, &classified) || classified.Class != FailureConflict {
		t.Fatalf("expected local edit conflict, got %v", err)
	}
}

func TestPeopleTargetReportsCrossSourceEmailConflict(t *testing.T) {
	peopleStore := repository.NewMemoryPeopleStore()
	now := time.Now().UTC()
	_, err := peopleStore.CreateIdentity(context.Background(), people.Identity{ID: testDigest("local")[:32], OrganizationID: "example-org", Kind: people.IdentityPerson,
		DisplayName: "Local", NormalizedName: "local", Email: "same@example.test", NormalizedEmail: "same@example.test", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), contractPasswordHasher{}, foundation.NopAuditor{}, nil, guard.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewPeopleTarget(peopleStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := target.Preview(context.Background(), "example-org", SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "v1"},
		Record{SourceRecordID: "external", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "External", Email: "same@example.test", Status: "active"}, nil)
	if err != nil || !plan.Conflict {
		t.Fatalf("expected email ownership conflict, got %#v %v", plan, err)
	}
}

func TestPeopleTargetGivesNeitherSailPointNorEntraImplicitPrecedence(t *testing.T) {
	peopleStore := repository.NewMemoryPeopleStore()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), contractPasswordHasher{}, foundation.NopAuditor{}, nil,
		guard.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewPeopleTarget(peopleStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	sailPoint := SourceSystem{ID: "sailpoint-primary", Provider: SailPointProvider, ConfigRevision: "sailpoint-v1"}
	sailPointRecord := Record{SourceRecordID: "identity:sailpoint-ada", Kind: RecordIdentity, IdentityKind: "person",
		DisplayName: "SailPoint Ada", Email: "ada@example.test", Status: "active"}
	create, err := target.Preview(context.Background(), "example-org", sailPoint, sailPointRecord, nil)
	if err != nil || create.Found || create.Conflict {
		t.Fatalf("unexpected SailPoint create plan %#v %v", create, err)
	}
	if _, err := target.Apply(context.Background(), contractAuthentication(), sailPoint, Item{OrganizationID: "example-org",
		Record: sailPointRecord, TargetID: create.TargetID, PlannedTargetDigest: create.DesiredDigest, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}

	entra := SourceSystem{ID: "entra-primary", Provider: "entra", ConfigRevision: "entra-v1"}
	entraRecord := Record{SourceRecordID: "user:entra-ada", Kind: RecordIdentity, IdentityKind: "person",
		DisplayName: "Entra Ada", Email: "ada@example.test", Status: "active"}
	conflict, err := target.Preview(context.Background(), "example-org", entra, entraRecord, nil)
	if err != nil || !conflict.Found || !conflict.Conflict || conflict.SourceMatched ||
		conflict.ConflictReason != "email belongs to another managed or local identity" {
		t.Fatalf("cross-provider precedence was not explicit %#v %v", conflict, err)
	}
	stored, err := peopleStore.GetIdentity(context.Background(), "example-org", create.TargetID)
	if err != nil || stored.DisplayName != "SailPoint Ada" || stored.ProviderSubject != sailPointRecord.SourceRecordID {
		t.Fatalf("cross-provider preview changed the existing identity %#v %v", stored, err)
	}
}

func newContractMemoryStore() Store {
	return repository.NewMemoryDirectoryImportStore()
}

func newContractService(t *testing.T, store Store, target Target, connector Connector) *Service {
	t.Helper()
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, target, foundation.NopAuditor{}, registry, ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func onePageConnector(id string, complete bool, records []Record) *contractConnector {
	return &contractConnector{system: SourceSystem{ID: id, Provider: "example", ConfigRevision: "v1"}, pages: map[string]Page{"": {Records: records, CompleteSnapshot: complete}}, errors: map[string]error{}}
}

func contractAuthentication() guard.Authentication {
	return guard.Authentication{Principal: guard.Principal{Subject: "account:test"}}
}

func testDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}

func TestSyntheticSeederRequiresExplicitEnablement(t *testing.T) {
	result, err := (SyntheticSeeder{}).Seed(context.Background())
	if err != nil || result.Enabled {
		t.Fatalf("synthetic data enabled unexpectedly")
	}
	if _, err = (SyntheticSeeder{Enabled: true, OrganizationID: "production"}).Seed(context.Background()); err == nil {
		t.Fatal("synthetic data accepted an incomplete production configuration")
	}
}

func TestGraphFiltersNodesAndEdges(t *testing.T) {
	store := NewMemoryGraph(Graph{Nodes: []Node{{ID: "a", Kind: "person", Label: "Alice"}, {ID: "b", Kind: "group", Label: "Staff"}}, Edges: []Edge{{ID: "e", From: "a", To: "b", Kind: "member_of"}}})
	graph, err := store.Graph(context.Background(), GraphQuery{Search: "alice", Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
		t.Fatalf("unexpected filtered graph: %#v", graph)
	}
}
