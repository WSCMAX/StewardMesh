import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'

// Requirements: SEC-GUARD-001, SEC-HTTP-001, A11Y-001.
// Feature: authorization.security.

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
  assignments: RoleAssignment[]
}

type GuardAccessManagerProps = {
  csrfToken: string
}

const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2.5 text-steward-mist transition hover:border-steward-blue disabled:cursor-not-allowed disabled:opacity-60'
const labelClass = 'block text-sm font-semibold text-steward-mist-muted'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2.5 font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const dangerButtonClass = 'min-h-11 rounded-lg border border-steward-danger/60 px-3 py-2 text-sm font-semibold text-[#ffadb5] transition hover:bg-steward-danger/15 disabled:cursor-wait disabled:opacity-60'

const scopeLabels: Record<ScopeKind, string> = {
  organization: 'Entire organization',
  site: 'One site',
  department: 'One department',
  resource: 'One resource',
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
    || !Array.isArray(directory.assignments) || !directory.assignments.every(isAssignment)) {
    throw new Error('invalid Guard access response')
  }
  return {
    accounts: directory.accounts,
    roles: directory.roles,
    assignments: directory.assignments,
  }
}

function formatScope(assignment: RoleAssignment) {
  if (assignment.scope.kind === 'organization') return scopeLabels.organization
  return `${scopeLabels[assignment.scope.kind]} · ${assignment.scope.resourceId}`
}

export default function GuardAccessManager({ csrfToken }: GuardAccessManagerProps) {
  const [directory, setDirectory] = useState<AuthorizationDirectory>({ accounts: [], roles: [], assignments: [] })
  const [scopeKind, setScopeKind] = useState<ScopeKind>('organization')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const accountNames = useMemo(() => new Map(directory.accounts.map((account) => [account.id, account.displayName])), [directory.accounts])
  const roleNames = useMemo(() => new Map(directory.roles.map((role) => [role.id, role.name])), [directory.roles])

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
      <p className="text-sm font-semibold text-steward-teal">Guard · Access administration</p>
      <h2 className="mt-2 text-2xl font-semibold" id="guard-access-heading">Assign the right access at the right scope</h2>
      <p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">Apply an existing role to the whole organization or limit it to a site, department, or resource. Identity-provider assignments stay synchronized with the provider and cannot be removed here.</p>

      {error && <div className="mt-5 rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      <p aria-live="polite" className="mt-4 text-sm text-[#67dd99]" role="status">{status}</p>

      {loading ? <p className="mt-5 text-steward-mist-muted" role="status">Loading Guard access…</p> : (
        <>
          <div className="mt-5 grid gap-3 sm:grid-cols-3">
            <Summary label="Accounts" value={directory.accounts.length} />
            <Summary label="Roles" value={directory.roles.length} />
            <Summary label="Assignments" value={directory.assignments.length} />
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
