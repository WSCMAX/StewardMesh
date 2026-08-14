package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-DIRECTORY-EXPANSION-005, REQ-PATTERNS-001. Features: migration.packages, integrations.protocols, templates.schemas.

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type directoryProviderHasher struct{}

func (directoryProviderHasher) Hash(string) (string, error) { return "directory-test-hash", nil }
func (directoryProviderHasher) Verify(string, string) (bool, bool, error) {
	return true, false, nil
}

func newDirectoryProviderTarget(t *testing.T, organizationID string, now time.Time) (*directoryexpansion.GroupTarget, directoryexpansion.ExchangeImporter, *repository.MemoryDirectoryImportStore, *guard.Service) {
	t.Helper()
	store := repository.NewMemoryDirectoryImportStore()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), directoryProviderHasher{}, foundation.NopAuditor{}, nil,
		guard.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	target, importer, err := directoryexpansion.NewGroupTargetWithExchangeImporter(store, guardService, foundation.NopAuditor{},
		directoryexpansion.GroupTargetExchangeConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return target, importer, store, guardService
}

func applyDirectoryFixture(t *testing.T, target *directoryexpansion.GroupTarget, guardService *guard.Service, system directoryexpansion.SourceSystem, records ...directoryexpansion.Record) []directoryexpansion.TargetResult {
	t.Helper()
	credentials, err := guardService.Bootstrap(t.Context(), guard.BootstrapInput{Username: "administrator", Email: "administrator@example.test",
		DisplayName: "Administrator", Password: "correct horse battery staple"}, true)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]directoryexpansion.TargetResult, 0, len(records))
	for _, record := range records {
		plan, err := target.Preview(t.Context(), "source-org", system, record, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := target.Apply(t.Context(), credentials.Authentication, system, directoryexpansion.Item{OrganizationID: "source-org",
			Record: record, TargetID: plan.TargetID, PlannedTargetDigest: plan.DesiredDigest, Action: directoryexpansion.ActionCreate})
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, result)
	}
	return results
}

func TestDirectoryProviderRoundTripUsesRealManagedSubjectAndNestedGroupDependencies(t *testing.T) {
	now := time.Date(2026, time.August, 13, 20, 10, 11, 987000000, time.UTC)
	sourceTarget, sourceImporter, _, sourceGuard := newDirectoryProviderTarget(t, "source-org", now)
	if _, err := exchange.NewDirectoryProvider(sourceTarget, nil); err == nil {
		t.Fatal("expected Directory provider to require its importer capability")
	}
	sourceProvider, err := exchange.NewDirectoryProvider(sourceTarget, sourceImporter)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourceProvider.Types(); !slices.Equal(got, []string{"directory.group", "directory.membership"}) {
		t.Fatalf("unexpected Directory provider types %#v", got)
	}
	system := directoryexpansion.SourceSystem{ID: "directory-source", Provider: directoryexpansion.GrouperProvider, ConfigRevision: "config-v1"}
	parent := directoryexpansion.Record{SourceRecordID: "group-parent", Kind: directoryexpansion.RecordGroup, GroupName: "parent",
		DisplayName: "Parent group", Description: "Portable parent", Status: "active", NormalizedMetadata: map[string]string{"origin": "grouper", "scope": "all"}}
	nested := directoryexpansion.Record{SourceRecordID: "group-nested", Kind: directoryexpansion.RecordGroup, GroupName: "nested",
		DisplayName: "Nested group", Status: "inactive", NormalizedMetadata: map[string]string{"origin": "grouper"}}
	subjectMembership := directoryexpansion.Record{SourceRecordID: "membership-subject", Kind: directoryexpansion.RecordMembership,
		DisplayName: "Embedded subject", Status: "active", GroupSourceID: parent.SourceRecordID, MemberSourceID: "subject-one",
		MemberKind: directoryexpansion.MemberSubject, NormalizedMetadata: map[string]string{"membership": "direct"}}
	nestedMembership := directoryexpansion.Record{SourceRecordID: "membership-nested", Kind: directoryexpansion.RecordMembership,
		DisplayName: nested.DisplayName, Status: "active", GroupSourceID: parent.SourceRecordID, MemberSourceID: nested.SourceRecordID,
		MemberKind: directoryexpansion.MemberGroup, NormalizedMetadata: map[string]string{"membership": "nested"}}
	results := applyDirectoryFixture(t, sourceTarget, sourceGuard, system, parent, nested, subjectMembership, nestedMembership)
	records, err := sourceProvider.ListRecords(t.Context())
	if err != nil || len(records) != 4 {
		t.Fatalf("list Directory records %#v err=%v", records, err)
	}
	byID := make(map[string]exchange.Record, len(records))
	for _, record := range records {
		byID[record.ID] = record
		for _, forbidden := range [][]byte{[]byte("organizationId"), []byte("administrator@example.test"), []byte("correct horse")} {
			if bytes.Contains(record.Payload, forbidden) {
				t.Fatalf("Directory payload leaked deployment/operator state: %s", record.Payload)
			}
		}
	}
	subjectRecord := byID[results[2].TargetID]
	if got := subjectRecord.Dependencies; !slices.Equal(got, []exchange.Reference{{Type: "directory.group", ID: results[0].TargetID}}) {
		t.Fatalf("subject membership must be self-contained except for its parent group: %#v", got)
	}
	nestedRecord := byID[results[3].TargetID]
	wantNestedDependencies := []exchange.Reference{{Type: "directory.group", ID: results[0].TargetID}, {Type: "directory.group", ID: results[1].TargetID}}
	slices.SortFunc(wantNestedDependencies, func(left, right exchange.Reference) int {
		if left.Key() < right.Key() {
			return -1
		}
		if left.Key() > right.Key() {
			return 1
		}
		return 0
	})
	if !slices.Equal(nestedRecord.Dependencies, wantNestedDependencies) {
		t.Fatalf("unexpected nested-group dependencies %#v", nestedRecord.Dependencies)
	}

	targetTarget, targetImporter, _, _ := newDirectoryProviderTarget(t, "target-org", now.Add(24*time.Hour))
	targetProvider, err := exchange.NewDirectoryProvider(targetTarget, targetImporter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewDirectoryProvider(sourceTarget, targetImporter); err == nil {
		t.Fatal("expected Directory provider to reject another target's importer")
	}
	for index, id := range []string{results[0].TargetID, results[1].TargetID, results[2].TargetID, results[3].TargetID} {
		record := byID[id]
		result, err := targetProvider.ImportRecord(t.Context(), exchange.ProviderImportOperation{Token: "directory-import-" + string(rune('a'+index)),
			OccurredAt: now, ExpectedCreated: true}, "package-source", record, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s result=%#v err=%v", record.Type, result, err)
		}
		exact, err := targetProvider.ImportRecordExists(t.Context(), record, nil)
		if err != nil || !exact {
			t.Fatalf("exact compare %s exact=%t err=%v", record.Type, exact, err)
		}
	}
	storedSubject, err := targetTarget.GetManagedMembership(t.Context(), results[2].TargetID)
	if err != nil || storedSubject.MemberKind != directoryexpansion.MemberSubject || storedSubject.MemberID == "" ||
		storedSubject.MemberDisplayName != subjectMembership.DisplayName || storedSubject.Revision != 1 || !storedSubject.CreatedAt.Equal(now) {
		t.Fatalf("subject membership did not round trip losslessly: %#v err=%v", storedSubject, err)
	}
	replay, err := targetProvider.ImportRecord(t.Context(), exchange.ProviderImportOperation{Token: "directory-replay", OccurredAt: now, ExpectedCreated: false},
		"package-source", subjectRecord, nil)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("exact Directory replay: %#v err=%v", replay, err)
	}
	conflicting := subjectRecord
	conflicting.Payload = bytes.Replace(conflicting.Payload, []byte(`"status":"active"`), []byte(`"status":"inactive"`), 1)
	if _, err := targetProvider.ImportRecord(t.Context(), exchange.ProviderImportOperation{Token: "directory-conflict", OccurredAt: now,
		ExpectedCreated: false}, "package-source", conflicting, nil); !errors.Is(err, exchange.ErrConflict) {
		t.Fatalf("expected exact replay conflict, got %v", err)
	}
}

func TestDirectoryProviderRejectsMissingNestedGroupAndSensitiveMetadata(t *testing.T) {
	now := time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC)
	target, importer, store, _ := newDirectoryProviderTarget(t, "target-org", now)
	provider, err := exchange.NewDirectoryProvider(target, importer)
	if err != nil {
		t.Fatal(err)
	}
	groupID := "11111111111111111111111111111111"
	memberID := "22222222222222222222222222222222"
	membershipID := "33333333333333333333333333333333"
	parent := directoryexpansion.ManagedGroup{ID: groupID, OrganizationID: "target-org", SourceSystemID: "directory-source", SourceRecordID: "parent",
		Name: "parent", DisplayName: "Parent", Status: "active", Revision: 5, CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateManagedGroup(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"createdAt":"2026-08-13T21:00:00Z","groupId":"` + groupID + `","groupSourceId":"parent","memberDisplayName":"Nested","memberId":"` + memberID + `","memberKind":"group","memberSourceId":"nested","metadata":"{}","sourceRecordId":"membership","sourceSystemId":"directory-source","status":"active","updatedAt":"2026-08-13T21:00:00Z"}`)
	record := exchange.Record{Type: "directory.membership", ID: membershipID, Revision: 9,
		Dependencies: []exchange.Reference{{Type: "directory.group", ID: groupID}, {Type: "directory.group", ID: memberID}}, Payload: payload}
	reorderedPayload := record
	reorderedPayload.Payload = []byte(`{"sourceSystemId":"directory-source","sourceRecordId":"membership","groupId":"` + groupID + `","groupSourceId":"parent","memberId":"` + memberID + `","memberSourceId":"nested","memberKind":"group","memberDisplayName":"Nested","status":"active","metadata":"{}","createdAt":"2026-08-13T21:00:00Z","updatedAt":"2026-08-13T21:00:00Z"}`)
	if exact, err := provider.ImportRecordExists(t.Context(), reorderedPayload, nil); err != nil || exact {
		t.Fatalf("expected equivalent field-order Directory payload to parse without invalid input and not match an existing record: exact=%t err=%v", exact, err)
	}
	leadingWhitespace := record
	leadingWhitespace.Payload = append([]byte(" "), leadingWhitespace.Payload...)
	if _, err := provider.ImportRecordExists(t.Context(), leadingWhitespace, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected leading-whitespace Directory payload rejection, got %v", err)
	}
	nonCanonical := record
	nonCanonical.ID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := provider.ImportRecordExists(t.Context(), nonCanonical, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected non-canonical managed ID rejection, got %v", err)
	}
	withFile := record
	withFile.File = &exchange.FileMetadata{}
	if _, err := provider.ImportRecordExists(t.Context(), withFile, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected Directory file metadata rejection, got %v", err)
	}
	if _, err := provider.ImportRecord(t.Context(), exchange.ProviderImportOperation{Token: "directory-missing", OccurredAt: now, ExpectedCreated: true},
		"package-source", record, nil); !errors.Is(err, exchange.ErrDependencyMissing) {
		t.Fatalf("expected missing nested group dependency, got %v", err)
	}
	sensitive := directoryexpansion.ManagedGroup{ID: "44444444444444444444444444444444", OrganizationID: "target-org", SourceSystemID: "directory-source",
		SourceRecordID: "sensitive", Name: "sensitive", DisplayName: "Sensitive", Status: "active",
		Metadata: map[string]string{"access_token": "must-not-export"}, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateManagedGroup(t.Context(), sensitive); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ListRecords(t.Context()); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected credential-bearing metadata rejection, got %v", err)
	}
	if err := store.DeleteManagedGroup(t.Context(), "target-org", sensitive.ID, sensitive.Revision); err != nil {
		t.Fatal(err)
	}
	sensitive.Metadata = map[string]string{"documentation": "https://objects.example.test/file?X-Amz-Signature=private"}
	sensitive.ID = "55555555555555555555555555555555"
	sensitive.SourceRecordID = "signed-url"
	if _, err := store.CreateManagedGroup(t.Context(), sensitive); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ListRecords(t.Context()); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected signed-URL metadata rejection, got %v", err)
	}
}
