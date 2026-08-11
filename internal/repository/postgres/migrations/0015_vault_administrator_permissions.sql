-- Grant existing built-in administrators Vault read and write permissions.
-- Custom roles remain unchanged and require an explicit administrator choice.
-- Requirements: REQ-STORAGE-001, SEC-GUARD-001. Feature: storage.blobs.

INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, permission.name
FROM guard_role_policy_bundles rb
JOIN guard_roles r
  ON r.organization_id = rb.organization_id
 AND r.id = rb.role_id
CROSS JOIN (VALUES ('storage.read'), ('storage.write')) AS permission(name)
WHERE r.source = 'builtin'
  AND lower(btrim(r.name)) = 'administrator'
ON CONFLICT (bundle_id, permission) DO NOTHING;
