# Atlas Models — Product model catalog

- **Canonical ID:** `inventory.models`
- **Requirement:** `REQ-ATLAS-MODELS-001`
- **GitHub issue:** [#68 — Atlas Models product model catalog](https://github.com/WSCMAX/StewardMesh/issues/68)
- **Bulk intake slice:** [#73 — Bulk intake and import from model catalog](https://github.com/WSCMAX/StewardMesh/issues/73)
- **Default provenance slice:** [#74 — Default provenance and instance override visibility](https://github.com/WSCMAX/StewardMesh/issues/74)
- **Owning product area:** [Atlas](atlas.md)

## Purpose

Atlas Models lets an organization describe a purchased product model once and reuse that shared record across many individual assets. The model stores manufacturer, model name and number, asset kind, vendor identifier, support URL, warranty and useful-life defaults, bounded shared specifications, source provenance, status, and revision.

Individual assets keep their own identity and deployment facts: asset tag, serial number, hostname, deployment notes, assigned user, site, room, department, lifecycle status, purchase date, and revision. An asset may reference one active model through `modelId`; the model relationship is visible but does not silently overwrite instance-specific fields.

When a model is linked, Atlas stores an immutable `modelContext` snapshot containing the shared defaults, model revision, import source system and record, the time those defaults became effective, and the time they were applied to the asset. Later model changes never rewrite that snapshot. The asset response exposes an explicit `overrides` list; `kind` is currently the only model-derived asset field and is listed when the instance value differs from the saved default.

## Roles and permissions

Model reads use `assets.read`. Model create, update, and retirement use `assets.write`, same-origin CSRF protection, organization scope, optimistic revisions, and the same Guard enforcement path as Atlas assets. Retired models remain readable for historical asset context but cannot be assigned to new or updated assets.

## Data rules

Manufacturer, model name, and model number are normalized with trimmed lowercase values and are unique within an organization. This keeps imported and manually entered models deterministic without depending on provider-specific IDs.

Provider and import adapters resolve that exact organization-scoped identity through the model resolver before creating instances. Bulk intake accepts 1–100 instances for one active model, validates the complete batch first, rejects duplicate IDs, asset tags, and serial numbers, and atomically creates all assets and initial lifecycle events or none. The model supplies its kind when an instance omits it; instance identity, deployment notes, status, purchase date, and People references remain per asset.

Shared specifications are a bounded key/value map. Keys are 1-80 characters, values are at most 500 characters, and each model stores at most 25 shared fields. Support URLs must use HTTPS. Warranty and useful-life defaults are integer month values from 0 through 1200.

## APIs and provider boundaries

REST endpoints:

- `GET|POST /api/v1/asset-models`
- `GET|PUT /api/v1/asset-models/{modelId}`
- `GET /api/v1/asset-models/resolve?manufacturer=...&name=...&modelNumber=...`
- `POST /api/v1/asset-models/{modelId}/retire?revision=...`
- `POST /api/v1/asset-models/{modelId}/assets/bulk`
- `GET /api/v1/assets?modelId=...`

OpenAPI and protobuf contracts include the same model fields, exact resolver, bulk operation, `modelId`, and immutable `modelContext` on assets. The `atlas.Store` contract is extended for memory and PostgreSQL providers, and repository conformance tests prove uniqueness, resolution, atomic bulk creation, instance counts, asset filtering, retirement, and snapshot preservation across model updates.

Migration `0023_atlas_models.sql` creates the durable model catalog, adds the nullable `atlas_assets.model_id` foreign key, and indexes model-linked asset counts and filters. Migration `0025_atlas_model_bulk_intake.sql` adds bounded per-instance deployment notes without moving shared product facts out of the model. Migration `0026_atlas_model_default_provenance.sql` snapshots defaults for existing links and requires every future model link to carry a valid context object.

## Accessible workflow

Atlas shows a compact Model catalog above the asset list. Users with write permission can add, edit, retire, choose **Use** for one repeated asset, or choose **Bulk add** for an atomic batch. The bulk form uses numbered fieldsets, supports adding and removing rows, exposes text status and validation failures, and keeps model-owned facts separate from each instance's identity, deployment, People references, and lifecycle status.

Asset detail separates the instance-specific record from **Model defaults when linked**. It shows the saved model label and revision, shared defaults and specifications, source provenance, effective/application dates, whether the instance kind uses the default or overrides it, and text explaining that the snapshot will not change with the model record. Empty source provenance is labeled as manual entry. The controls and detail layout remain keyboard operable and contained at a 320-pixel viewport.

The implemented slices cover model CRUD, retirement, deterministic uniqueness and import resolution, single and bounded bulk asset creation, asset linking, immutable default provenance, explicit override visibility, instance counts, API contracts, provider seams, audits, documentation, and focused UI tests. Model inventory grouping remains tracked separately under #75.
