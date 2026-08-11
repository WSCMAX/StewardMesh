-- StewardMesh Guard — SAML 2.0 SP request tracking and JIT provisioning
-- Requirements: SEC-GUARD-001, SEC-HTTP-001
ALTER TABLE guard_role_assignments
    DROP CONSTRAINT guard_role_assignments_source_check;

ALTER TABLE guard_role_assignments
    ADD CONSTRAINT guard_role_assignments_source_check
    CHECK (source = 'local' OR source ~ '^(oidc|saml):[a-f0-9]{32}$');

CREATE TABLE guard_saml_requests (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    state_hash BYTEA NOT NULL CHECK (octet_length(state_hash) = 32),
    request_id TEXT NOT NULL CHECK (char_length(request_id) BETWEEN 1 AND 512),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, state_hash),
    CHECK (expires_at > created_at)
);

CREATE INDEX guard_saml_requests_expiry_idx
    ON guard_saml_requests (organization_id, expires_at);
