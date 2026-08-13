import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import AtlasScanner from './AtlasScanner'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001. Feature: inventory.identifiers.

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('finds an authorized asset from explicit Code 128 keyboard-wedge input and suppresses a duplicate burst', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ assetId: 'asset-1' }))
  vi.stubGlobal('fetch', fetchMock)
  const onResolveAsset = vi.fn(async () => undefined)
  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={onResolveAsset} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  const input = screen.getByLabelText('Scanned or entered value')
  fireEvent.change(input, { target: { value: 'LAB-001' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  expect(await screen.findByText(/Identifier matched/)).toBeInTheDocument()
  expect(onResolveAsset).toHaveBeenCalledWith('asset-1')
  expect(fetchMock).toHaveBeenCalledTimes(1)
  expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ symbology: 'code128', value: 'LAB-001' })

  fireEvent.change(input, { target: { value: 'LAB-001' } })
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(await screen.findByText(/Duplicate scan ignored/)).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledTimes(1)
})

test('associates QR input only with the explicitly selected asset and sends CSRF', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ identifier: { assetId: 'asset-1' }, created: true }, 201))
  vi.stubGlobal('fetch', fetchMock)
  const onAssociated = vi.fn()
  render(<AtlasScanner canWrite csrfToken="csrf-token" onAssociated={onAssociated} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={{ id: 'asset-1', name: 'Lab server' }} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'associate' } })
  fireEvent.change(screen.getByLabelText('Symbology'), { target: { value: 'qr' } })
  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'asset-route-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Associate identifier' }))

  expect(await screen.findByText('Identifier associated with Lab server.')).toBeInTheDocument()
  expect(onAssociated).toHaveBeenCalledTimes(1)
  expect(String(fetchMock.mock.calls[0]?.[0])).toBe('/api/v1/assets/asset-1/identifiers')
  expect(fetchMock.mock.calls[0]?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
    symbology: 'qr', value: 'asset-route-1', displayValue: 'asset-route-1', source: 'user_entered', primary: false,
  })
})

test('keeps manual input available when camera access is unavailable and rejects malformed values locally', async () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasScanner canWrite={false} csrfToken="" onAssociated={vi.fn()} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  expect(screen.queryByRole('option', { name: 'Associate with selected asset' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))
  expect(await screen.findByText(/Camera scanning is not available/)).toBeInTheDocument()

  const input = screen.getByLabelText('Scanned or entered value')
  fireEvent.change(input, { target: { value: 'bad\u007fvalue' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find asset' }))
  expect(await screen.findByText(/printable ASCII/)).toBeInTheDocument()
  expect(fetchMock).not.toHaveBeenCalled()

  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('retains a failed value for an explicit retry and cancellation closes the active surface', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ error: { message: 'No visible match.' } }, 404))
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'UNKNOWN' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find asset' }))
  expect(await screen.findByRole('button', { name: 'Retry scan' })).toBeInTheDocument()
  expect(screen.getByLabelText('Scanned or entered value')).toHaveValue('UNKNOWN')

  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'REVIEWED' } })
  expect(screen.getByRole('button', { name: 'Find asset' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Cancel scanning' }))
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Scan an Atlas Code' })).not.toBeInTheDocument())
  expect(screen.getByText(/Scanning cancelled/)).toBeInTheDocument()
})
