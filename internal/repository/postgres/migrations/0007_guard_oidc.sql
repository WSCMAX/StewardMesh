-- StewardMesh Guard — OpenID Connect JIT account provisioning
-- Requirements: SEC-GUARD-001, SEC-HTTP-001
ALTER TABLE guard_accounts
    ALTER COLUMN password_hash DROP NOT NULL;

ALTER TABLE guard_accounts
    DROP CONSTRAINT guard_accounts_password_hash_check;

ALTER TABLE guard_accounts
    ADD CONSTRAINT guard_accounts_password_hash_check
    CHECK (password_hash IS NULL OR char_length(password_hash) BETWEEN 32 AND 512);

ALTER TABLE guard_role_assignments
    ADD COLUMN source TEXT NOT NULL DEFAULT 'local'
    CHECK (source = 'local' OR source ~ '^oidc:[a-f0-9]{32}$');

CREATE TABLE guard_external_identities (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    issuer TEXT NOT NULL CHECK (char_length(issuer) BETWEEN 1 AND 2048),
    subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 512),
    account_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, issuer, subject),
    UNIQUE (organization_id, account_id, issuer),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES guard_accounts (organization_id, id) ON DELETE CASCADE,
    CHECK (updated_at >= created_at)
);

CREATE INDEX guard_external_identities_account_idx
    ON guard_external_identities (organization_id, account_id);
