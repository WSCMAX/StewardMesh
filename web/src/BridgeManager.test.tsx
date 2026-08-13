import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import BridgeManager from './BridgeManager'

// Requirements: REQ-API-001, SEC-MCP-001, A11Y-001. Feature: integrations.protocols. GitHub: #14.

const client = { id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', name: 'Inventory assistant', redirectUris: ['https://client.example/callback'], allowedScopes: ['mcp:resources', 'assets:read'], createdAt: '2026-08-13T12:00:00Z' }
const grant = { id: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', clientId: client.id, clientName: client.name, actorId: 'administrator', scopes: ['mcp:resources', 'assets:read'], accessExpiresAt: '2026-08-13T12:15:00Z', refreshExpiresAt: '2026-08-13T20:00:00Z' }
function response(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }

beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); window.history.replaceState(null, '', '/') })

test('renders bounded client and grant administration without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => new URL(String(input), window.location.origin).pathname.endsWith('/clients') ? response({ items: [client] }) : response({ items: [grant] })))
  const { container } = render(<BridgeManager csrfToken="csrf" permissions={['integrations.read']} />)
  expect(await screen.findAllByText('Inventory assistant')).not.toHaveLength(0)
  expect(screen.getByText('MCP 2026-07-28')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Register public client' })).not.toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('registers an exact public redirect and sends in-memory CSRF', async () => {
  let clients = [client]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), window.location.origin).pathname
    if (path.endsWith('/clients') && init?.method === 'POST') { clients = [...clients, { ...client, id: 'cccccccccccccccccccccccccccccccc', name: 'Reporting client' }]; return response(clients.at(-1), 201) }
    if (path.endsWith('/clients')) return response({ items: clients })
    if (path.endsWith('/grants')) return response({ items: [] })
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<BridgeManager csrfToken="csrf-value" permissions={['integrations.read', 'integrations.write']} />)
  await screen.findByText('Inventory assistant')
  fireEvent.change(screen.getByLabelText('Client name'), { target: { value: 'Reporting client' } })
  fireEvent.change(screen.getByLabelText('Exact redirect URI'), { target: { value: 'https://reports.example/callback' } })
  fireEvent.click(screen.getByLabelText('signals:read'))
  fireEvent.click(screen.getByRole('button', { name: 'Register public client' }))
  expect(await screen.findByText('OAuth client registered.')).toBeInTheDocument()
  const call = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/clients') && init?.method === 'POST')
  expect(call?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(JSON.parse(String(call?.[1]?.body))).toEqual({ name: 'Reporting client', redirectUris: ['https://reports.example/callback'], allowedScopes: ['mcp:resources', 'signals:read'] })
  await waitFor(() => expect(screen.getAllByText('Reporting client').length).toBeGreaterThan(0))
})

test('loads bounded client and grant continuation pages', async () => {
  const secondClient = { ...client, id: 'cccccccccccccccccccccccccccccccc', name: 'Second client' }
  const secondGrant = { ...grant, id: 'dddddddddddddddddddddddddddddddd', clientId: secondClient.id, clientName: secondClient.name }
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const target = new URL(String(input), window.location.origin)
    if (target.pathname.endsWith('/clients')) return target.searchParams.has('cursor') ? response({ items: [secondClient] }) : response({ items: [client], nextCursor: client.id })
    if (target.pathname.endsWith('/grants')) return target.searchParams.has('cursor') ? response({ items: [secondGrant] }) : response({ items: [grant], nextCursor: grant.id })
    throw new Error(`unexpected request ${target}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<BridgeManager csrfToken="csrf" permissions={['integrations.read']} />)
  await screen.findAllByText('Inventory assistant')
  fireEvent.click(screen.getByRole('button', { name: 'Load more clients' }))
  expect(await screen.findByText('Second client')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Load more grants' }))
  await waitFor(() => expect(screen.getAllByText('Second client').length).toBeGreaterThan(1))
  expect(fetchMock.mock.calls.some(([input]) => new URL(String(input), window.location.origin).searchParams.get('cursor') === client.id)).toBe(true)
})
