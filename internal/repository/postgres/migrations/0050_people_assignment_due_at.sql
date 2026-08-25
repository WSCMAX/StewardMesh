-- Temporary asset loans record an expected return-by date separately from
-- checkout (effective_from) and the actual returned date (effective_to).
-- Requirement: REQ-PEOPLE-001. Feature: identity.directory.

ALTER TABLE people_asset_assignments
    ADD COLUMN due_at TIMESTAMPTZ;

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_due_at_chk
    CHECK (due_at IS NULL OR due_at >= effective_from);
