package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
)

type identifierTestAssets struct {
	organizationID string
	missing        bool
}

func (a identifierTestAssets) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	if a.missing {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return domain.Asset{ID: id, OrganizationID: a.organizationID}, nil
}

type identifierTestAuditor struct {
	events   []foundation.AuditEvent
	seen     map[string]foundation.AuditEvent
	failures map[string]int
}

func (a *identifierTestAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.failures[event.Action] > 0 {
		a.failures[event.Action]--
		return errors.New("temporary audit failure")
	}
	if a.seen == nil {
		a.seen = make(map[string]foundation.AuditEvent)
	}
	if existing, exists := a.seen[event.ID]; exists {
		if reflect.DeepEqual(existing, event) {
			return nil
		}
		return errors.New("audit event id collision")
	}
	a.seen[event.ID] = event
	a.events = append(a.events, event)
	return nil
}

type identifierTestStore struct {
	mu    sync.Mutex
	items map[string]Identifier
}

func newIdentifierTestStore() *identifierTestStore {
	return &identifierTestStore{items: make(map[string]Identifier)}
}

func identifierTestKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *identifierTestStore) ListIdentifiers(_ context.Context, organizationID, assetID string) ([]Identifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Identifier, 0)
	for _, item := range s.items {
		if item.OrganizationID == organizationID && item.AssetID == assetID {
			items = append(items, cloneIdentifier(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *identifierTestStore) GetIdentifier(_ context.Context, organizationID, assetID, identifierID string) (Identifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.items[identifierTestKey(organizationID, identifierID)]
	if !exists || item.AssetID != assetID {
		return Identifier{}, ErrNotFound
	}
	return cloneIdentifier(item), nil
}

func (s *identifierTestStore) ResolveIdentifier(_ context.Context, organizationID string, symbology Symbology, normalizedValue string) (Identifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.OrganizationID == organizationID && item.Status == StatusActive &&
			item.Symbology == symbology && item.NormalizedValue == normalizedValue {
			return cloneIdentifier(item), nil
		}
	}
	return Identifier{}, ErrNotFound
}

func (s *identifierTestStore) CreateIdentifier(_ context.Context, identifier Identifier) (Identifier, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := identifierTestKey(identifier.OrganizationID, identifier.ID)
	if existing, exists := s.items[key]; exists {
		if sameIdentifierAssociation(existing, identifier) {
			return cloneIdentifier(existing), false, nil
		}
		return Identifier{}, false, ErrConflict
	}
	if s.activeConflict(identifier, "") {
		return Identifier{}, false, ErrConflict
	}
	s.items[key] = cloneIdentifier(identifier)
	return cloneIdentifier(identifier), true, nil
}

func (s *identifierTestStore) ReplaceIdentifier(
	_ context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	replacement Identifier,
	changedAt time.Time,
) (Identifier, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := identifierTestKey(organizationID, identifierID)
	existing, exists := s.items[key]
	if !exists || existing.AssetID != assetID {
		return Identifier{}, false, ErrNotFound
	}
	if existing.Status == StatusReplaced {
		current, found := s.items[identifierTestKey(organizationID, existing.ReplacedByID)]
		if found && expectedRevision == existing.Revision-1 && sameIdentifierAssociation(current, replacement) {
			return cloneIdentifier(current), false, nil
		}
		return Identifier{}, false, ErrConflict
	}
	if existing.Status != StatusActive || existing.Revision != expectedRevision ||
		replacement.SupersedesID != existing.ID || s.activeConflict(replacement, existing.ID) {
		return Identifier{}, false, ErrConflict
	}
	replacementKey := identifierTestKey(organizationID, replacement.ID)
	if _, found := s.items[replacementKey]; found {
		return Identifier{}, false, ErrConflict
	}
	existing.Status = StatusReplaced
	existing.ReplacedByID = replacement.ID
	existing.Revision++
	existing.UpdatedAt = changedAt
	existing.UpdatedBy = replacement.UpdatedBy
	existing.UpdatedCorrelationID = replacement.UpdatedCorrelationID
	s.items[key] = existing
	s.items[replacementKey] = cloneIdentifier(replacement)
	return cloneIdentifier(replacement), true, nil
}

func (s *identifierTestStore) DeactivateIdentifier(
	_ context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	deactivatedAt time.Time,
	actorID, correlationID string,
) (Identifier, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := identifierTestKey(organizationID, identifierID)
	item, exists := s.items[key]
	if !exists || item.AssetID != assetID {
		return Identifier{}, false, ErrNotFound
	}
	if item.Status == StatusDeactivated {
		if expectedRevision == item.Revision-1 {
			return cloneIdentifier(item), false, nil
		}
		return Identifier{}, false, ErrConflict
	}
	if item.Status != StatusActive || item.Revision != expectedRevision {
		return Identifier{}, false, ErrConflict
	}
	item.Status = StatusDeactivated
	item.Revision++
	item.UpdatedAt = deactivatedAt
	item.UpdatedBy = actorID
	item.UpdatedCorrelationID = correlationID
	item.DeactivatedAt = &deactivatedAt
	s.items[key] = item
	return cloneIdentifier(item), true, nil
}

func (s *identifierTestStore) activeConflict(candidate Identifier, excludingID string) bool {
	for _, item := range s.items {
		if item.OrganizationID != candidate.OrganizationID || item.Status != StatusActive || item.ID == excludingID {
			continue
		}
		if item.Symbology == candidate.Symbology && item.NormalizedValue == candidate.NormalizedValue {
			return true
		}
		if candidate.Primary && item.Primary && item.AssetID == candidate.AssetID {
			return true
		}
	}
	return false
}

func sameIdentifierAssociation(left, right Identifier) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.AssetID == right.AssetID &&
		left.Symbology == right.Symbology && left.NormalizedValue == right.NormalizedValue &&
		left.DisplayValue == right.DisplayValue && left.Source == right.Source && left.Primary == right.Primary &&
		left.Status == right.Status && left.SupersedesID == right.SupersedesID
}

func TestNormalizeCodePreservesCaseAndEnforcesSymbologyBounds(t *testing.T) {
	symbology, value, err := normalizeCode(" CODE128 ", "  AbC-123  ")
	if err != nil || symbology != SymbologyCode128 || value != "AbC-123" {
		t.Fatalf("unexpected Code 128 normalization %q %q err=%v", symbology, value, err)
	}
	if _, _, err := normalizeCode(SymbologyCode128, "café"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected Code 128 to reject non-ASCII input, got %v", err)
	}
	if _, _, err := normalizeCode(SymbologyCode128, strings.Repeat("a", maximumCode128Bytes+1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected oversized Code 128 rejection, got %v", err)
	}
	if _, _, err := normalizeCode(SymbologyQR, "line\nbreak"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected control-character rejection, got %v", err)
	}
	if _, _, err := normalizeCode(SymbologyQR, strings.Repeat("é", maximumQRBytes/2)); err != nil {
		t.Fatalf("expected a %d-byte printable QR value, got %v", maximumQRBytes, err)
	}
	if _, _, err := normalizeCode(SymbologyQR, strings.Repeat("é", maximumQRBytes/2+1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected byte-bounded QR rejection, got %v", err)
	}
	if _, _, err := normalizeCode(Symbology("pdf417"), "value"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected unsupported symbology rejection, got %v", err)
	}
}

func TestServiceCreatesResolvesListsAndAuditsWithoutValues(t *testing.T) {
	now := time.Date(2026, time.August, 11, 15, 0, 0, 0, time.UTC)
	store := newIdentifierTestStore()
	auditor := &identifierTestAuditor{}
	service, err := NewService(store, identifierTestAssets{organizationID: "org-one"}, auditor, ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "correlation-one",
	})
	input := CreateIdentifierInput{
		ID: "identifier-one", AssetID: "asset-one", Symbology: SymbologyQR,
		Value: "  SensitiveCase/Token  ", DisplayValue: "Visible sensitive label", Source: SourceImported, Primary: true,
	}
	created, applied, err := service.CreateIdentifier(ctx, input)
	if err != nil || !applied {
		t.Fatalf("unexpected creation %#v applied=%t err=%v", created, applied, err)
	}
	if created.NormalizedValue != "SensitiveCase/Token" || created.DisplayValue != "Visible sensitive label" ||
		created.CreatedBy != "account-one" || created.CreatedCorrelationID != "correlation-one" ||
		created.UpdatedBy != "account-one" || created.UpdatedCorrelationID != "correlation-one" ||
		created.Revision != 1 || created.Status != StatusActive {
		t.Fatalf("unexpected normalized identifier %#v", created)
	}
	retried, applied, err := service.CreateIdentifier(ctx, input)
	if err != nil || applied || retried.ID != created.ID {
		t.Fatalf("expected stable-ID retry %#v applied=%t err=%v", retried, applied, err)
	}
	resolved, err := service.ResolveIdentifier(ctx, SymbologyQR, "SensitiveCase/Token")
	if err != nil || resolved.ID != created.ID {
		t.Fatalf("unexpected resolve %#v err=%v", resolved, err)
	}
	if _, err := service.ResolveIdentifier(ctx, SymbologyQR, "sensitivecase/token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected case-sensitive resolution, got %v", err)
	}
	if _, _, err := service.CreateIdentifier(ctx, CreateIdentifierInput{
		ID: "identifier-different", AssetID: "asset-one", Symbology: SymbologyQR,
		Value: "SensitiveCase/Token", DisplayValue: "Visible sensitive label", Source: SourceImported, Primary: true,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected an explicit different ID for the same code to conflict, got %v", err)
	}
	listed, err := service.ListIdentifiers(ctx, "asset-one")
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected list %#v err=%v", listed, err)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("expected one audit for creation plus retry, got %#v", auditor.events)
	}
	event := auditor.events[0]
	if event.Action != "atlas.identifier.created" || event.ResourceType != "asset_identifier" ||
		event.ResourceID != created.ID || event.Metadata["requirementId"] != RequirementID {
		t.Fatalf("unexpected audit %#v", event)
	}
	for key, value := range event.Metadata {
		if strings.Contains(key, "value") || strings.Contains(value, "SensitiveCase/Token") || strings.Contains(value, "Visible sensitive label") {
			t.Fatalf("audit leaked identifier content in %q=%q", key, value)
		}
	}
}

func TestServiceValidatesReferencesAndInput(t *testing.T) {
	service, err := NewService(
		newIdentifierTestStore(), identifierTestAssets{organizationID: "org-one", missing: true},
		foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateIdentifier(context.Background(), CreateIdentifierInput{
		AssetID: "missing-asset", Symbology: SymbologyQR, Value: "value",
	}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected missing asset reference, got %v", err)
	}
	crossOrganization, err := NewService(
		newIdentifierTestStore(), identifierTestAssets{organizationID: "other-org"},
		foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := crossOrganization.CreateIdentifier(context.Background(), CreateIdentifierInput{
		AssetID: "asset-one", Symbology: SymbologyQR, Value: "value",
	}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected cross-organization asset rejection, got %v", err)
	}
	validAssets := identifierTestAssets{organizationID: "org-one"}
	validService, err := NewService(newIdentifierTestStore(), validAssets, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []CreateIdentifierInput{
		{AssetID: "bad asset id", Symbology: SymbologyQR, Value: "value"},
		{AssetID: "asset-one", Symbology: Symbology("unsupported"), Value: "value"},
		{AssetID: "asset-one", Symbology: SymbologyQR, Value: "value", Source: Source("unknown")},
		{AssetID: "asset-one", Symbology: SymbologyQR, Value: "value", DisplayValue: strings.Repeat("x", maximumDisplayBytes+1)},
	} {
		if _, _, err := validService.CreateIdentifier(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid input rejection for %#v, got %v", input, err)
		}
	}
}

func TestServiceReplacesAndDeactivatesWithRetrySafeResults(t *testing.T) {
	now := time.Date(2026, time.August, 11, 16, 0, 0, 0, time.UTC)
	store := newIdentifierTestStore()
	auditor := &identifierTestAuditor{}
	service, err := NewService(store, identifierTestAssets{organizationID: "org-one"}, auditor, ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "correlation-one",
	})
	_, _, err = service.CreateIdentifier(ctx, CreateIdentifierInput{
		ID: "identifier-old", AssetID: "asset-one", Symbology: SymbologyCode128,
		Value: "OLD-CODE", Primary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	replacementInput := ReplaceIdentifierInput{
		AssetID: "asset-one", IdentifierID: "identifier-old", Revision: 1,
		ReplacementID: "identifier-new", ReplacementSymbology: SymbologyQR,
		ReplacementValue: "NewCaseSensitiveValue", Source: SourceGenerated,
	}
	replacement, changed, err := service.ReplaceIdentifier(ctx, replacementInput)
	if err != nil || !changed || replacement.ID != "identifier-new" || replacement.SupersedesID != "identifier-old" || !replacement.Primary {
		t.Fatalf("unexpected replacement %#v changed=%t err=%v", replacement, changed, err)
	}
	retried, changed, err := service.ReplaceIdentifier(ctx, replacementInput)
	if err != nil || changed || retried.ID != replacement.ID {
		t.Fatalf("expected safe replacement retry %#v changed=%t err=%v", retried, changed, err)
	}
	old, err := store.GetIdentifier(ctx, "org-one", "asset-one", "identifier-old")
	if err != nil || old.Status != StatusReplaced || old.ReplacedByID != replacement.ID || old.Revision != 2 {
		t.Fatalf("unexpected replacement history %#v err=%v", old, err)
	}
	deactivated, changed, err := service.DeactivateIdentifier(ctx, DeactivateIdentifierInput{
		AssetID: "asset-one", IdentifierID: replacement.ID, Revision: 1,
	})
	if err != nil || !changed || deactivated.Status != StatusDeactivated || deactivated.Revision != 2 || deactivated.DeactivatedAt == nil {
		t.Fatalf("unexpected deactivation %#v changed=%t err=%v", deactivated, changed, err)
	}
	retriedDeactivation, changed, err := service.DeactivateIdentifier(ctx, DeactivateIdentifierInput{
		AssetID: "asset-one", IdentifierID: replacement.ID, Revision: 1,
	})
	if err != nil || changed || retriedDeactivation.Status != StatusDeactivated {
		t.Fatalf("expected safe deactivation retry %#v changed=%t err=%v", retriedDeactivation, changed, err)
	}
	if _, _, err := service.DeactivateIdentifier(ctx, DeactivateIdentifierInput{
		AssetID: "asset-one", IdentifierID: replacement.ID, Revision: 2,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected stale deactivation conflict, got %v", err)
	}
	actions := make(map[string]int)
	for _, event := range auditor.events {
		actions[event.Action]++
	}
	if actions["atlas.identifier.created"] != 1 || actions["atlas.identifier.replaced"] != 1 ||
		actions["atlas.identifier.deactivated"] != 1 || len(auditor.events) != 3 {
		t.Fatalf("expected one audit per applied mutation, got %#v", actions)
	}
}

func TestServiceRepairsFailedMutationAuditsOnIdempotentRetry(t *testing.T) {
	now := time.Date(2026, time.August, 11, 18, 0, 0, 0, time.UTC)
	store := newIdentifierTestStore()
	auditor := &identifierTestAuditor{failures: map[string]int{"atlas.identifier.created": 1}}
	service, err := NewService(store, identifierTestAssets{organizationID: "org-one"}, auditor, ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	createCtx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-create", CorrelationID: "correlation-create-original",
	})
	replaceCtx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-replace", CorrelationID: "correlation-replace-original",
	})
	deactivateCtx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-deactivate", CorrelationID: "correlation-deactivate-original",
	})
	retryCtx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-retry", CorrelationID: "correlation-retry-different",
	})
	createInput := CreateIdentifierInput{
		AssetID: "asset-one", Symbology: SymbologyCode128, Value: "AUDIT-REPAIR-001",
		DisplayValue: "Audit repair code", Source: SourceUserEntered, Primary: true,
	}
	if _, _, err := service.CreateIdentifier(createCtx, createInput); err == nil || !strings.Contains(err.Error(), "audit Atlas Codes identifier creation") {
		t.Fatalf("expected the first create audit to fail, got %v", err)
	}
	now = now.Add(time.Minute)
	created, applied, err := service.CreateIdentifier(retryCtx, createInput)
	if err != nil || applied || created.ID == "" {
		t.Fatalf("expected same-intent create retry to repair audit, created=%#v applied=%t err=%v", created, applied, err)
	}

	now = now.Add(time.Minute)
	auditor.failures["atlas.identifier.replaced"] = 1
	replaceInput := ReplaceIdentifierInput{
		AssetID: "asset-one", IdentifierID: created.ID, Revision: 1,
		ReplacementSymbology: SymbologyQR, ReplacementValue: "AuditRepairQR002", Source: SourceGenerated,
	}
	if _, _, err := service.ReplaceIdentifier(replaceCtx, replaceInput); err == nil || !strings.Contains(err.Error(), "audit Atlas Codes identifier replacement") {
		t.Fatalf("expected the first replacement audit to fail, got %v", err)
	}
	now = now.Add(time.Minute)
	replacement, changed, err := service.ReplaceIdentifier(retryCtx, replaceInput)
	if err != nil || changed || replacement.ID == "" || replacement.SupersedesID != created.ID {
		t.Fatalf("expected replacement retry to repair audit, replacement=%#v changed=%t err=%v", replacement, changed, err)
	}

	now = now.Add(time.Minute)
	auditor.failures["atlas.identifier.deactivated"] = 1
	deactivateInput := DeactivateIdentifierInput{AssetID: "asset-one", IdentifierID: replacement.ID, Revision: 1}
	if _, _, err := service.DeactivateIdentifier(deactivateCtx, deactivateInput); err == nil || !strings.Contains(err.Error(), "audit Atlas Codes identifier deactivation") {
		t.Fatalf("expected the first deactivation audit to fail, got %v", err)
	}
	now = now.Add(time.Minute)
	deactivated, changed, err := service.DeactivateIdentifier(retryCtx, deactivateInput)
	if err != nil || changed || deactivated.Status != StatusDeactivated {
		t.Fatalf("expected deactivation retry to repair audit, identifier=%#v changed=%t err=%v", deactivated, changed, err)
	}

	actions := make(map[string]int)
	ids := make(map[string]struct{})
	for _, event := range auditor.events {
		actions[event.Action]++
		ids[event.ID] = struct{}{}
	}
	if len(auditor.events) != 3 || len(ids) != 3 || actions["atlas.identifier.created"] != 1 ||
		actions["atlas.identifier.replaced"] != 1 || actions["atlas.identifier.deactivated"] != 1 {
		t.Fatalf("expected one durable event per repaired mutation, events=%#v", auditor.events)
	}
	expectedProvenance := map[string]struct{ actor, correlation string }{
		"atlas.identifier.created":     {actor: "account-create", correlation: "correlation-create-original"},
		"atlas.identifier.replaced":    {actor: "account-replace", correlation: "correlation-replace-original"},
		"atlas.identifier.deactivated": {actor: "account-deactivate", correlation: "correlation-deactivate-original"},
	}
	for _, event := range auditor.events {
		expected := expectedProvenance[event.Action]
		if event.ActorID != expected.actor || event.CorrelationID != expected.correlation {
			t.Fatalf("expected repaired %s audit provenance actor=%q correlation=%q, got actor=%q correlation=%q",
				event.Action, expected.actor, expected.correlation, event.ActorID, event.CorrelationID)
		}
	}
}

func TestServiceDoesNotTreatAnActiveReplacementAsANoIDCreateRetry(t *testing.T) {
	now := time.Date(2026, time.August, 11, 19, 0, 0, 0, time.UTC)
	store := newIdentifierTestStore()
	auditor := &identifierTestAuditor{}
	service, err := NewService(store, identifierTestAssets{organizationID: "org-one"}, auditor, ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "correlation-original",
	})
	original, _, err := service.CreateIdentifier(ctx, CreateIdentifierInput{
		ID: "replacement-retry-original", AssetID: "asset-one", Symbology: SymbologyCode128,
		Value: "ORIGINAL-CODE", Primary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	replacement, _, err := service.ReplaceIdentifier(ctx, ReplaceIdentifierInput{
		AssetID: "asset-one", IdentifierID: original.ID, Revision: original.Revision,
		ReplacementID: "replacement-retry-current", ReplacementSymbology: SymbologyQR,
		ReplacementValue: "ACTIVE-REPLACEMENT", DisplayValue: "Replacement label", Source: SourceGenerated,
	})
	if err != nil {
		t.Fatal(err)
	}
	auditsBefore := len(auditor.events)
	if _, _, err := service.CreateIdentifier(ctx, CreateIdentifierInput{
		AssetID: "asset-one", Symbology: replacement.Symbology, Value: replacement.NormalizedValue,
		DisplayValue: replacement.DisplayValue, Source: replacement.Source, Primary: replacement.Primary,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected active replacement not to masquerade as a create retry, got %v", err)
	}
	if len(auditor.events) != auditsBefore {
		t.Fatalf("expected no fabricated create audit for replacement, got %#v", auditor.events[auditsBefore:])
	}
}

func TestNewServiceRequiresDependenciesAndOrganization(t *testing.T) {
	assets := identifierTestAssets{organizationID: "org-one"}
	if service, err := NewService(nil, assets, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one"}); err == nil || service != nil {
		t.Fatal("expected a required store")
	}
	if service, err := NewService(newIdentifierTestStore(), assets, foundation.NopAuditor{}, ServiceConfig{}); err == nil || service != nil {
		t.Fatal("expected a required organization")
	}
}
