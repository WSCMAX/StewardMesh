import { expect, test } from 'vitest'
import { anyPermissionAccess, meshReadPermissions, permissionAccess, scopeSummary, type SessionGrant } from './workspaceAccess'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

const grants: SessionGrant[] = [
  { permission: 'assets.read', scope: { kind: 'site', resourceId: 'site-one' } },
  { permission: 'assets.read', scope: { kind: 'department', resourceId: 'department-one' } },
  { permission: 'assets.write', scope: { kind: 'organization', resourceId: 'example-org' } },
]

test('keeps scoped grants distinct from organization-wide access', () => {
  const read = permissionAccess(grants, 'assets.read')
  expect(read).toEqual({ level: 'scoped', scopeKinds: ['department', 'site'], scopeCount: 2 })
  expect(scopeSummary(read)).toBe('2 scoped grants (department, site)')
  expect(permissionAccess(grants, 'assets.write').level).toBe('organization')
  expect(permissionAccess(grants, 'finance.read').level).toBe('none')
})

test('treats Mesh as available for any product read grant, including scoped access', () => {
  expect(anyPermissionAccess(grants, meshReadPermissions)).toEqual({
    level: 'scoped',
    scopeKinds: ['department', 'site'],
    scopeCount: 2,
  })
  expect(anyPermissionAccess(
    [{ permission: 'finance.read', scope: { kind: 'organization', resourceId: 'example-org' } }],
    meshReadPermissions,
  ).level).toBe('organization')
  expect(anyPermissionAccess(
    [{ permission: 'messaging.read', scope: { kind: 'organization', resourceId: 'example-org' } }],
    meshReadPermissions,
  ).level).toBe('none')
})
