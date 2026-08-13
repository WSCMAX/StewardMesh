import { type CSSProperties, type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { ApiRequestError, authenticationRequiredEventName, requestJSON } from './api'
import AtlasInventory, { isAsset, type Asset } from './AtlasInventory'
import { documentationHref } from './documentation'
import GuideExperience, { GuideInvitation, type GuideDestination } from './GuideExperience'
import GuardAccessManager from './GuardAccessManager'
import HorizonPlanner from './HorizonPlanner'
import LedgerManager from './LedgerManager'
import PatternsManager from './PatternsManager'
import PeopleDirectory from './PeopleDirectory'
import StackManager from './StackManager'
import ThreadsManager from './ThreadsManager'
import { AreaIcon, ChevronRightIcon, StatusBadge, buttonClass, cx, inputClass, panelClass, plainButtonClass, secondaryButtonClass, sectionKickerClass, type AreaIconName } from './ui'
import VaultManager from './VaultManager'
import { brandingStyle, readWalkthroughStatus, resolveBranding, type WalkthroughStatus, writeWalkthroughStatus } from './guide'
import WorkspaceShell, { workspaceAreaFromHash, workspaceHash, type WorkspaceArea, type WorkspaceAreaID } from './WorkspaceShell'
import { permissionAccess, type PermissionAccess, type SessionGrant } from './workspaceAccess'

// Requirements include REQ-WORKSPACE-001, REQ-PATTERNS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-STACK-001, REQ-HORIZON-001, A11Y-001, DOC-001, and DOC-002.

type WorkspaceModule = {
  id: Exclude<WorkspaceAreaID, 'overview'>
  name: string
  descriptor: string
  summary: string
  permission: string
  writePermission?: string
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
  grants: SessionGrant[]
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
const guardHelpUrl = documentationHref('guard')

const workspaceModules: readonly WorkspaceModule[] = [
  { id: 'atlas', name: 'Atlas', descriptor: 'Asset inventory', summary: 'Register, locate, and maintain the assets your organization stewards.', permission: 'assets.read', writePermission: 'assets.write' },
  { id: 'horizon', name: 'Horizon', descriptor: 'Lifecycle planning', summary: 'Plan useful life, replacement timing, scenarios, and forecasts.', permission: 'planning.read', writePermission: 'planning.write' },
  { id: 'ledger', name: 'Ledger', descriptor: 'Procurement and budgets', summary: 'Work with vendors, purchases, contracts, commitments, costs, and budgets.', permission: 'finance.read', writePermission: 'finance.write' },
  { id: 'stack', name: 'Stack', descriptor: 'Software and licenses', summary: 'Connect installed software, purchased entitlements, assignments, usage, and compliance.', permission: 'software.read', writePermission: 'software.write' },
  { id: 'threads', name: 'Threads', descriptor: 'Tags and strategic goals', summary: 'Connect inventory to hierarchical tags, goals, and visible provenance.', permission: 'goals.read', writePermission: 'goals.write' },
  { id: 'vault', name: 'Vault', descriptor: 'Private files and evidence', summary: 'Store checksummed evidence and authorize private downloads.', permission: 'storage.read', writePermission: 'storage.write' },
  { id: 'people', name: 'People', descriptor: 'Users and departments', summary: 'Organize locations, departments, identities, and asset assignments.', permission: 'directory.read', writePermission: 'directory.write' },
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
    && Array.isArray(candidate.grants)
    && candidate.grants.every((grant) => {
      if (typeof grant !== 'object' || grant === null) return false
      const record = grant as Record<string, unknown>
      if (typeof record.permission !== 'string' || typeof record.scope !== 'object' || record.scope === null) return false
      const scope = record.scope as Record<string, unknown>
      return ['organization', 'site', 'department', 'resource'].includes(String(scope.kind))
        && typeof scope.resourceId === 'string' && scope.resourceId.length > 0 && scope.resourceId.length <= 200
    })
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
  const [grants, setGrants] = useState<SessionGrant[]>([])
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
          setGrants(session.grants)
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
    function requireAuthentication() {
      setPrincipal(null)
      setCSRFToken('')
      setPermissions([])
      setGrants([])
      setAssets([])
      setAuthError('Your session expired. Sign in again to continue; unsaved work was not submitted.')
      setAuthPhase('login')
      queueMicrotask(() => errorRef.current?.focus())
    }
    window.addEventListener(authenticationRequiredEventName, requireAuthentication)
    return () => window.removeEventListener(authenticationRequiredEventName, requireAuthentication)
  }, [])

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
    if (permissions.includes('organization.read')) {
      requestJSON('/api/v1/organization')
        .then((organization) => {
          if (!isOrganization(organization)) throw new Error('invalid organization response')
          if (active) setOrganizationName(organization.name)
        })
        .catch(() => { if (active) setOrganizationName('Your organization') })
    }
    if (permissions.includes('assets.read')) {
      requestJSON('/api/v1/assets')
        .then((body) => {
          if (typeof body !== 'object' || body === null) throw new Error('invalid asset response')
          const items = (body as AssetResponse).items
          if (active) setAssets(Array.isArray(items) ? items.filter(isAsset) : [])
        })
        .catch(() => { if (active) setAssets([]) })
    }
    return () => {
      active = false
    }
  }, [authPhase, permissions])

  function acceptSession(session: SessionResponse) {
    setPrincipal(session.principal)
    setPermissions(session.permissions)
    setGrants(session.grants)
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
      setGrants([])
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
    stack: <StackManager assets={assets} csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'stack' })} permissions={permissions} />,
    threads: <ThreadsManager assets={assets} csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'threads' })} permissions={permissions} />,
    vault: <VaultManager csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'vault' })} permissions={permissions} />,
    people: <PeopleDirectory assets={assets} csrfToken={csrfToken} issuesUrl={issuesUrl} onOpenHelp={() => openGuide({ view: 'help', topic: 'people' })} onReportIssue={() => openGuide({ view: 'report', topic: 'people' })} permissions={permissions} />,
    guard: <div className="grid gap-6"><GuardAccessManager csrfToken={csrfToken} onOpenHelp={() => openGuide({ view: 'help', topic: 'guard' })} /><PatternsManager csrfToken={csrfToken} /></div>,
  }
  const workspaceAreas: WorkspaceArea[] = [
    {
      id: 'overview', name: 'Overview', descriptor: 'Work queue and product areas',
      summary: 'Choose a focused area, see what is available, and return here without losing work already in progress.',
      content: <WorkspaceOverview assets={assets} grants={grants} guideOpen={guideOpen} health={health} modules={workspaceModules} onNavigate={navigateWorkspace} onOpenGuide={openGuide} onWalkthroughStatus={updateWalkthroughStatus} principal={principal} walkthroughStatus={walkthroughStatus} />,
    },
    ...workspaceModules.map((module): WorkspaceArea => {
      const readAccess = permissionAccess(grants, module.permission)
      const writeAccess = module.writePermission ? permissionAccess(grants, module.writePermission) : readAccess
      return {
        ...module,
        readAccess,
        writeAccess,
        content: readAccess.level === 'organization'
          ? workspaceContent[module.id]
          : <PermissionLimitedArea module={module} onOpenGuide={() => openGuide({ view: 'help', topic: module.id })} readAccess={readAccess} writeAccess={writeAccess} />,
      }
    }),
  ]

  return (
    <div className="min-h-screen text-steward-mist" data-feature="authorization.security experience.help" data-requirement="SEC-GUARD-001 A11Y-001 DOC-001 DOC-002" style={brandingStyle(branding.appliedTheme) as CSSProperties}>
      <a className="sr-only rounded-xl bg-steward-teal px-3 py-2 font-semibold text-steward-ink-950 focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[70]" href="#main-content">Skip to main content</a>
      <header className="sticky top-0 z-30 border-b border-white/[0.07] bg-steward-ink-950/88 shadow-sm shadow-black/20 backdrop-blur-xl">
        <div className="mx-auto flex max-w-[100rem] flex-wrap items-center justify-between gap-4 px-4 py-3 sm:px-6">
          <div className="flex min-w-0 items-center gap-3">
            <span className="grid size-11 shrink-0 place-items-center rounded-xl border border-white/10 bg-steward-ink-900 shadow-sm"><img alt="" aria-hidden="true" className="h-9 w-auto" height="370" src="/brand/stewardmesh-s-mark.svg" width="294" /></span>
            <div className="min-w-0">
              <div className="flex items-baseline gap-2">
                <h1 className="truncate text-xl font-bold tracking-tight text-white">StewardMesh</h1>
                <p className="hidden text-[0.6875rem] font-semibold uppercase tracking-[0.14em] text-steward-slate sm:block">By Binary Cornfield</p>
              </div>
              <p className="truncate text-xs text-steward-mist-muted sm:text-sm" aria-live="polite" data-requirement="REQ-FOUNDATION-001">{organizationName}</p>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 sm:gap-3">
            {principal && <div className="hidden items-center gap-2 xl:flex"><span aria-hidden="true" className="grid size-8 place-items-center rounded-full bg-steward-blue/15 text-xs font-bold text-[#a9c7ff] ring-1 ring-inset ring-steward-blue/30">{principal.displayName.split(/\s+/).slice(0, 2).map((part) => part[0]).join('').toUpperCase()}</span><p className="max-w-64 truncate text-sm text-steward-mist-muted">Signed in as <strong className="font-semibold text-steward-mist">{principal.displayName}</strong></p></div>}
            <button className={cx(secondaryButtonClass, authPhase === 'authenticated' && 'max-sm:hidden')} onClick={() => openGuide({ view: 'help', topic: authPhase === 'authenticated' ? 'workspace' : 'guard' })} type="button">Open Guide</button>
            {principal && <button className={`${plainButtonClass} text-steward-mist-muted`} disabled={busy} onClick={handleLogout} type="button">Sign out</button>}
            <p aria-live="polite"><StatusBadge tone={health === 'connected' ? 'success' : health === 'unavailable' ? 'warning' : 'info'}><span aria-hidden="true" className={cx('size-1.5 rounded-full', health === 'connected' ? 'bg-steward-green' : health === 'unavailable' ? 'bg-steward-warning' : 'steward-pulse bg-steward-blue')} />{health === 'connected' ? 'Service connected' : health === 'unavailable' ? 'Service unavailable' : 'Checking service'}</StatusBadge></p>
          </div>
        </div>
      </header>

      <main id="main-content" className={cx('mx-auto max-w-[100rem] space-y-8 px-4 py-5 sm:px-6 lg:py-7', authPhase !== 'authenticated' && 'grid min-h-[calc(100svh-5.5rem)] place-items-center')} tabIndex={-1}>
        {authError && <div ref={errorRef} className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert" tabIndex={-1}>{authError}</div>}

        {authPhase === 'loading' && <section aria-labelledby="auth-loading-heading" className={`${panelClass} w-full max-w-xl p-6`}><span aria-hidden="true" className="mb-4 block h-1 w-20 overflow-hidden rounded-full bg-steward-ink-800"><span className="steward-pulse block h-full w-1/2 rounded-full bg-steward-teal" /></span><h2 id="auth-loading-heading" className="text-xl font-semibold">Guard — Checking access</h2><p className="mt-2 text-steward-mist-muted" role="status">Checking administrator setup and your secure session.</p></section>}

        {authPhase === 'unavailable' && <section aria-labelledby="auth-unavailable-heading" className={`${panelClass} w-full max-w-xl border-steward-warning/35 bg-steward-warning/10 p-6`}><h2 id="auth-unavailable-heading" className="text-xl font-semibold">Guard — Authentication unavailable</h2><p className="mt-2 text-steward-mist-muted">Confirm the Go service and database are running, then reload this page.</p><HelpLinks onHelp={() => openGuide({ view: 'help', topic: 'guard' })} onReport={() => openGuide({ view: 'report', topic: 'guard' })} /></section>}

        {authPhase === 'bootstrap' && (
          <section aria-labelledby="bootstrap-heading" className={`${panelClass} relative mx-auto w-full max-w-2xl overflow-hidden p-6 sm:p-8`}>
            <span aria-hidden="true" className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-steward-green via-steward-teal to-steward-blue" />
            <p className={sectionKickerClass}>Guard / Secure local authentication</p>
            <h2 id="bootstrap-heading" className="mt-3 text-3xl font-bold tracking-tight">Create the first administrator</h2>
            <p className="mt-3 leading-7 text-steward-mist-muted">This one-time account receives the organization-scoped Administrator role. Passwords are hashed before storage, and the browser receives only an HttpOnly session cookie.</p>
            <form className="mt-6 space-y-5" onSubmit={handleBootstrap}>
              <Field id="displayName" label="Display name" autoComplete="name" required />
              <Field id="email" label="Email address" autoComplete="email" type="email" required />
              <Field id="username" label="Username" autoComplete="username" pattern="[A-Za-z0-9][A-Za-z0-9._\-]{2,63}" help="Use 3 to 64 letters, numbers, periods, underscores, or hyphens." required />
              <Field id="password" label="Password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} help={`Use at least ${minimumPasswordCharacters} characters. StewardMesh does not require arbitrary symbol or capitalization rules.`} required />
              <Field id="confirmPassword" label="Confirm password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} required />
              {tokenRequired && <Field id="bootstrapToken" label="Deployment bootstrap token" autoComplete="off" type="password" help="Enter the token configured by your server administrator. It is sent only to the local StewardMesh API." required />}
              <button className={`${buttonClass} w-full`} disabled={busy} type="submit">{busy ? 'Creating administrator…' : 'Create administrator'}</button>
            </form>
            <HelpLinks onHelp={() => openGuide({ view: 'help', topic: 'guard' })} onReport={() => openGuide({ view: 'report', topic: 'guard' })} />
          </section>
        )}

        {authPhase === 'login' && (
          <section aria-labelledby="login-heading" className={`${panelClass} relative mx-auto w-full max-w-xl overflow-hidden p-6 sm:p-9`}>
            <span aria-hidden="true" className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-steward-green via-steward-teal to-steward-blue" />
            <div className="mb-6 grid size-12 place-items-center rounded-xl border border-steward-teal/25 bg-steward-teal/10 text-steward-teal"><AreaIcon area="guard" className="size-6" /></div>
            <p className={sectionKickerClass}>Guard / Secure authentication</p>
            <h2 id="login-heading" className="mt-3 text-3xl font-bold tracking-tight">Sign in to StewardMesh</h2>
            <p className="mt-3 text-steward-mist-muted">Use your local organization account{oidcEnabled || samlEnabled ? ' or an organization identity provider' : ''}.</p>
            {(oidcEnabled || samlEnabled) && <div className="mt-6 grid gap-3">
              {oidcEnabled && <a className="block min-h-11 w-full rounded-lg border border-steward-teal px-4 py-3 text-center font-semibold text-steward-teal transition hover:bg-steward-teal/10" href="/api/v1/auth/oidc/start">Continue with OpenID Connect</a>}
              {samlEnabled && <a className="block min-h-11 w-full rounded-lg border border-steward-teal px-4 py-3 text-center font-semibold text-steward-teal transition hover:bg-steward-teal/10" href="/api/v1/auth/saml/start">Continue with SAML</a>}
            </div>}
            {(oidcEnabled || samlEnabled) && <div className="my-6 flex items-center gap-3 text-sm text-steward-mist-muted" aria-hidden="true"><span className="h-px flex-1 bg-steward-ink-800" /><span>or use a local account</span><span className="h-px flex-1 bg-steward-ink-800" /></div>}
            <form className="mt-6 space-y-5" onSubmit={handleLogin}>
              <Field id="username" label="Username" autoComplete="username" required />
              <Field id="password" label="Password" autoComplete="current-password" type="password" required />
              <button className={`${buttonClass} w-full`} disabled={busy} type="submit">{busy ? 'Signing in…' : 'Sign in'}</button>
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

function WorkspaceOverview({ assets, grants, guideOpen, health, modules, onNavigate, onOpenGuide, onWalkthroughStatus, principal, walkthroughStatus }: {
  assets: readonly Asset[]
  grants: readonly SessionGrant[]
  guideOpen: boolean
  health: ServiceHealth
  modules: readonly WorkspaceModule[]
  onNavigate: (area: WorkspaceAreaID) => void
  onOpenGuide: (destination: GuideDestination) => void
  onWalkthroughStatus: (status: WalkthroughStatus) => void
  principal: Principal | null
  walkthroughStatus: WalkthroughStatus
}) {
  const availableCount = modules.filter((module) => permissionAccess(grants, module.permission).level !== 'none').length
  return <div className="space-y-5">
    <section aria-labelledby="workspace-overview-heading" className={`${panelClass} relative overflow-hidden p-5 sm:p-7`}>
      <div aria-hidden="true" className="absolute -right-20 -top-20 size-72 rounded-full bg-steward-blue/10 blur-3xl" />
      <p className={sectionKickerClass}>Connect what you steward. Plan what comes next.</p>
      <h3 className="relative mt-3 max-w-4xl text-2xl font-bold tracking-tight text-white sm:text-3xl" id="workspace-overview-heading">A clear starting point for inventory, people, evidence, goals, planning, and finance.</h3>
      <p className="mt-3 max-w-4xl leading-7 text-steward-mist-muted">Open one product area at a time from the navigation. StewardMesh keeps previously opened areas mounted, so filters, selected records, and incomplete form work remain in context while you move around.</p>
      <dl className="mt-5 grid gap-3 sm:grid-cols-3">
        <OverviewMetric label="Assets tracked" value={String(assets.length)} detail="Current Atlas records" />
        <OverviewMetric label="Work areas available" value={`${availableCount} of ${modules.length}`} detail="Based on your current grants" />
        <OverviewMetric label="Service state" value={health === 'connected' ? 'Connected' : health === 'unavailable' ? 'Unavailable' : 'Checking'} detail={health === 'unavailable' ? 'Protected work is temporarily unavailable' : 'Live application status'} />
      </dl>
    </section>

    {!guideOpen && <GuideInvitation onNavigate={onOpenGuide} onWalkthroughStatus={onWalkthroughStatus} roles={principal?.roles ?? []} status={walkthroughStatus} />}

    <section aria-labelledby="workspace-areas-heading" className={`${panelClass} p-5 sm:p-7`}>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-mist-muted">Product areas</p><h3 className="mt-1 text-xl font-semibold" id="workspace-areas-heading">Choose where to work</h3></div>
        <button className={plainButtonClass} onClick={() => onOpenGuide({ view: 'help', topic: 'workspace' })} type="button">How Workspace works</button>
      </div>
      <ul className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {modules.map((module, index) => {
          const readAccess = permissionAccess(grants, module.permission)
          const writeAccess = module.writePermission ? permissionAccess(grants, module.writePermission) : readAccess
          const available = readAccess.level !== 'none'
          const accessLabel = readAccess.level === 'none' ? 'Limited' : readAccess.level === 'scoped' ? 'Scoped' : writeAccess.level === 'organization' ? 'Read and change' : 'Read only'
          return <li className="group relative overflow-hidden rounded-xl border border-white/[0.08] bg-steward-ink-950/38 p-4 transition hover:-translate-y-0.5 hover:border-steward-teal/30 hover:bg-steward-ink-950/58" key={module.id}>
            <span aria-hidden="true" className={`absolute inset-x-0 top-0 h-0.5 ${index % 3 === 0 ? 'bg-steward-green' : index % 3 === 1 ? 'bg-steward-teal' : 'bg-steward-blue'}`} />
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0"><span aria-hidden="true" className="mb-4 grid size-10 place-items-center rounded-xl border border-white/[0.08] bg-white/[0.04] text-steward-teal transition group-hover:border-steward-teal/25 group-hover:bg-steward-teal/10"><AreaIcon area={module.id as AreaIconName} /></span><h4 className="font-semibold text-white">{module.name} — {module.descriptor}</h4><p className="mt-2 text-sm leading-6 text-steward-mist-muted">{module.summary}</p></div>
              <StatusBadge tone={available ? 'success' : 'warning'}>{accessLabel}</StatusBadge>
            </div>
            <button className={`${plainButtonClass} mt-3 px-0 group-hover:px-2`} onClick={() => onNavigate(module.id)} type="button">Open {module.name}<ChevronRightIcon /></button>
          </li>
        })}
      </ul>
    </section>
  </div>
}

function OverviewMetric({ detail, label, value }: { detail: string; label: string; value: string }) {
  return <div className="rounded-xl border border-white/[0.08] bg-steward-ink-950/40 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.025)]"><dt className="text-[0.6875rem] font-semibold uppercase tracking-[0.12em] text-steward-slate">{label}</dt><dd className="mt-2 text-2xl font-bold tracking-tight text-white">{value}</dd><dd className="mt-1 text-xs text-steward-mist-muted">{detail}</dd></div>
}

function PermissionLimitedArea({ module, onOpenGuide, readAccess, writeAccess }: { module: WorkspaceModule; onOpenGuide: () => void; readAccess: PermissionAccess; writeAccess: PermissionAccess }) {
  const scoped = readAccess.level === 'scoped'
  return <section aria-labelledby={`${module.id}-limited-heading`} className={`${panelClass} p-6`}>
    <p className="text-sm font-semibold text-steward-warning">{scoped ? 'Scoped access' : 'Permission-limited area'}</p>
    <h3 className="mt-2 text-xl font-semibold" id={`${module.id}-limited-heading`}>{scoped ? `${module.name} access is limited to assigned records` : `${module.name} data is protected`}</h3>
    <p className="mt-3 max-w-3xl leading-7 text-steward-mist-muted">{scoped
      ? `Your ${module.permission} grant is limited to ${readAccess.scopeCount} assigned ${readAccess.scopeCount === 1 ? 'scope' : 'scopes'}. Organization-wide lists stay closed so Workspace cannot reveal records outside those boundaries.`
      : <>Your current access does not include <code className="rounded bg-steward-ink-950 px-1.5 py-0.5 text-steward-mist">{module.permission}</code>. Ask a StewardMesh administrator for the appropriate scoped role if this work is part of your responsibilities.</>}</p>
    {scoped && <p className="mt-3 max-w-3xl text-sm leading-6 text-steward-mist-muted">Direct record links still require a matching server-side grant. {writeAccess.level === 'none' ? `Changes require ${module.writePermission}.` : 'Any change remains limited to the matching server-authorized scope.'}</p>}
    <button className={`${secondaryButtonClass} mt-4`} onClick={onOpenGuide} type="button">Learn about {module.name}</button>
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
        className={inputClass}
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
