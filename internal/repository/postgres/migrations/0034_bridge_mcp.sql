-- Bridge MCP, OAuth, confirmation, and abuse-control persistence.
-- Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

CREATE TABLE bridge_oauth_clients (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    redirect_uris JSONB NOT NULL,
    allowed_scopes TEXT[] NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    CONSTRAINT bridge_oauth_clients_id_check CHECK (id ~ '^[a-f0-9]{32}$'),
    CONSTRAINT bridge_oauth_clients_name_check CHECK (char_length(name) BETWEEN 1 AND 120),
    CONSTRAINT bridge_oauth_clients_redirects_check CHECK (jsonb_typeof(redirect_uris) = 'array' AND jsonb_array_length(redirect_uris) BETWEEN 1 AND 10),
    CONSTRAINT bridge_oauth_clients_scopes_check CHECK (cardinality(allowed_scopes) BETWEEN 1 AND 5),
    CONSTRAINT bridge_oauth_clients_revoked_check CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE UNIQUE INDEX bridge_oauth_clients_active_name_unique
    ON bridge_oauth_clients (organization_id, lower(name)) WHERE revoked_at IS NULL;

CREATE TABLE bridge_oauth_authorization_requests (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    resource_uri TEXT NOT NULL,
    scopes TEXT[] NOT NULL,
    oauth_state TEXT,
    code_challenge TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ,
    approved BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, client_id) REFERENCES bridge_oauth_clients(organization_id, id) ON DELETE CASCADE,
    CONSTRAINT bridge_oauth_authorization_requests_id_check CHECK (id ~ '^[a-f0-9]{32}$'),
    CONSTRAINT bridge_oauth_authorization_requests_scopes_check CHECK (cardinality(scopes) BETWEEN 1 AND 5),
    CONSTRAINT bridge_oauth_authorization_requests_challenge_check CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    CONSTRAINT bridge_oauth_authorization_requests_expiry_check CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '15 minutes'),
    CONSTRAINT bridge_oauth_authorization_requests_decision_check CHECK ((decided_at IS NULL AND approved = FALSE) OR decided_at >= created_at)
);

CREATE INDEX bridge_oauth_authorization_requests_expiry_idx
    ON bridge_oauth_authorization_requests (organization_id, expires_at);

CREATE TABLE bridge_oauth_authorization_codes (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    resource_uri TEXT NOT NULL,
    scopes TEXT[] NOT NULL,
    code_hash BYTEA NOT NULL,
    code_challenge TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, request_id) REFERENCES bridge_oauth_authorization_requests(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, client_id) REFERENCES bridge_oauth_clients(organization_id, id) ON DELETE CASCADE,
    CONSTRAINT bridge_oauth_authorization_codes_hash_check CHECK (octet_length(code_hash) = 32),
    CONSTRAINT bridge_oauth_authorization_codes_expiry_check CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '5 minutes'),
    CONSTRAINT bridge_oauth_authorization_codes_consumed_check CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    UNIQUE (organization_id, code_hash)
);

CREATE TABLE bridge_oauth_grants (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    resource_uri TEXT NOT NULL,
    scopes TEXT[] NOT NULL,
    access_token_hash BYTEA NOT NULL,
    refresh_token_hash BYTEA NOT NULL,
    access_expires_at TIMESTAMPTZ NOT NULL,
    refresh_expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, client_id) REFERENCES bridge_oauth_clients(organization_id, id) ON DELETE CASCADE,
    CONSTRAINT bridge_oauth_grants_hash_check CHECK (octet_length(access_token_hash) = 32 AND octet_length(refresh_token_hash) = 32),
    CONSTRAINT bridge_oauth_grants_scopes_check CHECK (cardinality(scopes) BETWEEN 1 AND 5),
    CONSTRAINT bridge_oauth_grants_expiry_check CHECK (access_expires_at > created_at AND refresh_expires_at >= access_expires_at AND refresh_expires_at <= created_at + INTERVAL '24 hours'),
    CONSTRAINT bridge_oauth_grants_revoked_check CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    UNIQUE (organization_id, access_token_hash),
    UNIQUE (organization_id, refresh_token_hash)
);

CREATE INDEX bridge_oauth_grants_actor_idx ON bridge_oauth_grants (organization_id, actor_id, created_at DESC);

CREATE TABLE bridge_mcp_confirmations (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    arguments_hash BYTEA NOT NULL,
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    CONSTRAINT bridge_mcp_confirmations_action_check CHECK (action ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    CONSTRAINT bridge_mcp_confirmations_hash_check CHECK (octet_length(arguments_hash) = 32 AND octet_length(token_hash) = 32),
    CONSTRAINT bridge_mcp_confirmations_expiry_check CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '5 minutes'),
    CONSTRAINT bridge_mcp_confirmations_consumed_check CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    UNIQUE (organization_id, token_hash)
);

CREATE INDEX bridge_mcp_confirmations_expiry_idx ON bridge_mcp_confirmations (organization_id, expires_at);

CREATE TABLE bridge_rate_windows (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key_hash BYTEA NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    count INTEGER NOT NULL,
    PRIMARY KEY (organization_id, key_hash, window_start),
    CONSTRAINT bridge_rate_windows_hash_check CHECK (octet_length(key_hash) = 32),
    CONSTRAINT bridge_rate_windows_count_check CHECK (count BETWEEN 1 AND 10000)
);
