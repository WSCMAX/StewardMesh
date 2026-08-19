# Relationship Graph

- **Canonical ID:** `threads.relationships`
- **Requirement:** `REQ-DIRECTORY-EXPANSION-008`
- **Roadmap issue:** [#31](https://github.com/WSCMAX/StewardMesh/issues/31)

Relationship Graph is StewardMesh's reusable, read-only view of connections
among authoritative records. The People graph includes visible locations and
identities, organization-wide imported groups, and Atlas assets permitted by
both Atlas and directory visibility. The mesh graph adds purchase orders,
vendors, contracts, budgets, software products and licenses, labels, goals,
documents, models, and lifecycle plans when the caller can read those
products. Source services keep ownership of their records.

## Contract and safety

The REST endpoint `GET /api/v1/graph` remains the People-scoped projection
(`RelationshipGraphService.GetRelationshipGraph`). The cross-product endpoint
is `GET /api/v1/mesh/graph`. It layers Ledger, Stack, Labels, Goals, Vault, and
Horizon records onto the same typed node/edge model, using only Guard grants
the caller already has. Visibility never appears in a client request.

`GET /api/v1/mesh/graph` accepts bounded search, one or more node types
(`kind` / `kinds`), one or more relationship types (`relationship` /
`relationships`), and a record limit. Selecting kinds drops every other record
type and any edge that would have pointed at it. Search keeps matching records
and their direct connections within the limit.

Node IDs are typed (`site:<id>`, `person:<id>`, or `asset:<id>`), labels are
bounded, and attributes contain only minimal status, origin, and asset-kind
context. Every edge references two returned nodes. Results are deterministic,
cache-disabled, capped at 500 nodes and 2,000 edges, duplicate-safe, and valid
when cyclic, disconnected, or empty.

## Accessible use

Open Mesh for the graph. People edits directory records in the shared
spreadsheet; it no longer hosts a separate relationship-graph tab. Zoom and
gravity sliders, a type legend, and optional
clustering make the visual readable; focusable relationship and disconnected-node
tables remain the complete keyboard and screen-reader representation. The Mesh
Data tab uses the same loaded records in the shared spreadsheet grid. At 320
pixels the controls stack, while wide tables scroll within labeled regions
instead of widening the page.
