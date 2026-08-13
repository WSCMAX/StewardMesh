# Atlas Models — Product model catalog

- **Canonical ID:** `inventory.models`
- **Requirement:** `REQ-ATLAS-MODELS-001`
- **GitHub issue:** [#68 — Atlas Models product model catalog](https://github.com/WSCMAX/StewardMesh/issues/68)
- **Owning product area:** [Atlas](atlas.md)

## Purpose

Atlas Models lets an organization describe a purchased product model once and reuse that shared record across many individual assets. The model stores manufacturer, model name and number, asset kind, vendor identifier, support URL, warranty and useful-life defaults, bounded shared specifications, source provenance, status, and revision.

Individual assets keep their own identity and deployment facts: asset tag, serial number, hostname, assigned user, site, room, department, lifecycle status, purchase date, and revision. An asset may reference one active model through `modelId`; the model relationship is visible but does not silently overwrite instance-specific fields.

## Roles and permissions

Model reads use `assets.read`. Model create, update, and retirement use `assets.write`, same-origin CSRF protection, organization scope, optimistic revisions, and the same Guard enforcement path as Atlas assets. Retired models remain readable for historical asset context but cannot be assigned to new or updated assets.

## Data rules

Manufacturer, model name, and model number are normalized with trimmed lowercase values and are unique within an organization. This keeps imported and manually entered models deterministic without depending on provider-specific IDs.

Shared specifications are a bounded key/value map. Keys are 1-80 characters, values are at most 500 characters, and each model stores at most 25 shared fields. Support URLs must use HTTPS. Warranty and useful-life defaults are integer month values from 0 through 1200.

## APIs and provider boundaries

REST endpoints:

- `GET|POST /api/v1/asset-models`
- `GET|PUT /api/v1/asset-models/{modelId}`
- `POST /api/v1/asset-models/{modelId}/retire?revision=...`
- `GET /api/v1/assets?modelId=...`

OpenAPI and protobuf contracts include the same model fields and `modelId` on assets. The `atlas.Store` contract is extended for memory and PostgreSQL providers, and repository conformance tests prove uniqueness, instance counts, asset filtering, and retirement.

Migration `0023_atlas_models.sql` creates the durable model catalog, adds the nullable `atlas_assets.model_id` foreign key, and indexes model-linked asset counts and filters.

## Accessible workflow

Atlas shows a compact Model catalog above the asset list. Users with write permission can add, edit, retire, or choose **Use** on a model before creating a repeated asset. The asset form keeps model selection separate from asset identity fields so operators can see what is shared and what is instance-specific.

The implemented slice covers model CRUD, retirement, deterministic uniqueness, asset linking, instance counts, API contracts, provider seams, audits, documentation, and focused UI tests. Bulk intake/import from a model remains a follow-up slice under issue #68.
