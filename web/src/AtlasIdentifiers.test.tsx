import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import AtlasIdentifiers, { type AssetIdentifier } from './AtlasIdentifiers'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001, SEC-GUARD-001.

const identifier: AssetIdentifier = {
  id: 'identifier-1', organizationId: 'example-org', assetId: 'asset-1', symbology: 'code128',
  normalizedValue: 'LAB-001', displayValue: 'LAB-001', source: 'user_entered', primary: true,
  status: 'active', revision: 1, createdBy: 'account-1',
  createdAt: '2026-08-11T12:00:00Z', updatedAt: '2026-08-11T12:00:00Z',
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('shows identifier history read-only and has no automated accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ items: [identifier] })))
  const { container } = render(<AtlasIdentifiers assetId="asset-1" assetName="Lab server" canWrite={false} csrfToken="csrf-token" />)
  expect(await screen.findByText('LAB-001')).toBeInTheDocument()
  expect(screen.getByText(/Code 128 · active · user entered · primary/)).toBeInTheDocument()
  expect(screen.getByText(/Created by account-1 on/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Associate identifier' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Replace' })).not.toBeInTheDocument()
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('associates a manual identifier with CSRF and server-owned state', async () => {
  let listed = false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.endsWith('/identifiers') && !init?.method) return jsonResponse({ items: listed ? [identifier] : [] })
    if (path.endsWith('/identifiers') && init?.method === 'POST') {
      listed = true
      return jsonResponse({ identifier, created: true }, 201)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasIdentifiers assetId="asset-1" assetName="Lab server" canWrite csrfToken="csrf-token" />)
  await screen.findByText('No identifiers are associated with this asset.')
  fireEvent.click(screen.getByRole('button', { name: 'Associate identifier' }))
  const form = within(screen.getByRole('form', { name: 'Associate an identifier' }))
  const encodedValue = form.getByLabelText('Encoded value')
  expect(encodedValue).toHaveAttribute('autocapitalize', 'none')
  expect(encodedValue).toHaveAttribute('autocorrect', 'off')
  expect(encodedValue).toHaveAttribute('spellcheck', 'false')
  fireEvent.change(encodedValue, { target: { value: 'LAB-001' } })
  fireEvent.change(form.getByLabelText(/Display value/), { target: { value: 'Lab label 001' } })
  fireEvent.click(form.getByLabelText('Primary identifier'))
  fireEvent.click(form.getByRole('button', { name: 'Associate identifier' }))
  expect(await screen.findByText('Identifier associated.')).toBeInTheDocument()

  const request = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toEqual({
    symbology: 'code128', value: 'LAB-001', displayValue: 'Lab label 001', source: 'user_entered', primary: true,
  })
})

test('adopts a camera-scanned QR value into the associate form without saving until confirm', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ items: [] })))
  const stop = vi.fn()
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), {
    mediaDevices: { getUserMedia: vi.fn(async () => ({ getTracks: () => [{ stop }] }) as unknown as MediaStream) },
  }))
  vi.stubGlobal('BarcodeDetector', class {
    detect = vi.fn(async () => [{ format: 'qr_code', rawValue: 'CAMERA-QR-VALUE' }])
  })
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    queueMicrotask(() => callback(1))
    return 18
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

  render(<AtlasIdentifiers assetId="asset-1" assetName="Lab server" canWrite csrfToken="csrf-token" />)
  await screen.findByText('No identifiers are associated with this asset.')
  fireEvent.click(screen.getByRole('button', { name: 'Associate identifier' }))
  fireEvent.click(screen.getByRole('button', { name: 'Scan with camera' }))

  expect(await screen.findByText(/Code captured/)).toBeInTheDocument()
  expect(screen.getByLabelText('Symbology')).toHaveValue('qr')
  expect(screen.getByLabelText('Encoded value')).toHaveValue('CAMERA-QR-VALUE')
  expect(screen.getByLabelText(/Display value/)).toHaveValue('CAMERA-QR-VALUE')
  expect(stop).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: 'Associate identifier', hidden: false })).toBeInTheDocument()
})

test('replaces and explicitly confirms deactivation using current revisions', async () => {
  const replaced = { ...identifier, status: 'replaced' as const, replacedById: 'identifier-2', revision: 2 }
  const replacement = {
    ...identifier, id: 'identifier-2', normalizedValue: 'LAB-002', displayValue: 'LAB-002',
    supersedesId: identifier.id, revision: 1,
  }
  const deactivated = { ...replacement, status: 'deactivated' as const, revision: 2, deactivatedAt: '2026-08-11T13:00:00Z' }
  let phase = 0
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (!init?.method && path.endsWith('/identifiers')) {
      const items = phase === 0 ? [identifier] : phase === 1 ? [replaced, replacement] : [replaced, deactivated]
      return jsonResponse({ items })
    }
    if (init?.method === 'POST' && path.endsWith('/identifier-1/replace')) {
      phase = 1
      return jsonResponse({ identifier: replacement, changed: true })
    }
    if (init?.method === 'POST' && path.endsWith('/identifier-2/deactivate')) {
      phase = 2
      return jsonResponse({ identifier: deactivated, changed: true })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasIdentifiers assetId="asset-1" assetName="Lab server" canWrite csrfToken="csrf-token" />)
  await screen.findByText('LAB-001')
  fireEvent.click(screen.getByRole('button', { name: 'Replace' }))
  const replaceForm = within(screen.getByRole('form', { name: 'Replace LAB-001' }))
  fireEvent.change(replaceForm.getByLabelText('Encoded value'), { target: { value: 'LAB-002' } })
  fireEvent.click(replaceForm.getByRole('button', { name: 'Confirm replacement' }))
  expect(await screen.findByText(/previous association remains in history/)).toBeInTheDocument()
  await screen.findByText('LAB-002')

  const replaceRequest = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/replace'))
  expect(JSON.parse(String(replaceRequest?.[1]?.body))).toMatchObject({ revision: 1, symbology: 'code128', value: 'LAB-002' })
  expect(JSON.parse(String(replaceRequest?.[1]?.body))).not.toHaveProperty('primary')

  fireEvent.click(screen.getByRole('button', { name: 'Deactivate' }))
  expect(screen.getByText(/Deactivate this association/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Confirm deactivation' }))
  expect(await screen.findByText(/history is preserved/)).toBeInTheDocument()
  const deactivateRequest = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/deactivate'))
  expect(JSON.parse(String(deactivateRequest?.[1]?.body))).toEqual({ revision: 1 })
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Deactivate' })).not.toBeInTheDocument())
})

test('ignores an out-of-order identifier response after the selected asset changes', async () => {
  let resolveFirst!: (response: Response) => void
  let resolveSecond!: (response: Response) => void
  const firstIdentifier = { ...identifier, assetId: 'asset-1', displayValue: 'First asset code' }
  const secondIdentifier = { ...identifier, id: 'identifier-2', assetId: 'asset-2', displayValue: 'Second asset code' }
  const fetchMock = vi.fn((input: RequestInfo | URL) => new Promise<Response>((resolve) => {
    if (String(input).endsWith('/asset-1/identifiers')) resolveFirst = resolve
    else if (String(input).endsWith('/asset-2/identifiers')) resolveSecond = resolve
    else throw new Error(`unexpected request: ${String(input)}`)
  }))
  vi.stubGlobal('fetch', fetchMock)

  const { rerender } = render(<AtlasIdentifiers assetId="asset-1" assetName="First asset" canWrite={false} csrfToken="csrf-token" />)
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  rerender(<AtlasIdentifiers assetId="asset-2" assetName="Second asset" canWrite={false} csrfToken="csrf-token" />)
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

  resolveSecond(jsonResponse({ items: [secondIdentifier] }))
  expect(await screen.findByText('Second asset code')).toBeInTheDocument()
  resolveFirst(jsonResponse({ items: [firstIdentifier] }))
  await waitFor(() => expect(screen.queryByText('First asset code')).not.toBeInTheDocument())
  expect(screen.getByText('Second asset code')).toBeInTheDocument()
})
