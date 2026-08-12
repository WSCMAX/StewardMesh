import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'

// Requirements: REQ-HORIZON-001, SEC-GUARD-001, SEC-HTTP-001, A11Y-001.
// Features: lifecycle.planning, authorization.security.

type ScopeKind = 'organization' | 'site' | 'department' | 'resource'

type GuardAccount = {
  id: string
  username: string
  email: string
  displayName: string
  status: string
}

type GuardRole = {
  id: string
  name: string
  description: string
  permissions: string[]
  policyBundleIds: string[]
  source: 'builtin' | 'local'
  managed: boolean
}

type PolicyBundle = {
  id: string
  name: string
  description: string
  permissions: string[]
}

type RoleAssignment = {
  id: string
  accountId: string
  roleId: string
  scope: { kind: ScopeKind; resourceId: string }
  source: string
  managed: boolean
  createdAt: string
}

type AuthorizationDirectory = {
  accounts: GuardAccount[]
  roles: GuardRole[]
  policyBundles: PolicyBundle[]
  availablePermissions: string[]
  assignments: RoleAssignment[]
}

type GuardAccessManagerProps = {
  csrfToken: string
  onOpenHelp?: () => void
}

const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2.5 text-steward-mist transition hover:border-steward-blue disabled:cursor-not-allowed disabled:opacity-60'
const labelClass = 'block text-sm font-semibold text-steward-mist-muted'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2.5 font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const secondaryButtonClass = 'min-h-11 rounded-lg border border-steward-ink-800 bg-steward-ink-900 px-4 py-2.5 text-sm font-semibold text-steward-mist transition hover:border-steward-blue hover:bg-steward-ink-800'
const dangerButtonClass = 'min-h-11 rounded-lg border border-steward-danger/60 px-3 py-2 text-sm font-semibold text-[#ffadb5] transition hover:bg-steward-danger/15 disabled:cursor-wait disabled:opacity-60'

const scopeLabels: Record<ScopeKind, string> = {
  organization: 'Entire organization',
  site: 'One site',
  department: 'One department',
  resource: 'One resource',
}

const permissionLabels: Record<string, string> = {
  'organization.read': 'View organization details',
  'assets.read': 'View assets',
  'assets.write': 'Create and update assets',
  'directory.read': 'View people and locations',
  'directory.write': 'Create and update people and locations',
  'goals.read': 'View goals and tags',
  'goals.write': 'Manage goals, tags, and relationships',
  'storage.read': 'View and download Vault files',
  'storage.write': 'Upload files to Vault',
  'finance.read': 'View Ledger financial records',
  'finance.write': 'Manage Ledger financial records',
  'planning.read': 'View Horizon lifecycle plans and forecasts',
  'planning.write': 'Manage Horizon lifecycle plans',
  'guard.manage': 'Manage Guard access',
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}

function isScopeKind(value: unknown): value is ScopeKind {
  return value === 'organization' || value === 'site' || value === 'department' || value === 'resource'
}

function isAccount(value: unknown): value is GuardAccount {
  if (typeof value !== 'object' || value === null) return false
  const account = value as Record<string, unknown>
  return isString(account.id) && account.id.length > 0
    && isString(account.username) && isString(account.email)
    && isString(account.displayName) && account.displayName.length > 0
    && isString(account.status)
}

function isRole(value: unknown): value is GuardRole {
  if (typeof value !== 'object' || value === null) return false
  const role = value as Record<string, unknown>
  return isString(role.id) && role.id.length > 0
    && isString(role.name) && role.name.length > 0 && isString(role.description)
    && Array.isArray(role.permissions) && role.permissions.every(isString)
    && Array.isArray(role.policyBundleIds) && role.policyBundleIds.every(isString)
    && (role.source === 'builtin' || role.source === 'local') && typeof role.managed === 'boolean'
}

function isPolicyBundle(value: unknown): value is PolicyBundle {
  if (typeof value !== 'object' || value === null) return false
  const bundle = value as Record<string, unknown>
  return isString(bundle.id) && bundle.id.length > 0 && isString(bundle.name) && bundle.name.length > 0
    && isString(bundle.description) && Array.isArray(bundle.permissions) && bundle.permissions.every(isString)
}

function isAssignment(value: unknown): value is RoleAssignment {
  if (typeof value !== 'object' || value === null) return false
  const assignment = value as Record<string, unknown>
  if (typeof assignment.scope !== 'object' || assignment.scope === null) return false
  const scope = assignment.scope as Record<string, unknown>
  return isString(assignment.id) && assignment.id.length > 0
    && isString(assignment.accountId) && isString(assignment.roleId)
    && isScopeKind(scope.kind) && isString(scope.resourceId) && scope.resourceId.length > 0
    && isString(assignment.source) && typeof assignment.managed === 'boolean'
    && isString(assignment.createdAt)
}

function readDirectory(value: unknown): AuthorizationDirectory {
  if (typeof value !== 'object' || value === null) throw new Error('invalid Guard access response')
  const directory = value as Record<string, unknown>
  if (!Array.isArray(directory.accounts) || !directory.accounts.every(isAccount)
    || !Array.isArray(directory.roles) || !directory.roles.every(isRole)
    || !Array.isArray(directory.policyBundles) || !directory.policyBundles.every(isPolicyBundle)
    || !Array.isArray(directory.availablePermissions) || !directory.availablePermissions.every(isString)
    || !Array.isArray(directory.assignments) || !directory.assignments.every(isAssignment)) {
    throw new Error('invalid Guard access response')
  }
  return {
    accounts: directory.accounts,
    roles: directory.roles,
    policyBundles: directory.policyBundles,
    availablePermissions: directory.availablePermissions,
    assignments: directory.assignments,
  }
}

function formatScope(assignment: RoleAssignment) {
  if (assignment.scope.kind === 'organization') return scopeLabels.organization
  return `${scopeLabels[assignment.scope.kind]} · ${assignment.scope.resourceId}`
}

export default function GuardAccessManager({ csrfToken, onOpenHelp }: GuardAccessManagerProps) {
  const [directory, setDirectory] = useState<AuthorizationDirectory>({ accounts: [], roles: [], policyBundles: [], availablePermissions: [], assignments: [] })
  const [scopeKind, setScopeKind] = useState<ScopeKind>('organization')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const accountNames = useMemo(() => new Map(directory.accounts.map((account) => [account.id, account.displayName])), [directory.accounts])
  const roleNames = useMemo(() => new Map(directory.roles.map((role) => [role.id, role.name])), [directory.roles])
  const bundleNames = useMemo(() => new Map(directory.policyBundles.map((bundle) => [bundle.id, bundle.name])), [directory.policyBundles])

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const loadAccess = useCallback(async (signal?: AbortSignal) => {
    const response = await requestJSON('/api/v1/guard/access', { signal })
    setDirectory(readDirectory(response))
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    loadAccess(controller.signal)
      .catch((loadError: unknown) => {
        if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) {
          setError(loadError instanceof ApiRequestError ? loadError.message : 'Guard access could not be loaded.')
        }
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [loadAccess])

  function reportError(mutationError: unknown, fallback: string) {
    setStatus('')
    setError(mutationError instanceof ApiRequestError ? mutationError.message : fallback)
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('create')
    setError('')
    setStatus('')
    try {
      const response = await requestJSON('/api/v1/guard/role-assignments', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          accountId: String(values.get('guardAccountId') ?? ''),
          roleId: String(values.get('guardRoleId') ?? ''),
          scope: {
            kind: scopeKind,
            resourceId: scopeKind === 'organization' ? '' : String(values.get('guardResourceId') ?? '').trim(),
          },
        }),
      })
      if (!isAssignment(response)) throw new Error('invalid role assignment response')
      await loadAccess()
      form.reset()
      setScopeKind('organization')
      setStatus('Scoped role assignment created.')
    } catch (mutationError) {
      reportError(mutationError, 'The scoped role assignment could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const permissions = values.getAll('guardPermission').map(String)
    const policyBundleIds = values.getAll('guardPolicyBundleId').map(String)
    if (permissions.length === 0 && policyBundleIds.length === 0) {
      setStatus('')
      setError('Select at least one direct permission or reusable policy bundle.')
      return
    }
    setBusy('create-role')
    setError('')
    setStatus('')
    try {
      const response = await requestJSON('/api/v1/guard/roles', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('guardRoleName') ?? '').trim(),
          description: String(values.get('guardRoleDescription') ?? '').trim(),
          permissions,
          policyBundleIds,
        }),
      })
      if (!isRole(response)) throw new Error('invalid role response')
      await loadAccess()
      form.reset()
      setStatus('Custom role created and ready to assign.')
    } catch (mutationError) {
      reportError(mutationError, 'The custom role could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleRevoke(assignment: RoleAssignment) {
    setBusy(assignment.id)
    setError('')
    setStatus('')
    try {
      await requestJSON(`/api/v1/guard/role-assignments/${encodeURIComponent(assignment.id)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken },
      })
      await loadAccess()
      setStatus('Role assignment removed.')
    } catch (mutationError) {
      reportError(mutationError, 'The role assignment could not be removed.')
    } finally {
      setBusy('')
    }
  }

  const activeAccounts = directory.accounts.filter((account) => account.status === 'active')

  return (
    <section aria-labelledby="guard-access-heading" className="rounded-xl border border-steward-teal/30 bg-steward-ink-900 p-6" data-feature="authorization.security" data-requirement="SEC-GUARD-001">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div><p className="text-sm font-semibold text-steward-teal">Guard · Access administration</p><h2 className="mt-2 text-2xl font-semibold" id="guard-access-heading">Build roles and assign the right access</h2><p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">Combine direct permissions with reusable policy bundles, then apply a role to the whole organization or limit it to a site, department, or resource. Built-in roles stay protected.</p></div>
        {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Guard help</button>}
      </div>

      {error && <div className="mt-5 rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      <p aria-live="polite" className="mt-4 text-sm text-[#67dd99]" role="status">{status}</p>

      {loading ? <p className="mt-5 text-steward-mist-muted" role="status">Loading Guard access…</p> : (
        <>
          <div className="mt-5 grid gap-3 sm:grid-cols-3">
            <Summary label="Accounts" value={directory.accounts.length} />
            <Summary label="Roles" value={directory.roles.length} />
            <Summary label="Assignments" value={directory.assignments.length} />
          </div>

          <details className="mt-6 rounded-xl border border-steward-teal/40 bg-steward-ink-950/40 p-5">
            <summary className="cursor-pointer text-base font-semibold text-steward-mist">Create a custom role</summary>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Give people only the capabilities they need. Direct permissions and policy bundles are combined when Guard evaluates access.</p>
            <form className="mt-5 grid gap-5 lg:grid-cols-2" onSubmit={handleCreateRole}>
              <div>
                <label className={labelClass} htmlFor="guardRoleName">Role name</label>
                <input autoComplete="off" className={inputClass} disabled={busy !== ''} id="guardRoleName" maxLength={120} name="guardRoleName" required />
              </div>
              <div>
                <label className={labelClass} htmlFor="guardRoleDescription">Description <span className="font-normal">(optional)</span></label>
                <textarea className={`${inputClass} min-h-24 resize-y`} disabled={busy !== ''} id="guardRoleDescription" maxLength={1000} name="guardRoleDescription" />
              </div>
              <fieldset className="rounded-xl border border-steward-ink-800 p-4">
                <legend className="px-1 text-sm font-semibold text-steward-mist">Direct permissions</legend>
                <div className="mt-2 grid gap-3">
                  {directory.availablePermissions.map((permission) => (
                    <label className="flex min-h-11 items-start gap-3 rounded-lg px-2 py-2 text-sm transition hover:bg-steward-ink-800/60" key={permission}>
                      <input className="mt-1 size-4 accent-steward-teal" disabled={busy !== ''} name="guardPermission" type="checkbox" value={permission} />
                      <span><span className="block font-medium text-steward-mist">{permissionLabels[permission] ?? permission}</span><span className="mt-0.5 block font-mono text-xs text-steward-mist-muted">{permission}</span></span>
                    </label>
                  ))}
                </div>
              </fieldset>
              <fieldset className="rounded-xl border border-steward-ink-800 p-4">
                <legend className="px-1 text-sm font-semibold text-steward-mist">Reusable policy bundles</legend>
                {directory.policyBundles.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No policy bundles are available.</p> : (
                  <div className="mt-2 grid gap-3">
                    {directory.policyBundles.map((bundle) => (
                      <label className="flex min-h-11 items-start gap-3 rounded-lg px-2 py-2 text-sm transition hover:bg-steward-ink-800/60" key={bundle.id}>
                        <input className="mt-1 size-4 accent-steward-teal" disabled={busy !== ''} name="guardPolicyBundleId" type="checkbox" value={bundle.id} />
                        <span><span className="block font-medium text-steward-mist">{bundle.name}</span><span className="mt-0.5 block leading-5 text-steward-mist-muted">{bundle.description || `${bundle.permissions.length} permissions`}</span></span>
                      </label>
                    ))}
                  </div>
                )}
              </fieldset>
              <div className="lg:col-span-2">
                <button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'create-role' ? 'Creating role…' : 'Create custom role'}</button>
              </div>
            </form>
          </details>

          <div className="mt-6">
            <h3 className="text-lg font-semibold">Current roles</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Built-in roles are read only. Custom roles can be assigned at any supported scope.</p>
            <div className="mt-4 grid gap-4 lg:grid-cols-2">
              {directory.roles.map((role) => (
                <article className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/30 p-4" key={role.id}>
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div><h4 className="font-semibold text-steward-mist">{role.name}</h4><p className="mt-1 text-sm leading-6 text-steward-mist-muted">{role.description || 'No description provided.'}</p></div>
                    <span className="rounded-full border border-steward-teal/40 px-2.5 py-1 text-xs font-semibold text-steward-teal">{role.managed ? 'Built in · protected' : 'Custom role'}</span>
                  </div>
                  <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
                    <div><dt className="font-semibold text-steward-mist-muted">Direct permissions</dt><dd className="mt-1 break-words text-steward-mist">{role.permissions.length > 0 ? role.permissions.map((permission) => permissionLabels[permission] ?? permission).join(', ') : 'None'}</dd></div>
                    <div><dt className="font-semibold text-steward-mist-muted">Policy bundles</dt><dd className="mt-1 break-words text-steward-mist">{role.policyBundleIds.length > 0 ? role.policyBundleIds.map((id) => bundleNames.get(id) ?? 'Unknown bundle').join(', ') : 'None'}</dd></div>
                  </dl>
                </article>
              ))}
            </div>
          </div>

          <details className="mt-6 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-5">
            <summary className="cursor-pointer text-base font-semibold text-steward-mist">Add a scoped role assignment</summary>
            <form className="mt-5 grid gap-4 lg:grid-cols-2" onSubmit={handleCreate}>
              <div>
                <label className={labelClass} htmlFor="guardAccountId">Account</label>
                <select className={inputClass} disabled={busy !== '' || activeAccounts.length === 0} id="guardAccountId" name="guardAccountId" required>
                  {activeAccounts.map((account) => <option key={account.id} value={account.id}>{account.displayName} · {account.username}</option>)}
                </select>
              </div>
              <div>
                <label className={labelClass} htmlFor="guardRoleId">Role</label>
                <select className={inputClass} disabled={busy !== '' || directory.roles.length === 0} id="guardRoleId" name="guardRoleId" required>
                  {directory.roles.map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}
                </select>
              </div>
              <div>
                <label className={labelClass} htmlFor="guardScopeKind">Access scope</label>
                <select className={inputClass} disabled={busy !== ''} id="guardScopeKind" onChange={(event) => setScopeKind(event.target.value as ScopeKind)} value={scopeKind}>
                  {Object.entries(scopeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                </select>
              </div>
              {scopeKind !== 'organization' && (
                <div>
                  <label className={labelClass} htmlFor="guardResourceId">Scoped resource ID</label>
                  <p className="mt-1 text-sm text-steward-mist-muted">Enter the exact {scopeKind} ID from StewardMesh.</p>
                  <input className={inputClass} disabled={busy !== ''} id="guardResourceId" maxLength={128} name="guardResourceId" required />
                </div>
              )}
              <div className="flex items-end lg:col-span-2">
                <button className={buttonClass} disabled={busy !== '' || activeAccounts.length === 0 || directory.roles.length === 0} type="submit">{busy === 'create' ? 'Assigning role…' : 'Assign role'}</button>
              </div>
            </form>
          </details>

          <div className="mt-6">
            <h3 className="text-lg font-semibold">Current role assignments</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Guard prevents removal of the final active organization administrator.</p>
            {directory.assignments.length === 0 ? <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">No role assignments are available.</p> : (
              <div className="mt-4 rounded-xl border border-steward-ink-800">
                <table className="w-full border-collapse text-left text-sm">
                  <thead className="hidden bg-steward-ink-800/60 text-steward-mist-muted md:table-header-group"><tr><th className="px-4 py-3 font-semibold" scope="col">Account</th><th className="px-4 py-3 font-semibold" scope="col">Role</th><th className="px-4 py-3 font-semibold" scope="col">Scope</th><th className="px-4 py-3 font-semibold" scope="col">Managed by</th><th className="px-4 py-3 font-semibold" scope="col"><span className="sr-only">Actions</span></th></tr></thead>
                  <tbody className="block divide-y divide-steward-ink-800 md:table-row-group">
                    {directory.assignments.map((assignment) => (
                      <tr className="grid grid-cols-2 gap-4 p-4 md:table-row md:p-0" key={assignment.id}>
                        <td className="font-medium md:px-4 md:py-3"><MobileLabel>Account</MobileLabel>{accountNames.get(assignment.accountId) ?? 'Unknown account'}</td>
                        <td className="md:px-4 md:py-3"><MobileLabel>Role</MobileLabel>{roleNames.get(assignment.roleId) ?? 'Unknown role'}</td>
                        <td className="text-steward-mist-muted md:px-4 md:py-3"><MobileLabel>Scope</MobileLabel>{formatScope(assignment)}</td>
                        <td className="text-steward-mist-muted md:px-4 md:py-3"><MobileLabel>Managed by</MobileLabel>{assignment.managed ? 'Identity provider' : 'StewardMesh'}</td>
                        <td className="col-span-2 md:table-cell md:px-4 md:py-3 md:text-right">{assignment.managed ? <span className="text-xs text-steward-mist-muted">Read only</span> : <button aria-label={`Remove ${roleNames.get(assignment.roleId) ?? 'role'} assignment for ${accountNames.get(assignment.accountId) ?? 'account'}`} className={dangerButtonClass} disabled={busy !== ''} onClick={() => handleRevoke(assignment)} type="button">{busy === assignment.id ? 'Removing…' : 'Remove'}</button>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}
    </section>
  )
}

function Summary({ label, value }: { label: string; value: number }) {
  return <div className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4"><p className="text-2xl font-semibold">{value}</p><p className="mt-1 text-sm text-steward-mist-muted">{label}</p></div>
}

function MobileLabel({ children }: { children: string }) {
  return <span className="mb-1 block text-xs font-semibold text-steward-mist-muted md:hidden">{children}</span>
}
