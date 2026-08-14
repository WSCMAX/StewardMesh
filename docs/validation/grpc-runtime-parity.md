# gRPC runtime parity validation

- **Canonical ID:** `integrations.protocols`
- **Requirements:** `REQ-API-001`, `REQ-ATLAS-CODES-001`, `REQ-SIGNALS-001`, `SEC-GUARD-001`
- **Roadmap issues:** [#14](https://github.com/WSCMAX/StewardMesh/issues/14), [#60](https://github.com/WSCMAX/StewardMesh/issues/60)
- **Adapter status:** Implemented and covered by the validation contracts below
- **Production activation:** Active in the standalone dual-listener command

## Runtime boundary

`internal/grpcapi` implements every unary service and method declared by
`api/proto/stewardmesh.proto`. Registration is descriptor-driven, but execution
is not inferred from request data: `internal/grpcapi/routes.go` is a complete,
fixed method/path/converter allowlist. Server construction fails if one declared
RPC has no route or if the allowlist names an undeclared RPC. Production wiring
for the all-domain adapter is active in `cmd/stewardmesh-grpc`. It uses a public
listener at `STEWARDMESH_GRPC_PUBLIC_ADDR` (default `127.0.0.1:9091`) and a
protected listener at `STEWARDMESH_GRPC_ADDR` (default `127.0.0.1:9090`). The
three public Guard methods and the standard unary gRPC health `Check` method are
registered only on the 64 KiB receive/send-envelope listener. The other 151 RPCs are registered
only on the authenticated 34 MiB listener. These grpc-go server limits apply
before codec invocation, which preserves the allocation boundary even though
grpc-go decompresses requests before invoking the codec.

The public listener intentionally returns `Unimplemented` for health `Watch`
and `List`. This keeps anonymous clients from holding long-lived health streams
and exhausting the four public pre-decode permits needed by Guard bootstrap and
login. Operators should use bounded unary `Check` probes.

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

Plaintext listeners are accepted only on loopback. If either address is
non-loopback, the command requires a configured certificate/key pair, an HTTPS
application origin, secure session cookies, and TLS 1.3 or newer. Until initial
administrator setup is complete, a non-loopback public listener also requires
the deployment bootstrap token. Both listeners cap header lists at 16 KiB,
connection setup at five seconds, RPC lifetime at five minutes, keepalive
behavior, and concurrent streams. Before authentication or protobuf decode, a
shared process limiter admits at most 12 RPCs while protected and public
listeners reserve limits of eight and four respectively. Existing route and
service limits remain authoritative: Exchange accepts at most 32 MiB compressed
and 64 MiB after decompression; Atlas label batches remain bounded; Vault
retains its configured object limit and the authenticated gRPC envelope;
ordinary JSON inputs keep their existing smaller limits.

HTTP and gRPC are separate application processes when both commands are
deployed. They must use the same PostgreSQL database and Valkey service to share
Guard sessions, domain state, and cross-process rate state. Memory repositories
and the `none`/memory cache modes are process-local and are limited to isolated
development or test runtimes.

## Special converters

- CSV RPCs return exact response bytes plus server media type and attachment
  filename. Patterns additionally returns the resolved template identity and
  version.
- Exchange import consumes the raw `.openinventory` archive rather than a JSON
  base64 copy; export returns the exact archive and server checksum headers.
- Vault create uses the same bounded multipart handler as REST, accepts a
  zero-byte file part, and rejects non-canonical or header-bearing media types
  before MIME construction. Download checks `storage.read`, then uses one Vault
  capability that atomically audits authorization and opens verified content;
  gateway construction rejects a Vault from another organization.
- Atlas create/update converters reject populated response-only fields instead
  of sending them into mutation bodies. Reach provider creation preserves
  omitted-versus-explicit-false enabled state, and manual-send variables are a
  protobuf `map<string,string>` matching REST.
- Atlas label generation uses the existing authorized renderer. It returns the
  exact SVG/PDF bytes plus batch, immutable template, checksum, media, physical
  count, creation time, and replay metadata. ZPL remains absent from the public
  enum and runtime.
- Signals pending-delivery and attempt-result operations now have protected
  REST/OpenAPI routes as well as gRPC methods, preserving the provider-neutral
  Reach seam without exposing URLs, credentials, or provider response bodies.

## Automated evidence

- `internal/grpcapi/server_test.go` compares descriptor and runtime registration
  for all 16 services and 154 RPCs, and verifies exact Bearer parsing plus
  client cookie/origin/CSRF stripping, pre-decode authentication, public-request
  limits through a real transport with preserved `ResourceExhausted` status,
  route/OpenAPI coverage, opaque Struct keys, strict mutation projections,
  zero-byte Vault content, cancellation, and binary envelope edges.
- `internal/application/grpc_runtime_test.go` invokes every declared RPC through
  a real in-memory gRPC connection and rejects any `Unimplemented`, `Unknown`,
  or adapter `Internal` result. It also exercises Guard bootstrap/session/logout,
  Atlas asset and Code 128 association/resolve/label SVG, Patterns CSV, Vault
  multipart create and verified download, and Exchange binary export/conflict.
- `internal/application/bridge_grpc_test.go` retains state-level REST parity for
  Bridge cursor paging, client/grant creation, revocation, and expired session
  behavior through the all-service runtime.
- `cmd/stewardmesh-grpc/main_test.go` exercises the production registration
  split, public unary health checks with streaming health disabled, pre-decode message/header/concurrency limits,
  protected bearer authentication, Bridge compatibility, TLS 1.3 enforcement,
  startup policy, graceful cleanup, and in-memory secret scrubbing.
- `internal/httpapi/server_test.go` covers authentication, permission, browser
  CSRF, bounded reads, and sanitized attempt writes for the Signals delivery
  seam.
- OpenAPI lint, protobuf descriptor generation, `go test -race ./...`, `go vet
  ./...`, traceability, and the repository's PostgreSQL provider tests are the
  release gates.

The all-domain runtime was activated on 2026-08-13 after the listener split and
command-level controls above were implemented. Pull-request CI remains the
publication gate. Exact integrated results belong in the
[phase-one release record](phase-one-release.md).
