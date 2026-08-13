package patterns

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

// BuiltInTemplates returns immutable v1 templates for every record family
// currently exposed by StewardMesh. These definitions are code-versioned and
// can be consumed by forms, API metadata, CSV import/export, and Exchange.
func BuiltInTemplates() []Template {
	t := func(id, recordType, name, description string, fields ...Field) Template {
		return Template{ID: id, RecordType: recordType, Name: name, Description: description, Version: 1,
			BuiltIn: true, Status: StatusActive, Fields: fields, CreatedBy: "system:patterns"}
	}
	text := func(key, label string, required bool) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldText, Required: required, MaximumLength: 200}
	}
	enum := func(key, label string, required bool, values ...string) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldEnum, Required: required, Options: values}
	}
	ref := func(key, label, target string, required, hold bool) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldReference, Required: required, AllowHolding: hold, ReferenceType: target}
	}
	date := func(key, label string, required bool) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldDate, Required: required}
	}
	money := func(key, label string, required bool) Field {
		zero := 0.0
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldMoney, Required: required, Minimum: &zero, CurrencyField: "currency"}
	}
	number := func(key, label string, required bool) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldNumber, Required: required}
	}
	attachment := func(key, label string) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldAttachment, ReferenceType: "vault.blob", AllowHolding: true}
	}
	return []Template{
		t("builtin-foundation-organization", "foundation.organization", "Foundation organization", "Describe the organization boundary.", text("name", "Organization name", true)),
		t("builtin-atlas-asset", "atlas.asset", "Atlas asset", "Register an organization-owned asset.", text("name", "Asset name", true), enum("kind", "Asset kind", true, "server", "desktop", "laptop", "network", "storage", "mobile", "printer", "virtual", "other"), text("assetTag", "Asset tag", false), text("serialNumber", "Serial number", false), ref("modelId", "Model", "atlas.model", false, true), ref("siteId", "Site", "people.site", false, true), ref("departmentId", "Department", "people.department", false, true), date("purchaseDate", "Purchase date", false)),
		t("builtin-atlas-model", "atlas.model", "Atlas model", "Define reusable product defaults.", text("manufacturer", "Manufacturer", true), text("name", "Model name", true), text("modelNumber", "Model number", false), enum("kind", "Asset kind", true, "server", "desktop", "laptop", "network", "storage", "mobile", "printer", "virtual", "other"), text("supportUrl", "Support URL", false), number("warrantyMonths", "Warranty months", false)),
		t("builtin-atlas-identifier", "atlas.identifier", "Atlas identifier", "Associate a Code 128 or QR identifier.", ref("assetId", "Asset", "atlas.asset", true, false), enum("symbology", "Code format", true, "code128", "qr"), text("value", "Encoded value", true), text("displayValue", "Human-readable value", false)),
		t("builtin-atlas-lifecycle-event", "atlas.lifecycle-event", "Atlas lifecycle event", "Record an immutable asset lifecycle event.", ref("assetId", "Asset", "atlas.asset", true, false), enum("kind", "Event kind", true, "registered", "assigned", "moved", "maintained", "retired", "disposed"), date("effectiveOn", "Effective date", true), text("note", "Event note", false)),
		t("builtin-atlas-catalog-configuration", "atlas.catalog-configuration", "Atlas catalog configuration", "Define a purchasable model configuration.", ref("modelId", "Model", "atlas.model", true, true), text("name", "Configuration name", true), text("sku", "SKU", false), text("specifications", "Specifications", false)),
		t("builtin-atlas-catalog-price", "atlas.catalog-price", "Atlas catalog price", "Record an effective-dated catalog price.", ref("configurationId", "Configuration", "atlas.catalog-configuration", true, true), enum("kind", "Price kind", true, "list", "street", "contract"), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), date("effectiveFrom", "Effective from", true), date("effectiveTo", "Effective to", false)),
		t("builtin-atlas-catalog-upgrade-path", "atlas.catalog-upgrade-path", "Atlas catalog upgrade path", "Relate a configuration to its successor, replacement, or upgrade.", ref("fromConfigurationId", "From configuration", "atlas.catalog-configuration", true, true), ref("toConfigurationId", "To configuration", "atlas.catalog-configuration", true, true), enum("relationshipKind", "Relationship kind", true, "successor", "replacement", "upgrade")),
		t("builtin-people-site", "people.site", "People site", "Create a directory site.", text("name", "Site name", true), text("city", "City", false), text("country", "Country", false)),
		t("builtin-people-building", "people.building", "People building", "Create a building beneath a site.", ref("siteId", "Site", "people.site", true, true), text("name", "Building name", true)),
		t("builtin-people-room", "people.room", "People room", "Create a room beneath a building.", ref("siteId", "Site", "people.site", true, true), ref("buildingId", "Building", "people.building", true, true), text("number", "Room number", true), text("name", "Room name", false)),
		t("builtin-people-department", "people.department", "People department", "Create a department.", text("name", "Department name", true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-people-identity", "people.identity", "People identity", "Create a person or shared identity.", enum("kind", "Identity kind", true, "person", "shared", "public", "lab"), text("displayName", "Display name", true), text("email", "Email address", false), ref("departmentId", "Department", "people.department", false, true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-people-assignment", "people.assignment", "Asset assignment", "Assign an identity or department to an asset.", ref("assetId", "Asset", "atlas.asset", true, true), enum("assigneeKind", "Assignee type", true, "identity", "department"), ref("assigneeId", "Assignee", "people.identity-or-department", true, true), enum("role", "Assignment role", true, "primary", "user", "department"), date("effectiveFrom", "Effective from", true)),
		t("builtin-threads-tag", "threads.tag", "Threads tag", "Create a hierarchical tag.", text("name", "Tag name", true), ref("parentId", "Parent tag", "threads.tag", false, true), enum("inheritance", "Default inheritance", true, "include", "suppress")),
		t("builtin-threads-goal", "threads.goal", "Threads goal", "Create a strategic goal.", text("name", "Goal name", true), text("description", "Description", false), ref("parentId", "Parent goal", "threads.goal", false, true)),
		t("builtin-threads-tag-rule", "threads.tag-rule", "Threads tag rule", "Explicitly include or suppress a tag on a record.", text("targetType", "Target record type", true), ref("targetId", "Target record", "stewardmesh.record", true, true), ref("tagId", "Tag", "threads.tag", true, true), enum("rule", "Rule", true, "include", "suppress")),
		t("builtin-threads-goal-link", "threads.goal-link", "Threads goal link", "Link a record to a strategic goal.", text("targetType", "Target record type", true), ref("targetId", "Target record", "stewardmesh.record", true, true), ref("goalId", "Goal", "threads.goal", true, true)),
		t("builtin-vault-blob", "vault.blob", "Vault evidence", "Describe a private attachment.", attachment("file", "File"), text("sourceSystemId", "Source system ID", false), text("sourceRecordId", "Source record ID", false), text("resourceType", "Related record type", false), ref("resourceId", "Related record", "stewardmesh.record", false, true)),
		t("builtin-ledger-vendor", "ledger.vendor", "Ledger vendor", "Create a vendor.", text("name", "Vendor name", true), text("externalId", "External ID", false)),
		t("builtin-ledger-purchase-order", "ledger.purchase-order", "Ledger purchase order", "Create a purchase order.", text("number", "Purchase order number", true), ref("vendorId", "Vendor", "ledger.vendor", true, true), text("currency", "Currency", true), money("totalMinor", "Total in minor units", true), date("orderedOn", "Ordered on", false), attachment("receiptDocumentId", "Receipt evidence")),
		t("builtin-ledger-contract", "ledger.contract", "Ledger contract", "Create a vendor contract.", text("name", "Contract name", true), ref("vendorId", "Vendor", "ledger.vendor", true, true), text("currency", "Currency", true), money("ceilingMinor", "Contract ceiling in minor units", true), date("startsOn", "Starts on", true), date("endsOn", "Ends on", true), attachment("documentId", "Contract evidence")),
		t("builtin-ledger-commitment", "ledger.commitment", "Ledger commitment", "Record a financial commitment.", ref("contractId", "Contract", "ledger.contract", true, true), enum("kind", "Commitment type", true, "subscription", "lease", "maintenance", "license", "financing", "other"), text("description", "Description", true), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), date("startsOn", "Starts on", true), date("endsOn", "Ends on", true)),
		t("builtin-ledger-budget", "ledger.budget", "Ledger budget", "Create a fiscal budget.", text("name", "Budget name", true), text("fiscalPeriod", "Fiscal period", true), text("scenario", "Scenario", true), text("currency", "Currency", true), money("allocatedMinor", "Allocation in minor units", true), ref("departmentId", "Department", "people.department", false, true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-ledger-cost", "ledger.cost", "Ledger cost", "Reconcile a current cost.", text("description", "Description", true), enum("kind", "Cost state", true, "planned", "estimated", "actual", "billed", "paid", "committed", "normalized_real", "tco"), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), text("sourceSystemId", "Source system ID", false), text("sourceRecordId", "Source record ID", false)),
		t("builtin-horizon-plan", "horizon.plan", "Horizon lifecycle plan", "Create an effective-dated lifecycle plan.", ref("assetId", "Asset", "atlas.asset", true, true), text("scenario", "Scenario", true), number("expectedUsefulLifeMonths", "Expected useful life in months", true), date("replacementDate", "Replacement date", false), text("currency", "Currency", true), money("replacementCostMinor", "Replacement cost in minor units", true), date("effectiveFrom", "Effective from", true)),
		t("builtin-guard-role", "guard.role", "Guard custom role", "Create a custom authorization role.", text("name", "Role name", true), text("description", "Role description", false), text("permissions", "Permission identifiers", true)),
		t("builtin-guard-account", "guard.account", "Guard account", "Describe a local or provider-managed account.", text("username", "Username", true), text("email", "Email address", true), text("displayName", "Display name", true), enum("status", "Account status", true, "active", "disabled")),
		t("builtin-guard-policy-bundle", "guard.policy-bundle", "Guard policy bundle", "Describe a reusable permission bundle.", text("name", "Bundle name", true), text("description", "Bundle description", false), text("permissions", "Permission identifiers", true)),
		t("builtin-guard-role-assignment", "guard.role-assignment", "Guard role assignment", "Assign a role at an explicit scope.", ref("accountId", "Account", "guard.account", true, true), ref("roleId", "Role", "guard.role", true, true), enum("scopeKind", "Scope kind", true, "organization", "site", "department", "resource"), text("scopeResourceId", "Scope resource ID", false)),
		t("builtin-guard-resource-ownership", "guard.resource-ownership", "Guard resource ownership", "Register authoritative ownership for a resource.", text("resourceType", "Resource type", true), ref("resourceId", "Resource", "stewardmesh.record", true, true), ref("ownerAccountId", "Owner account", "guard.account", true, true)),
		t("builtin-patterns-template", "patterns.template", "Patterns template", "Describe an organization-scoped custom schema version.", text("recordType", "Record type", true), text("name", "Template name", true), text("description", "Description", false), number("version", "Version", true), text("fields", "Field definitions", true)),
	}
}
