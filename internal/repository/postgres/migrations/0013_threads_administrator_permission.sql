-- Grant existing built-in administrators the Threads mutation permission.
-- Custom roles remain unchanged and require an explicit administrator choice.
-- Requirements: REQ-THREADS-001, SEC-GUARD-001. Feature: goals.tags.

INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, 'goals.write'
FROM guard_role_policy_bundles rb
JOIN guard_roles r
  ON r.organization_id = rb.organization_id
 AND r.id = rb.role_id
WHERE r.source = 'builtin'
  AND lower(btrim(r.name)) = 'administrator'
ON CONFLICT (bundle_id, permission) DO NOTHING;
