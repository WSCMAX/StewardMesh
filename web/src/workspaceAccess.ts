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

export function scopeSummary(access: PermissionAccess) {
  if (access.level === 'organization') return 'Organization-wide'
  if (access.level === 'none') return 'Not granted'
  const labels = access.scopeKinds.map((kind) => kind === 'department' ? 'department' : kind).join(', ')
  return `${access.scopeCount} scoped ${access.scopeCount === 1 ? 'grant' : 'grants'} (${labels})`
}
