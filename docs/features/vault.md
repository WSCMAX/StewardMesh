# Vault — Private files and evidence

- **Canonical ID:** `storage.blobs`
- **Requirement:** `REQ-STORAGE-001`
- **GitHub issue:** [#10 — Vault file storage abstraction](https://github.com/WSCMAX/StewardMesh/issues/10)

## Purpose

Vault stores organization-owned attachments, evidence, and export inputs behind one provider-neutral contract. PostgreSQL or the memory repository holds searchable metadata while the object adapter holds file bytes. Local development uses a hardened filesystem adapter; shared deployments use the S3-compatible adapter without changing the service, API, or metadata repository.

Durable records contain the organization, server-generated ID, display name, MIME type, byte size, SHA-256 checksum, provider name, actor and timestamp, optional source-system provenance, and an optional related resource. Object keys stay private to the repository boundary. Credentials and temporary download URLs are never record fields, API exports, audit metadata, or browser persistence.

## Roles and permissions

- `storage.read` lists metadata, reads a specific record, downloads content through the authenticated application, and requests a short-lived provider authorization.
- `storage.write` uploads a file with optional provenance and resource ownership metadata.
- Every browser upload and download-authorization request also requires the in-memory CSRF token and an allowed same-origin request.

Migration `0015_vault_administrator_permissions.sql` adds both Vault permissions only to existing built-in Administrator policy bundles. Custom roles remain unchanged so an administrator must opt them in deliberately.

## Local filesystem adapter

The local adapter accepts only server-generated `{organization-hash}/{random-id}` keys. Absolute paths, traversal, malformed keys, NUL bytes, symbolic-link roots, symbolic-link directories, and non-regular files fail closed. Uploads stream through a context-aware size limiter and SHA-256 hasher into a private temporary file, flush to disk, then commit without overwriting an existing key. Failed, cancelled, oversized, and conflicting writes remove their partial file. Local downloads require both the active Guard session and a process-local, HMAC-signed expiring grant; a restart invalidates all outstanding grants.

Use `STEWARDMESH_STORAGE_DRIVER=local`, `STEWARDMESH_BLOB_DIR`, and `STEWARDMESH_BLOB_MAXIMUM_BYTES`. Local storage is suitable for one application instance and development. It is not a shared filesystem or a horizontal-deployment strategy.

## S3-compatible adapter

Use `STEWARDMESH_STORAGE_DRIVER=s3` with a bucket and region. The adapter uses the AWS SDK default credential chain, so IAM instance or task roles, web/workload identity, and local shared profiles work without putting a secret in StewardMesh records. `STEWARDMESH_S3_ROLE_ARN` adds STS assume-role. Explicit access key, secret key, and optional session token remain available for S3-compatible providers and must come from the deployment secret manager.

Uploads are private by default because Vault never sends a public ACL. Every write includes a known content length, a base64 SHA-256 checksum, `If-None-Match: *`, MIME type, and mandatory server-side encryption:

- `AES256` uses S3-managed encryption.
- `aws:kms` requires `STEWARDMESH_S3_KMS_KEY_ID` and enables an S3 bucket key.

Custom endpoints must be an origin without credentials, query, fragment, or path. Remote endpoints require HTTPS. Plain HTTP is accepted only for loopback development, and unspecified, multicast, or link-local IP endpoints are rejected. The configured endpoint is fixed at startup and cannot be supplied by an upload request. Path-style addressing can be enabled for a compatible provider.

Vault requests S3 checksum validation on reads. Download authorization produces an HTTPS provider URL with a 1-to-15-minute lifetime. The configured default is five minutes. The URL is returned once, audited without its signature, and never persisted.

## API and metadata persistence

REST endpoints:

- `GET|POST /api/v1/blobs`
- `GET /api/v1/blobs/{blobId}`
- `GET /api/v1/blobs/{blobId}/content`
- `POST /api/v1/blobs/{blobId}/download-authorization`

OpenAPI and protobuf contracts carry the same safe metadata, upload, content, and authorization shapes. `storage.ObjectStore` is the local/S3 byte contract, while `storage.MetadataStore` is the memory/PostgreSQL record contract. This separation lets future repository and object providers vary independently.

Migration `0014_vault_blobs.sql` creates organization-scoped metadata, unique private object keys, paired provenance/resource constraints, checksum/provider checks, and a recent-files index. Adapter conformance tests cover tenant isolation, conflicts, traversal, symlinks, cancellation-safe cleanup, size limits, checksums, encryption headers, unsafe endpoints, conditional writes, missing objects, and expiring downloads.

## Audit events

- `vault.blob.created` records safe provider, MIME type, size, checksum, source, and resource metadata.
- `vault.blob.download_authorized` records provider and expiration.

Both include `REQ-STORAGE-001` and `storage.blobs`. They do not contain object keys, file content, access keys, secret keys, session tokens, session cookies, CSRF tokens, or signed URLs.

## Accessible workflow

1. Choose a file and optionally enter a paired source system/record and related resource type/ID.
2. Review the configured upload limit and upload status.
3. Inspect filename, time, provider, MIME type, size, provenance, and the full SHA-256 checksum in the file table.
4. Select **Prepare download**. Vault creates a short-lived authorization only for that file.
5. Follow **Download ready** before the authorization expires.

The interface has semantic headings, a labeled native file input, keyboard-operable actions, non-color status text, an accessible table caption, visible checksums/provenance, minimum-height controls, and horizontal containment for dense metadata at narrow widths.

## Issue reporting

Report problems through the application issue link or GitHub issue #10. Include the safe correlation ID, blob ID, provider name, MIME type, size, and checksum. Never include file content, object keys, credentials, session material, or a temporary download URL.
