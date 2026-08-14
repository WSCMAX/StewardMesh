# Relationship Graph

- **Canonical ID:** `threads.relationships`
- **Requirement:** `REQ-DIRECTORY-EXPANSION-008`
- **Roadmap issue:** [#31](https://github.com/WSCMAX/StewardMesh/issues/31)

Relationship Graph is StewardMesh's reusable, read-only view of connections
among authoritative records. It includes visible People locations and
identities, organization-wide imported groups, and Atlas assets permitted by
both Atlas and directory visibility. Future features can add node and edge
types without moving ownership of their records into the graph.

## Contract and safety

The REST endpoint is `GET /api/v1/graph`; the protobuf surface is
`RelationshipGraphService.GetRelationshipGraph`. Both accept only bounded
search, node type, relationship type, and record limit filters. Organization,
site, department, resource, and asset visibility never appear in a client
request. The server derives them from the authenticated Guard principal and
intersects scopes before projection.

Search and record-type filters select anchor records. When a relationship type
is also selected, the response retains each matching relationship's other
endpoint as context even when that endpoint has another record type or label;
the complete response still stays within the selected record limit. This makes
cross-type questions such as "assets located at" useful without exposing an
edge whose endpoint is absent.

Node IDs are typed (`site:<id>`, `person:<id>`, or `asset:<id>`), labels are
bounded, and attributes contain only minimal status, origin, and asset-kind
context. Every edge references two returned nodes. Results are deterministic,
cache-disabled, capped at 500 nodes and 2,000 edges, duplicate-safe, and valid
when cyclic, disconnected, or empty.

## Accessible use

Open People and use the Relationship graph filters. The SVG supplies a compact
overview of the first 40 records. Focusable relationship and disconnected-node
tables are the complete keyboard and screen-reader representation, including
when filters leave nodes without a matching edge. At 320 pixels the controls
stack, while wide tables scroll within labeled regions instead of widening the
page.
