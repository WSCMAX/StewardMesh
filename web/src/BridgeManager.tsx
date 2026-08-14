import { type FormEvent, useCallback, useEffect, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, dangerButtonClass, inputClass, labelClass, panelClass, secondaryButtonClass, StatusBadge, subpanelClass } from './ui'

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

type Scope = 'mcp:resources' | 'assets:read' | 'directory:read' | 'signals:read' | 'signals:acknowledge'
type Client = { id: string; name: string; redirectUris: string[]; allowedScopes: Scope[]; createdAt: string; revokedAt?: string }
type Grant = { id: string; clientId: string; clientName?: string; actorId: string; scopes: Scope[]; accessExpiresAt: string; refreshExpiresAt: string; revokedAt?: string }
type Consent = { id: string; clientId: string; clientName: string; scopes: Scope[]; expiresAt: string }

const scopes: readonly Scope[] = ['mcp:resources', 'assets:read', 'directory:read', 'signals:read', 'signals:acknowledge']
const idPattern = /^[a-f0-9]{32}$/

function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function validScopes(value: unknown): value is Scope[] { return Array.isArray(value) && value.length > 0 && value.length <= scopes.length && value.every((scope) => scopes.includes(scope as Scope)) }
function validDate(value: unknown) { return typeof value === 'string' && value.length <= 64 && !Number.isNaN(Date.parse(value)) }
function isClient(value: unknown): value is Client {
  return record(value) && typeof value.id === 'string' && idPattern.test(value.id) && typeof value.name === 'string' && value.name.length > 0 && value.name.length <= 120
    && Array.isArray(value.redirectUris) && value.redirectUris.length > 0 && value.redirectUris.length <= 10 && value.redirectUris.every((uri) => typeof uri === 'string' && uri.length <= 2048)
    && validScopes(value.allowedScopes) && validDate(value.createdAt) && (value.revokedAt === undefined || validDate(value.revokedAt))
}
function isGrant(value: unknown): value is Grant {
  return record(value) && typeof value.id === 'string' && idPattern.test(value.id) && typeof value.clientId === 'string' && idPattern.test(value.clientId)
    && typeof value.actorId === 'string' && value.actorId.length > 0 && value.actorId.length <= 128 && validScopes(value.scopes)
    && validDate(value.accessExpiresAt) && validDate(value.refreshExpiresAt) && (value.revokedAt === undefined || validDate(value.revokedAt))
}
function isConsent(value: unknown): value is Consent {
  return record(value) && typeof value.id === 'string' && idPattern.test(value.id) && typeof value.clientId === 'string' && idPattern.test(value.clientId)
    && typeof value.clientName === 'string' && value.clientName.length > 0 && value.clientName.length <= 120 && validScopes(value.scopes) && validDate(value.expiresAt)
}
function page<T>(value: unknown, validator: (item: unknown) => item is T): { items: T[]; nextCursor: string } {
  if (!record(value) || !Array.isArray(value.items) || value.items.length > 25 || !value.items.every(validator)) throw new Error('invalid Bridge response')
  const nextCursor = value.nextCursor === undefined ? '' : value.nextCursor
  if (typeof nextCursor !== 'string' || (nextCursor !== '' && !idPattern.test(nextCursor))) throw new Error('invalid Bridge cursor')
  return { items: value.items, nextCursor }
}
function redirectTarget(value: unknown) {
  if (!record(value) || typeof value.redirectTo !== 'string' || value.redirectTo.length > 4096) throw new Error('invalid consent redirect')
  const target = new URL(value.redirectTo)
  const localHTTP = target.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(target.hostname)
  if ((!localHTTP && target.protocol !== 'https:') || target.username || target.password) throw new Error('unsafe consent redirect')
  return target.toString()
}

export default function BridgeManager({ csrfToken, permissions }: { csrfToken: string; permissions: readonly string[] }) {
  const [clients, setClients] = useState<Client[]>([])
  const [grants, setGrants] = useState<Grant[]>([])
  const [clientCursor, setClientCursor] = useState('')
  const [grantCursor, setGrantCursor] = useState('')
  const [consent, setConsent] = useState<Consent | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const canWrite = permissions.includes('integrations.write')

  const load = useCallback(async () => {
    setError('')
    try {
      const [clientValue, grantValue] = await Promise.all([requestJSON('/api/v1/bridge/clients?limit=25'), requestJSON('/api/v1/bridge/grants?limit=25')])
      const clientPage = page(clientValue, isClient); const grantPage = page(grantValue, isGrant)
      setClients(clientPage.items); setClientCursor(clientPage.nextCursor); setGrants(grantPage.items); setGrantCursor(grantPage.nextCursor)
      const consentID = new URL(window.location.href).searchParams.get('consent') ?? ''
      if (idPattern.test(consentID)) {
        const value = await requestJSON(`/api/v1/bridge/consents/${encodeURIComponent(consentID)}`)
        if (!isConsent(value)) throw new Error('invalid consent response')
        setConsent(value)
      }
    } catch (caught) { setError(caught instanceof ApiRequestError ? caught.message : 'Bridge data could not be loaded.') }
  }, [])

  useEffect(() => { void load() }, [load])

  async function loadMore(kind: 'clients' | 'grants') {
    const cursor = kind === 'clients' ? clientCursor : grantCursor
    if (!cursor) return
    setBusy(true); setError('')
    try {
      const value = await requestJSON(`/api/v1/bridge/${kind}?limit=25&cursor=${encodeURIComponent(cursor)}`)
      if (kind === 'clients') {
        const next = page(value, isClient); setClients((current) => [...current, ...next.items]); setClientCursor(next.nextCursor)
      } else {
        const next = page(value, isGrant); setGrants((current) => [...current, ...next.items]); setGrantCursor(next.nextCursor)
      }
    } catch (caught) { setError(caught instanceof ApiRequestError ? caught.message : 'More Bridge data could not be loaded.') } finally { setBusy(false) }
  }

  async function createClient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!canWrite) return
    const formElement = event.currentTarget
    const form = new FormData(formElement)
    const selected = scopes.filter((scope) => form.getAll('scope').includes(scope))
    setBusy(true); setError(''); setNotice('')
    try {
      await requestJSON('/api/v1/bridge/clients', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ name: form.get('name'), redirectUris: [form.get('redirectUri')], allowedScopes: selected }) })
      formElement.reset(); setNotice('OAuth client registered.'); await load()
    } catch (caught) { setError(caught instanceof ApiRequestError ? caught.message : 'OAuth client could not be registered.') } finally { setBusy(false) }
  }

  async function remove(path: string, noticeText: string) {
    if (!canWrite || !window.confirm('Revoke this access? Existing credentials will stop working.')) return
    setBusy(true); setError(''); setNotice('')
    try { await requestJSON(path, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } }); setNotice(noticeText); await load() }
    catch (caught) { setError(caught instanceof ApiRequestError ? caught.message : 'Bridge access could not be revoked.') } finally { setBusy(false) }
  }

  async function decide(approved: boolean) {
    if (!consent) return
    setBusy(true); setError('')
    try {
      const value = await requestJSON(`/api/v1/bridge/consents/${encodeURIComponent(consent.id)}/decision`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ approved }) })
      window.location.assign(redirectTarget(value))
    } catch (caught) { setError(caught instanceof ApiRequestError ? caught.message : 'Consent could not be completed.'); setBusy(false) }
  }

  return <div className="space-y-5" data-feature="integrations.protocols" data-requirement="REQ-API-001 SEC-MCP-001">
    {consent && <section aria-labelledby="bridge-consent-heading" className={`${panelClass} border-steward-teal/35 p-5 sm:p-7`}>
      <p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Permission request</p>
      <h3 className="mt-2 text-2xl font-semibold" id="bridge-consent-heading">Allow {consent.clientName} to use Bridge?</h3>
      <p className="mt-2 text-sm leading-6 text-steward-mist-muted">Only the listed scopes will be granted. StewardMesh rechecks your current Guard permissions on every MCP request.</p>
      <ul className="mt-4 list-disc space-y-1 pl-5 text-sm text-steward-mist">{consent.scopes.map((scope) => <li key={scope}><code>{scope}</code></li>)}</ul>
      <div className="mt-5 flex flex-wrap gap-3"><button className={buttonClass} disabled={busy} onClick={() => void decide(true)} type="button">Allow access</button><button className={secondaryButtonClass} disabled={busy} onClick={() => void decide(false)} type="button">Deny</button></div>
    </section>}
    <section aria-labelledby="bridge-heading" className={`${panelClass} p-5 sm:p-7`}>
      <div className="flex flex-wrap items-start justify-between gap-3"><div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Bridge / MCP and OAuth</p><h3 className="mt-2 text-2xl font-semibold" id="bridge-heading">Connected clients</h3><p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Register exact redirects, grant narrow scopes, and revoke clients or sessions. Raw OAuth tokens are never shown here.</p></div><StatusBadge tone="success">MCP 2026-07-28</StatusBadge></div>
      {error && <p className="mt-4 rounded-xl border border-steward-danger/40 bg-steward-danger/10 p-3 text-sm" role="alert">{error}</p>}
      {notice && <p className="mt-4 rounded-xl border border-steward-success/40 bg-steward-success/10 p-3 text-sm" role="status">{notice}</p>}
      {canWrite && <form className={`${subpanelClass} mt-5 grid gap-4 p-4`} onSubmit={createClient}>
        <div><label className={labelClass} htmlFor="bridge-client-name">Client name</label><input autoComplete="off" className={inputClass} id="bridge-client-name" maxLength={120} name="name" required /></div>
        <div><label className={labelClass} htmlFor="bridge-redirect-uri">Exact redirect URI</label><input autoCapitalize="none" autoComplete="off" className={inputClass} id="bridge-redirect-uri" maxLength={2048} name="redirectUri" placeholder="https://client.example/callback" required type="url" /></div>
        <fieldset><legend className={labelClass}>Allowed scopes</legend><div className="mt-2 grid gap-2 sm:grid-cols-2">{scopes.map((scope) => <label className="flex min-h-11 items-center gap-3 rounded-xl border border-white/10 px-3 text-sm" key={scope}><input defaultChecked={scope === 'mcp:resources'} disabled={scope === 'mcp:resources'} name={scope === 'mcp:resources' ? undefined : 'scope'} type="checkbox" value={scope} /><span>{scope}</span>{scope === 'mcp:resources' && <input name="scope" type="hidden" value={scope} />}</label>)}</div></fieldset>
        <button className={buttonClass} disabled={busy} type="submit">Register public client</button>
      </form>}
      <ul className="mt-5 grid gap-3">{clients.map((client) => <li className={`${subpanelClass} p-4`} key={client.id}><div className="flex flex-wrap items-start justify-between gap-3"><div><h4 className="font-semibold text-white">{client.name}</h4><p className="mt-1 break-all font-mono text-xs text-steward-slate">{client.id}</p><p className="mt-2 break-all text-sm text-steward-mist-muted">{client.redirectUris.join(', ')}</p><p className="mt-2 text-xs text-steward-slate">{client.allowedScopes.join(' · ')}</p></div>{client.revokedAt ? <StatusBadge tone="neutral">Revoked</StatusBadge> : canWrite && <button className={dangerButtonClass} disabled={busy} onClick={() => void remove(`/api/v1/bridge/clients/${encodeURIComponent(client.id)}`, 'OAuth client revoked.')} type="button">Revoke client</button>}</div></li>)}</ul>
      {clientCursor && <button className={`${secondaryButtonClass} mt-4`} disabled={busy} onClick={() => void loadMore('clients')} type="button">Load more clients</button>}
    </section>
    <section aria-labelledby="bridge-grants-heading" className={`${panelClass} p-5 sm:p-7`}><h3 className="text-xl font-semibold" id="bridge-grants-heading">Active and historical grants</h3><ul className="mt-4 grid gap-3">{grants.length === 0 && <li className="text-sm text-steward-mist-muted">No OAuth grants yet.</li>}{grants.map((grant) => <li className={`${subpanelClass} flex flex-wrap items-start justify-between gap-3 p-4`} key={grant.id}><div><p className="font-semibold">{grant.clientName || grant.clientId}</p><p className="mt-1 text-xs text-steward-slate">{grant.scopes.join(' · ')}</p><p className="mt-2 text-xs text-steward-mist-muted">Access expires {new Date(grant.accessExpiresAt).toLocaleString()}</p></div>{grant.revokedAt ? <StatusBadge tone="neutral">Revoked</StatusBadge> : canWrite && <button className={dangerButtonClass} disabled={busy} onClick={() => void remove(`/api/v1/bridge/grants/${encodeURIComponent(grant.id)}`, 'OAuth grant revoked.')} type="button">Revoke grant</button>}</li>)}</ul>{grantCursor && <button className={`${secondaryButtonClass} mt-4`} disabled={busy} onClick={() => void loadMore('grants')} type="button">Load more grants</button>}</section>
  </div>
}
