import { type FormEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import AtlasInventory, { isAsset, type Asset } from './AtlasInventory'
import GuardAccessManager from './GuardAccessManager'
import PeopleDirectory from './PeopleDirectory'

type Module = readonly [name: string, description: string]

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

const modules: Module[] = [
  ['Atlas', 'Asset inventory'],
  ['Horizon', 'Lifecycle planning'],
  ['Ledger', 'Procurement and budgets'],
  ['Threads', 'Tags and strategic goals'],
  ['People', 'Users and departments'],
  ['Guard', 'Authentication, roles, policies, and audit'],
  ['Guide', 'Help and walkthroughs'],
]

export function resolvePublicUrl(value: string | undefined, fallback = defaultIssuesUrl) {
  if (!value) return fallback
  if (value.startsWith('/') && !value.startsWith('//')) return value
  try {
    const url = new URL(value)
    const isLocalHttp = url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    return url.protocol === 'https:' || isLocalHttp ? url.toString() : fallback
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
  const errorRef = useRef<HTMLDivElement>(null)

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
      setAuthPhase('login')
    } catch (error) {
      setAuthError(error instanceof ApiRequestError ? error.message : 'Sign out could not be completed.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen bg-steward-ink-950 text-steward-mist" data-feature="authorization.security" data-requirement="SEC-GUARD-001">
      <a className="sr-only rounded-lg bg-steward-teal px-3 py-2 font-semibold text-steward-ink-950 focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50" href="#main-content">Skip to main content</a>
      <header className="border-b border-steward-ink-800/70 bg-steward-ink-900/90">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-5 px-4 py-4 sm:px-6">
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
            {principal && <button className="min-h-11 rounded-lg border border-steward-ink-800 bg-steward-ink-900 px-4 py-2 text-sm font-semibold transition hover:border-steward-blue hover:bg-steward-ink-800 disabled:cursor-wait disabled:opacity-60" disabled={busy} onClick={handleLogout} type="button">Sign out</button>}
            <p className={`inline-flex min-h-11 items-center gap-2 rounded-full border px-3 py-2 text-sm font-semibold ${health === 'connected' ? 'border-steward-success/50 bg-steward-success/15 text-[#67dd99]' : health === 'unavailable' ? 'border-steward-warning/60 bg-steward-warning/15 text-[#ffc46b]' : 'border-steward-blue/50 bg-steward-blue/15 text-[#8eb7ff]'}`} aria-live="polite">
              <span aria-hidden="true" className={`size-2 rounded-full ${health === 'connected' ? 'bg-steward-green' : health === 'unavailable' ? 'bg-steward-warning' : 'bg-steward-blue'}`} />
              {health === 'connected' ? 'Service connected' : health === 'unavailable' ? 'Start the Go service to connect' : 'Checking service…'}
            </p>
          </div>
        </div>
      </header>

      <main id="main-content" className="mx-auto max-w-7xl space-y-10 px-4 py-8 sm:px-6 lg:py-10" tabIndex={-1}>
        {authError && <div ref={errorRef} className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert" tabIndex={-1}>{authError}</div>}

        {authPhase === 'loading' && <section aria-labelledby="auth-loading-heading" className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6"><h2 id="auth-loading-heading" className="text-xl font-semibold">Guard — Checking access</h2><p className="mt-2 text-steward-mist-muted" role="status">Checking administrator setup and your secure session.</p></section>}

        {authPhase === 'unavailable' && <section aria-labelledby="auth-unavailable-heading" className="rounded-xl border border-steward-warning/40 bg-steward-warning/15 p-6"><h2 id="auth-unavailable-heading" className="text-xl font-semibold">Guard — Authentication unavailable</h2><p className="mt-2 text-steward-mist-muted">Confirm the Go service and database are running, then reload this page.</p><HelpLinks /></section>}

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
            <HelpLinks />
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
            <HelpLinks />
          </section>
        )}

        {authPhase === 'authenticated' && (
          <>
            <section aria-labelledby="welcome-heading" className="max-w-3xl">
              <p className="text-sm font-medium text-steward-teal">Connect what you steward. Plan what comes next.</p>
              <h2 id="welcome-heading" className="mt-3 text-4xl font-bold tracking-tight sm:text-5xl">A clear view of what your organization owns, funds, and operates.</h2>
              <p className="mt-5 text-lg leading-8 text-steward-mist-muted">StewardMesh brings inventory, lifecycle planning, procurement, goals, and ownership into one accessible workspace.</p>
              {principal && <p className="mt-4 text-sm text-steward-mist-muted">Guard role: {principal.roles.join(', ') || 'No role assigned'}</p>}
            </section>

            <section aria-labelledby="modules-heading">
              <div className="flex flex-wrap items-end justify-between gap-4">
                <div><h2 id="modules-heading" className="text-xl font-semibold">StewardMesh modules</h2><p className="mt-1 text-sm text-steward-mist-muted">Every module has a plain-language descriptor and accessible help.</p></div>
                <a className="text-sm text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={issuesUrl}>Report an issue</a>
              </div>
              <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {modules.map(([name, description], index) => <article key={name} className="relative overflow-hidden rounded-xl border border-steward-ink-800/70 bg-steward-ink-900 p-5 shadow-sm"><span aria-hidden="true" className={`absolute inset-x-0 top-0 h-0.5 ${index % 3 === 0 ? 'bg-steward-green' : index % 3 === 1 ? 'bg-steward-teal' : 'bg-steward-blue'}`} /><p className="text-lg font-semibold">{name}</p><p className="mt-2 text-sm text-steward-mist-muted">{description}</p></article>)}
              </div>
            </section>

            <AtlasInventory assets={assets} csrfToken={csrfToken} onAssetsChange={setAssets} permissions={permissions} />

            {permissions.includes('guard.manage') && <GuardAccessManager csrfToken={csrfToken} />}

            <PeopleDirectory assets={assets} csrfToken={csrfToken} issuesUrl={issuesUrl} permissions={permissions} />
          </>
        )}
      </main>
    </div>
  )
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

function HelpLinks() {
  return (
    <p className="mt-6 text-sm text-steward-mist-muted">
      <a className="text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={guardHelpUrl}>Read Guard setup help</a>
      <span aria-hidden="true"> · </span>
      <a className="text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={issuesUrl}>Report an issue</a>
    </p>
  )
}
