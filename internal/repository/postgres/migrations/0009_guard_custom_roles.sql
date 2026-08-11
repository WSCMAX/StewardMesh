-- StewardMesh Guard custom roles
-- Requirement: SEC-GUARD-001.
ALTER TABLE guard_roles
    ADD COLUMN source TEXT NOT NULL DEFAULT 'builtin'
    CHECK (source IN ('builtin', 'local'));

CREATE UNIQUE INDEX guard_roles_organization_normalized_name_idx
    ON guard_roles (organization_id, lower(btrim(name)));
