-- StewardMesh Threads -- durable tags, goals, provenance rules, and links
-- Requirement: REQ-THREADS-001. Feature: goals.tags.

CREATE TABLE threads_tags (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    parent_id TEXT,
    inherit_by_default BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, parent_id)
        REFERENCES threads_tags (organization_id, id),
    CHECK (parent_id IS NULL OR parent_id <> id),
    CHECK (updated_at >= created_at),
    UNIQUE (organization_id, normalized_name)
);

CREATE INDEX threads_tags_parent_idx
    ON threads_tags (organization_id, parent_id, normalized_name, id);

CREATE TABLE threads_goals (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    parent_id TEXT,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, parent_id)
        REFERENCES threads_goals (organization_id, id),
    CHECK (parent_id IS NULL OR parent_id <> id),
    CHECK (updated_at >= created_at),
    UNIQUE (organization_id, normalized_name)
);

CREATE INDEX threads_goals_parent_idx
    ON threads_goals (organization_id, parent_id, normalized_name, id);

CREATE TABLE threads_tag_rules (
    organization_id TEXT NOT NULL,
    target_type TEXT NOT NULL CHECK (target_type IN (
        'asset', 'purchase', 'contract', 'software', 'budget', 'goal'
    )),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    tag_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('include', 'suppress')),
    revision BIGINT NOT NULL CHECK (revision > 0),
    updated_by TEXT NOT NULL CHECK (char_length(updated_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, target_type, target_id, tag_id),
    FOREIGN KEY (organization_id, tag_id)
        REFERENCES threads_tags (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX threads_tag_rules_target_idx
    ON threads_tag_rules (organization_id, target_type, target_id, mode, tag_id);

CREATE TABLE threads_goal_links (
    organization_id TEXT NOT NULL,
    goal_id TEXT NOT NULL,
    target_type TEXT NOT NULL CHECK (target_type IN ('asset', 'purchase')),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, target_type, target_id, goal_id),
    FOREIGN KEY (organization_id, goal_id)
        REFERENCES threads_goals (organization_id, id)
);

CREATE INDEX threads_goal_links_goal_idx
    ON threads_goal_links (organization_id, goal_id, target_type, target_id);
