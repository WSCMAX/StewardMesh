-- Allow Stack license assignments to lab rooms (device and site entitlements).
-- Requirements: REQ-STACK-001. Features: software.licenses.

ALTER TABLE stack_assignments
    DROP CONSTRAINT stack_assignments_assignee_kind_check,
    ADD CONSTRAINT stack_assignments_assignee_kind_check
        CHECK (assignee_kind IN ('asset', 'identity', 'department', 'site', 'room'));
