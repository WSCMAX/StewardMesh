-- REQ-DIRECTORY-EXPANSION-005 / REQ-EXCHANGE-001 / migration.packages
-- Managed Directory IDs are stable within an organization. Exchange must be
-- able to preserve the same source IDs in two destination organizations that
-- share one PostgreSQL database.

-- The original composite UNIQUE constraints already own indexes with the key
-- shape Exchange needs. PostgreSQL cannot attach a primary-key constraint to
-- an index while that index is still owned by a UNIQUE constraint, so remove
-- the redundant constraint before creating the replacement primary key. The
-- group foreign key is recreated around that operation because it depends on
-- the original group composite-unique constraint.
ALTER TABLE directory_managed_memberships
    DROP CONSTRAINT directory_managed_memberships_organization_id_group_id_fkey;

ALTER TABLE directory_managed_groups
    DROP CONSTRAINT directory_managed_groups_pkey,
    DROP CONSTRAINT directory_managed_groups_organization_id_id_key,
    ADD CONSTRAINT directory_managed_groups_pkey PRIMARY KEY (organization_id, id);

ALTER TABLE directory_managed_memberships
    DROP CONSTRAINT directory_managed_memberships_pkey,
    DROP CONSTRAINT directory_managed_memberships_organization_id_id_key,
    ADD CONSTRAINT directory_managed_memberships_pkey PRIMARY KEY (organization_id, id),
    ADD CONSTRAINT directory_managed_memberships_organization_id_group_id_fkey
        FOREIGN KEY (organization_id, group_id)
        REFERENCES directory_managed_groups (organization_id, id)
        ON DELETE CASCADE;
