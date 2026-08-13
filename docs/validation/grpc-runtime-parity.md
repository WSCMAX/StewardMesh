# gRPC runtime parity validation

- **Canonical ID:** `integrations.protocols`
- **Requirements:** `REQ-API-001`, `REQ-ATLAS-CODES-001`, `REQ-SIGNALS-001`, `SEC-GUARD-001`
- **Roadmap issues:** [#14](https://github.com/WSCMAX/StewardMesh/issues/14), [#60](https://github.com/WSCMAX/StewardMesh/issues/60)

## Runtime boundary

`internal/grpcapi` implements every unary service and method declared by
`api/proto/stewardmesh.proto`. Registration is descriptor-driven, but execution
is not inferred from request data: `internal/grpcapi/routes.go` is a complete,
fixed method/path/converter allowlist. Server construction fails if one declared
RPC has no route or if the allowlist names an undeclared RPC. Production wiring
in `cmd/stewardmesh-grpc` is intentionally not part of this change: the existing
Bridge-only command remains the reachable surface until the broader listener,
34 MiB envelope, and sensitive Guard/Vault exposure receive explicit deployment
approval.

Each authenticated call accepts exactly one `authorization: Bearer <opaque
Guard session>` metadata value of at most 512 bytes. Guard validates protected
calls before protobuf unmarshalling. Cookie, origin, CSRF, and
authorization metadata are never copied into the in-process HTTP request.
Instead, Guard revalidates the session and current grants once for that RPC and
places the resulting authentication in a private transport context. The
existing protected REST handler then performs the same organization permission,
scoped visibility, resource ownership, rate, input, repository, audit, and error
checks. Browser CSRF tokens are neither accepted nor rotated by gRPC, so a gRPC
call cannot invalidate a concurrent browser session.

`GetBootstrapStatus`, `BootstrapAdministrator`, and `AuthenticateLocal` are the
only public RPCs. Bootstrap without a deployment token is trusted only from the
loopback listener; a non-loopback deployment must use the configured bootstrap
token. Login uses the connection peer for Guard attempt limiting. Successful
bootstrap/login responses return the new opaque session token for subsequent
Bearer metadata. All remaining RPCs, including logout, require revalidation on
every call.

Any production listener using this adapter must remain plaintext only on
loopback. Non-loopback binding requires
a configured TLS certificate/key pair and TLS 1.3 or newer. The adapter's
protobuf receive and send envelope is 34 MiB, which admits a documented 32 MiB Exchange archive
plus bounded protobuf framing. Existing route and service limits remain
authoritative: Exchange accepts at most 32 MiB compressed and 64 MiB after
decompression; Atlas label batches remain bounded; Vault retains its configured
object limit and the gRPC envelope; ordinary JSON inputs keep their existing
smaller limits.

## Special converters

- CSV RPCs return exact response bytes plus server media type and attachment
  filename. Patterns additionally returns the resolved template identity and
  version.
- Exchange import consumes the raw `.openinventory` archive rather than a JSON
  base64 copy; export returns the exact archive and server checksum headers.
- Vault create uses the same bounded multipart handler as REST. Download first
  runs the ordinary authorized/audited download-authorization handler, then
  streams the verified private object into the protobuf response.
- Atlas label generation uses the existing authorized renderer. It returns the
  exact SVG/PDF bytes plus batch, immutable template, checksum, media, physical
  count, creation time, and replay metadata. ZPL remains absent from the public
  enum and runtime.
- Signals pending-delivery and attempt-result operations now have protected
  REST/OpenAPI routes as well as gRPC methods, preserving the provider-neutral
  Reach seam without exposing URLs, credentials, or provider response bodies.

## Automated evidence

- `internal/grpcapi/server_test.go` compares descriptor and runtime registration
  for all 16 services and 153 RPCs, and verifies exact Bearer parsing plus
  client cookie/origin/CSRF stripping, pre-decode authentication, public-request
  limits, route/OpenAPI coverage, opaque Struct keys, and binary envelope edges.
- `internal/application/grpc_runtime_test.go` invokes every declared RPC through
  a real in-memory gRPC connection and rejects any `Unimplemented`, `Unknown`,
  or adapter `Internal` result. It also exercises Guard bootstrap/session/logout,
  Atlas asset and Code 128 association/resolve/label SVG, Patterns CSV, Vault
  multipart create and verified download, and Exchange binary export/conflict.
- `internal/application/bridge_grpc_test.go` retains state-level REST parity for
  Bridge cursor paging, client/grant creation, revocation, and expired session
  behavior through the all-service runtime.
- `internal/httpapi/server_test.go` covers authentication, permission, browser
  CSRF, bounded reads, and sanitized attempt writes for the Signals delivery
  seam.
- OpenAPI lint, protobuf descriptor generation, `go test -race ./...`, `go vet
  ./...`, traceability, and the repository's PostgreSQL provider tests are the
  release gates.
