package patterns

// Requirements: REQ-PATTERNS-001, REQ-ATLAS-CODES-001. Features: templates.schemas, inventory.identifiers. GitHub: #8, #62.

// CoreRecordTypes is the authoritative phase-one set of durable,
// user-visible record families. BuiltInTemplates must expose at least one
// active immutable schema for every entry. Keep this list explicit: forms,
// CSV tooling, and Exchange use the same contract boundary.
func CoreRecordTypes() []string {
	return []string{
		"foundation.organization",
		"atlas.asset", "atlas.model", "atlas.identifier", "atlas.label-template", "atlas.lifecycle-event",
		"atlas.catalog-configuration", "atlas.catalog-price", "atlas.catalog-upgrade-path",
		"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment",
		"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link",
		"vault.blob",
		"ledger.vendor", "ledger.purchase-order", "ledger.contract", "ledger.commitment", "ledger.budget", "ledger.cost",
		"horizon.plan",
		"guard.role", "guard.account", "guard.policy-bundle", "guard.role-assignment", "guard.resource-ownership",
		"patterns.template",
		"stack.product", "stack.version", "stack.installation", "stack.license", "stack.assignment",
		"signals.rule", "signals.alert", "signals.subscription",
		"reach.provider", "reach.template", "reach.subscriber-group", "reach.message",
		"directory.import-batch", "directory.group", "directory.membership",
		"exchange.package",
		"bridge.oauth-client", "bridge.oauth-grant",
	}
}

// ExplicitlyExcludedRecordTypes documents phase-one data that must not become
// editable/importable Patterns rows. These values are derived, operational,
// security-sensitive, short-lived, or internal relationship machinery.
func ExplicitlyExcludedRecordTypes() []string {
	return []string{
		"atlas.graph-node", "atlas.graph-edge", "atlas.label-artifact-batch",
		"ledger.analytics", "horizon.forecast", "foundation.audit-event",
		"directory.import-item", "directory.import-attempt",
		"signals.alert-history", "signals.delivery",
		"reach.provider-test", "reach.delivery-attempt",
		"exchange.record-outcome",
		"bridge.authorization-request", "bridge.authorization-code", "bridge.rate-window", "bridge.confirmation",
	}
}

// BuiltInTemplateReference returns the stable schema identity pinned by
// Exchange for a core record type. Two label layouts share a record family and
// therefore require the caller to select their concrete template explicitly.
func BuiltInTemplateReference(recordType string) (string, int64, bool) {
	return builtInTemplateReference(BuiltInTemplates(), recordType)
}

func builtInTemplateReference(templates []Template, recordType string) (string, int64, bool) {
	if recordType == "atlas.label-template" {
		return "", 0, false
	}
	var selected Template
	for _, template := range templates {
		if template.RecordType != recordType || template.Status != StatusActive {
			continue
		}
		if selected.ID == "" || template.Version > selected.Version || template.Version == selected.Version && template.ID < selected.ID {
			selected = template
		}
	}
	return selected.ID, selected.Version, selected.ID != ""
}

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
	boundedText := func(key, label string, required bool, maximum int) Field {
		field := text(key, label, required)
		field.MaximumLength = maximum
		return field
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
	boundedNumber := func(key, label string, required bool, minimum, maximum float64) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldNumber, Required: required, Minimum: &minimum, Maximum: &maximum}
	}
	labelNumber := func(key, label string, value float64) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldNumber, Required: true, Minimum: &value, Maximum: &value}
	}
	labelEnum := func(key, label, value string) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldEnum, Required: true, Options: []string{value}}
	}
	attachment := func(key, label string) Field {
		return Field{Key: key, Label: label, AccessibleLabel: label, CSVHeader: key, Type: FieldAttachment, ReferenceType: "vault.blob", AllowHolding: true}
	}
	return []Template{
		t("builtin-foundation-organization", "foundation.organization", "Foundation organization", "Describe the organization boundary.", text("name", "Organization name", true)),
		t("builtin-atlas-asset", "atlas.asset", "Atlas asset", "Register an organization-owned asset.", text("name", "Asset name", true), enum("kind", "Asset kind", true, "server", "desktop", "laptop", "network", "storage", "mobile", "printer", "virtual", "other"), text("assetTag", "Asset tag", false), text("serialNumber", "Serial number", false), ref("modelId", "Model", "atlas.model", false, true), ref("siteId", "Site", "people.site", false, true), ref("departmentId", "Department", "people.department", false, true), date("purchaseDate", "Purchase date", false)),
		t("builtin-atlas-model", "atlas.model", "Atlas model", "Define reusable product defaults.", text("manufacturer", "Manufacturer", true), text("name", "Model name", true), text("modelNumber", "Model number", false), enum("kind", "Asset kind", true, "server", "desktop", "laptop", "network", "storage", "mobile", "printer", "virtual", "other"), boundedText("vendorIdentifier", "Vendor identifier", false, 160), boundedText("specifications", "Specifications (portable JSON)", false, 15_000), boundedText("supportUrl", "Support URL", false, 500), boundedNumber("warrantyMonths", "Warranty months", false, 0, 1_200), boundedNumber("usefulLifeMonths", "Useful life months", false, 0, 1_200)),
		t("builtin-atlas-identifier", "atlas.identifier", "Atlas identifier", "Associate a Code 128 or QR identifier.", ref("assetId", "Asset", "atlas.asset", true, false), enum("symbology", "Code format", true, "code128", "qr"), text("value", "Encoded value", true), text("displayValue", "Human-readable value", false)),
		t("builtin-atlas-label-code128", "atlas.label-template", "Atlas Code 128 label", "Immutable 70 by 30 millimeter Code 128 label definition.", labelEnum("symbology", "Code format", "code128"), labelNumber("widthMm", "Width in millimeters", 70), labelNumber("heightMm", "Height in millimeters", 30), labelNumber("marginMm", "Margin in millimeters", 3), labelNumber("quietZoneMm", "Quiet zone in millimeters", 3), labelEnum("payloadSource", "Encoded payload source", "identifier_value"), labelEnum("humanReadableField", "Human-readable field", "identifier.displayValue"), labelEnum("safeAssetFields", "Optional safe asset fields", "asset.name,asset.assetTag"), labelEnum("organizationBranding", "Organization branding", "StewardMesh")),
		t("builtin-atlas-label-qr", "atlas.label-template", "Atlas QR label", "Immutable 50 by 30 millimeter QR label definition.", labelEnum("symbology", "Code format", "qr"), labelNumber("widthMm", "Width in millimeters", 50), labelNumber("heightMm", "Height in millimeters", 30), labelNumber("marginMm", "Margin in millimeters", 3), labelNumber("quietZoneMm", "Quiet zone in millimeters", 2), labelEnum("payloadSource", "Encoded payload source", "organization_route"), labelEnum("humanReadableField", "Human-readable field", "identifier.displayValue"), labelEnum("safeAssetFields", "Optional safe asset fields", "asset.name,asset.assetTag"), labelEnum("organizationBranding", "Organization branding", "StewardMesh")),
		t("builtin-atlas-lifecycle-event", "atlas.lifecycle-event", "Atlas lifecycle event", "Record an immutable asset lifecycle event.", ref("assetId", "Asset", "atlas.asset", true, false), enum("kind", "Event kind", true, "registered", "assigned", "moved", "maintained", "retired", "disposed"), date("effectiveOn", "Effective date", true), text("note", "Event note", false)),
		t("builtin-atlas-catalog-configuration", "atlas.catalog-configuration", "Atlas catalog configuration", "Define a purchasable model configuration.", ref("modelId", "Model", "atlas.model", true, true), text("name", "Configuration name", true), text("sku", "SKU", false), boundedText("specifications", "Specifications", false, 40_000)),
		t("builtin-atlas-catalog-price", "atlas.catalog-price", "Atlas catalog price", "Record an effective-dated catalog price.", ref("configurationId", "Configuration", "atlas.catalog-configuration", true, true), enum("kind", "Price kind", true, "list", "street", "contract"), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), date("effectiveFrom", "Effective from", true), date("effectiveTo", "Effective to", false)),
		t("builtin-atlas-catalog-upgrade-path", "atlas.catalog-upgrade-path", "Atlas catalog upgrade path", "Relate a configuration to its successor, replacement, or upgrade.", ref("fromConfigurationId", "From configuration", "atlas.catalog-configuration", true, true), ref("toConfigurationId", "To configuration", "atlas.catalog-configuration", true, true), enum("relationshipKind", "Relationship kind", true, "successor", "replacement", "upgrade")),
		t("builtin-people-site", "people.site", "People site", "Create a directory site.", text("name", "Site name", true), text("city", "City", false), text("country", "Country", false)),
		t("builtin-people-building", "people.building", "People building", "Create a building beneath a site.", ref("siteId", "Site", "people.site", true, true), text("name", "Building name", true)),
		t("builtin-people-room", "people.room", "People room", "Create a room beneath a building.", ref("siteId", "Site", "people.site", true, true), ref("buildingId", "Building", "people.building", true, true), text("number", "Room number", true), text("name", "Room name", false)),
		t("builtin-people-department", "people.department", "People department", "Create a department.", text("name", "Department name", true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-people-identity", "people.identity", "People identity", "Create a person or shared identity.", enum("kind", "Identity kind", true, "person", "shared", "public", "lab"), text("displayName", "Display name", true), text("email", "Email address", false), ref("departmentId", "Department", "people.department", false, true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-people-assignment", "people.assignment", "Asset assignment", "Assign an identity or department to an asset.", ref("assetId", "Asset", "atlas.asset", true, true), enum("assigneeKind", "Assignee type", true, "identity", "department"), ref("assigneeId", "Assignee", "people.identity-or-department", true, true), enum("role", "Assignment role", true, "primary", "user", "department"), date("effectiveFrom", "Effective from", true)),
		t("builtin-threads-tag", "threads.tag", "Threads tag", "Create a hierarchical tag.", text("name", "Tag name", true), ref("parentId", "Parent tag", "threads.tag", false, true), enum("inheritance", "Default inheritance", true, "include", "suppress")),
		t("builtin-threads-goal", "threads.goal", "Threads goal", "Create a strategic goal.", text("name", "Goal name", true), boundedText("description", "Description", false, 2_000), ref("parentId", "Parent goal", "threads.goal", false, true)),
		t("builtin-threads-tag-rule", "threads.tag-rule", "Threads tag rule", "Explicitly include or suppress a tag on a record.", text("targetType", "Target record type", true), ref("targetId", "Target record", "stewardmesh.record", true, true), ref("tagId", "Tag", "threads.tag", true, true), enum("rule", "Rule", true, "include", "suppress")),
		t("builtin-threads-goal-link", "threads.goal-link", "Threads goal link", "Link a record to a strategic goal.", text("targetType", "Target record type", true), ref("targetId", "Target record", "stewardmesh.record", true, true), ref("goalId", "Goal", "threads.goal", true, true)),
		t("builtin-vault-blob", "vault.blob", "Vault evidence", "Describe portable private-attachment metadata; object keys, credentials, and signed URLs are never fields.", boundedText("name", "File name", true, 255), boundedText("mediaType", "Media type", true, 127), number("sizeBytes", "Size in bytes", true), boundedText("sha256", "SHA-256 digest", true, 64), text("provider", "Storage provider", true), text("resourceType", "Related record type", false), ref("resourceId", "Related record", "stewardmesh.record", false, true)),
		t("builtin-ledger-vendor", "ledger.vendor", "Ledger vendor", "Create a vendor.", text("name", "Vendor name", true), text("externalId", "External ID", false)),
		t("builtin-ledger-purchase-order", "ledger.purchase-order", "Ledger purchase order", "Create a purchase order.", text("number", "Purchase order number", true), ref("vendorId", "Vendor", "ledger.vendor", true, true), text("currency", "Currency", true), money("totalMinor", "Total in minor units", true), date("orderedOn", "Ordered on", false), attachment("receiptDocumentId", "Receipt evidence")),
		t("builtin-ledger-contract", "ledger.contract", "Ledger contract", "Create a vendor contract.", text("name", "Contract name", true), ref("vendorId", "Vendor", "ledger.vendor", true, true), text("currency", "Currency", true), money("ceilingMinor", "Contract ceiling in minor units", true), date("startsOn", "Starts on", true), date("endsOn", "Ends on", true), attachment("documentId", "Contract evidence")),
		t("builtin-ledger-commitment", "ledger.commitment", "Ledger commitment", "Record a financial commitment.", ref("contractId", "Contract", "ledger.contract", true, true), enum("kind", "Commitment type", true, "subscription", "lease", "maintenance", "license", "financing", "other"), boundedText("description", "Description", true, 500), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), date("startsOn", "Starts on", true), date("endsOn", "Ends on", true)),
		t("builtin-ledger-budget", "ledger.budget", "Ledger budget", "Create a fiscal budget.", text("name", "Budget name", true), text("fiscalPeriod", "Fiscal period", true), text("scenario", "Scenario", true), text("currency", "Currency", true), money("allocatedMinor", "Allocation in minor units", true), ref("departmentId", "Department", "people.department", false, true), ref("siteId", "Site", "people.site", false, true)),
		t("builtin-ledger-cost", "ledger.cost", "Ledger cost", "Reconcile a current cost.", boundedText("description", "Description", true, 500), enum("kind", "Cost state", true, "planned", "estimated", "actual", "billed", "paid", "committed", "normalized_real", "tco"), text("currency", "Currency", true), money("amountMinor", "Amount in minor units", true), text("sourceSystemId", "Source system ID", false), text("sourceRecordId", "Source record ID", false)),
		t("builtin-horizon-plan", "horizon.plan", "Horizon lifecycle plan", "Create an effective-dated lifecycle plan.", ref("assetId", "Asset", "atlas.asset", true, true), text("scenario", "Scenario", true), number("expectedUsefulLifeMonths", "Expected useful life in months", true), date("replacementDate", "Replacement date", false), text("currency", "Currency", true), money("replacementCostMinor", "Replacement cost in minor units", true), date("effectiveFrom", "Effective from", true)),
		t("builtin-guard-role", "guard.role", "Guard custom role", "Create a custom authorization role.", boundedText("name", "Role name", true, 120), boundedText("description", "Role description", false, 1_000), text("permissions", "Permission identifiers", true)),
		t("builtin-guard-account", "guard.account", "Guard account", "Describe a local or provider-managed account.", text("username", "Username", true), text("email", "Email address", true), text("displayName", "Display name", true), enum("status", "Account status", true, "active", "disabled")),
		t("builtin-guard-policy-bundle", "guard.policy-bundle", "Guard policy bundle", "Describe a reusable permission bundle.", boundedText("name", "Bundle name", true, 120), boundedText("description", "Bundle description", false, 1_000), text("permissions", "Permission identifiers", true)),
		t("builtin-guard-role-assignment", "guard.role-assignment", "Guard role assignment", "Assign a role at an explicit scope.", ref("accountId", "Account", "guard.account", true, true), ref("roleId", "Role", "guard.role", true, true), enum("scopeKind", "Scope kind", true, "organization", "site", "department", "resource"), text("scopeResourceId", "Scope resource ID", false)),
		t("builtin-guard-resource-ownership", "guard.resource-ownership", "Guard resource ownership", "Register authoritative ownership for a resource.", text("resourceType", "Resource type", true), ref("resourceId", "Resource", "stewardmesh.record", true, true), ref("ownerAccountId", "Owner account", "guard.account", true, true)),
		t("builtin-patterns-template", "patterns.template", "Patterns template", "Describe an organization-scoped custom schema version.", text("recordType", "Record type", true), text("name", "Template name", true), text("description", "Description", false), number("version", "Version", true), text("fields", "Field definitions", true)),
		t("builtin-stack-product", "stack.product", "Stack product", "Describe a portable software product.", text("name", "Product name", true), text("publisher", "Publisher", true), boundedText("category", "Category", false, 100), enum("status", "Status", true, "active", "retired")),
		t("builtin-stack-version", "stack.version", "Stack version", "Describe a portable software version.", ref("productId", "Product", "stack.product", true, true), boundedText("name", "Version name", true, 100), date("releasedOn", "Release date", false), enum("status", "Status", true, "active", "unsupported", "retired")),
		t("builtin-stack-installation", "stack.installation", "Stack installation", "Describe an installation without organization or operator identifiers.", ref("versionId", "Version", "stack.version", true, true), ref("assetId", "Asset", "atlas.asset", true, true), enum("status", "Status", true, "installed", "removed"), enum("usageState", "Usage state", true, "unknown", "used", "unused"), text("installedAt", "Installed at (RFC 3339)", true), text("lastUsedAt", "Last used at (RFC 3339)", false), text("removedAt", "Removed at (RFC 3339)", false)),
		t("builtin-stack-license", "stack.license", "Stack license", "Describe a software entitlement and its portable relationships.", ref("productId", "Product", "stack.product", true, true), ref("versionId", "Version", "stack.version", false, true), text("name", "License name", true), enum("entitlementMetric", "Entitlement metric", true, "device", "user", "concurrent", "site", "enterprise"), boundedNumber("quantity", "Quantity", true, 1, 1_000_000_000), enum("status", "Status", true, "active", "expired", "retired"), date("startsOn", "Starts on", false), date("expiresOn", "Expires on", false), ref("vendorId", "Vendor", "ledger.vendor", false, true), ref("purchaseOrderId", "Purchase order", "ledger.purchase-order", false, true), ref("contractId", "Contract", "ledger.contract", false, true), ref("costRecordId", "Cost record", "ledger.cost", false, true), boundedText("documentIds", "Evidence IDs (comma separated)", false, 12_899)),
		t("builtin-stack-assignment", "stack.assignment", "Stack assignment", "Assign licensed seats without embedding identity details.", ref("licenseId", "License", "stack.license", true, true), enum("assigneeKind", "Assignee kind", true, "asset", "identity", "department", "site"), ref("assigneeId", "Assignee", "stewardmesh.record", true, true), boundedNumber("seats", "Seats", true, 1, 1_000_000_000), enum("usageState", "Usage state", true, "unknown", "used", "unused"), text("assignedAt", "Assigned at (RFC 3339)", true), text("lastUsedAt", "Last used at (RFC 3339)", false), text("endedAt", "Ended at (RFC 3339)", false)),
		t("builtin-signals-rule", "signals.rule", "Signals rule", "Describe a durable alert rule.", text("name", "Rule name", true), enum("condition", "Condition", true, "over_budget", "forecast_over_budget", "unpaid", "overdue", "expiration", "renewal", "unused_commitment", "reconciliation"), enum("severity", "Severity", true, "info", "warning", "critical"), enum("enabled", "Enabled", true, "true", "false"), text("thresholdDays", "Threshold days (comma separated)", false), text("fiscalPeriod", "Fiscal period", false), text("scenario", "Scenario", false)),
		t("builtin-signals-alert", "signals.alert", "Signals alert", "Describe the current durable alert state; history and delivery attempts are excluded.", ref("ruleId", "Rule", "signals.rule", true, true), enum("condition", "Condition", true, "over_budget", "forecast_over_budget", "unpaid", "overdue", "expiration", "renewal", "unused_commitment", "reconciliation"), enum("severity", "Severity", true, "info", "warning", "critical"), enum("status", "Status", true, "active", "acknowledged", "resolved"), text("title", "Title", true), boundedText("summary", "Summary", true, 500), text("targetType", "Target type", true), ref("targetId", "Target", "stewardmesh.record", true, true), text("dueAt", "Due at (RFC 3339)", false)),
		t("builtin-signals-subscription", "signals.subscription", "Signals subscription", "Describe a configured subscriber reference without routes or credentials.", ref("ruleId", "Rule", "signals.rule", false, true), enum("targetKind", "Target kind", true, "group", "webhook"), ref("targetId", "Subscriber target", "stewardmesh.record", true, true), enum("enabled", "Enabled", true, "true", "false")),
		t("builtin-reach-provider", "reach.provider", "Reach provider", "Describe provider selection without secret references, values, endpoints, or test results.", text("name", "Provider name", true), enum("kind", "Provider kind", true, "smtp", "ses", "gmail_oauth", "outlook_oauth", "teams", "webhook"), text("endpointId", "Deployment endpoint ID", true), text("sender", "Sender", false), enum("enabled", "Enabled", true, "true", "false")),
		t("builtin-reach-template", "reach.template", "Reach message template", "Describe a durable message template.", text("name", "Template name", true), text("subject", "Subject", true), boundedText("body", "Body", true, 4_000)),
		t("builtin-reach-subscriber-group", "reach.subscriber-group", "Reach subscriber group", "Describe a bounded subscriber group without provider routes or credentials.", text("name", "Group name", true), ref("providerId", "Provider", "reach.provider", true, true), ref("templateId", "Template", "reach.template", true, true), boundedText("recipients", "Recipients (portable JSON)", true, 40_000)),
		t("builtin-reach-message", "reach.message", "Reach message", "Describe a durable queued message; delivery attempts and provider responses are excluded.", ref("groupId", "Subscriber group", "reach.subscriber-group", false, true), ref("providerId", "Provider", "reach.provider", true, true), ref("templateId", "Template", "reach.template", false, true), text("sourceKind", "Source kind", true), ref("sourceId", "Source", "stewardmesh.record", false, true), text("subject", "Subject", true), boundedText("body", "Body", true, 4_000), boundedText("recipients", "Recipients (portable JSON)", true, 40_000), enum("status", "Status", true, "queued", "retrying", "delivered", "failed")),
		t("builtin-directory-import-batch", "directory.import-batch", "Directory import batch", "Describe a durable directory preview/apply batch; individual intake rows and attempts are excluded. Provider remains open-ended and is validated by the runtime connector registry.", text("sourceSystemId", "Source system ID", true), text("provider", "Provider", true), text("configRevision", "Connector configuration revision", true), enum("status", "Status", true, "previewed", "applying", "applied", "partially_applied", "failed"), enum("completeSnapshot", "Complete snapshot", true, "true", "false"), number("createdCount", "Created count", true), number("updatedCount", "Updated count", true), number("unchangedCount", "Unchanged count", true), number("deactivatedCount", "Deactivated count", true), number("conflictCount", "Conflict count", true), number("failedCount", "Failed count", true), text("createdAt", "Created at (RFC 3339)", true), text("updatedAt", "Updated at (RFC 3339)", true), text("completedAt", "Completed at (RFC 3339)", false)),
		t("builtin-directory-group", "directory.group", "Directory group", "Describe a durable synchronized group.", text("sourceSystemId", "Source system ID", true), text("sourceRecordId", "Source record ID", true), boundedText("name", "Stable group name", true, 512), text("displayName", "Display name", true), boundedText("description", "Description", false, 2_000), enum("status", "Status", true, "active", "inactive"), boundedText("metadata", "Normalized metadata (portable JSON)", false, 10_000), number("revision", "Revision", true)),
		t("builtin-directory-membership", "directory.membership", "Directory membership", "Describe a durable subject or nested-group membership relationship.", text("sourceSystemId", "Source system ID", true), text("sourceRecordId", "Source record ID", true), ref("groupId", "Group", "directory.group", true, true), text("groupSourceId", "Group source ID", true), ref("memberId", "Member", "stewardmesh.record", true, true), text("memberSourceId", "Member source ID", true), enum("memberKind", "Member kind", true, "subject", "group"), text("memberDisplayName", "Member display name", true), enum("status", "Status", true, "active", "inactive"), text("metadata", "Normalized metadata (portable JSON)", false), number("revision", "Revision", true)),
		t("builtin-exchange-package", "exchange.package", "Exchange package receipt", "Describe a durable migration receipt; record outcomes and raw archives are excluded.", text("packageId", "Package ID", true), enum("direction", "Direction", true, "export", "import"), text("schemaVersion", "Package schema version", true), text("sourceSystemId", "Source system ID", true), enum("fileMode", "File mode", true, "metadata", "include"), enum("status", "Status", true, "processing", "completed", "holding", "failed"), number("recordCount", "Record count", true), number("fileCount", "File count", true)),
		t("builtin-bridge-oauth-client", "bridge.oauth-client", "Bridge OAuth client", "Describe a public PKCE client without a client secret.", text("name", "Client name", true), text("redirectUris", "Redirect URIs (one per line)", true), text("allowedScopes", "Allowed scopes (comma separated)", true), text("revokedAt", "Revoked at (RFC 3339)", false)),
		t("builtin-bridge-oauth-grant", "bridge.oauth-grant", "Bridge OAuth grant", "Describe revocable authorization metadata; token hashes and credentials are excluded.", ref("clientId", "OAuth client", "bridge.oauth-client", true, true), text("clientName", "Client name", false), ref("actorId", "Actor", "guard.account", true, true), text("resourceUri", "Resource URI", true), text("scopes", "Scopes (comma separated)", true), text("accessExpiresAt", "Access expires at (RFC 3339)", true), text("refreshExpiresAt", "Refresh expires at (RFC 3339)", true), text("revokedAt", "Revoked at (RFC 3339)", false)),
	}
}
