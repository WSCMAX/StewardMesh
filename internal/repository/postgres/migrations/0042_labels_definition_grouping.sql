-- Tag definition hierarchy and strategic goal links.
-- Requirement: REQ-LABELS-001. Feature: identity.labels.

ALTER TABLE labels_definitions
    ADD COLUMN parent_id TEXT,
    ADD COLUMN goal_id TEXT,
    ADD CONSTRAINT labels_definitions_parent_fkey
        FOREIGN KEY (organization_id, parent_id)
        REFERENCES labels_definitions (organization_id, id),
    ADD CONSTRAINT labels_definitions_goal_fkey
        FOREIGN KEY (organization_id, goal_id)
        REFERENCES threads_goals (organization_id, id),
    ADD CONSTRAINT labels_definitions_parent_not_self
        CHECK (parent_id IS NULL OR parent_id <> id);

CREATE INDEX labels_definitions_parent_idx
    ON labels_definitions (organization_id, parent_id, normalized_name, id);

CREATE INDEX labels_definitions_goal_idx
    ON labels_definitions (organization_id, goal_id, normalized_name, id);
