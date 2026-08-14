package patterns_test

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestBuiltInTemplatesCoverCoreRecordsAndEveryFieldType(t *testing.T) {
	service := newPatternsService(t)
	items, err := service.ListTemplates(context.Background(), patterns.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	wantRecords := patterns.CoreRecordTypes()
	records := map[string]bool{}
	types := map[patterns.FieldType]bool{}
	for _, item := range items {
		wantVersion := int64(1)
		if strings.HasPrefix(item.RecordType, "atlas.catalog-") || item.RecordType == "horizon.plan" ||
			item.RecordType == "atlas.asset" || item.RecordType == "atlas.model" ||
			item.RecordType == "atlas.identifier" || item.RecordType == "atlas.lifecycle-event" ||
			strings.HasPrefix(item.RecordType, "people.") || strings.HasPrefix(item.RecordType, "threads.") || strings.HasPrefix(item.RecordType, "ledger.") || strings.HasPrefix(item.RecordType, "signals.") && item.RecordType != "signals.alert" || strings.HasPrefix(item.RecordType, "reach.") && item.RecordType != "reach.message" || strings.HasPrefix(item.RecordType, "directory.") && item.RecordType != "directory.import-batch" || item.RecordType == "patterns.template" || item.RecordType == "bridge.oauth-client" {
			wantVersion = 2
		}
		if !item.BuiltIn || item.Version != wantVersion || item.Status != patterns.StatusActive {
			t.Fatalf("unexpected built-in metadata: %#v", item)
		}
		records[item.RecordType] = true
		for _, field := range item.Fields {
			types[field.Type] = true
			if field.AccessibleLabel == "" || field.CSVHeader == "" {
				t.Fatalf("field metadata is incomplete: %#v", field)
			}
		}
	}
	for _, record := range wantRecords {
		if !records[record] {
			t.Errorf("missing core template for %s", record)
		}
		if record != "atlas.label-template" {
			id, version, ok := patterns.BuiltInTemplateReference(record)
			wantVersion := int64(1)
			if strings.HasPrefix(record, "atlas.catalog-") || record == "horizon.plan" ||
				record == "atlas.asset" || record == "atlas.model" || record == "atlas.identifier" || record == "atlas.lifecycle-event" ||
				strings.HasPrefix(record, "people.") || strings.HasPrefix(record, "threads.") || strings.HasPrefix(record, "ledger.") || strings.HasPrefix(record, "signals.") && record != "signals.alert" || strings.HasPrefix(record, "reach.") && record != "reach.message" || strings.HasPrefix(record, "directory.") && record != "directory.import-batch" || record == "patterns.template" || record == "bridge.oauth-client" {
				wantVersion = 2
			}
			if !ok || id == "" || version != wantVersion {
				t.Errorf("missing stable built-in reference for %s: %q v%d", record, id, version)
			}
		}
	}
	if len(records) != len(wantRecords) || len(items) != len(wantRecords)+1 { // atlas.label-template has two immutable layouts.
		t.Errorf("authoritative core list drifted from built-ins: templates=%d records=%d core=%d", len(items), len(records), len(wantRecords))
	}
	// Historical immutable schemas remain part of the supported field-type
	// contract even when the lossless active version uses portable ID arrays.
	for _, item := range patterns.BuiltInTemplates() {
		for _, field := range item.Fields {
			types[field.Type] = true
		}
	}
	for _, fieldType := range []patterns.FieldType{patterns.FieldText, patterns.FieldNumber, patterns.FieldDate, patterns.FieldMoney, patterns.FieldEnum, patterns.FieldAttachment, patterns.FieldReference} {
		if !types[fieldType] {
			t.Errorf("missing field type %s", fieldType)
		}
	}
}

func TestExplicitExclusionsNeverBecomeBuiltInTemplates(t *testing.T) {
	builtIns := map[string]bool{}
	for _, template := range patterns.BuiltInTemplates() {
		builtIns[template.RecordType] = true
	}
	for _, excluded := range patterns.ExplicitlyExcludedRecordTypes() {
		if builtIns[excluded] {
			t.Errorf("excluded operational record %s became an importable built-in", excluded)
		}
	}
}

func TestBuiltInTemplateContractFingerprint(t *testing.T) {
	encoded, err := json.Marshal(patterns.BuiltInTemplates())
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(encoded))
	const want = "646e304be8e3301fb18931a501bc0a558ec54bc2f9baa8a00410c38ab3fa07eb"
	if got != want {
		t.Fatalf("built-in contract changed; review domain parity and intentionally update the fingerprint: got %s want %s", got, want)
	}
}

func TestHorizonPlanV2RequiresLosslessLifecycleStage(t *testing.T) {
	service := newPatternsService(t)
	legacy, err := service.GetTemplate(context.Background(), "builtin-horizon-plan", 1)
	if err != nil || legacy.Version != 1 {
		t.Fatalf("historical Horizon schema is not resolvable: %#v err=%v", legacy, err)
	}
	latest, err := service.ActiveTemplateForRecordType(context.Background(), "horizon.plan")
	if err != nil || latest.ID != "builtin-horizon-plan" || latest.Version != 2 {
		t.Fatalf("unexpected active Horizon schema %#v err=%v", latest, err)
	}
	values := map[string]any{
		"assetId": "asset-portable", "scenario": "baseline", "expectedUsefulLifeMonths": int64(60),
		"replacementDate": "2030-06-30", "lifecycleStage": "approved", "replacementCostMinor": int64(450_000),
		"currency": "USD", "effectiveFrom": "2027-01-01",
	}
	valid, err := service.Validate(context.Background(), latest.ID, latest.Version, patterns.ValidationInput{Values: values})
	if err != nil || valid.Status != patterns.ValidationValid || valid.NormalizedValues["lifecycleStage"] != "approved" {
		t.Fatalf("Horizon v2 did not preserve lifecycle stage: %#v err=%v", valid, err)
	}
	delete(values, "lifecycleStage")
	invalid, err := service.Validate(context.Background(), latest.ID, latest.Version, patterns.ValidationInput{Values: values})
	if err != nil || invalid.Status != patterns.ValidationInvalid {
		t.Fatalf("Horizon v2 accepted a lossy plan payload: %#v err=%v", invalid, err)
	}
}

func TestSignalsV2SchemasPreservePortableStateAndHistoricalV1(t *testing.T) {
	service := newPatternsService(t)
	for _, recordType := range []string{"signals.rule", "signals.subscription"} {
		latest, err := service.ActiveTemplateForRecordType(context.Background(), recordType)
		if err != nil || latest.Version != 2 {
			t.Fatalf("unexpected active Signals schema for %s: %#v err=%v", recordType, latest, err)
		}
		legacy, err := service.GetTemplate(context.Background(), latest.ID, 1)
		if err != nil || legacy.Version != 1 {
			t.Fatalf("historical Signals schema for %s is unavailable: %#v err=%v", recordType, legacy, err)
		}
	}
	rule, _ := service.ActiveTemplateForRecordType(context.Background(), "signals.rule")
	valid, err := service.Validate(context.Background(), rule.ID, rule.Version, patterns.ValidationInput{Values: map[string]any{
		"name": "Renewals", "condition": "renewal", "severity": "warning", "enabled": "false", "thresholdDays": "[365,90,30]",
		"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-14T12:00:00Z",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("Signals rule v2 rejected lossless fields: %#v err=%v", valid, err)
	}
	if _, exists := valid.NormalizedValues["createdBy"]; exists {
		t.Fatal("Signals schema exposed operator identity")
	}
}

func TestBridgeOAuthClientV2PreservesOnlyPublicConfiguration(t *testing.T) {
	service := newPatternsService(t)
	legacy, err := service.GetTemplate(context.Background(), "builtin-bridge-oauth-client", 1)
	if err != nil || legacy.Version != 1 {
		t.Fatalf("historical Bridge OAuth client schema is not resolvable: %#v err=%v", legacy, err)
	}
	latest, err := service.ActiveTemplateForRecordType(context.Background(), "bridge.oauth-client")
	if err != nil || latest.ID != "builtin-bridge-oauth-client" || latest.Version != 2 {
		t.Fatalf("unexpected active Bridge OAuth client schema %#v err=%v", latest, err)
	}
	valid, err := service.Validate(context.Background(), latest.ID, latest.Version, patterns.ValidationInput{Values: map[string]any{
		"name": "Portable client", "redirectUris": "http://127.0.0.1:8181/callback\nhttps://client.example.test/callback",
		"allowedScopes": "assets:read,mcp:resources", "revokedAt": "2026-08-13T18:00:00Z",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("Bridge OAuth client v2 rejected public portable fields: %#v err=%v", valid, err)
	}
	invalid, err := service.Validate(context.Background(), latest.ID, latest.Version, patterns.ValidationInput{Values: map[string]any{
		"name": "Portable client", "redirectUris": "https://client.example.test/callback", "allowedScopes": "mcp:resources", "clientSecret": "forbidden",
	}})
	if err != nil || invalid.Status != patterns.ValidationInvalid {
		t.Fatalf("Bridge OAuth client v2 accepted a secret field: %#v err=%v", invalid, err)
	}
}

func TestPeopleV2SchemasPreservePortableStateAndHistory(t *testing.T) {
	service := newPatternsService(t)
	for _, recordType := range []string{"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment"} {
		latest, err := service.ActiveTemplateForRecordType(context.Background(), recordType)
		if err != nil || latest.Version != 2 {
			t.Fatalf("unexpected active People schema for %s: %#v err=%v", recordType, latest, err)
		}
		legacy, err := service.GetTemplate(context.Background(), latest.ID, 1)
		if err != nil || legacy.Version != 1 {
			t.Fatalf("historical People schema for %s is not resolvable: %#v err=%v", recordType, legacy, err)
		}
	}
	identity, err := service.ActiveTemplateForRecordType(context.Background(), "people.identity")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.Validate(context.Background(), identity.ID, identity.Version, patterns.ValidationInput{Values: map[string]any{
		"kind": "shared", "displayName": "Lab operators", "email": "lab@example.test", "departmentId": "department-one",
		"siteId": "site-one", "status": "active", "provider": "directory.example", "providerSubject": "subject-one",
		"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-14T12:00:00Z",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("People identity v2 rejected lossless fields: %#v err=%v", valid, err)
	}
	assignment, err := service.ActiveTemplateForRecordType(context.Background(), "people.assignment")
	if err != nil {
		t.Fatal(err)
	}
	valid, err = service.Validate(context.Background(), assignment.ID, assignment.Version, patterns.ValidationInput{Values: map[string]any{
		"assetId": "asset-one", "assigneeKind": "identity", "assigneeId": "identity-one", "role": "user",
		"effectiveFrom": "2026-08-13T12:00:00Z", "effectiveTo": "2026-08-14T12:00:00Z", "createdAt": "2026-08-13T12:00:00Z",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("People assignment v2 rejected ended history: %#v err=%v", valid, err)
	}
}

func TestThreadsV2SchemasPreservePortableHierarchyAndRelationships(t *testing.T) {
	service := newPatternsService(t)
	for _, recordType := range []string{"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link"} {
		latest, err := service.ActiveTemplateForRecordType(context.Background(), recordType)
		if err != nil || latest.Version != 2 {
			t.Fatalf("unexpected active Threads schema for %s: %#v err=%v", recordType, latest, err)
		}
		legacy, err := service.GetTemplate(context.Background(), latest.ID, 1)
		if err != nil || legacy.Version != 1 {
			t.Fatalf("historical Threads schema for %s is not resolvable: %#v err=%v", recordType, legacy, err)
		}
	}
	rule, err := service.ActiveTemplateForRecordType(context.Background(), "threads.tag-rule")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.Validate(context.Background(), rule.ID, rule.Version, patterns.ValidationInput{Values: map[string]any{
		"targetType": "software", "targetId": "installation-one", "tagId": "security", "rule": "suppress",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("Threads tag-rule v2 rejected portable fields: %#v err=%v", valid, err)
	}
	valid, err = service.Validate(context.Background(), "builtin-threads-goal-link", 2, patterns.ValidationInput{Values: map[string]any{
		"targetType": "purchase", "targetId": "purchase-one", "goalId": "reduce-risk",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("Threads goal-link v2 rejected portable fields: %#v err=%v", valid, err)
	}
}

func TestDirectoryV2SchemasPreserveNormalizedStateAndConditionalMembershipReferences(t *testing.T) {
	service := newPatternsService(t)
	for _, recordType := range []string{"directory.group", "directory.membership"} {
		latest, err := service.ActiveTemplateForRecordType(context.Background(), recordType)
		if err != nil || latest.Version != 2 {
			t.Fatalf("unexpected active Directory schema for %s: %#v err=%v", recordType, latest, err)
		}
		legacy, err := service.GetTemplate(context.Background(), latest.ID, 1)
		if err != nil || legacy.Version != 1 {
			t.Fatalf("historical Directory schema for %s is unavailable: %#v err=%v", recordType, legacy, err)
		}
	}
	membership, err := service.ActiveTemplateForRecordType(context.Background(), "directory.membership")
	if err != nil {
		t.Fatal(err)
	}
	memberID := templateField(t, membership, "memberId")
	if memberID.Type != patterns.FieldText || memberID.ReferenceType != "" {
		t.Fatalf("embedded subject memberId was made an unconditional external reference: %#v", memberID)
	}
	valid, err := service.Validate(context.Background(), membership.ID, membership.Version, patterns.ValidationInput{Values: map[string]any{
		"sourceSystemId": "directory-source", "sourceRecordId": "membership-one", "groupId": "11111111111111111111111111111111",
		"groupSourceId": "group-one", "memberId": "22222222222222222222222222222222", "memberSourceId": "subject-one",
		"memberKind": "subject", "memberDisplayName": "Embedded subject", "status": "active", "metadata": "{}",
		"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-14T12:00:00Z",
	}})
	if err != nil || valid.Status != patterns.ValidationValid {
		t.Fatalf("Directory membership v2 rejected lossless embedded-subject fields: %#v err=%v", valid, err)
	}
}

func TestPhaseOneExpansionBuiltInsMatchDurablePortableShapes(t *testing.T) {
	templates := map[string]patterns.Template{}
	for _, template := range patterns.BuiltInTemplates() {
		current, exists := templates[template.RecordType]
		if !exists || template.Version > current.Version {
			templates[template.RecordType] = template
		}
	}
	tests := []struct {
		recordType string
		keys       []string
		forbidden  []string
	}{
		{"stack.product", []string{"name", "publisher", "status"}, []string{"organizationId", "createdBy"}},
		{"stack.version", []string{"productId", "name", "releasedOn", "status"}, []string{"organizationId"}},
		{"stack.installation", []string{"versionId", "assetId", "installedAt", "usageState"}, []string{"organizationId"}},
		{"stack.license", []string{"productId", "entitlementMetric", "quantity", "documentIds"}, []string{"organizationId"}},
		{"stack.assignment", []string{"licenseId", "assigneeKind", "assigneeId", "seats"}, []string{"organizationId"}},
		{"signals.rule", []string{"name", "condition", "severity", "enabled", "thresholdDays"}, []string{"createdBy"}},
		{"signals.alert", []string{"ruleId", "condition", "severity", "status", "targetId"}, []string{"deduplicationKey", "acknowledgedBy"}},
		{"signals.subscription", []string{"ruleId", "targetKind", "targetId", "enabled"}, []string{"createdBy"}},
		{"reach.provider", []string{"name", "kind", "createdAt", "updatedAt"}, []string{"endpointId", "secretRef", "enabled", "createdBy"}},
		{"reach.template", []string{"name", "subject", "body"}, []string{"createdBy"}},
		{"reach.subscriber-group", []string{"name", "providerId", "templateId", "recipients"}, []string{"createdBy"}},
		{"reach.message", []string{"providerId", "sourceKind", "subject", "body", "recipients", "status"}, []string{"lastErrorCode", "createdBy"}},
		{"directory.import-batch", []string{"sourceSystemId", "provider", "configRevision", "completeSnapshot", "createdCount", "failedCount"}, []string{"leaseToken", "organizationId"}},
		{"directory.group", []string{"sourceSystemId", "sourceRecordId", "name", "displayName", "status", "metadata", "createdAt", "updatedAt"}, []string{"organizationId", "revision"}},
		{"directory.membership", []string{"groupId", "groupSourceId", "memberId", "memberSourceId", "memberKind", "memberDisplayName", "status", "metadata", "createdAt", "updatedAt"}, []string{"identityId", "organizationId", "revision"}},
		{"exchange.package", []string{"packageId", "direction", "schemaVersion", "sourceSystemId", "recordCount"}, []string{"archive", "createdBy"}},
		{"bridge.oauth-client", []string{"name", "redirectUris", "allowedScopes", "revokedAt"}, []string{"clientSecret", "organizationId"}},
		{"bridge.oauth-grant", []string{"clientId", "actorId", "resourceUri", "scopes", "accessExpiresAt", "refreshExpiresAt"}, []string{"accessTokenHash", "refreshTokenHash"}},
	}
	for _, test := range tests {
		t.Run(test.recordType, func(t *testing.T) {
			template, ok := templates[test.recordType]
			if !ok {
				t.Fatalf("missing template")
			}
			fields := map[string]patterns.Field{}
			for _, field := range template.Fields {
				fields[field.Key] = field
			}
			for _, key := range test.keys {
				if _, ok := fields[key]; !ok {
					t.Errorf("portable field %q is missing", key)
				}
			}
			for _, key := range test.forbidden {
				if _, ok := fields[key]; ok {
					t.Errorf("private or incompatible field %q must not be portable", key)
				}
			}
		})
	}

	directoryProvider := templateField(t, templates["directory.import-batch"], "provider")
	if directoryProvider.Type != patterns.FieldText || len(directoryProvider.Options) != 0 {
		t.Fatalf("open-ended connector provider was narrowed to an enum: %#v", directoryProvider)
	}
	membershipKind := templateField(t, templates["directory.membership"], "memberKind")
	if !equalStrings(membershipKind.Options, []string{"subject", "group"}) {
		t.Fatalf("nested group membership is not representable: %#v", membershipKind)
	}
	messageStatus := templateField(t, templates["reach.message"], "status")
	if !equalStrings(messageStatus.Options, []string{"queued", "retrying", "delivered", "failed"}) {
		t.Fatalf("Reach message states drifted: %#v", messageStatus)
	}
	targetKind := templateField(t, templates["signals.subscription"], "targetKind")
	if !equalStrings(targetKind.Options, []string{"group", "webhook"}) {
		t.Fatalf("Signals target kinds drifted: %#v", targetKind)
	}
}

func TestBuiltInFieldBoundsMatchOwningDomainLimits(t *testing.T) {
	service := newPatternsService(t)
	templates := map[string]patterns.Template{}
	for _, template := range patterns.BuiltInTemplates() {
		templates[template.ID] = template
	}
	textCases := []struct {
		templateID string
		field      string
		maximum    int
		values     map[string]any
	}{
		{"builtin-vault-blob", "name", 255, map[string]any{"name": "x", "mediaType": "text/plain", "sizeBytes": 1, "sha256": strings.Repeat("a", 64), "provider": "local"}},
		{"builtin-stack-version", "name", 100, map[string]any{"productId": "product-1", "name": "x", "status": "active"}},
		{"builtin-stack-license", "documentIds", 12_899, map[string]any{"productId": "product-1", "name": "License", "entitlementMetric": "device", "quantity": 1, "status": "active", "documentIds": "x"}},
		{"builtin-reach-template", "body", 4_000, map[string]any{"name": "Template", "subject": "Subject", "body": "x"}},
		{"builtin-directory-group", "name", 512, map[string]any{"sourceSystemId": "source", "sourceRecordId": "row", "name": "x", "displayName": "Group", "status": "active", "revision": 1}},
		{"builtin-threads-goal", "description", 2_000, map[string]any{"name": "Goal", "description": "x"}},
		{"builtin-ledger-commitment", "description", 500, map[string]any{"contractId": "contract-1", "kind": "subscription", "description": "x", "currency": "USD", "amountMinor": 1, "startsOn": "2026-01-01", "endsOn": "2026-12-31"}},
		{"builtin-ledger-cost", "description", 500, map[string]any{"description": "x", "kind": "actual", "currency": "USD", "amountMinor": 1}},
		{"builtin-signals-alert", "summary", 500, map[string]any{"ruleId": "rule-1", "condition": "overdue", "severity": "warning", "status": "active", "title": "Alert", "summary": "x", "targetType": "ledger.cost", "targetId": "cost-1"}},
		{"builtin-guard-role", "description", 1_000, map[string]any{"name": "Role", "description": "x", "permissions": "assets.read"}},
	}
	for _, test := range textCases {
		t.Run(test.templateID+"/"+test.field, func(t *testing.T) {
			field := templateField(t, templates[test.templateID], test.field)
			if field.MaximumLength != test.maximum {
				t.Fatalf("domain maximum drifted: got %d want %d", field.MaximumLength, test.maximum)
			}
			test.values[test.field] = strings.Repeat("x", test.maximum)
			valid, err := service.Validate(context.Background(), test.templateID, 1, patterns.ValidationInput{Values: test.values})
			if err != nil || valid.Status != patterns.ValidationValid {
				t.Fatalf("domain maximum was rejected: %#v err=%v", valid, err)
			}
			test.values[test.field] = strings.Repeat("x", test.maximum+1)
			invalid, err := service.Validate(context.Background(), test.templateID, 1, patterns.ValidationInput{Values: test.values})
			if err != nil || invalid.Status != patterns.ValidationInvalid {
				t.Fatalf("domain maximum + 1 was accepted: %#v err=%v", invalid, err)
			}
		})
	}
	for _, test := range []struct{ templateID, field string }{{"builtin-stack-license", "quantity"}, {"builtin-stack-assignment", "seats"}} {
		field := templateField(t, templates[test.templateID], test.field)
		if field.Minimum == nil || *field.Minimum != 1 || field.Maximum == nil || *field.Maximum != 1_000_000_000 {
			t.Errorf("%s %s numeric domain drifted: %#v", test.templateID, test.field, field)
		}
	}
}

func templateField(t *testing.T, template patterns.Template, key string) patterns.Field {
	t.Helper()
	for _, field := range template.Fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("template %s is missing field %s", template.ID, key)
	return patterns.Field{}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestCustomTemplatesAreCopyableAndAppendOnlyVersioned(t *testing.T) {
	service := newPatternsService(t)
	created, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "custom-intake", RecordType: "atlas.asset", Name: "Custom intake",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Help: "Shown on inventory pages.", Type: patterns.FieldText, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.BuiltIn || created.Fields[0].AccessibleLabel != "Name" || created.Fields[0].CSVHeader != "name" {
		t.Fatalf("unexpected custom template: %#v", created)
	}
	copy, err := service.CopyTemplate(context.Background(), "builtin-atlas-asset", 1, patterns.CopyTemplateInput{ID: "asset-copy", Name: "Asset copy"})
	if err != nil {
		t.Fatal(err)
	}
	if copy.BuiltIn || copy.RecordType != "atlas.asset" || len(copy.Fields) < 2 {
		t.Fatalf("unexpected built-in copy: %#v", copy)
	}
	versionTwo, err := service.CreateVersion(context.Background(), created.ID, patterns.NewVersionInput{
		Description: "Second immutable version.",
		Fields: []patterns.Field{
			{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
			{Key: "commissionedOn", Label: "Commissioned on", Type: patterns.FieldDate},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	versionOne, err := service.GetTemplate(context.Background(), created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := service.GetTemplate(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if versionTwo.Version != 2 || latest.Version != 2 || len(versionOne.Fields) != 1 || len(latest.Fields) != 2 {
		t.Fatalf("versions were not preserved: v1=%#v v2=%#v latest=%#v", versionOne, versionTwo, latest)
	}
	current, err := service.ListTemplates(context.Background(), patterns.ListQuery{RecordType: "atlas.asset"})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.ListTemplates(context.Background(), patterns.ListQuery{RecordType: "atlas.asset", IncludeVersions: true})
	if err != nil {
		t.Fatal(err)
	}
	customCurrent, customHistory := 0, 0
	for _, item := range current {
		if item.ID == created.ID {
			customCurrent++
		}
	}
	for _, item := range history {
		if item.ID == created.ID {
			customHistory++
		}
	}
	if customCurrent != 1 || customHistory != 2 {
		t.Fatalf("exact version discovery drifted: current=%d history=%d", customCurrent, customHistory)
	}
}

func TestValidationSupportsTypedValuesAndVisibleHoldingRecords(t *testing.T) {
	service := newPatternsService(t)
	minimum, maximum := 1.0, 10.0
	template, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "typed-intake", RecordType: "exchange.row", Name: "Typed intake",
		Fields: []patterns.Field{
			{Key: "title", Label: "Title", Help: "A useful name.", Type: patterns.FieldText, Required: true, MaximumLength: 20},
			{Key: "quantity", Label: "Quantity", Type: patterns.FieldNumber, Minimum: &minimum, Maximum: &maximum},
			{Key: "dueOn", Label: "Due on", Type: patterns.FieldDate, Required: true},
			{Key: "currency", Label: "Currency", Type: patterns.FieldText, Required: true, MaximumLength: 3},
			{Key: "budgetMinor", Label: "Budget", Type: patterns.FieldMoney, Required: true, CurrencyField: "currency"},
			{Key: "state", Label: "State", Type: patterns.FieldEnum, Required: true, Options: []string{"new", "ready"}},
			{Key: "evidence", Label: "Evidence", Type: patterns.FieldAttachment, AllowHolding: true, ReferenceType: "vault.blob"},
			{Key: "owner", Label: "Owner", Type: patterns.FieldReference, Required: true, AllowHolding: true, ReferenceType: "people.identity"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"title": "  Row one  ", "quantity": 3.5, "dueOn": "2026-08-12", "currency": "USD", "budgetMinor": int64(1250), "state": "ready", "evidence": "blob-1", "owner": "person-1"}
	valid, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Status != patterns.ValidationValid || len(valid.Errors) != 0 || valid.NormalizedValues["title"] != "Row one" || valid.NormalizedValues["budgetMinor"] != int64(1250) {
		t.Fatalf("unexpected valid result: %#v", valid)
	}
	for _, unsafe := range []any{json.Number("9007199254740990.5"), json.Number("1.0000000000000001"), json.Number("0.99999999999999999"), json.Number("9007199254740992"), float64(1250)} {
		candidate := make(map[string]any, len(values))
		for key, value := range values {
			candidate[key] = value
		}
		candidate["budgetMinor"] = unsafe
		result, validationErr := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: candidate})
		if validationErr != nil || result.Status != patterns.ValidationInvalid {
			t.Fatalf("unsafe exact-money token %q was accepted: %#v err=%v", unsafe, result, validationErr)
		}
	}
	holding, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values, MissingReferences: []string{"owner"}, AllowHoldingRecord: true})
	if err != nil {
		t.Fatal(err)
	}
	if holding.Status != patterns.ValidationHolding || len(holding.HoldingReferences) != 1 || holding.HoldingReferences[0].Field != "owner" {
		t.Fatalf("missing reference was not held visibly: %#v", holding)
	}
	attachmentHolding, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values, MissingReferences: []string{"evidence"}, AllowHoldingRecord: true})
	if err != nil {
		t.Fatal(err)
	}
	if attachmentHolding.Status != patterns.ValidationHolding || len(attachmentHolding.HoldingReferences) != 1 || attachmentHolding.HoldingReferences[0].Field != "evidence" {
		t.Fatalf("missing attachment was not held visibly: %#v", attachmentHolding)
	}
	blankOptional := make(map[string]any, len(values))
	for key, value := range values {
		blankOptional[key] = value
	}
	delete(blankOptional, "evidence")
	blankHolding, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: blankOptional, MissingReferences: []string{"evidence"}, AllowHoldingRecord: true})
	if err != nil || blankHolding.Status != patterns.ValidationInvalid || len(blankHolding.HoldingReferences) != 0 {
		t.Fatalf("blank optional holding marker was silently ignored: %#v err=%v", blankHolding, err)
	}
	invalidValues := map[string]any{"title": "A title", "quantity": 11.0, "dueOn": "12/08/2026", "currency": "usd", "budgetMinor": 1.5, "state": "unknown", "evidence": "not/a/stable/id", "owner": "person-1", "surprise": true}
	invalid, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: invalidValues, MissingReferences: []string{"owner"}})
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Status != patterns.ValidationInvalid || len(invalid.Errors) < 7 || len(invalid.HoldingReferences) != 0 {
		t.Fatalf("typed errors were not surfaced: %#v", invalid)
	}
}

func TestTemplateMetadataRejectsSpreadsheetFormulaHeadersAndUnknownMissingKeys(t *testing.T) {
	service := newPatternsService(t)
	if _, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		RecordType: "exchange.row", Name: "Unsafe CSV",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Type: patterns.FieldText, CSVHeader: "=HYPERLINK(example)"}},
	}); !errors.Is(err, patterns.ErrInvalidInput) {
		t.Fatalf("expected formula-like CSV header rejection, got %v", err)
	}
	if _, err := service.Validate(context.Background(), "builtin-atlas-asset", 1, patterns.ValidationInput{
		Values: map[string]any{"name": "Asset", "kind": "server"}, MissingReferences: []string{"unknown"},
	}); !errors.Is(err, patterns.ErrInvalidInput) {
		t.Fatalf("expected unknown missing-reference key rejection, got %v", err)
	}
}

func TestCSVTemplateUsesVersionedHeaders(t *testing.T) {
	service := newPatternsService(t)
	contents, err := service.CSVTemplate(context.Background(), "builtin-atlas-asset", 1)
	if err != nil {
		t.Fatal(err)
	}
	line := string(contents)
	if !strings.HasPrefix(line, "name,kind,assetTag") || !strings.HasSuffix(line, "\n") {
		t.Fatalf("unexpected CSV template %q", line)
	}
}

func newPatternsService(t *testing.T) *patterns.Service {
	t.Helper()
	service, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{
		OrganizationID: "example-org",
		Now:            func() time.Time { return time.Date(2026, time.August, 12, 9, 30, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
