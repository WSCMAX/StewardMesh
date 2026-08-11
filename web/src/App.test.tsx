import axe from 'axe-core'
import { StrictMode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import App, { resolvePublicUrl } from './App'

// Requirements: A11Y-001, DOC-001, DOC-002, SEC-GUARD-001, SEC-HTTP-001. Feature: experience.help.

const session = {
  principal: {
    subject: 'account-1',
    organizationId: 'example-org',
    username: 'administrator',
    email: 'administrator@example.test',
    displayName: 'Example Administrator',
    roles: ['Administrator'],
  },
  permissions: ['assets.read', 'assets.write', 'directory.read', 'directory.write'],
  csrfToken: 'csrf-token-with-at-least-thirty-two-characters',
  expiresAt: '2030-01-01T00:00:00Z',
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function installAuthenticatedFetch() {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/healthz') return jsonResponse({ status: 'ok' })
    if (path === '/api/v1/auth/bootstrap') return jsonResponse({ required: false, tokenRequired: false, minimumPasswordCharacters: 15, oidcEnabled: false, samlEnabled: false })
    if (path === '/api/v1/auth/session') return jsonResponse(session)
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
  expect(await screen.findByText('Atlas — Asset inventory')).toBeInTheDocument()
  expect(screen.getByText('Signed in as', { exact: false })).toHaveTextContent('Example Administrator')
  expect(screen.getByText('Guard role:', { exact: false })).toHaveTextContent('Administrator')
})

test('opens contextual Guide help and a sanitized issue report from the workspace', async () => {
  installAuthenticatedFetch()
  render(<App />)
  await screen.findByText('Atlas — Asset inventory')

  fireEvent.click(screen.getByRole('button', { name: 'Open Atlas help' }))
  expect(screen.getByRole('heading', { name: 'What you can do here' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Take a quick tour of your workspace' })).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Read Atlas documentation' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Report issue' }))
  expect(screen.getByRole('heading', { name: 'Prepare technical context' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Review issue before submitting' })).toHaveAttribute('href', expect.stringContaining('/issues/new'))
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
