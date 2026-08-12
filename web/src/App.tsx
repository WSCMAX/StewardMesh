import { type CSSProperties, type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import AtlasInventory, { isAsset, type Asset } from './AtlasInventory'
import GuideExperience, { GuideInvitation, type GuideDestination } from './GuideExperience'
import GuardAccessManager from './GuardAccessManager'
import HorizonPlanner from './HorizonPlanner'
import LedgerManager from './LedgerManager'
import PeopleDirectory from './PeopleDirectory'
import ThreadsManager from './ThreadsManager'
import VaultManager from './VaultManager'
import { brandingStyle, readWalkthroughStatus, resolveBranding, type WalkthroughStatus, writeWalkthroughStatus } from './guide'
import WorkspaceShell, { workspaceAreaFromHash, workspaceHash, type WorkspaceArea, type WorkspaceAreaID } from './WorkspaceShell'

// Requirements include REQ-WORKSPACE-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001, A11Y-001, DOC-001, and DOC-002.

type WorkspaceModule = {
  id: Exclude<WorkspaceAreaID, 'overview'>
  name: string
  descriptor: string
  summary: string
  permission: string
}

type AssetResponse = {
  items?: Asset[]
}

type Organization = {
  id: string
  name: string
}

type Principal = {
  subject: string
  organizationId: string
  username: string
  email: string
  displayName: string
  roles: string[]
}

type SessionResponse = {
  principal: Principal
  permissions: string[]
  csrfToken: string
  expiresAt: string
}

type BootstrapStatus = {
  required: boolean
  tokenRequired: boolean
  minimumPasswordCharacters: number
  oidcEnabled: boolean
  samlEnabled: boolean
}

type AuthPhase = 'loading' | 'bootstrap' | 'login' | 'authenticated' | 'unavailable'
type ServiceHealth = 'checking' | 'connected' | 'unavailable'

const defaultIssuesUrl = 'https://github.com/WSCMAX/StewardMesh/issues'
const guardHelpUrl = 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/guard.md'

const workspaceModules: readonly WorkspaceModule[] = [
  { id: 'atlas', name: 'Atlas', descriptor: 'Asset inventory', summary: 'Register, locate, and maintain the assets your organization stewards.', permission: 'assets.read' },
  { id: 'horizon', name: 'Horizon', descriptor: 'Lifecycle planning', summary: 'Plan useful life, replacement timing, scenarios, and forecasts.', permission: 'planning.read' },
  { id: 'ledger', name: 'Ledger', descriptor: 'Procurement and budgets', summary: 'Work with vendors, purchases, contracts, commitments, costs, and budgets.', permission: 'finance.read' },
  { id: 'threads', name: 'Threads', descriptor: 'Tags and strategic goals', summary: 'Connect inventory to hierarchical tags, goals, and visible provenance.', permission: 'goals.read' },
  { id: 'vault', name: 'Vault', descriptor: 'Private files and evidence', summary: 'Store checksummed evidence and authorize private downloads.', permission: 'storage.read' },
  { id: 'people', name: 'People', descriptor: 'Users and departments', summary: 'Organize locations, departments, identities, and asset assignments.', permission: 'directory.read' },
  { id: 'guard', name: 'Guard', descriptor: 'Authentication and authorization', summary: 'Manage roles, scoped assignments, ownership, and access policy.', permission: 'guard.manage' },
]

export function resolvePublicUrl(value: string | undefined, fallback = defaultIssuesUrl) {
  if (!value) return fallback
  if (value.startsWith('/') && !value.startsWith('//')) return value
  try {
    const url = new URL(value)
    const isLocalHttp = url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    return (url.protocol === 'https:' || isLocalHttp) && !url.username && !url.password ? url.toString() : fallback
  } catch {
    return fallback
  }
}

function isOrganization(value: unknown): value is Organization {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.id === 'string' && candidate.id.length > 0
    && typeof candidate.name === 'string' && candidate.name.length > 0 && candidate.name.length <= 200
}

function isBootstrapStatus(value: unknown): value is BootstrapStatus {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.required === 'boolean'
    && typeof candidate.tokenRequired === 'boolean'
    && typeof candidate.oidcEnabled === 'boolean'
    && typeof candidate.samlEnabled === 'boolean'
    && typeof candidate.minimumPasswordCharacters === 'number'
    && candidate.minimumPasswordCharacters >= 8
    && candidate.minimumPasswordCharacters <= 1024
}

function isSessionResponse(value: unknown): value is SessionResponse {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  if (typeof candidate.csrfToken !== 'string' || candidate.csrfToken.length < 32 || typeof candidate.expiresAt !== 'string') return false
  if (typeof candidate.principal !== 'object' || candidate.principal === null) return false
  const principal = candidate.principal as Record<string, unknown>
  return Array.isArray(candidate.permissions)
    && candidate.permissions.every((permission) => typeof permission === 'string')
    && typeof principal.subject === 'string'
    && typeof principal.organizationId === 'string'
    && typeof principal.username === 'string'
    && typeof principal.email === 'string'
    && typeof principal.displayName === 'string'
    && Array.isArray(principal.roles)
    && principal.roles.every((role) => typeof role === 'string')
}

const issuesUrl = resolvePublicUrl(import.meta.env.VITE_ISSUES_URL)
const appVersion = String(import.meta.env.VITE_APP_VERSION || '0.1.0').trim().slice(0, 64) || 'development'
const branding = resolveBranding({
  darkCanvas: import.meta.env.VITE_STEWARD_DARK_CANVAS,
  darkSurface: import.meta.env.VITE_STEWARD_DARK_SURFACE,
  lightCanvas: import.meta.env.VITE_STEWARD_LIGHT_CANVAS,
  textOnDark: import.meta.env.VITE_STEWARD_TEXT_ON_DARK,
  textOnLight: import.meta.env.VITE_STEWARD_TEXT_ON_LIGHT,
  primary: import.meta.env.VITE_STEWARD_PRIMARY,
  success: import.meta.env.VITE_STEWARD_SUCCESS,
  warning: import.meta.env.VITE_STEWARD_WARNING,
  danger: import.meta.env.VITE_STEWARD_DANGER,
})

export default function App() {
  const [authFailure] = useState(() => new URL(window.location.href).searchParams.get('auth'))
  const [health, setHealth] = useState<ServiceHealth>('checking')
  const [assets, setAssets] = useState<Asset[]>([])
  const [organizationName, setOrganizationName] = useState('Your organization')
  const [authPhase, setAuthPhase] = useState<AuthPhase>('loading')
  const [principal, setPrincipal] = useState<Principal | null>(null)
  const [csrfToken, setCSRFToken] = useState('')
  const [permissions, setPermissions] = useState<string[]>([])
  const [tokenRequired, setTokenRequired] = useState(false)
  const [minimumPasswordCharacters, setMinimumPasswordCharacters] = useState(15)
  const [oidcEnabled, setOIDCEnabled] = useState(false)
  const [samlEnabled, setSAMLEnabled] = useState(false)
  const [authError, setAuthError] = useState(() => {
    if (authFailure === 'oidc_error') return 'OpenID Connect sign-in could not be completed. Try again or use a local account.'
    if (authFailure === 'saml_error') return 'SAML sign-in could not be completed. Try again or use a local account.'
    return ''
  })
  const [busy, setBusy] = useState(false)
  const [guideOpen, setGuideOpen] = useState(false)
  const [guideDestination, setGuideDestination] = useState<GuideDestination>({ view: 'help', topic: 'workspace' })
  const [walkthroughStatus, setWalkthroughStatus] = useState(readWalkthroughStatus)
  const [activeWorkspaceArea, setActiveWorkspaceArea] = useState<WorkspaceAreaID>(() => workspaceAreaFromHash(window.location.hash))
  const [visitedWorkspaceAreas, setVisitedWorkspaceAreas] = useState<ReadonlySet<WorkspaceAreaID>>(() => new Set([workspaceAreaFromHash(window.location.hash)]))
  const errorRef = useRef<HTMLDivElement>(null)

  function openGuide(destination: GuideDestination) {
    setGuideDestination(destination)
    setGuideOpen(true)
  }

  function updateWalkthroughStatus(status: WalkthroughStatus) {
    setWalkthroughStatus(status)
    writeWalkthroughStatus(status)
  }

  function navigateWorkspace(area: WorkspaceAreaID, history: 'push' | 'replace' = 'push', focus = true) {
    setActiveWorkspaceArea(area)
    setVisitedWorkspaceAreas((current) => current.has(area) ? current : new Set([...current, area]))
    const nextHash = workspaceHash(area)
    if (window.location.hash !== nextHash) {
      window.history[history === 'push' ? 'pushState' : 'replaceState']({ workspaceArea: area }, '', `${window.location.pathname}${window.location.search}${nextHash}`)
    }
    if (focus) queueMicrotask(() => document.getElementById('workspace-context-heading')?.focus())
  }

  useEffect(() => {
    if (authError) errorRef.current?.focus()
  }, [authError])

  useEffect(() => {
    let active = true
    const currentURL = new URL(window.location.href)
    if (authFailure === 'oidc_error' || authFailure === 'saml_error') {
      currentURL.searchParams.delete('auth')
      window.history.replaceState(null, '', `${currentURL.pathname}${currentURL.search}${currentURL.hash}`)
    }
    fetch('/healthz', { credentials: 'same-origin' })
      .then((response) => {
        if (!response.ok) throw new Error('health request failed')
        return response.json()
      })
      .then(() => {
        if (active) setHealth('connected')
      })
      .catch(() => {
        if (active) setHealth('unavailable')
      })

    // SEC-GUARD-001: restore only the server-managed HttpOnly session.
    requestJSON('/api/v1/auth/bootstrap')
      .then(async (value) => {
        if (!isBootstrapStatus(value)) throw new Error('invalid bootstrap status')
        if (!active) return
        setTokenRequired(value.tokenRequired)
        setMinimumPasswordCharacters(value.minimumPasswordCharacters)
        setOIDCEnabled(value.oidcEnabled)
        setSAMLEnabled(value.samlEnabled)
        if (value.required) {
          setAuthPhase('bootstrap')
          return
        }
        try {
          const session = await requestJSON('/api/v1/auth/session')
          if (!isSessionResponse(session)) throw new Error('invalid session response')
          if (!active) return
          setPrincipal(session.principal)
          setPermissions(session.permissions)
          setCSRFToken(session.csrfToken)
          setAuthPhase('authenticated')
        } catch (error) {
          if (!active) return
          if (error instanceof ApiRequestError && error.status === 401) {
            setAuthPhase('login')
            return
          }
          setAuthPhase('unavailable')
        }
      })
      .catch(() => {
        if (active) setAuthPhase('unavailable')
      })
    return () => {
      active = false
    }
  }, [authFailure])

  useEffect(() => {
    function restoreWorkspaceLocation() {
      const area = workspaceAreaFromHash(window.location.hash)
      setActiveWorkspaceArea(area)
      setVisitedWorkspaceAreas((current) => current.has(area) ? current : new Set([...current, area]))
      queueMicrotask(() => document.getElementById('workspace-context-heading')?.focus())
    }
    window.addEventListener('popstate', restoreWorkspaceLocation)
    window.addEventListener('hashchange', restoreWorkspaceLocation)
    return () => {
      window.removeEventListener('popstate', restoreWorkspaceLocation)
      window.removeEventListener('hashchange', restoreWorkspaceLocation)
    }
  }, [])

  useEffect(() => {
    if (authPhase !== 'authenticated') return
    let active = true
    requestJSON('/api/v1/organization')
      .then((organization) => {
        if (!isOrganization(organization)) throw new Error('invalid organization response')
        if (active) setOrganizationName(organization.name)
      })
      .catch(() => {
        if (active) setOrganizationName('Your organization')
      })
    requestJSON('/api/v1/assets')
      .then((body) => {
        if (typeof body !== 'object' || body === null) throw new Error('invalid asset response')
        const items = (body as AssetResponse).items
        if (active) setAssets(Array.isArray(items) ? items.filter(isAsset) : [])
      })
      .catch(() => {
        if (active) setAssets([])
      })
    return () => {
      active = false
    }
  }, [authPhase])

  function acceptSession(session: SessionResponse) {
    setPrincipal(session.principal)
    setPermissions(session.permissions)
    setCSRFToken(session.csrfToken)
    setAuthError('')
    setAuthPhase('authenticated')
  }

  async function handleBootstrap(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setAuthError('')
    const form = event.currentTarget
    const values = new FormData(form)
    const password = String(values.get('password') ?? '')
    if (password !== String(values.get('confirmPassword') ?? '')) {
      setAuthError('The password confirmation does not match.')
      return
    }
    setBusy(true)
    try {
      const bootstrapRequest: {
        username: string
        email: string
        displayName: string
        password: string
        bootstrapToken?: string
      } = {
        username: String(values.get('username') ?? ''),
        email: String(values.get('email') ?? ''),
        displayName: String(values.get('displayName') ?? ''),
        password,
      }
      if (tokenRequired) {
        bootstrapRequest.bootstrapToken = String(values.get('bootstrapToken') ?? '')
      }
      const response = await requestJSON('/api/v1/auth/bootstrap', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(bootstrapRequest),
      })
      if (!isSessionResponse(response)) throw new Error('invalid session response')
      form.reset()
      acceptSession(response)
    } catch (error) {
      setAuthError(error instanceof ApiRequestError ? error.message : 'Administrator setup could not be completed.')
    } finally {
      setBusy(false)
    }
  }

  async function handleLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setAuthError('')
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy(true)
    try {
      const response = await requestJSON('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: String(values.get('username') ?? ''),
          password: String(values.get('password') ?? ''),
        }),
      })
      if (!isSessionResponse(response)) throw new Error('invalid session response')
      form.reset()
      acceptSession(response)
    } catch (error) {
      setAuthError(error instanceof ApiRequestError ? error.message : 'Sign in could not be completed.')
    } finally {
      setBusy(false)
    }
  }

  async function handleLogout() {
    setBusy(true)
    setAuthError('')
    try {
      await requestJSON('/api/v1/auth/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken },
      })
      setPrincipal(null)
      setCSRFToken('')
      setPermissions([])
      setAssets([])
      setActiveWorkspaceArea('overview')
      setVisitedWorkspaceAreas(new Set(['overview']))
      setAuthPhase('login')
    } catch (error) {
      setAuthError(error instanceof ApiRequestError ? error.message : 'Sign out could not be completed.')
    } finally {
      setBusy(false)
    }
  }

  const serviceLabel = health === 'connected' ? 'Connected' : health === 'unavailable' ? 'Unavailable' : 'Checking connection'
  const workspaceContent: Record<Exclude<WorkspaceAreaID, 'overview'>, ReactNode> = {
    atlas: <AtlasInventory assets={assets} csrfToken={csrfToken} onAssetsChange={setAssets} onOpenHelp={() => openGuide({ view: 'help', topic: 'atlas' })} permissions={permissions} />,
    horizon: <HorizonPlanner assets={assets} csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'horizon' })} permissions={permissions} />,
    ledger: <LedgerManager csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'ledger' })} permissions={permissions} />,
    threads: <ThreadsManager assets={assets} csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'threads' })} permissions={permissions} />,
    vault: <VaultManager csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'vault' })} permissions={permissions} />,
    people: <PeopleDirectory assets={assets} csrfToken={csrfToken} issuesUrl={issuesUrl} onOpenHelp={() => openGuide({ view: 'help', topic: 'people' })} onReportIssue={() => openGuide({ view: 'report', topic: 'people' })} permissions={permissions} />,
    guard: <GuardAccessManager csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'guard' })} />,
  }
  const workspaceAreas: WorkspaceArea[] = [
    {
      id: 'overview', name: 'Overview', descriptor: 'Work queue and product areas',
      summary: 'Choose a focused area, see what is available, and return here without losing work already in progress.',
      content: <WorkspaceOverview assets={assets} guideOpen={guideOpen} health={health} modules={workspaceModules} onNavigate={navigateWorkspace} onOpenGuide={openGuide} onWalkthroughStatus={updateWalkthroughStatus} permissions={permissions} principal={principal} walkthroughStatus={walkthroughStatus} />,
    },
    ...workspaceModules.map((module): WorkspaceArea => {
      const limited = !permissions.includes(module.permission)
      return {
        ...module,
        limited,
        content: limited ? <PermissionLimitedArea module={module} onOpenGuide={() => openGuide({ view: 'help', topic: module.id })} /> : workspaceContent[module.id],
      }
    }),
  ]

  return (
    <div className="min-h-screen bg-steward-ink-950 text-steward-mist" data-feature="authorization.security experience.help" data-requirement="SEC-GUARD-001 A11Y-001 DOC-001 DOC-002" style={brandingStyle(branding.appliedTheme) as CSSProperties}>
      <a className="sr-only rounded-lg bg-steward-teal px-3 py-2 font-semibold text-steward-ink-950 focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50" href="#main-content">Skip to main content</a>
      <header className="border-b border-steward-ink-800/70 bg-steward-ink-900/90">
        <div className="mx-auto flex max-w-[96rem] flex-wrap items-center justify-between gap-5 px-4 py-4 sm:px-6">
          <div className="flex items-center gap-4">
            <img alt="" aria-hidden="true" className="h-16 w-auto shrink-0" height="370" src="/brand/stewardmesh-s-mark.svg" width="294" />
            <div>
              <p className="text-xs font-semibold text-steward-mist-muted">By Binary Cornfield</p>
              <h1 className="mt-0.5 text-3xl font-bold tracking-tight">StewardMesh</h1>
              <p className="mt-0.5 text-sm text-steward-mist-muted" aria-live="polite" data-requirement="REQ-FOUNDATION-001">{organizationName}</p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {principal && <p className="text-sm text-steward-mist-muted">Signed in as <strong className="text-steward-mist">{principal.displayName}</strong></p>}
            <button className="min-h-11 rounded-lg border border-steward-teal px-4 py-2 text-sm font-semibold text-steward-teal transition hover:bg-steward-teal/10" onClick={() => openGuide({ view: 'help', topic: authPhase === 'authenticated' ? 'workspace' : 'guard' })} type="button">Open Guide</button>
            {principal && <button className="min-h-11 rounded-lg border border-steward-ink-800 bg-steward-ink-900 px-4 py-2 text-sm font-semibold transition hover:border-steward-blue hover:bg-steward-ink-800 disabled:cursor-wait disabled:opacity-60" disabled={busy} onClick={handleLogout} type="button">Sign out</button>}
            <p className={`inline-flex min-h-11 items-center gap-2 rounded-full border px-3 py-2 text-sm font-semibold ${health === 'connected' ? 'border-steward-success/50 bg-steward-success/15 text-[#67dd99]' : health === 'unavailable' ? 'border-steward-warning/60 bg-steward-warning/15 text-[#ffc46b]' : 'border-steward-blue/50 bg-steward-blue/15 text-[#8eb7ff]'}`} aria-live="polite">
              <span aria-hidden="true" className={`size-2 rounded-full ${health === 'connected' ? 'bg-steward-green' : health === 'unavailable' ? 'bg-steward-warning' : 'bg-steward-blue'}`} />
              {health === 'connected' ? 'Service connected' : health === 'unavailable' ? 'Start the Go service to connect' : 'Checking service…'}
            </p>
          </div>
        </div>
      </header>

      <main id="main-content" className="mx-auto max-w-[96rem] space-y-10 px-4 py-6 sm:px-6 lg:py-8" tabIndex={-1}>
        {authError && <div ref={errorRef} className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert" tabIndex={-1}>{authError}</div>}

        {authPhase === 'loading' && <section aria-labelledby="auth-loading-heading" className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6"><h2 id="auth-loading-heading" className="text-xl font-semibold">Guard — Checking access</h2><p className="mt-2 text-steward-mist-muted" role="status">Checking administrator setup and your secure session.</p></section>}

        {authPhase === 'unavailable' && <section aria-labelledby="auth-unavailable-heading" className="rounded-xl border border-steward-warning/40 bg-steward-warning/15 p-6"><h2 id="auth-unavailable-heading" className="text-xl font-semibold">Guard — Authentication unavailable</h2><p className="mt-2 text-steward-mist-muted">Confirm the Go service and database are running, then reload this page.</p><HelpLinks onHelp={() => openGuide({ view: 'help', topic: 'guard' })} onReport={() => openGuide({ view: 'report', topic: 'guard' })} /></section>}

        {authPhase === 'bootstrap' && (
          <section aria-labelledby="bootstrap-heading" className="mx-auto max-w-2xl rounded-xl border border-steward-teal/30 bg-steward-ink-900 p-6 shadow-sm">
            <p className="text-sm font-semibold text-steward-teal">Guard — Secure local authentication</p>
            <h2 id="bootstrap-heading" className="mt-2 text-3xl font-semibold">Create the first administrator</h2>
            <p className="mt-3 leading-7 text-steward-mist-muted">This one-time account receives the organization-scoped Administrator role. Passwords are hashed before storage, and the browser receives only an HttpOnly session cookie.</p>
            <form className="mt-6 space-y-5" onSubmit={handleBootstrap}>
              <Field id="displayName" label="Display name" autoComplete="name" required />
              <Field id="email" label="Email address" autoComplete="email" type="email" required />
              <Field id="username" label="Username" autoComplete="username" pattern="[A-Za-z0-9][A-Za-z0-9._\-]{2,63}" help="Use 3 to 64 letters, numbers, periods, underscores, or hyphens." required />
              <Field id="password" label="Password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} help={`Use at least ${minimumPasswordCharacters} characters. StewardMesh does not require arbitrary symbol or capitalization rules.`} required />
              <Field id="confirmPassword" label="Confirm password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} required />
              {tokenRequired && <Field id="bootstrapToken" label="Deployment bootstrap token" autoComplete="off" type="password" help="Enter the token configured by your server administrator. It is sent only to the local StewardMesh API." required />}
              <button className="min-h-11 w-full rounded-lg bg-steward-teal px-4 py-3 font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60" disabled={busy} type="submit">{busy ? 'Creating administrator…' : 'Create administrator'}</button>
            </form>
            <HelpLinks onHelp={() => openGuide({ view: 'help', topic: 'guard' })} onReport={() => openGuide({ view: 'report', topic: 'guard' })} />
          </section>
        )}

        {authPhase === 'login' && (
          <section aria-labelledby="login-heading" className="mx-auto max-w-xl rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6 shadow-sm">
            <p className="text-sm font-semibold text-steward-teal">Guard — Secure authentication</p>
            <h2 id="login-heading" className="mt-2 text-3xl font-semibold">Sign in to StewardMesh</h2>
            <p className="mt-3 text-steward-mist-muted">Use your local organization account{oidcEnabled || samlEnabled ? ' or an organization identity provider' : ''}.</p>
            {(oidcEnabled || samlEnabled) && <div className="mt-6 grid gap-3">
              {oidcEnabled && <a className="block min-h-11 w-full rounded-lg border border-steward-teal px-4 py-3 text-center font-semibold text-steward-teal transition hover:bg-steward-teal/10" href="/api/v1/auth/oidc/start">Continue with OpenID Connect</a>}
              {samlEnabled && <a className="block min-h-11 w-full rounded-lg border border-steward-teal px-4 py-3 text-center font-semibold text-steward-teal transition hover:bg-steward-teal/10" href="/api/v1/auth/saml/start">Continue with SAML</a>}
            </div>}
            {(oidcEnabled || samlEnabled) && <div className="my-6 flex items-center gap-3 text-sm text-steward-mist-muted" aria-hidden="true"><span className="h-px flex-1 bg-steward-ink-800" /><span>or use a local account</span><span className="h-px flex-1 bg-steward-ink-800" /></div>}
            <form className="mt-6 space-y-5" onSubmit={handleLogin}>
              <Field id="username" label="Username" autoComplete="username" required />
              <Field id="password" label="Password" autoComplete="current-password" type="password" required />
              <button className="min-h-11 w-full rounded-lg bg-steward-teal px-4 py-3 font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60" disabled={busy} type="submit">{busy ? 'Signing in…' : 'Sign in'}</button>
            </form>
            <HelpLinks onHelp={() => openGuide({ view: 'help', topic: 'guard' })} onReport={() => openGuide({ view: 'report', topic: 'guard' })} />
          </section>
        )}

        {authPhase === 'authenticated' && (
          <WorkspaceShell activeArea={activeWorkspaceArea} areas={workspaceAreas} assetCount={assets.length} healthLabel={serviceLabel} onNavigate={navigateWorkspace} onOpenHelp={(topic) => openGuide({ view: 'help', topic })} onReportIssue={() => openGuide({ view: 'report', topic: activeWorkspaceArea === 'overview' ? 'workspace' : activeWorkspaceArea })} roles={principal?.roles ?? []} visitedAreas={visitedWorkspaceAreas} />
        )}
      </main>
      <GuideExperience branding={branding} destination={guideDestination} issuesUrl={issuesUrl} onClose={() => setGuideOpen(false)} onFollowSection={(topic) => { if (topic !== 'workspace' && topic !== 'guide') navigateWorkspace(topic, 'push', false) }} onNavigate={openGuide} onWalkthroughStatus={updateWalkthroughStatus} open={guideOpen} permissions={permissions} roles={principal?.roles ?? []} version={appVersion} />
    </div>
  )
}

function WorkspaceOverview({ assets, guideOpen, health, modules, onNavigate, onOpenGuide, onWalkthroughStatus, permissions, principal, walkthroughStatus }: {
  assets: readonly Asset[]
  guideOpen: boolean
  health: ServiceHealth
  modules: readonly WorkspaceModule[]
  onNavigate: (area: WorkspaceAreaID) => void
  onOpenGuide: (destination: GuideDestination) => void
  onWalkthroughStatus: (status: WalkthroughStatus) => void
  permissions: readonly string[]
  principal: Principal | null
  walkthroughStatus: WalkthroughStatus
}) {
  const availableCount = modules.filter((module) => permissions.includes(module.permission)).length
  return <div className="space-y-5">
    <section aria-labelledby="workspace-overview-heading" className="overflow-hidden rounded-2xl border border-steward-ink-800 bg-steward-ink-900 p-5 sm:p-6">
      <p className="text-sm font-semibold text-steward-teal">Connect what you steward. Plan what comes next.</p>
      <h3 className="mt-2 max-w-4xl text-2xl font-bold tracking-tight sm:text-3xl" id="workspace-overview-heading">A clear starting point for inventory, people, evidence, goals, planning, and finance.</h3>
      <p className="mt-3 max-w-4xl leading-7 text-steward-mist-muted">Open one product area at a time from the navigation. StewardMesh keeps previously opened areas mounted, so filters, selected records, and incomplete form work remain in context while you move around.</p>
      <dl className="mt-5 grid gap-3 sm:grid-cols-3">
        <OverviewMetric label="Assets tracked" value={String(assets.length)} detail="Current Atlas records" />
        <OverviewMetric label="Work areas available" value={`${availableCount} of ${modules.length}`} detail="Based on your current grants" />
        <OverviewMetric label="Service state" value={health === 'connected' ? 'Connected' : health === 'unavailable' ? 'Unavailable' : 'Checking'} detail={health === 'unavailable' ? 'Protected work is temporarily unavailable' : 'Live application status'} />
      </dl>
    </section>

    {!guideOpen && <GuideInvitation onNavigate={onOpenGuide} onWalkthroughStatus={onWalkthroughStatus} roles={principal?.roles ?? []} status={walkthroughStatus} />}

    <section aria-labelledby="workspace-areas-heading" className="rounded-2xl border border-steward-ink-800 bg-steward-ink-900 p-5 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-mist-muted">Product areas</p><h3 className="mt-1 text-xl font-semibold" id="workspace-areas-heading">Choose where to work</h3></div>
        <button className="min-h-11 text-sm font-semibold text-steward-teal underline underline-offset-4" onClick={() => onOpenGuide({ view: 'help', topic: 'workspace' })} type="button">How Workspace works</button>
      </div>
      <ul className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {modules.map((module, index) => {
          const available = permissions.includes(module.permission)
          return <li className="relative overflow-hidden rounded-xl border border-steward-ink-800 bg-steward-ink-950/35 p-4" key={module.id}>
            <span aria-hidden="true" className={`absolute inset-y-0 left-0 w-1 ${index % 3 === 0 ? 'bg-steward-green' : index % 3 === 1 ? 'bg-steward-teal' : 'bg-steward-blue'}`} />
            <div className="flex items-start justify-between gap-3">
              <div><h4 className="font-semibold">{module.name} — {module.descriptor}</h4><p className="mt-2 text-sm leading-6 text-steward-mist-muted">{module.summary}</p></div>
              <span className={`shrink-0 rounded-full border px-2 py-1 text-xs font-semibold ${available ? 'border-steward-success/55 bg-steward-success/15 text-[#aaf0c6]' : 'border-steward-warning/55 bg-steward-warning/15 text-[#ffc46b]'}`}>{available ? 'Available' : 'Limited'}</span>
            </div>
            <button className="mt-4 min-h-11 rounded-lg border border-steward-teal px-3 py-2 text-sm font-semibold text-steward-teal transition hover:bg-steward-teal/10" onClick={() => onNavigate(module.id)} type="button">Open {module.name}</button>
          </li>
        })}
      </ul>
    </section>
  </div>
}

function OverviewMetric({ detail, label, value }: { detail: string; label: string; value: string }) {
  return <div className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4"><dt className="text-xs font-semibold uppercase tracking-wide text-steward-mist-muted">{label}</dt><dd className="mt-1 text-2xl font-bold text-steward-mist">{value}</dd><dd className="mt-1 text-xs text-steward-mist-muted">{detail}</dd></div>
}

function PermissionLimitedArea({ module, onOpenGuide }: { module: WorkspaceModule; onOpenGuide: () => void }) {
  return <section aria-labelledby={`${module.id}-limited-heading`} className="rounded-2xl border border-steward-ink-800 bg-steward-ink-900 p-6">
    <p className="text-sm font-semibold text-steward-warning">Permission-limited area</p>
    <h3 className="mt-2 text-xl font-semibold" id={`${module.id}-limited-heading`}>{module.name} data is protected</h3>
    <p className="mt-3 max-w-3xl leading-7 text-steward-mist-muted">Your current access does not include <code className="rounded bg-steward-ink-950 px-1.5 py-0.5 text-steward-mist">{module.permission}</code>. Ask a StewardMesh administrator for the appropriate scoped role if this work is part of your responsibilities.</p>
    <button className="mt-4 min-h-11 rounded-lg border border-steward-teal px-4 py-2 text-sm font-semibold text-steward-teal" onClick={onOpenGuide} type="button">Learn about {module.name}</button>
  </section>
}

type FieldProps = {
  id: string
  label: string
  type?: 'text' | 'email' | 'password'
  autoComplete: string
  help?: string
  minLength?: number
  pattern?: string
  required?: boolean
}

function Field({ id, label, type = 'text', autoComplete, help, minLength, pattern, required }: FieldProps) {
  const helpID = help ? `${id}-help` : undefined
  return (
    <div>
      <label className="block text-sm font-semibold text-steward-mist-muted" htmlFor={id}>{label}</label>
      {help && <p className="mt-1 text-sm leading-6 text-steward-mist-muted" id={helpID}>{help}</p>}
      <input
        aria-describedby={helpID}
        autoComplete={autoComplete}
        className="mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-4 py-3 text-steward-mist shadow-inner shadow-black/20 transition hover:border-steward-blue"
        id={id}
        minLength={minLength}
        name={id}
        pattern={pattern}
        required={required}
        type={type}
      />
    </div>
  )
}

function HelpLinks({ onHelp, onReport }: { onHelp: () => void; onReport: () => void }) {
  return (
    <p className="mt-6 text-sm text-steward-mist-muted">
      <button className="min-h-11 text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" onClick={onHelp} type="button">Open accessible Guard help</button>
      <span aria-hidden="true"> · </span>
      <a className="text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={guardHelpUrl}>Read setup documentation</a>
      <span aria-hidden="true"> · </span>
      <button className="min-h-11 text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" onClick={onReport} type="button">Report an issue</button>
    </p>
  )
}
