# Patterns — Versioned record templates and field validation

- **Canonical ID:** `templates.schemas`
- **Requirement:** `REQ-PATTERNS-001`
- **GitHub issue:** #8
- **Delivery state:** Implemented

## Purpose

Patterns gives every StewardMesh record family a stable, machine-readable field contract. Forms, API clients, CSV intake, help content, accessibility labels, and Exchange packages select the same template ID and immutable version instead of maintaining separate field definitions.

Built-in version 1 templates cover the authoritative phase-one record set in Foundation, Atlas and Atlas Catalog, People, Threads, Vault, Ledger, Horizon, Guard, Patterns, Stack, Signals, Reach, Directory, Exchange, and Bridge. `CoreRecordTypes` is the executable inventory and the coverage test fails if a family has no active built-in. Built-ins are code-versioned and read only. An administrator can create a custom template from an empty definition or copy any exact built-in/custom version, then append a new version without changing records that name an older version.

The phase-one boundary explicitly excludes derived graph nodes/edges, analytics and forecasts, audit events, directory import rows/attempts, alert history/deliveries, messaging provider tests/delivery attempts, Exchange record outcomes, OAuth authorization codes/requests, rate windows, confirmations, and label artifact batches. Those values are computed, short-lived, security-sensitive, or internal workflow state rather than independently editable/importable records. `ExplicitlyExcludedRecordTypes` keeps this decision reviewable and tested.

## Field contract

Every field has a stable key, visible label, optional help, accessible label, CSV header, required/optional state, and exactly one type:

| Type | Accepted value |
|---|---|
| `text` | Unicode text within the configured length. |
| `number` | A finite JSON number within optional minimum and maximum bounds. |
| `date` | A real calendar date in `YYYY-MM-DD` form. |
| `money` | An exact integer minor-unit amount within the browser-safe integer range, paired with a three-letter uppercase currency field. |
| `enum` | One exact value from the versioned option list. |
| `attachment` | A stable Vault blob identifier and target type. |
| `reference` | A stable related-record identifier and target type. |

Unknown fields, malformed values, missing required values, invalid currency companions, and unresolved references return field-addressable errors. Error messages begin with the field's accessible label so a form or import review can announce them without reconstructing presentation metadata.

Patterns has exactly seven scalar field types, so some domain values use an explicit portable form representation rather than their storage JSON shape. Booleans use an enum with `true` and `false`; bounded lists use documented comma-separated or newline-separated text; bounded lists/maps that must retain structure use portable JSON text; instants use RFC 3339 text while calendar-only values use `date`. A domain or Exchange provider owns the reversible conversion and must reject malformed, lossy, or over-limit values. These representations never authorize generic writes to an otherwise excluded operational record.

## Visible holding records

A reference or attachment field can explicitly allow holding. Validation reports `holding` only when all of these are true:

1. the selected template version marks the field `allowHolding`;
2. the caller opts into a holding record for this validation; and
3. the caller identifies the field as unresolved.

The result lists each unresolved field, target record type, and supplied identifier. Without those conditions, the same missing reference is invalid. Patterns validates and describes holding state; Exchange owns package staging, dependency resolution, durable receipts, and promotion of held rows.

## Administration and authorization

Any authenticated user can read template/schema metadata, validate a candidate record, and export a one-line CSV header template. Custom create, copy, and version operations require Guard's `guard.manage` permission plus the normal same-origin CSRF control. Templates are organization scoped; built-ins contain no organization data and custom versions cannot cross an organization boundary.

In Workspace, administrators manage Patterns below Guard. The definition and append-version forms support all seven field types, repeatable fields, required state, enum options, reference target types, holding behavior, money currency companions, accessible labels, CSV headers, numeric bounds, text-length bounds, copying, and exact version selection. A separate generated record workbench renders the selected version's accessible labels, help, required state, options, and native typed controls, then submits values and explicitly marked unresolved references to the server's exact `/validate?version=` endpoint.

The CSV workbench accepts exactly one header row and one data row, capped at 128 KiB. Headers must match the selected version's `csvHeader` values in order. Number and money cells are parsed into JSON numbers, money remains a safe integer in minor units, malformed quoting and extra rows fail, and text/reference/attachment/enum/date cells beginning with `=`, `+`, `-`, or `@` after leading whitespace are rejected to prevent spreadsheet formula execution. Export applies the same checks, RFC 4180 quoting, and exact-version filename before making a local data download available.

## APIs and integration seam

The REST surface is documented in [OpenAPI](../../api/openapi/openapi.yaml), and transport-neutral parity is documented in [protobuf](../../api/proto/stewardmesh.proto).

- `GET/POST /api/v1/templates` (`includeVersions=true` returns immutable history; the default returns each latest version)
- `GET /api/v1/templates/{templateID}` and `/schema`
- `POST /api/v1/templates/{templateID}/copy`
- `POST /api/v1/templates/{templateID}/versions`
- `POST /api/v1/templates/{templateID}/validate`
- `GET /api/v1/templates/{templateID}/template.csv`

REST `includeVersions` and protobuf `ListPatternsTemplatesRequest.include_versions` expose the same exact-version discovery choice. `ValidationInput` and `ValidationResult` live in the provider-neutral Patterns package. Exchange calls that service directly with the exact manifest template ID/version, typed portable values, and unresolved-reference field keys. Exchange schema `1.1` repeats the immutable reference in a sorted manifest schema registry and on every checksummed record. Unknown, mismatched, ambiguous, or retired schemas fail before Guard ownership registration or a provider mutation; allowed missing references become visible holding outcomes. Imports continue to resolve the exact pinned immutable version after a newer custom version is appended.

## Accessibility and help

Pattern fields carry both visible labels and explicit accessible labels. The management surface uses native labels, fieldsets, selects, checkboxes, headings, status and alert regions, visible focus, non-color built-in/version text, and layouts that collapse without a table dependency. Field help is part of the schema response and appears beside the field contract, so generated forms and imports can expose the same instruction to sighted users and assistive technology.

## Audit and validation coverage

Custom template creation and version creation emit `patterns.template.created` and `patterns.template.version.created`. Audit metadata includes requirement ID, record type, template ID, and version, but excludes template values and candidate record data.

Tests cover authoritative built-in/exclusion and seven-type coverage, default label/CSV metadata, custom creation/copying, immutable version reads, exact money and currency validation, ranges, dates, enums, attachments, unknown fields, explicit holding behavior, bounded typed CSV round trips and formula rejection, HTTP authorization/metadata/validation/export, Exchange exact-schema and pre-mutation holding gates, Stack/Vault portable projections, migration ordering, React workflows, and axe accessibility checks.
