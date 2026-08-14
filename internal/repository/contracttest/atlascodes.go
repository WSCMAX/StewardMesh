package contracttest

// Provider-neutral Atlas Codes adapter contract.
// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlascodes"
)

func AtlasCodesStore(
	t testing.TB,
	subject atlascodes.Store,
	organizationID, assetID, otherOrganizationID, otherAssetID, suffix string,
) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	primary := atlasCodesIdentifier(
		"codes-primary-"+suffix, organizationID, assetID, atlascodes.SymbologyCode128,
		"Case-Sensitive-"+suffix, true, now,
	)
	for name, mutate := range map[string]func(*atlascodes.Identifier){
		"oversized Code 128": func(item *atlascodes.Identifier) {
			item.NormalizedValue = strings.Repeat("A", 129)
			item.DisplayValue = item.NormalizedValue
		},
		"non-ASCII Code 128": func(item *atlascodes.Identifier) {
			item.NormalizedValue = "Café"
			item.DisplayValue = item.NormalizedValue
		},
		"QR control character": func(item *atlascodes.Identifier) {
			item.Symbology = atlascodes.SymbologyQR
			item.NormalizedValue = "unsafe\nvalue"
			item.DisplayValue = "safe display"
		},
		"oversized QR": func(item *atlascodes.Identifier) {
			item.Symbology = atlascodes.SymbologyQR
			item.NormalizedValue = strings.Repeat("Q", 513)
			item.DisplayValue = "oversized QR"
		},
		"oversized display value": func(item *atlascodes.Identifier) {
			item.DisplayValue = strings.Repeat("D", 513)
		},
		"missing audit provenance": func(item *atlascodes.Identifier) {
			item.CreatedCorrelationID = ""
		},
		"mismatched initial audit provenance": func(item *atlascodes.Identifier) {
			item.UpdatedBy = "different-contract-user"
		},
		"padded audit actor": func(item *atlascodes.Identifier) {
			item.CreatedBy = " contract-user "
			item.UpdatedBy = item.CreatedBy
		},
		"padded audit correlation": func(item *atlascodes.Identifier) {
			item.CreatedCorrelationID = " contract-correlation "
			item.UpdatedCorrelationID = item.CreatedCorrelationID
		},
	} {
		invalid := primary
		invalid.ID = primary.ID + "-invalid"
		mutate(&invalid)
		if _, _, err := subject.CreateIdentifier(ctx, invalid); !errors.Is(err, atlascodes.ErrInvalidInput) {
			t.Fatalf("expected invalid Atlas Codes payload for %s, got %v", name, err)
		}
	}
	if _, err := subject.GetIdentifier(ctx, organizationID, assetID, primary.ID); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected missing Atlas Codes identifier, got %v", err)
	}
	created, applied, err := subject.CreateIdentifier(ctx, primary)
	if err != nil || !applied || created.ID != primary.ID || created.Revision != 1 {
		t.Fatalf("unexpected Atlas Codes creation %#v applied=%t err=%v", created, applied, err)
	}
	byID, err := subject.GetIdentifierByID(ctx, organizationID, primary.ID)
	if err != nil || byID.AssetID != assetID {
		t.Fatalf("unexpected organization-scoped Atlas Codes ID lookup %#v err=%v", byID, err)
	}
	if _, err := subject.GetIdentifierByID(ctx, otherOrganizationID, primary.ID); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected organization-isolated Atlas Codes ID lookup, got %v", err)
	}

	retry := primary
	retry.CreatedAt = retry.CreatedAt.Add(time.Hour)
	retry.UpdatedAt = retry.UpdatedAt.Add(time.Hour)
	persisted, applied, err := subject.CreateIdentifier(ctx, retry)
	if err != nil || applied || persisted.ID != primary.ID || !persisted.CreatedAt.Equal(now) {
		t.Fatalf("expected stable-ID create retry %#v applied=%t err=%v", persisted, applied, err)
	}
	conflictingID := primary
	conflictingID.NormalizedValue += "-different"
	conflictingID.DisplayValue = conflictingID.NormalizedValue
	if _, _, err := subject.CreateIdentifier(ctx, conflictingID); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected conflicting stable ID, got %v", err)
	}

	resolved, err := subject.ResolveIdentifier(ctx, organizationID, primary.Symbology, primary.NormalizedValue)
	if err != nil || resolved.ID != primary.ID {
		t.Fatalf("unexpected Atlas Codes resolution %#v err=%v", resolved, err)
	}
	if _, err := subject.ResolveIdentifier(ctx, organizationID, primary.Symbology, "case-sensitive-"+suffix); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected case-sensitive resolution, got %v", err)
	}
	if _, err := subject.ResolveIdentifier(ctx, otherOrganizationID, primary.Symbology, primary.NormalizedValue); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected organization-isolated resolution before create, got %v", err)
	}

	crossOrganization := primary
	crossOrganization.ID = "codes-cross-" + suffix
	crossOrganization.OrganizationID = otherOrganizationID
	crossOrganization.AssetID = otherAssetID
	if _, applied, err := subject.CreateIdentifier(ctx, crossOrganization); err != nil || !applied {
		t.Fatalf("expected the same code in another organization, applied=%t err=%v", applied, err)
	}
	if _, err := subject.GetIdentifier(ctx, otherOrganizationID, otherAssetID, primary.ID); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected organization-isolated ID lookup, got %v", err)
	}
	duplicateValue := primary
	duplicateValue.ID = "codes-duplicate-" + suffix
	duplicateValue.AssetID = otherAssetID
	duplicateValue.Primary = false
	if _, _, err := subject.CreateIdentifier(ctx, duplicateValue); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected active symbology-and-value conflict, got %v", err)
	}

	secondPrimary := atlasCodesIdentifier(
		"codes-second-primary-"+suffix, organizationID, assetID, atlascodes.SymbologyQR,
		"https://codes.example.test/"+suffix, true, now.Add(time.Minute),
	)
	if _, _, err := subject.CreateIdentifier(ctx, secondPrimary); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected one active primary per asset, got %v", err)
	}
	secondary := secondPrimary
	secondary.ID = "codes-secondary-" + suffix
	secondary.Primary = false
	secondary.NormalizedValue += "/secondary"
	secondary.DisplayValue = "Secondary " + suffix
	if _, applied, err := subject.CreateIdentifier(ctx, secondary); err != nil || !applied {
		t.Fatalf("create secondary Atlas Codes identifier: applied=%t err=%v", applied, err)
	}

	items, err := subject.ListIdentifiers(ctx, organizationID, assetID)
	if err != nil || len(items) != 2 {
		t.Fatalf("unexpected initial Atlas Codes history %#v err=%v", items, err)
	}
	if isolated, err := subject.ListIdentifiers(ctx, otherOrganizationID, assetID); err != nil || len(isolated) != 0 {
		t.Fatalf("expected organization-and-asset isolation, items=%#v err=%v", isolated, err)
	}

	changedAt := now.Add(2 * time.Minute)
	replacement := atlasCodesIdentifier(
		"codes-replacement-"+suffix, organizationID, assetID, atlascodes.SymbologyQR,
		"https://codes.example.test/replacement/"+suffix, true, changedAt,
	)
	replacement.Source = atlascodes.SourceGenerated
	replacement.SupersedesID = primary.ID
	replaced, changed, err := subject.ReplaceIdentifier(
		ctx, organizationID, assetID, primary.ID, primary.Revision, replacement, changedAt,
	)
	if err != nil || !changed || replaced.ID != replacement.ID || replaced.SupersedesID != primary.ID {
		t.Fatalf("unexpected Atlas Codes replacement %#v changed=%t err=%v", replaced, changed, err)
	}
	previous, err := subject.GetIdentifier(ctx, organizationID, assetID, primary.ID)
	if err != nil || previous.Status != atlascodes.StatusReplaced || previous.ReplacedByID != replacement.ID ||
		previous.Revision != 2 || previous.DeactivatedAt == nil || !previous.DeactivatedAt.Equal(changedAt) ||
		previous.UpdatedBy != replacement.UpdatedBy || previous.UpdatedCorrelationID != replacement.UpdatedCorrelationID {
		t.Fatalf("unexpected replaced Atlas Codes history %#v err=%v", previous, err)
	}
	retryReplacement := replacement
	retryReplacement.CreatedAt = retryReplacement.CreatedAt.Add(time.Hour)
	retryReplacement.UpdatedAt = retryReplacement.UpdatedAt.Add(time.Hour)
	retryReplacement.CreatedBy = "contract-retry-user"
	retryReplacement.CreatedCorrelationID = "contract-retry-correlation"
	retryReplacement.UpdatedBy = retryReplacement.CreatedBy
	retryReplacement.UpdatedCorrelationID = retryReplacement.CreatedCorrelationID
	replaced, changed, err = subject.ReplaceIdentifier(
		ctx, organizationID, assetID, primary.ID, primary.Revision, retryReplacement, changedAt.Add(time.Hour),
	)
	if err != nil || changed || replaced.ID != replacement.ID ||
		replaced.CreatedBy != replacement.CreatedBy || replaced.CreatedCorrelationID != replacement.CreatedCorrelationID {
		t.Fatalf("expected idempotent Atlas Codes replacement %#v changed=%t err=%v", replaced, changed, err)
	}
	if _, _, err := subject.ReplaceIdentifier(
		ctx, organizationID, assetID, primary.ID, previous.Revision, replacement, changedAt,
	); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected current replaced revision not to masquerade as a retry, got %v", err)
	}
	mismatchedReplacement := replacement
	mismatchedReplacement.NormalizedValue += "-different"
	mismatchedReplacement.DisplayValue = mismatchedReplacement.NormalizedValue
	if _, _, err := subject.ReplaceIdentifier(
		ctx, organizationID, assetID, primary.ID, primary.Revision, mismatchedReplacement, changedAt,
	); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected mismatched replacement retry conflict, got %v", err)
	}
	if _, err := subject.ResolveIdentifier(ctx, organizationID, primary.Symbology, primary.NormalizedValue); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected replaced value to stop resolving, got %v", err)
	}
	if resolved, err := subject.ResolveIdentifier(ctx, organizationID, replacement.Symbology, replacement.NormalizedValue); err != nil || resolved.ID != replacement.ID {
		t.Fatalf("unexpected replacement resolution %#v err=%v", resolved, err)
	}

	staleReplacement := atlasCodesIdentifier(
		"codes-stale-replacement-"+suffix, organizationID, assetID, atlascodes.SymbologyQR,
		"https://codes.example.test/stale/"+suffix, false, changedAt.Add(time.Minute),
	)
	staleReplacement.SupersedesID = secondary.ID
	if _, _, err := subject.ReplaceIdentifier(
		ctx, organizationID, assetID, secondary.ID, 99, staleReplacement, changedAt.Add(time.Minute),
	); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected stale Atlas Codes replacement conflict, got %v", err)
	}

	concurrentValue := "CONCURRENT-" + suffix
	const workers = 12
	type createResult struct {
		created bool
		err     error
	}
	results := make(chan createResult, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			candidate := atlasCodesIdentifier(
				fmt.Sprintf("codes-claim-%02d-%s", index, suffix), organizationID, otherAssetID,
				atlascodes.SymbologyCode128, concurrentValue, false, changedAt.Add(2*time.Minute),
			)
			_, created, err := subject.CreateIdentifier(ctx, candidate)
			results <- createResult{created: created, err: err}
		}()
	}
	wait.Wait()
	close(results)
	winners, conflicts := 0, 0
	for result := range results {
		switch {
		case result.err == nil && result.created:
			winners++
		case errors.Is(result.err, atlascodes.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent Atlas Codes claim result created=%t err=%v", result.created, result.err)
		}
	}
	if winners != 1 || conflicts != workers-1 {
		t.Fatalf("expected one concurrent Atlas Codes winner, winners=%d conflicts=%d", winners, conflicts)
	}
	if resolved, err := subject.ResolveIdentifier(ctx, organizationID, atlascodes.SymbologyCode128, concurrentValue); err != nil || resolved.AssetID != otherAssetID {
		t.Fatalf("unexpected concurrently claimed identifier %#v err=%v", resolved, err)
	}

	deactivatedAt := changedAt.Add(3 * time.Minute)
	deactivatedBy := "contract-deactivator"
	deactivationCorrelationID := "contract-deactivation-correlation"
	deactivated, changed, err := subject.DeactivateIdentifier(
		ctx, organizationID, assetID, replacement.ID, replacement.Revision, deactivatedAt,
		deactivatedBy, deactivationCorrelationID,
	)
	if err != nil || !changed || deactivated.Status != atlascodes.StatusDeactivated || deactivated.Revision != 2 ||
		deactivated.DeactivatedAt == nil || !deactivated.DeactivatedAt.Equal(deactivatedAt) ||
		deactivated.UpdatedBy != deactivatedBy || deactivated.UpdatedCorrelationID != deactivationCorrelationID {
		t.Fatalf("unexpected Atlas Codes deactivation %#v changed=%t err=%v", deactivated, changed, err)
	}
	deactivated, changed, err = subject.DeactivateIdentifier(
		ctx, organizationID, assetID, replacement.ID, replacement.Revision, deactivatedAt.Add(time.Hour),
		"contract-retry-user", "contract-retry-correlation",
	)
	if err != nil || changed || deactivated.Revision != 2 || !deactivated.DeactivatedAt.Equal(deactivatedAt) ||
		deactivated.UpdatedBy != deactivatedBy || deactivated.UpdatedCorrelationID != deactivationCorrelationID {
		t.Fatalf("expected idempotent Atlas Codes deactivation %#v changed=%t err=%v", deactivated, changed, err)
	}
	if _, _, err := subject.DeactivateIdentifier(
		ctx, organizationID, assetID, replacement.ID, deactivated.Revision, deactivatedAt,
		deactivatedBy, deactivationCorrelationID,
	); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected current deactivated revision not to masquerade as a retry, got %v", err)
	}
	if _, err := subject.ResolveIdentifier(ctx, organizationID, replacement.Symbology, replacement.NormalizedValue); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("expected deactivated identifier to stop resolving, got %v", err)
	}
	if _, _, err := subject.DeactivateIdentifier(
		ctx, organizationID, assetID, secondary.ID, 99, deactivatedAt, deactivatedBy, deactivationCorrelationID,
	); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected stale Atlas Codes deactivation conflict, got %v", err)
	}
	if _, _, err := subject.DeactivateIdentifier(
		ctx, organizationID, assetID, secondary.ID, secondary.Revision, deactivatedAt, "", "",
	); !errors.Is(err, atlascodes.ErrInvalidInput) {
		t.Fatalf("expected missing deactivation audit provenance to fail, got %v", err)
	}
	if _, changed, err := subject.DeactivateIdentifier(
		ctx, organizationID, assetID, secondary.ID, secondary.Revision, deactivatedAt, deactivatedBy, deactivationCorrelationID,
	); err != nil || !changed {
		t.Fatalf("deactivate secondary Atlas Codes identifier: changed=%t err=%v", changed, err)
	}

	newPrimary := atlasCodesIdentifier(
		"codes-new-primary-"+suffix, organizationID, assetID, atlascodes.SymbologyCode128,
		"NEW-PRIMARY-"+suffix, true, deactivatedAt.Add(time.Minute),
	)
	if _, applied, err := subject.CreateIdentifier(ctx, newPrimary); err != nil || !applied {
		t.Fatalf("expected deactivation to release active primary uniqueness, applied=%t err=%v", applied, err)
	}
	history, err := subject.ListIdentifiers(ctx, organizationID, assetID)
	if err != nil || len(history) != 4 {
		t.Fatalf("unexpected complete Atlas Codes history %#v err=%v", history, err)
	}
	assertAtlasCodesExchangeStore(t, subject, organizationID, assetID, suffix, now)
}

func assertAtlasCodesExchangeStore(
	t testing.TB,
	subject atlascodes.Store,
	organizationID, assetID, suffix string,
	now time.Time,
) {
	t.Helper()
	ctx := context.Background()
	if _, err := subject.SnapshotIdentifiers(ctx, organizationID, 1); !errors.Is(err, atlascodes.ErrTooLarge) {
		t.Fatalf("expected bounded Atlas Codes Exchange snapshot, got %v", err)
	}
	replacedAt := now.Add(12 * time.Hour)
	chain := atlascodes.IdentifierChain{TerminalID: "exchange-code-new-" + suffix, Items: []atlascodes.Identifier{
		{
			ID: "exchange-code-old-" + suffix, OrganizationID: organizationID, AssetID: assetID,
			Symbology: atlascodes.SymbologyCode128, NormalizedValue: "EXCHANGE-OLD-" + suffix,
			DisplayValue: "Exchange old " + suffix, Source: atlascodes.SourceImported, Primary: false,
			Status: atlascodes.StatusReplaced, ReplacedByID: "exchange-code-new-" + suffix, Revision: 2,
			CreatedBy: "exchange-source", CreatedCorrelationID: "exchange-create-" + suffix,
			UpdatedBy: "exchange-replacer", UpdatedCorrelationID: "exchange-replace-" + suffix,
			CreatedAt: now.Add(10 * time.Hour), UpdatedAt: replacedAt, DeactivatedAt: &replacedAt,
		},
		{
			ID: "exchange-code-new-" + suffix, OrganizationID: organizationID, AssetID: assetID,
			Symbology: atlascodes.SymbologyQR, NormalizedValue: "https://exchange.example.test/codes/" + suffix,
			DisplayValue: "Exchange new " + suffix, Source: atlascodes.SourceGenerated, Primary: false,
			Status: atlascodes.StatusActive, SupersedesID: "exchange-code-old-" + suffix, Revision: 1,
			CreatedBy: "exchange-replacer", CreatedCorrelationID: "exchange-replace-" + suffix,
			UpdatedBy: "exchange-replacer", UpdatedCorrelationID: "exchange-replace-" + suffix,
			CreatedAt: replacedAt, UpdatedAt: replacedAt,
		},
	}}
	persisted, created, err := subject.ImportIdentifierChain(ctx, organizationID, chain)
	if err != nil || !created || !reflect.DeepEqual(persisted, chain) {
		t.Fatalf("atomic Atlas Codes chain import failed: chain=%#v created=%t err=%v", persisted, created, err)
	}
	if replayed, created, err := subject.ImportIdentifierChain(ctx, organizationID, chain); err != nil || created || !reflect.DeepEqual(replayed, chain) {
		t.Fatalf("exact Atlas Codes chain replay failed: chain=%#v created=%t err=%v", replayed, created, err)
	}
	conflicting := chain
	conflicting.Items = append([]atlascodes.Identifier(nil), chain.Items...)
	conflicting.Items[1].DisplayValue = "changed"
	if _, _, err := subject.ImportIdentifierChain(ctx, organizationID, conflicting); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected changed Atlas Codes chain replay conflict, got %v", err)
	}
	partial := atlascodes.IdentifierChain{TerminalID: "exchange-code-partial-" + suffix, Items: []atlascodes.Identifier{
		chain.Items[0],
		{
			ID: "exchange-code-partial-" + suffix, OrganizationID: organizationID, AssetID: assetID,
			Symbology: atlascodes.SymbologyQR, NormalizedValue: "https://exchange.example.test/partial/" + suffix,
			DisplayValue: "Exchange partial " + suffix, Source: atlascodes.SourceImported, Primary: false,
			Status: atlascodes.StatusActive, SupersedesID: chain.Items[0].ID, Revision: 1,
			CreatedBy: "exchange-replacer", CreatedCorrelationID: "exchange-replace-" + suffix,
			UpdatedBy: "exchange-replacer", UpdatedCorrelationID: "exchange-replace-" + suffix,
			CreatedAt: replacedAt, UpdatedAt: replacedAt,
		},
	}}
	if _, _, err := subject.ImportIdentifierChain(ctx, organizationID, partial); !errors.Is(err, atlascodes.ErrConflict) {
		t.Fatalf("expected partial Atlas Codes chain conflict, got %v", err)
	}
	if _, err := subject.GetIdentifierByID(ctx, organizationID, partial.TerminalID); !errors.Is(err, atlascodes.ErrNotFound) {
		t.Fatalf("partial Atlas Codes chain left a row behind: %v", err)
	}
	snapshot, err := subject.SnapshotIdentifiers(ctx, organizationID, 10_000)
	if err != nil {
		t.Fatalf("complete Atlas Codes Exchange snapshot failed: %v", err)
	}
	for _, expected := range chain.Items {
		found := false
		for _, item := range snapshot {
			if reflect.DeepEqual(item, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Atlas Codes Exchange snapshot omitted %#v", expected)
		}
	}
}

func atlasCodesIdentifier(
	id, organizationID, assetID string,
	symbology atlascodes.Symbology,
	value string,
	primary bool,
	createdAt time.Time,
) atlascodes.Identifier {
	return atlascodes.Identifier{
		ID: id, OrganizationID: organizationID, AssetID: assetID, Symbology: symbology,
		NormalizedValue: value, DisplayValue: value, Source: atlascodes.SourceUserEntered,
		Primary: primary, Status: atlascodes.StatusActive, Revision: 1,
		CreatedBy: "contract-user", CreatedCorrelationID: "contract-correlation",
		UpdatedBy: "contract-user", UpdatedCorrelationID: "contract-correlation",
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}
