# Patterns — Versioned record templates and field validation

- **Canonical ID:** `templates.schemas`
- **Requirement:** `REQ-PATTERNS-001`
- **GitHub issue:** #8
- **Delivery state:** Implemented

## Purpose

Patterns gives every StewardMesh record family a stable, machine-readable field contract. Forms, API clients, CSV intake, help content, accessibility labels, and the future Exchange package flow can select the same template ID and immutable version instead of maintaining separate field definitions.

Built-in version 1 templates cover the current core record types in Atlas and Atlas Catalog, People, Threads, Vault, Ledger, Horizon, and Guard. Built-ins are code-versioned and read only. An administrator can create a custom template from an empty definition or copy any exact built-in/custom version, then append a new version without changing records that name an older version.

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

## Visible holding records

A reference or attachment field can explicitly allow holding. Validation reports `holding` only when all of these are true:

1. the selected template version marks the field `allowHolding`;
2. the caller opts into a holding record for this validation; and
3. the caller identifies the field as unresolved.

The result lists each unresolved field, target record type, and supplied identifier. Without those conditions, the same missing reference is invalid. Patterns validates and describes holding state; Exchange issue #9 will own package staging, dependency resolution, persistence, and promotion of held rows.

## Administration and authorization

Any authenticated user can read template/schema metadata, validate a candidate record, and export a one-line CSV header template. Custom create, copy, and version operations require Guard's `guard.manage` permission plus the normal same-origin CSRF control. Templates are organization scoped; built-ins contain no organization data and custom versions cannot cross an organization boundary.

In Workspace, administrators manage Patterns below Guard. The native form supports all seven field types, repeatable fields, required state, enum options, reference target types, holding behavior, money currency companions, copying, exact version selection, and CSV download. Built-in versions are visibly identified as read only.

## APIs and integration seam

The REST surface is documented in [OpenAPI](../../api/openapi/openapi.yaml), and transport-neutral parity is documented in [protobuf](../../api/proto/stewardmesh.proto).

- `GET/POST /api/v1/templates`
- `GET /api/v1/templates/{templateID}` and `/schema`
- `POST /api/v1/templates/{templateID}/copy`
- `POST /api/v1/templates/{templateID}/versions`
- `POST /api/v1/templates/{templateID}/validate`
- `GET /api/v1/templates/{templateID}/template.csv`

`ValidationInput` and `ValidationResult` live in the provider-neutral Patterns package. Exchange can call that service directly with a template ID/version, typed values, and the unresolved-reference field keys. It does not need to depend on REST, React, or PostgreSQL.

## Accessibility and help

Pattern fields carry both visible labels and explicit accessible labels. The management surface uses native labels, fieldsets, selects, checkboxes, headings, status and alert regions, visible focus, non-color built-in/version text, and layouts that collapse without a table dependency. Field help is part of the schema response and appears beside the field contract, so generated forms and imports can expose the same instruction to sighted users and assistive technology.

## Audit and validation coverage

Custom template creation and version creation emit `patterns.template.created` and `patterns.template.version.created`. Audit metadata includes requirement ID, record type, template ID, and version, but excludes template values and candidate record data.

Tests cover built-in core-record and seven-type coverage, default label/CSV metadata, custom creation/copying, immutable version reads, exact money and currency validation, ranges, dates, enums, attachments, unknown fields, explicit holding behavior, CSV output, HTTP authorization/metadata/validation/export, migration ordering, React workflows, and axe accessibility checks.
