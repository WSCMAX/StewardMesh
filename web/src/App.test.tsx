import axe from 'axe-core'
import { StrictMode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import App, { resolvePublicUrl } from './App'
import { authenticationRequiredEventName } from './api'

// Requirements: REQ-WORKSPACE-001, REQ-HORIZON-001, A11Y-001, DOC-001, DOC-002, SEC-GUARD-001, SEC-HTTP-001. Features: experience.workspace, lifecycle.planning, experience.help.

const session = {
  principal: {
    subject: 'account-1',
    organizationId: 'example-org',
    username: 'administrator',
    email: 'administrator@example.test',
    displayName: 'Example Administrator',
    roles: ['Administrator'],
  },
  permissions: ['organization.read', 'assets.read', 'assets.write', 'directory.read', 'directory.write'],
  grants: ['organization.read', 'assets.read', 'assets.write', 'directory.read', 'directory.write'].map((permission) => ({
    permission,
    scope: { kind: 'organization', resourceId: 'example-org' },
  })),
  csrfToken: 'csrf-token-with-at-least-thirty-two-characters',
  expiresAt: '2030-01-01T00:00:00Z',
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function installAuthenticatedFetch(healthAvailable = true, sessionValue = session) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') {
      if (!healthAvailable) throw new Error('service unavailable')
      return jsonResponse({ status: 'ok' })
    }
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({ required: false, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    if (path === '/api/v1/auth/session') return jsonResponse(sessionValue)
    if (path === '/api/v1/organization') return jsonResponse({ id: 'example-org', name: 'Example Organization' })
    if (path === '/api/v1/assets') return jsonResponse({ items: [] })
    if (path === '/api/v1/sites' || path === '/api/v1/departments' || path.startsWith('/api/v1/identities?')) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  window.history.replaceState(null, '', '/')
})

test('restores a server-managed session and renders StewardMesh modules', async () => {
  installAuthenticatedFetch()
  render(<App />)
  expect(screen.getByRole('heading', { name: 'StewardMesh' })).toBeInTheDocument()
  expect(document.querySelector('img[src="/brand/stewardmesh-s-mark.svg"]')).toBeInTheDocument()
  expect(await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Atlas — Asset inventory' })).toBeInTheDocument()
  expect(screen.getByText('Signed in as', { exact: false })).toHaveTextContent('Example Administrator')
  expect(screen.getByText('Your access').parentElement).toHaveTextContent('Administrator')
})

test('opens contextual Guide help and a sanitized issue report from the workspace', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  fireEvent.click(screen.getByRole('button', { name: 'Open Atlas' }))
  await waitFor(() => expect(document.getElementById('assets-heading')).toBeVisible())
  fireEvent.click(screen.getByRole('button', { name: 'Help for Atlas' }))
  expect(screen.getByRole('heading', { name: 'What you can do here' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Take a quick tour of your workspace' })).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Read Atlas documentation' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Report issue' }))
  expect(screen.getByRole('heading', { name: 'Prepare technical context' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Review issue before submitting' })).toHaveAttribute('href', expect.stringContaining('/issues/new'))
})

test('activates a focused work area before Guide follows its section link', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })
  const atlasTarget = document.getElementById('guide-atlas') as HTMLElement
  const scrollIntoView = vi.fn<(arg?: boolean | ScrollIntoViewOptions) => void>()
  atlasTarget.scrollIntoView = scrollIntoView

  fireEvent.click(screen.getAllByRole('button', { name: 'Open Guide' })[0])
  fireEvent.change(screen.getByRole('combobox', { name: 'Help topic' }), { target: { value: 'atlas' } })
  fireEvent.click(screen.getByRole('link', { name: 'Go to Atlas' }))

  await waitFor(() => expect(document.getElementById('assets-heading')).toBeVisible())
  expect(window.location.hash).toBe('#workspace-atlas')
  expect(scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
})

test('switches work areas, updates the deep link, and preserves in-progress Atlas context', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  fireEvent.click(screen.getByRole('button', { name: 'Open Atlas' }))
  const search = await screen.findByRole('searchbox', { name: 'Search' })
  fireEvent.change(search, { target: { value: 'server awaiting deployment' } })
  expect(window.location.hash).toBe('#workspace-atlas')

  fireEvent.click(screen.getByRole('link', { name: 'Overview — Work queue and product areas' }))
  expect(screen.getByRole('heading', { name: 'Overview — Work queue and product areas' })).toBeVisible()
  fireEvent.click(screen.getByRole('link', { name: 'Atlas — Asset inventory' }))
  expect(screen.getByRole('searchbox', { name: 'Search' })).toHaveValue('server awaiting deployment')
  expect(document.getElementById('assets-heading')).toBeVisible()
})

test('renders the authenticated Workspace without automated accessibility violations', async () => {
  installAuthenticatedFetch()
  const { container } = render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('explains permission-limited areas without mounting protected feature content', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  fireEvent.click(screen.getByRole('button', { name: 'Open Horizon' }))
  expect(await screen.findByRole('heading', { name: 'Horizon data is protected' })).toBeVisible()
  expect(screen.getByText('planning.read')).toBeVisible()
  expect(document.getElementById('horizon-heading')).not.toBeInTheDocument()
})

test('shows scoped access without requesting or mounting an organization-wide collection', async () => {
  const scopedSession = {
    ...session,
    permissions: [],
    grants: [{ permission: 'assets.read', scope: { kind: 'site', resourceId: 'site-one' } }],
  }
  const fetchMock = installAuthenticatedFetch(true, scopedSession)
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  expect(screen.getByText('Scoped')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Open Atlas' }))
  expect(await screen.findByRole('heading', { name: 'Atlas access is limited to assigned records' })).toBeVisible()
  expect(screen.getByText(/Organization-wide lists stay closed/)).toBeVisible()
  expect(document.getElementById('assets-heading')).not.toBeInTheDocument()
  expect(fetchMock.mock.calls.some(([path]) => path === '/api/v1/assets')).toBe(false)
})

test('labels organization-wide readers as read only and keeps mutation actions hidden', async () => {
  const readOnlySession = {
    ...session,
    permissions: ['organization.read', 'assets.read'],
    grants: ['organization.read', 'assets.read'].map((permission) => ({ permission, scope: { kind: 'organization', resourceId: 'example-org' } })),
  }
  installAuthenticatedFetch(true, readOnlySession)
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  expect(screen.getByText('Read only')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Open Atlas' }))
  expect(await screen.findByText('Requires assets.write')).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Add asset' })).not.toBeInTheDocument()
})

test('returns to a recoverable login state when an authenticated request reports an expired session', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })

  window.dispatchEvent(new CustomEvent(authenticationRequiredEventName))
  expect(await screen.findByRole('heading', { name: 'Sign in to StewardMesh' })).toBeVisible()
  expect(screen.getByRole('alert')).toHaveTextContent('Your session expired. Sign in again to continue')
})

test('marks retained work as potentially stale while the service is unavailable', async () => {
  installAuthenticatedFetch(false)
  render(<App />)
  await screen.findByRole('heading', { name: 'Overview — Work queue and product areas' })
  expect(await screen.findByText('Service unavailable.')).toBeVisible()
  expect(screen.getByText(/Previously loaded context may be stale/)).toBeVisible()
})

test('renders an accessible one-time administrator setup and submits without browser token storage', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap' && !init?.method) {
      return jsonResponse({ required: true, tokenRequired: true, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    }
    if (path === '/api/v1/auth/bootstrap' && init?.method === 'POST') return jsonResponse(session, 201)
    if (path === '/api/v1/organization') return jsonResponse({ id: 'example-org', name: 'Example Organization' })
    if (path === '/api/v1/assets') return jsonResponse({ items: [] })
    if (path === '/api/v1/sites' || path === '/api/v1/departments' || path.startsWith('/api/v1/identities?')) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<App />)

  expect(await screen.findByRole('heading', { name: 'Create the first administrator' })).toBeInTheDocument()
  expect(screen.getByLabelText('Username')).toHaveAttribute('pattern', '[A-Za-z0-9][A-Za-z0-9._\\-]{2,63}')
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Example Administrator' } })
  fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'administrator@example.test' } })
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'administrator' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery staple' } })
  fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'correct horse battery staple' } })
  fireEvent.change(screen.getByLabelText('Deployment bootstrap token'), { target: { value: 'deployment-bootstrap-token-value' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create administrator' }))

  expect(await screen.findByText('Atlas — Asset inventory')).toBeInTheDocument()
  const bootstrapCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/auth/bootstrap' && init?.method === 'POST')
  expect(bootstrapCall).toBeDefined()
  expect(bootstrapCall?.[1]).toMatchObject({ credentials: 'same-origin' })
  expect(JSON.parse(String(bootstrapCall?.[1]?.body))).toMatchObject({
    bootstrapToken: 'deployment-bootstrap-token-value',
  })
})

test('omits the bootstrap token when the deployment does not require one', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap' && !init?.method) {
      return jsonResponse({ required: true, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    }
    if (path === '/api/v1/auth/bootstrap' && init?.method === 'POST') return jsonResponse(session, 201)
    if (path === '/api/v1/organization') return jsonResponse({ id: 'example-org', name: 'Example Organization' })
    if (path === '/api/v1/assets') return jsonResponse({ items: [] })
    if (path === '/api/v1/sites' || path === '/api/v1/departments' || path.startsWith('/api/v1/identities?')) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<App />)

  await screen.findByRole('heading', { name: 'Create the first administrator' })
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Example Administrator' } })
  fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'administrator@example.test' } })
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'administrator' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery staple' } })
  fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'correct horse battery staple' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create administrator' }))

  await screen.findByText('Atlas — Asset inventory')
  const bootstrapCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/auth/bootstrap' && init?.method === 'POST')
  const requestBody = JSON.parse(String(bootstrapCall?.[1]?.body)) as Record<string, unknown>
  expect(requestBody).not.toHaveProperty('bootstrapToken')
})

test('falls back to login when no authenticated session exists', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({ required: false, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    if (path === '/api/v1/auth/session') return jsonResponse({ error: { message: 'sign in is required' } }, 401)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Sign in to StewardMesh' })).toBeInTheDocument()
  expect(screen.getByLabelText('Username')).toHaveAttribute('autocomplete', 'username')
  expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'current-password')
})

test('offers organization sign-in and announces a safe callback failure', async () => {
  window.history.replaceState(null, '', '/?auth=oidc_error')
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({
      required: false, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: true, samlEnabled: false,
    })
    if (path === '/api/v1/auth/session') return jsonResponse({ error: { message: 'sign in is required' } }, 401)
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<StrictMode><App /></StrictMode>)
  const providerLink = await screen.findByRole('link', { name: 'Continue with OpenID Connect' })
  expect(providerLink).toHaveAttribute('href', '/api/v1/auth/oidc/start')
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('OpenID Connect sign-in could not be completed')
  await waitFor(() => expect(alert).toHaveFocus())
  expect(window.location.search).toBe('')
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('offers SAML sign-in and announces a safe assertion failure', async () => {
  window.history.replaceState(null, '', '/?auth=saml_error')
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({
      required: false, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: true,
    })
    if (path === '/api/v1/auth/session') return jsonResponse({ error: { message: 'sign in is required' } }, 401)
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<App />)
  const providerLink = await screen.findByRole('link', { name: 'Continue with SAML' })
  expect(providerLink).toHaveAttribute('href', '/api/v1/auth/saml/start')
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('SAML sign-in could not be completed')
  await waitFor(() => expect(alert).toHaveFocus())
  expect(window.location.search).toBe('')
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('Guard administrator setup has no automated WCAG violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({ required: true, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<App />)
  await screen.findByRole('heading', { name: 'Create the first administrator' })
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('allows only safe configurable public links', () => {
  expect(resolvePublicUrl('javascript:alert(1)')).toBe('https://github.com/WSCMAX/StewardMesh/issues')
  expect(resolvePublicUrl('https://token@example.org/issues')).toBe('https://github.com/WSCMAX/StewardMesh/issues')
  expect(resolvePublicUrl('/support/issues')).toBe('/support/issues')
  expect(resolvePublicUrl('https://issues.example.org/project')).toBe('https://issues.example.org/project')
})

test('password mismatch is announced and receives keyboard focus', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({ required: true, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    throw new Error(`unexpected request: ${path}`)
  }))
  render(<App />)
  await screen.findByRole('heading', { name: 'Create the first administrator' })
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Example Administrator' } })
  fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'administrator@example.test' } })
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'administrator' } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery staple' } })
  fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'different password value' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create administrator' }))
  const alert = await screen.findByRole('alert')
  await waitFor(() => expect(alert).toHaveFocus())
})
