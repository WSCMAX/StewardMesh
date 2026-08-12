-- Grant existing built-in administrators Horizon planning permissions.
-- Custom roles remain unchanged and require an explicit administrator choice.
-- Requirements: REQ-HORIZON-001, SEC-GUARD-001. Feature: lifecycle.planning.

INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, permission.name
FROM guard_role_policy_bundles rb
JOIN guard_roles r
  ON r.organization_id = rb.organization_id
 AND r.id = rb.role_id
CROSS JOIN (VALUES ('planning.read'), ('planning.write')) AS permission(name)
WHERE r.source = 'builtin'
  AND lower(btrim(r.name)) = 'administrator'
ON CONFLICT (bundle_id, permission) DO NOTHING;
