-- Grant existing built-in administrators Labels mutation permissions.
-- Requirements: REQ-LABELS-001, SEC-GUARD-001. Feature: identity.labels.

INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, permission
FROM guard_role_policy_bundles rb
JOIN guard_roles r
  ON r.organization_id = rb.organization_id
 AND r.id = rb.role_id
CROSS JOIN (VALUES ('labels.read'), ('labels.write')) AS permissions(permission)
WHERE r.source = 'builtin'
  AND lower(btrim(r.name)) = 'administrator'
ON CONFLICT (bundle_id, permission) DO NOTHING;
