package stack

// Requirement: REQ-STACK-001. Feature: software.licenses.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

func TestServiceTracksEntitlementsUsageFinancialProvenanceAndCompliance(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	store := newTestStore()
	references := &testReferences{}
	auditor := &testAuditor{}
	service := newTestService(t, store, references, auditor, "org-one", now)
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "request-one"})

	product, err := service.CreateProduct(ctx, CreateProductInput{ID: "product-one", Name: "Steward Writer", Publisher: "Example Publisher", Category: "productivity"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.CreateVersion(ctx, CreateVersionInput{ID: "version-one", ProductID: product.ID, Name: "2026.8"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordInstallation(ctx, RecordInstallationInput{ID: "install-one", VersionID: version.ID, AssetID: "asset-one", InstalledAt: now.Add(-24 * time.Hour), UsageState: "used"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordInstallation(ctx, RecordInstallationInput{ID: "install-two", VersionID: version.ID, AssetID: "asset-two", InstalledAt: now.Add(-24 * time.Hour), UsageState: "unknown"}); err != nil {
		t.Fatal(err)
	}
	expires := now.AddDate(0, 0, 30)
	license, err := service.CreateLicense(ctx, CreateLicenseInput{
		ID: "license-one", ProductID: product.ID, VersionID: version.ID, Name: "Annual device subscription",
		EntitlementMetric: "device", Quantity: 1, ExpiresOn: &expires, VendorID: "vendor-one",
		PurchaseOrderID: "purchase-one", ContractID: "contract-one", CostRecordID: "cost-one",
		DocumentIDs: []string{"document-two", "document-one"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := service.CreateAssignment(ctx, CreateAssignmentInput{ID: "assignment-one", LicenseID: license.ID, AssigneeKind: "asset", AssigneeID: "asset-one", Seats: 2, UsageState: "unused", AssignedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	lastUsed := now
	if _, err := service.UpdateAssignmentUsage(ctx, UpdateAssignmentUsageInput{ID: assignment.ID, UsageState: "used", LastUsedAt: &lastUsed, Revision: assignment.Revision}); err != nil {
		t.Fatal(err)
	}
	assignment, err = store.GetAssignment(ctx, "org-one", assignment.ID)
	if err != nil {
		t.Fatal(err)
	}
	assignment.UsageState = "unused"
	assignment.Revision++
	if _, err := store.UpdateAssignment(ctx, assignment, assignment.Revision-1); err != nil {
		t.Fatal(err)
	}

	report, err := service.Analytics(ctx, now, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.Products != 1 || report.ActiveInstallations != 2 || report.ActiveLicenses != 1 || report.EntitledQuantity != 1 || report.AssignedQuantity != 2 || report.UnderusedAssignments != 1 {
		t.Fatalf("unexpected analytics totals: %#v", report)
	}
	codes := make([]string, 0, len(report.ComplianceConditions))
	for _, condition := range report.ComplianceConditions {
		codes = append(codes, condition.Code)
	}
	if strings.Join(codes, ",") != "expiring,missing_license,over_assigned,under_used" {
		t.Fatalf("unexpected condition codes %v", codes)
	}
	if !reflect.DeepEqual(references.assets, []string{"asset-one", "asset-two", "asset-one", "asset-two"}) || !reflect.DeepEqual(references.assignees, []string{"asset:asset-one"}) {
		t.Fatalf("unexpected reference validation: %#v", references)
	}
	if !reflect.DeepEqual(references.financial, []string{"vendor-one|purchase-one|contract-one|cost-one"}) || !reflect.DeepEqual(references.documents, []string{"document-one", "document-two"}) {
		t.Fatalf("expected Ledger and Vault references, got %#v", references)
	}
	for _, event := range auditor.events {
		serialized := event.Action + event.ResourceID
		for key, value := range event.Metadata {
			serialized += key + value
		}
		if strings.Contains(serialized, "Steward Writer") || strings.Contains(serialized, "Example Publisher") {
			t.Fatalf("audit leaked descriptive software data: %#v", event)
		}
		if event.Metadata["requirementId"] != RequirementID {
			t.Fatalf("missing requirement audit metadata: %#v", event)
		}
	}
}

func TestServiceExportsAndPreflightsDependencyOrderedRecordsForDurableImport(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	source := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "source-org", now)
	product, err := source.CreateProduct(context.Background(), CreateProductInput{ID: "product", Name: "Database", Publisher: "Example", SourceSystemID: "CATALOG", SourceRecordID: "product"})
	if err != nil {
		t.Fatal(err)
	}
	if product.SourceSystemID != "catalog" {
		t.Fatalf("expected normalized source identity, got %q", product.SourceSystemID)
	}
	version, err := source.CreateVersion(context.Background(), CreateVersionInput{ID: "version", ProductID: product.ID, Name: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	license, err := source.CreateLicense(context.Background(), CreateLicenseInput{ID: "license", ProductID: product.ID, Name: "Enterprise", EntitlementMetric: "enterprise", Quantity: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.RecordInstallation(context.Background(), RecordInstallationInput{ID: "installation", VersionID: version.ID, AssetID: "asset", InstalledAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "assignment", LicenseID: license.ID, AssigneeKind: "department", AssigneeID: "department", Seats: 25, AssignedAt: now}); err != nil {
		t.Fatal(err)
	}

	records, err := source.ExportRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || records[0].Type != "stack.product" || records[1].Type != "stack.version" || records[2].Type != "stack.license" {
		t.Fatalf("unexpected portable records %#v", records)
	}
	normalized, err := NormalizeImportRecords("EXCHANGE-SOURCE", reverseRecords(records))
	if err != nil || len(normalized) != 5 || normalized[0].Type != "stack.product" || normalized[1].Type != "stack.version" || normalized[0].SourceSystemID != "catalog" || normalized[1].SourceSystemID != "exchange-source" {
		t.Fatalf("unexpected normalized import %#v err=%v", normalized, err)
	}
	targetStore := newTestStore()
	target := newTestService(t, targetStore, &testReferences{}, &testAuditor{}, "target-org", now)
	if _, err := target.ImportRecords(context.Background(), "exchange-source", records); !errors.Is(err, ErrDurableImportRequired) {
		t.Fatalf("unsafe batch import was not rejected: %v", err)
	}
	if snapshot, err := target.Snapshot(context.Background()); err != nil || len(snapshot.Products)+len(snapshot.Versions)+len(snapshot.Installations)+len(snapshot.Licenses)+len(snapshot.Assignments) != 0 {
		t.Fatalf("rejected unsafe import wrote records %#v, %v", snapshot, err)
	}

	changed := append([]ExchangeRecord(nil), records...)
	var payload Product
	if err := json.Unmarshal(changed[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Name = "Conflicting name"
	changed[0].Payload = mustJSON(t, payload)
	if _, err := NormalizeImportRecords("exchange-source", changed); err != nil {
		t.Fatalf("a valid changed payload should pass read-only normalization before durable conflict detection: %v", err)
	}
}

func TestExchangeDependenciesUseCanonicalPatternsRecordTypes(t *testing.T) {
	dependencies := portableLicenseDependencies(License{ProductID: "product", PurchaseOrderID: "purchase-order"})
	if !reflect.DeepEqual(dependencies, []string{"ledger.purchase-order:purchase-order", "stack.product:product"}) {
		t.Fatalf("portable license dependencies drifted from Patterns: %#v", dependencies)
	}

	references := &testReferences{}
	service := newTestService(t, newTestStore(), references, &testAuditor{}, "source-org", time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC))
	handled, exists, err := service.ExchangeDependencyExists(context.Background(), "ledger.purchase-order", "purchase-order")
	if err != nil || !handled || !exists || !reflect.DeepEqual(references.financial, []string{"|purchase-order||"}) {
		t.Fatalf("canonical purchase-order lookup handled=%t exists=%t calls=%#v err=%v", handled, exists, references.financial, err)
	}
	if handled, exists, err := service.ExchangeDependencyExists(context.Background(), "ledger.purchase_order", "purchase-order"); err != nil || handled || exists {
		t.Fatalf("legacy noncanonical purchase_order lookup handled=%t exists=%t err=%v", handled, exists, err)
	}
}

func TestServiceRejectsTamperedPortableRecordsBeforeWriting(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	source := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "source-org", now)
	product, _ := source.CreateProduct(context.Background(), CreateProductInput{ID: "product", Name: "Database", Publisher: "Example"})
	_, _ = source.CreateVersion(context.Background(), CreateVersionInput{ID: "version", ProductID: product.ID, Name: "1.0"})
	records, err := source.ExportRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]func([]ExchangeRecord) []ExchangeRecord{
		"dependency metadata is missing": func(values []ExchangeRecord) []ExchangeRecord {
			values[0].Dependencies = nil
			return values
		},
		"header id does not match payload": func(values []ExchangeRecord) []ExchangeRecord {
			values[0].ID = "other-product"
			return values
		},
		"header revision does not match payload": func(values []ExchangeRecord) []ExchangeRecord {
			values[0].Revision++
			return values
		},
		"dependency metadata does not match payload": func(values []ExchangeRecord) []ExchangeRecord {
			values[1].Dependencies = []string{"stack.product:other-product"}
			return values
		},
		"payload contains an unknown field": func(values []ExchangeRecord) []ExchangeRecord {
			var payload map[string]any
			if err := json.Unmarshal(values[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload["unexpected"] = true
			values[0].Payload = mustJSON(t, payload)
			return values
		},
		"batch repeats a record identity": func(values []ExchangeRecord) []ExchangeRecord {
			return append(values, values[0])
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			targetStore := newTestStore()
			target := newTestService(t, targetStore, &testReferences{}, &testAuditor{}, "target-org", now)
			if _, err := NormalizeImportRecords("exchange-source", mutate(cloneExchangeRecords(records))); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid portable record, got %v", err)
			}
			snapshot, err := target.Snapshot(context.Background())
			if err != nil || len(snapshot.Products)+len(snapshot.Versions) != 0 {
				t.Fatalf("preflight failure wrote records: %#v, %v", snapshot, err)
			}
		})
	}
}

func TestServiceImportsMaximumLengthRecordIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	maximumID := "p" + strings.Repeat("x", 127)
	source := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "source-org", now)
	if _, err := source.CreateProduct(context.Background(), CreateProductInput{ID: maximumID, Name: "Database", Publisher: "Example"}); err != nil {
		t.Fatal(err)
	}
	records, err := source.ExportRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeImportRecords("exchange-source", records)
	if err != nil || len(normalized) != 1 || normalized[0].ID != maximumID {
		t.Fatalf("maximum-length identity should preflight, result=%#v err=%v", normalized, err)
	}
}

func TestServiceRejectsMismatchedReferencesDatesAndOptimisticUsage(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	store := newTestStore()
	references := &testReferences{contexts: map[string]AssetContext{"asset-mismatch": {ID: "another-asset"}}}
	service := newTestService(t, store, references, &testAuditor{}, "org-one", now)
	first, _ := service.CreateProduct(context.Background(), CreateProductInput{ID: "first", Name: "First", Publisher: "Publisher"})
	second, _ := service.CreateProduct(context.Background(), CreateProductInput{ID: "second", Name: "Second", Publisher: "Publisher"})
	version, _ := service.CreateVersion(context.Background(), CreateVersionInput{ID: "version", ProductID: first.ID, Name: "1"})
	if _, err := service.RecordInstallation(context.Background(), RecordInstallationInput{VersionID: version.ID, AssetID: "asset-mismatch", InstalledAt: now}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected mismatched resolved asset identity failure, got %v", err)
	}
	if _, err := service.CreateLicense(context.Background(), CreateLicenseInput{ProductID: second.ID, VersionID: version.ID, Name: "Mismatch", EntitlementMetric: "device", Quantity: 1}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected mismatched product/version reference failure, got %v", err)
	}
	before, after := now.AddDate(0, 0, 1), now
	if _, err := service.CreateLicense(context.Background(), CreateLicenseInput{ProductID: first.ID, Name: "Dates", EntitlementMetric: "device", Quantity: 1, StartsOn: &before, ExpiresOn: &after}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid date range, got %v", err)
	}
	license, _ := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "license", ProductID: first.ID, Name: "Valid", EntitlementMetric: "device", Quantity: 1})
	future := now.AddDate(0, 0, 1)
	futureLicense, _ := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "future-license", ProductID: first.ID, Name: "Future", EntitlementMetric: "device", Quantity: 1, StartsOn: &future})
	if _, err := service.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "future-assignment", LicenseID: futureLicense.ID, AssigneeKind: "asset", AssigneeID: "asset", Seats: 1, AssignedAt: now}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected inactive effective-dated entitlement conflict, got %v", err)
	}
	if _, err := service.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "scheduled-assignment", LicenseID: futureLicense.ID, AssigneeKind: "asset", AssigneeID: "asset", Seats: 1, AssignedAt: future}); err != nil {
		t.Fatalf("expected assignment scheduled within the entitlement period, got %v", err)
	}
	if _, err := service.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "wrong-metric", LicenseID: license.ID, AssigneeKind: "identity", AssigneeID: "person", Seats: 1, AssignedAt: now}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected device entitlement to require an asset assignment, got %v", err)
	}
	assignment, _ := service.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "assignment", LicenseID: license.ID, AssigneeKind: "asset", AssigneeID: "asset", Seats: 1, AssignedAt: now})
	if _, err := service.UpdateAssignmentUsage(context.Background(), UpdateAssignmentUsageInput{ID: assignment.ID, UsageState: "used", Revision: assignment.Revision + 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected optimistic revision conflict, got %v", err)
	}
}

func TestServiceUpdatesLifecycleAndCoversPeopleScopedInstallations(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	store := newTestStore()
	references := &testReferences{contexts: map[string]AssetContext{"asset-site": {ID: "asset-site", SiteID: "site-one"}}}
	service := newTestService(t, store, references, &testAuditor{}, "org-one", now)
	product, _ := service.CreateProduct(context.Background(), CreateProductInput{ID: "product", Name: "Writer", Publisher: "Example"})
	version, _ := service.CreateVersion(context.Background(), CreateVersionInput{ID: "version", ProductID: product.ID, Name: "1"})
	installation, err := service.RecordInstallation(context.Background(), RecordInstallationInput{ID: "installation", VersionID: version.ID, AssetID: "asset-site", InstalledAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	license, _ := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "license", ProductID: product.ID, Name: "Site", EntitlementMetric: "site", Quantity: 20})
	assignment, _ := service.CreateAssignment(context.Background(), CreateAssignmentInput{ID: "assignment", LicenseID: license.ID, AssigneeKind: "site", AssigneeID: "site-one", Seats: 20, AssignedAt: now.Add(-time.Hour)})
	report, err := service.Analytics(context.Background(), now, 90)
	if err != nil {
		t.Fatal(err)
	}
	for _, condition := range report.ComplianceConditions {
		if condition.Code == "missing_license" {
			t.Fatalf("site assignment should cover installation: %#v", condition)
		}
	}

	removedAt := now
	if _, err := service.UpdateInstallationState(context.Background(), UpdateInstallationStateInput{ID: installation.ID, Status: "removed", UsageState: "unused", RemovedAt: &removedAt, Revision: installation.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLicenseEntitlement(context.Background(), UpdateLicenseEntitlementInput{ID: license.ID, Quantity: 25, Status: "active", Revision: license.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EndAssignment(context.Background(), EndAssignmentInput{ID: assignment.ID, EndedAt: now, Revision: assignment.Revision}); err != nil {
		t.Fatal(err)
	}
	version, err = service.UpdateVersionStatus(context.Background(), UpdateVersionStatusInput{ID: version.ID, Status: "unsupported", Revision: version.Revision})
	if err != nil {
		t.Fatal(err)
	}
	product, err = service.UpdateProductStatus(context.Background(), UpdateProductStatusInput{ID: product.ID, Status: "retired", Revision: product.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateProductStatus(context.Background(), UpdateProductStatusInput{ID: product.ID, Status: "active", Revision: product.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected retired product to be terminal, got %v", err)
	}
	version, err = service.UpdateVersionStatus(context.Background(), UpdateVersionStatusInput{ID: version.ID, Status: "retired", Revision: version.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateVersionStatus(context.Background(), UpdateVersionStatusInput{ID: version.ID, Status: "active", Revision: version.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected retired version to be terminal, got %v", err)
	}
	historical, err := service.Analytics(context.Background(), now.Add(-30*time.Minute), 90)
	if err != nil || historical.ActiveInstallations != 1 || historical.AssignedQuantity != 20 {
		t.Fatalf("expected as-of analytics to include records before removal and assignment end: %#v err=%v", historical, err)
	}
	for _, condition := range historical.ComplianceConditions {
		if condition.Code == "missing_license" {
			t.Fatalf("historical site assignment should cover the installation: %#v", condition)
		}
	}
	current, err := service.Analytics(context.Background(), now, 90)
	if err != nil || current.ActiveInstallations != 0 || current.AssignedQuantity != 0 {
		t.Fatalf("expected removal and assignment end to take effect at their timestamp: %#v err=%v", current, err)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil || snapshot.Products[0].Status != "retired" || snapshot.Versions[0].Status != "retired" || snapshot.Installations[0].Status != "removed" || snapshot.Licenses[0].Quantity != 25 || snapshot.Assignments[0].EndedAt == nil {
		t.Fatalf("unexpected lifecycle snapshot %#v err=%v", snapshot, err)
	}
}

func TestServiceReportsExplicitExpiredLicenseWithoutExpirationDate(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "org-one", now)
	product, _ := service.CreateProduct(context.Background(), CreateProductInput{ID: "product", Name: "Writer", Publisher: "Example"})
	if _, err := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "license", ProductID: product.ID, Name: "Expired", EntitlementMetric: "enterprise", Quantity: 1, Status: "expired"}); err != nil {
		t.Fatal(err)
	}
	report, err := service.Analytics(context.Background(), now, 90)
	if err != nil || len(report.ComplianceConditions) != 1 || report.ComplianceConditions[0].Code != "expired" {
		t.Fatalf("expected explicit expired condition, report=%#v err=%v", report, err)
	}
}

func TestServiceAnalyticsUsesEmptyArrayWhenThereAreNoConditions(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "org-one", now)
	report, err := service.Analytics(context.Background(), now, 90)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"complianceConditions":null`) || !strings.Contains(string(encoded), `"complianceConditions":[]`) {
		t.Fatalf("empty analytics conditions must remain a JSON array: %s", encoded)
	}
}

func TestServiceTreatsExpirationDateAsInclusiveAndSkipsRetiredAlerts(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, newTestStore(), &testReferences{}, &testAuditor{}, "org-one", now)
	product, _ := service.CreateProduct(context.Background(), CreateProductInput{ID: "product", Name: "Writer", Publisher: "Example"})
	expiresToday := now
	if _, err := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "inclusive", ProductID: product.ID, Name: "Inclusive", EntitlementMetric: "enterprise", Quantity: 1, ExpiresOn: &expiresToday}); err != nil {
		t.Fatal(err)
	}
	past := now.AddDate(0, 0, -1)
	if _, err := service.CreateLicense(context.Background(), CreateLicenseInput{ID: "retired", ProductID: product.ID, Name: "Retired", EntitlementMetric: "enterprise", Quantity: 1, ExpiresOn: &past, Status: "retired"}); err != nil {
		t.Fatal(err)
	}

	report, err := service.Analytics(context.Background(), now, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.ActiveLicenses != 1 || report.EntitledQuantity != 1 || len(report.ComplianceConditions) != 1 || report.ComplianceConditions[0].Code != "expiring" || report.ComplianceConditions[0].DaysUntilExpiry != 0 {
		t.Fatalf("expiration day should remain active and retired licenses should not alert: %#v", report)
	}
	nextDay, err := service.Analytics(context.Background(), now.AddDate(0, 0, 1), 90)
	if err != nil || nextDay.ActiveLicenses != 0 || len(nextDay.ComplianceConditions) != 1 || nextDay.ComplianceConditions[0].Code != "expired" {
		t.Fatalf("license should expire after its calendar date: %#v err=%v", nextDay, err)
	}
}

func cloneExchangeRecords(values []ExchangeRecord) []ExchangeRecord {
	result := make([]ExchangeRecord, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Dependencies = append([]string(nil), value.Dependencies...)
		result[index].Payload = append(json.RawMessage(nil), value.Payload...)
	}
	return result
}

func reverseRecords(values []ExchangeRecord) []ExchangeRecord {
	result := append([]ExchangeRecord(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

type testReferences struct {
	assets, assignees, financial, documents []string
	contexts                                map[string]AssetContext
}

func (r *testReferences) ResolveAsset(_ context.Context, id string) (AssetContext, error) {
	r.assets = append(r.assets, id)
	if value, ok := r.contexts[id]; ok {
		return value, nil
	}
	return AssetContext{ID: id}, nil
}
func (r *testReferences) ValidateAssignee(_ context.Context, kind, id string) error {
	r.assignees = append(r.assignees, kind+":"+id)
	return nil
}
func (r *testReferences) ValidateFinancialReferences(_ context.Context, vendor, purchase, contract, cost string) error {
	r.financial = append(r.financial, strings.Join([]string{vendor, purchase, contract, cost}, "|"))
	return nil
}
func (r *testReferences) ValidateDocuments(_ context.Context, ids []string) error {
	r.documents = append(r.documents, ids...)
	return nil
}

type testAuditor struct{ events []foundation.AuditEvent }

func (a *testAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func newTestService(t *testing.T, store Store, references ReferenceValidator, auditor foundation.Auditor, organizationID string, now time.Time) *Service {
	t.Helper()
	service, err := NewService(store, references, auditor, ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type testStore struct {
	products      map[string]Product
	versions      map[string]Version
	installations map[string]Installation
	licenses      map[string]License
	assignments   map[string]Assignment
	sources       map[string]string
}

func newTestStore() *testStore {
	return &testStore{products: map[string]Product{}, versions: map[string]Version{}, installations: map[string]Installation{}, licenses: map[string]License{}, assignments: map[string]Assignment{}, sources: map[string]string{}}
}
func storeKey(org, id string) string { return org + "\x00" + id }
func sourceKey(org, system, record string) string {
	if system == "" {
		return ""
	}
	return org + "\x00" + system + "\x00" + record
}
func (s *testStore) Snapshot(_ context.Context, org string) (Snapshot, error) {
	result := Snapshot{}
	for _, value := range s.products {
		if value.OrganizationID == org {
			result.Products = append(result.Products, value)
		}
	}
	for _, value := range s.versions {
		if value.OrganizationID == org {
			result.Versions = append(result.Versions, value)
		}
	}
	for _, value := range s.installations {
		if value.OrganizationID == org {
			result.Installations = append(result.Installations, value)
		}
	}
	for _, value := range s.licenses {
		if value.OrganizationID == org {
			value.DocumentIDs = append([]string(nil), value.DocumentIDs...)
			result.Licenses = append(result.Licenses, value)
		}
	}
	for _, value := range s.assignments {
		if value.OrganizationID == org {
			result.Assignments = append(result.Assignments, value)
		}
	}
	sort.Slice(result.Products, func(i, j int) bool { return result.Products[i].ID < result.Products[j].ID })
	sort.Slice(result.Versions, func(i, j int) bool { return result.Versions[i].ID < result.Versions[j].ID })
	sort.Slice(result.Installations, func(i, j int) bool { return result.Installations[i].ID < result.Installations[j].ID })
	sort.Slice(result.Licenses, func(i, j int) bool { return result.Licenses[i].ID < result.Licenses[j].ID })
	sort.Slice(result.Assignments, func(i, j int) bool { return result.Assignments[i].ID < result.Assignments[j].ID })
	return result, nil
}
func (s *testStore) GetProduct(_ context.Context, org, id string) (Product, error) {
	v, ok := s.products[storeKey(org, id)]
	if !ok {
		return Product{}, ErrNotFound
	}
	return v, nil
}
func (s *testStore) GetVersion(_ context.Context, org, id string) (Version, error) {
	v, ok := s.versions[storeKey(org, id)]
	if !ok {
		return Version{}, ErrNotFound
	}
	return v, nil
}
func (s *testStore) GetInstallation(_ context.Context, org, id string) (Installation, error) {
	v, ok := s.installations[storeKey(org, id)]
	if !ok {
		return Installation{}, ErrNotFound
	}
	return v, nil
}
func (s *testStore) GetLicense(_ context.Context, org, id string) (License, error) {
	v, ok := s.licenses[storeKey(org, id)]
	if !ok {
		return License{}, ErrNotFound
	}
	v.DocumentIDs = append([]string(nil), v.DocumentIDs...)
	return v, nil
}
func (s *testStore) GetAssignment(_ context.Context, org, id string) (Assignment, error) {
	v, ok := s.assignments[storeKey(org, id)]
	if !ok {
		return Assignment{}, ErrNotFound
	}
	return v, nil
}
func (s *testStore) CreateProduct(_ context.Context, v Product) (Product, bool, error) {
	for _, x := range s.products {
		if x.OrganizationID == v.OrganizationID && strings.EqualFold(x.Publisher, v.Publisher) && strings.EqualFold(x.Name, v.Name) && x.ID != v.ID {
			return Product{}, false, ErrConflict
		}
	}
	existing, created, err := createExact(s.sources, s.products, v.OrganizationID, v.ID, v.SourceSystemID, v.SourceRecordID, v)
	return existing, created, err
}
func (s *testStore) CreateVersion(_ context.Context, v Version) (Version, bool, error) {
	for _, x := range s.versions {
		if x.OrganizationID == v.OrganizationID && x.ProductID == v.ProductID && strings.EqualFold(x.Name, v.Name) && x.ID != v.ID {
			return Version{}, false, ErrConflict
		}
	}
	return createExact(s.sources, s.versions, v.OrganizationID, v.ID, v.SourceSystemID, v.SourceRecordID, v)
}
func (s *testStore) CreateInstallation(_ context.Context, v Installation) (Installation, bool, error) {
	for _, x := range s.installations {
		if x.OrganizationID == v.OrganizationID && x.VersionID == v.VersionID && x.AssetID == v.AssetID && x.Status == "installed" && v.Status == "installed" && x.ID != v.ID {
			return Installation{}, false, ErrConflict
		}
	}
	return createExact(s.sources, s.installations, v.OrganizationID, v.ID, v.SourceSystemID, v.SourceRecordID, v)
}
func (s *testStore) CreateLicense(_ context.Context, v License) (License, bool, error) {
	v.DocumentIDs = append([]string(nil), v.DocumentIDs...)
	return createExact(s.sources, s.licenses, v.OrganizationID, v.ID, v.SourceSystemID, v.SourceRecordID, v)
}
func (s *testStore) CreateAssignment(_ context.Context, v Assignment) (Assignment, bool, error) {
	for _, x := range s.assignments {
		if x.OrganizationID == v.OrganizationID && x.LicenseID == v.LicenseID && x.AssigneeKind == v.AssigneeKind && x.AssigneeID == v.AssigneeID && x.EndedAt == nil && x.ID != v.ID {
			return Assignment{}, false, ErrConflict
		}
	}
	return createExact(s.sources, s.assignments, v.OrganizationID, v.ID, v.SourceSystemID, v.SourceRecordID, v)
}
func (s *testStore) UpdateAssignment(_ context.Context, v Assignment, revision int64) (Assignment, error) {
	key := storeKey(v.OrganizationID, v.ID)
	x, ok := s.assignments[key]
	if !ok {
		return Assignment{}, ErrNotFound
	}
	if x.Revision != revision {
		return Assignment{}, ErrConflict
	}
	s.assignments[key] = v
	return v, nil
}
func (s *testStore) UpdateProduct(_ context.Context, v Product, revision int64) (Product, error) {
	key := storeKey(v.OrganizationID, v.ID)
	x, ok := s.products[key]
	if !ok {
		return Product{}, ErrNotFound
	}
	if x.Revision != revision {
		return Product{}, ErrConflict
	}
	s.products[key] = v
	return v, nil
}
func (s *testStore) UpdateVersion(_ context.Context, v Version, revision int64) (Version, error) {
	key := storeKey(v.OrganizationID, v.ID)
	x, ok := s.versions[key]
	if !ok {
		return Version{}, ErrNotFound
	}
	if x.Revision != revision {
		return Version{}, ErrConflict
	}
	s.versions[key] = v
	return v, nil
}
func (s *testStore) UpdateInstallation(_ context.Context, v Installation, revision int64) (Installation, error) {
	key := storeKey(v.OrganizationID, v.ID)
	x, ok := s.installations[key]
	if !ok {
		return Installation{}, ErrNotFound
	}
	if x.Revision != revision {
		return Installation{}, ErrConflict
	}
	s.installations[key] = v
	return v, nil
}
func (s *testStore) UpdateLicense(_ context.Context, v License, revision int64) (License, error) {
	key := storeKey(v.OrganizationID, v.ID)
	x, ok := s.licenses[key]
	if !ok {
		return License{}, ErrNotFound
	}
	if x.Revision != revision {
		return License{}, ErrConflict
	}
	s.licenses[key] = v
	return v, nil
}

type sourced interface {
	Product | Version | Installation | License | Assignment
}

func createExact[T sourced](sources map[string]string, values map[string]T, org, id, system, record string, value T) (T, bool, error) {
	key := storeKey(org, id)
	source := sourceKey(org, system, record)
	if source != "" {
		if existingKey, ok := sources[source]; ok {
			existing := values[existingKey]
			if reflect.DeepEqual(existing, value) {
				return existing, false, nil
			}
			var zero T
			return zero, false, ErrConflict
		}
	}
	if existing, ok := values[key]; ok {
		if reflect.DeepEqual(existing, value) {
			return existing, false, nil
		}
		var zero T
		return zero, false, ErrConflict
	}
	values[key] = value
	if source != "" {
		sources[source] = key
	}
	return value, true, nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
