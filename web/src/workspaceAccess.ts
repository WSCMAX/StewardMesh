// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

export type ScopeKind = 'organization' | 'site' | 'department' | 'resource'

export type SessionGrant = {
  permission: string
  scope: {
    kind: ScopeKind
    resourceId: string
  }
}

export type PermissionAccess = {
  level: 'none' | 'scoped' | 'organization'
  scopeKinds: ScopeKind[]
  scopeCount: number
}

export function permissionAccess(grants: readonly SessionGrant[], permission: string): PermissionAccess {
  const matching = grants.filter((grant) => grant.permission === permission)
  if (matching.some((grant) => grant.scope.kind === 'organization')) {
    return { level: 'organization', scopeKinds: ['organization'], scopeCount: 1 }
  }
  const scoped = matching.filter((grant) => grant.scope.kind !== 'organization')
  const scopeKinds = [...new Set(scoped.map((grant) => grant.scope.kind))].sort()
  return { level: scoped.length > 0 ? 'scoped' : 'none', scopeKinds, scopeCount: scoped.length }
}

export const meshReadPermissions = [
  'directory.read',
  'assets.read',
  'finance.read',
  'software.read',
  'labels.read',
  'goals.read',
  'storage.read',
  'planning.read',
] as const

export function anyPermissionAccess(grants: readonly SessionGrant[], permissions: readonly string[]): PermissionAccess {
  const accesses = permissions.map((permission) => permissionAccess(grants, permission))
  if (accesses.some((access) => access.level === 'organization')) {
    return { level: 'organization', scopeKinds: ['organization'], scopeCount: 1 }
  }
  const scoped = accesses.filter((access) => access.level === 'scoped')
  if (scoped.length === 0) return { level: 'none', scopeKinds: [], scopeCount: 0 }
  const scopeKinds = [...new Set(scoped.flatMap((access) => access.scopeKinds))].sort()
  const scopeCount = scoped.reduce((total, access) => total + access.scopeCount, 0)
  return { level: 'scoped', scopeKinds, scopeCount }
}

export function scopeSummary(access: PermissionAccess) {
  if (access.level === 'organization') return 'Organization-wide'
  if (access.level === 'none') return 'Not granted'
  const labels = access.scopeKinds.map((kind) => kind === 'department' ? 'department' : kind).join(', ')
  return `${access.scopeCount} scoped ${access.scopeCount === 1 ? 'grant' : 'grants'} (${labels})`
}
