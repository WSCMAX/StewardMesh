import { type FormEvent, useEffect, useRef, useState } from 'react'

type Module = readonly [name: string, description: string]

type Asset = {
  id: string
  name: string
  kind: string
  status: string
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
  csrfToken: string
  expiresAt: string
}

type BootstrapStatus = {
  required: boolean
  tokenRequired: boolean
  minimumPasswordCharacters: number
}

type AuthPhase = 'loading' | 'bootstrap' | 'login' | 'authenticated' | 'unavailable'

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
  return typeof principal.subject === 'string'
    && typeof principal.organizationId === 'string'
    && typeof principal.username === 'string'
    && typeof principal.email === 'string'
    && typeof principal.displayName === 'string'
    && Array.isArray(principal.roles)
    && principal.roles.every((role) => typeof role === 'string')
}

class ApiRequestError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
  }
}

async function requestJSON(path: string, init?: RequestInit): Promise<unknown> {
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  })
  if (!response.ok) {
    let message = 'The request could not be completed.'
    try {
      const body = await response.json() as unknown
      if (typeof body === 'object' && body !== null) {
        const error = (body as Record<string, unknown>).error
        if (typeof error === 'object' && error !== null) {
          const candidate = (error as Record<string, unknown>).message
          if (typeof candidate === 'string' && candidate.length > 0 && candidate.length <= 300) message = candidate
        }
      }
    } catch {
      // The status code remains authoritative when an intermediary returns non-JSON.
    }
    throw new ApiRequestError(response.status, message)
  }
  if (response.status === httpNoContent) return undefined
  return response.json() as Promise<unknown>
}

const httpNoContent = 204
const issuesUrl = resolvePublicUrl(import.meta.env.VITE_ISSUES_URL)

export default function App() {
  const [health, setHealth] = useState('Checking service…')
  const [assets, setAssets] = useState<Asset[]>([])
  const [organizationName, setOrganizationName] = useState('Your organization')
  const [authPhase, setAuthPhase] = useState<AuthPhase>('loading')
  const [principal, setPrincipal] = useState<Principal | null>(null)
  const [csrfToken, setCSRFToken] = useState('')
  const [tokenRequired, setTokenRequired] = useState(false)
  const [minimumPasswordCharacters, setMinimumPasswordCharacters] = useState(15)
  const [authError, setAuthError] = useState('')
  const [busy, setBusy] = useState(false)
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (authError) errorRef.current?.focus()
  }, [authError])

  useEffect(() => {
    let active = true
    fetch('/healthz', { credentials: 'same-origin' })
      .then((response) => {
        if (!response.ok) throw new Error('health request failed')
        return response.json()
      })
      .then(() => {
        if (active) setHealth('Service connected')
      })
      .catch(() => {
        if (active) setHealth('Start the Go service to connect')
      })

    // SEC-GUARD-001: restore only the server-managed HttpOnly session.
    requestJSON('/api/v1/auth/bootstrap')
      .then(async (value) => {
        if (!isBootstrapStatus(value)) throw new Error('invalid bootstrap status')
        if (!active) return
        setTokenRequired(value.tokenRequired)
        setMinimumPasswordCharacters(value.minimumPasswordCharacters)
        if (value.required) {
          setAuthPhase('bootstrap')
          return
        }
        try {
          const session = await requestJSON('/api/v1/auth/session')
          if (!isSessionResponse(session)) throw new Error('invalid session response')
          if (!active) return
          setPrincipal(session.principal)
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
        if (active) setAssets(Array.isArray(items) ? items : [])
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
      setAssets([])
      setAuthPhase('login')
    } catch (error) {
      setAuthError(error instanceof ApiRequestError ? error.message : 'Sign out could not be completed.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100" data-feature="authorization.security" data-requirement="SEC-GUARD-001">
      <a className="sr-only rounded bg-cyan-300 px-3 py-2 font-semibold text-slate-950 focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50" href="#main-content">Skip to main content</a>
      <header className="border-b border-slate-800 bg-slate-900/80">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-4 px-6 py-5">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.25em] text-cyan-300">Binary Cornfield presents</p>
            <h1 className="mt-1 text-2xl font-semibold tracking-tight">StewardMesh</h1>
            <p className="mt-1 text-sm text-slate-400" aria-live="polite" data-requirement="REQ-FOUNDATION-001">{organizationName}</p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {principal && <p className="text-sm text-slate-300">Signed in as <strong>{principal.displayName}</strong></p>}
            {principal && <button className="rounded-lg border border-slate-600 px-3 py-2 text-sm font-semibold hover:border-cyan-300 hover:text-cyan-200 disabled:cursor-wait disabled:opacity-60" disabled={busy} onClick={handleLogout} type="button">Sign out</button>}
            <p className="rounded-full border border-emerald-500/30 px-3 py-1 text-sm text-emerald-300" aria-live="polite">{health}</p>
          </div>
        </div>
      </header>

      <main id="main-content" className="mx-auto max-w-7xl space-y-10 px-6 py-10" tabIndex={-1}>
        {authError && <div ref={errorRef} className="rounded-xl border border-red-400/50 bg-red-950/50 p-4 text-red-100" role="alert" tabIndex={-1}>{authError}</div>}

        {authPhase === 'loading' && <section aria-labelledby="auth-loading-heading" className="rounded-2xl border border-slate-800 bg-slate-900 p-6"><h2 id="auth-loading-heading" className="text-xl font-semibold">Guard — Checking access</h2><p className="mt-2 text-slate-300" role="status">Checking administrator setup and your secure session.</p></section>}

        {authPhase === 'unavailable' && <section aria-labelledby="auth-unavailable-heading" className="rounded-2xl border border-amber-400/40 bg-amber-950/30 p-6"><h2 id="auth-unavailable-heading" className="text-xl font-semibold">Guard — Authentication unavailable</h2><p className="mt-2 text-slate-300">Confirm the Go service and database are running, then reload this page.</p><HelpLinks /></section>}

        {authPhase === 'bootstrap' && (
          <section aria-labelledby="bootstrap-heading" className="mx-auto max-w-2xl rounded-2xl border border-cyan-400/30 bg-slate-900 p-6 shadow-2xl shadow-cyan-950/20">
            <p className="text-sm font-semibold text-cyan-300">Guard — Secure local authentication</p>
            <h2 id="bootstrap-heading" className="mt-2 text-3xl font-semibold">Create the first administrator</h2>
            <p className="mt-3 leading-7 text-slate-300">This one-time account receives the organization-scoped Administrator role. Passwords are hashed before storage, and the browser receives only an HttpOnly session cookie.</p>
            <form className="mt-6 space-y-5" onSubmit={handleBootstrap}>
              <Field id="displayName" label="Display name" autoComplete="name" required />
              <Field id="email" label="Email address" autoComplete="email" type="email" required />
              <Field id="username" label="Username" autoComplete="username" pattern="[A-Za-z0-9][A-Za-z0-9._-]{2,63}" help="Use 3 to 64 letters, numbers, periods, underscores, or hyphens." required />
              <Field id="password" label="Password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} help={`Use at least ${minimumPasswordCharacters} characters. StewardMesh does not require arbitrary symbol or capitalization rules.`} required />
              <Field id="confirmPassword" label="Confirm password" autoComplete="new-password" type="password" minLength={minimumPasswordCharacters} required />
              {tokenRequired && <Field id="bootstrapToken" label="Deployment bootstrap token" autoComplete="off" type="password" help="Enter the token configured by your server administrator. It is sent only to the local StewardMesh API." required />}
              <button className="w-full rounded-xl bg-cyan-300 px-4 py-3 font-semibold text-slate-950 hover:bg-cyan-200 disabled:cursor-wait disabled:opacity-60" disabled={busy} type="submit">{busy ? 'Creating administrator…' : 'Create administrator'}</button>
            </form>
            <HelpLinks />
          </section>
        )}

        {authPhase === 'login' && (
          <section aria-labelledby="login-heading" className="mx-auto max-w-xl rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl shadow-black/20">
            <p className="text-sm font-semibold text-cyan-300">Guard — Secure local authentication</p>
            <h2 id="login-heading" className="mt-2 text-3xl font-semibold">Sign in to StewardMesh</h2>
            <p className="mt-3 text-slate-300">Use your local organization account. OIDC and OAuth providers will use the same Guard boundary in a later delivery slice.</p>
            <form className="mt-6 space-y-5" onSubmit={handleLogin}>
              <Field id="username" label="Username" autoComplete="username" required />
              <Field id="password" label="Password" autoComplete="current-password" type="password" required />
              <button className="w-full rounded-xl bg-cyan-300 px-4 py-3 font-semibold text-slate-950 hover:bg-cyan-200 disabled:cursor-wait disabled:opacity-60" disabled={busy} type="submit">{busy ? 'Signing in…' : 'Sign in'}</button>
            </form>
            <HelpLinks />
          </section>
        )}

        {authPhase === 'authenticated' && (
          <>
            <section aria-labelledby="welcome-heading" className="max-w-3xl">
              <p className="text-sm font-medium text-cyan-300">Connect what you steward. Plan what comes next.</p>
              <h2 id="welcome-heading" className="mt-3 text-4xl font-semibold tracking-tight sm:text-5xl">A clear view of what your organization owns, funds, and operates.</h2>
              <p className="mt-5 text-lg leading-8 text-slate-300">StewardMesh brings inventory, lifecycle planning, procurement, goals, and ownership into one accessible workspace.</p>
              {principal && <p className="mt-4 text-sm text-slate-400">Guard role: {principal.roles.join(', ') || 'No role assigned'}</p>}
            </section>

            <section aria-labelledby="modules-heading">
              <div className="flex flex-wrap items-end justify-between gap-4">
                <div><h2 id="modules-heading" className="text-xl font-semibold">StewardMesh modules</h2><p className="mt-1 text-sm text-slate-400">Every module has a plain-language descriptor and accessible help.</p></div>
                <a className="text-sm text-cyan-300 underline underline-offset-4 hover:text-cyan-200" href={issuesUrl}>Report an issue</a>
              </div>
              <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {modules.map(([name, description]) => <article key={name} className="rounded-2xl border border-slate-800 bg-slate-900 p-5 shadow-xl shadow-black/10"><p className="text-lg font-semibold">{name}</p><p className="mt-2 text-sm text-slate-400">{description}</p></article>)}
              </div>
            </section>

            <section aria-labelledby="assets-heading" className="rounded-2xl border border-slate-800 bg-slate-900 p-6">
              <h2 id="assets-heading" className="text-xl font-semibold">Atlas — Asset inventory</h2>
              <p className="mt-1 text-sm text-slate-400">Asset records are protected by Guard permissions and the organization ownership boundary.</p>
              {assets.length === 0 ? <p className="mt-6 rounded-xl border border-dashed border-slate-700 p-5 text-sm text-slate-400">No assets yet. Add your first server or device through the API to begin.</p> : <ul className="mt-6 divide-y divide-slate-800">{assets.map((asset) => <li className="flex flex-wrap justify-between gap-2 py-4" key={asset.id}><span>{asset.name}</span><span className="text-sm text-slate-400">{asset.kind} · {asset.status}</span></li>)}</ul>}
            </section>
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
      <label className="block text-sm font-semibold text-slate-200" htmlFor={id}>{label}</label>
      {help && <p className="mt-1 text-sm leading-6 text-slate-400" id={helpID}>{help}</p>}
      <input
        aria-describedby={helpID}
        autoComplete={autoComplete}
        className="mt-2 w-full rounded-xl border border-slate-600 bg-slate-950 px-4 py-3 text-slate-100 shadow-inner shadow-black/20 hover:border-slate-500"
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
    <p className="mt-6 text-sm text-slate-400">
      <a className="text-cyan-300 underline underline-offset-4 hover:text-cyan-200" href={guardHelpUrl}>Read Guard setup help</a>
      <span aria-hidden="true"> · </span>
      <a className="text-cyan-300 underline underline-offset-4 hover:text-cyan-200" href={issuesUrl}>Report an issue</a>
    </p>
  )
}
