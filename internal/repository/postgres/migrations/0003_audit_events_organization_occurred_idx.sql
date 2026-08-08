-- StewardMesh Foundation
-- Requirement: REQ-FOUNDATION-001
CREATE INDEX audit_events_organization_occurred_idx
    ON audit_events (organization_id, occurred_at DESC);
