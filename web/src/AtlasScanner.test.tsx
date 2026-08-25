import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import AtlasScanner from './AtlasScanner'
import { identityBarcodeFormats } from './barcodeCapture'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001. Feature: inventory.identifiers.

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

afterEach(() => {
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
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/v1/asset-identifiers/resolve')).toHaveLength(1)
  expect(JSON.parse(String(fetchMock.mock.calls.find(([path]) => path === '/api/v1/asset-identifiers/resolve')?.[1]?.body))).toEqual({ symbology: 'code128', value: 'LAB-001' })

  fireEvent.change(input, { target: { value: 'LAB-001' } })
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(await screen.findByText(/Duplicate scan ignored/)).toBeInTheDocument()
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/v1/asset-identifiers/resolve')).toHaveLength(1)
})

test('supports a Tab terminator, bounds scanner bursts, and keeps paste as a manual fallback', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ assetId: 'asset-tab' }))
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Scanner terminator'), { target: { value: 'Tab' } })
  fireEvent.change(screen.getByLabelText('Scanner burst window'), { target: { value: '250' } })
  const input = screen.getByLabelText('Scanned or entered value')
  const now = vi.spyOn(Date, 'now')
  now.mockReturnValue(1_000)
  fireEvent.change(input, { target: { value: 'SLOW-SCAN' } })
  fireEvent.keyDown(input, { key: 'S' })
  now.mockReturnValue(1_300)
  fireEvent.keyDown(input, { key: 'Tab' })
  expect(await screen.findByText(/exceeded the 250 ms burst window/)).toBeInTheDocument()
  expect(input).toHaveValue('SLOW-SCAN')
  expect(fetchMock).not.toHaveBeenCalled()

  now.mockReturnValue(2_000)
  fireEvent.keyDown(input, { key: 'S' })
  fireEvent.paste(input, { clipboardData: { getData: () => 'PASTED-CODE' } })
  fireEvent.change(input, { target: { value: 'PASTED-CODE' } })
  now.mockReturnValue(5_000)
  fireEvent.keyDown(input, { key: 'Tab' })
  expect(await screen.findByText(/Identifier matched/)).toBeInTheDocument()
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/v1/asset-identifiers/resolve')).toHaveLength(1)
  expect(JSON.parse(String(fetchMock.mock.calls.find(([path]) => path === '/api/v1/asset-identifiers/resolve')?.[1]?.body))).toEqual({ symbology: 'code128', value: 'PASTED-CODE' })
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

test('lets the operator choose the asset to attach a code to from the Scan tab', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.startsWith('/api/v1/assets?q=')) {
      return jsonResponse({ items: [{ id: 'asset-1', name: 'Lab server', assetTag: 'LAB-001', serialNumber: 'SERIAL-001' }] })
    }
    if (path === '/api/v1/assets/asset-1/identifiers' && init?.method === 'POST') {
      return jsonResponse({ identifier: { assetId: 'asset-1' }, created: true }, 201)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssociated = vi.fn()
  function Harness() {
    const [selected, setSelected] = useState<{ id: string; name: string; assetTag?: string } | null>(null)
    return <AtlasScanner
      canWrite
      csrfToken="csrf-token"
      onAssociated={onAssociated}
      onClearAsset={() => setSelected(null)}
      onResolveAsset={async (assetId) => setSelected({ id: assetId, name: 'Lab server', assetTag: 'LAB-001' })}
      selectedAsset={selected}
    />
  }
  render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'associate' } })
  expect(screen.getByLabelText('Asset to attach this code to')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Associate identifier' })).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Asset to attach this code to'), { target: { value: 'lab' } })
  fireEvent.click(screen.getByRole('button', { name: 'Search' }))
  fireEvent.click(await screen.findByRole('option', { name: /Lab server/ }))
  expect(await screen.findByText(/Ready to attach a Code 128 or QR to Lab server/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'asset-route-1' } })
  fireEvent.change(screen.getByLabelText('Symbology'), { target: { value: 'qr' } })
  fireEvent.click(screen.getByRole('button', { name: 'Associate identifier' }))
  expect(await screen.findByText('Identifier associated with Lab server.')).toBeInTheDocument()
  expect(onAssociated).toHaveBeenCalledTimes(1)
})

test('keeps manual input available when camera access is unavailable and rejects malformed values locally', async () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasScanner canWrite={false} csrfToken="" onAssociated={vi.fn()} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  expect(screen.queryByRole('option', { name: 'Attach a code to an asset' })).not.toBeInTheDocument()
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

test('decodes a Code 128 camera frame into an explicit find and stops capture', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ assetId: 'asset-camera' }))
  vi.stubGlobal('fetch', fetchMock)
  const stop = vi.fn()
  const getUserMedia = vi.fn(async () => ({ getTracks: () => [{ stop }] }) as unknown as MediaStream)
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { mediaDevices: { getUserMedia } }))
  let requestedFormats: string[] | undefined
  vi.stubGlobal('BarcodeDetector', class {
    constructor(options?: { formats?: string[] }) {
      requestedFormats = options?.formats
    }

    detect = vi.fn(async () => [{ format: 'code_128', rawValue: 'CAMERA-CODE-128' }])
  })
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    queueMicrotask(() => callback(1))
    return 17
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  const onResolveAsset = vi.fn(async () => undefined)

  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={onResolveAsset} selectedAsset={null} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))

  expect(await screen.findByText(/Identifier matched/)).toBeInTheDocument()
  expect(requestedFormats).toEqual([...identityBarcodeFormats])
  expect(onResolveAsset).toHaveBeenCalledWith('asset-camera')
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/v1/asset-identifiers/resolve')).toHaveLength(1)
  expect(JSON.parse(String(fetchMock.mock.calls.find(([path]) => path === '/api/v1/asset-identifiers/resolve')?.[1]?.body))).toEqual({ symbology: 'code128', value: 'CAMERA-CODE-128' })
  expect(stop).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: 'Use camera' })).toBeInTheDocument()
})

test('decodes a QR camera frame only into the selected explicit association mode', async () => {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => jsonResponse({ identifier: { assetId: 'asset-camera' }, created: true }, 201))
  vi.stubGlobal('fetch', fetchMock)
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
  const onAssociated = vi.fn()

  render(<AtlasScanner canWrite csrfToken="camera-csrf" onAssociated={onAssociated} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={{ id: 'asset-camera', name: 'Camera asset' }} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'associate' } })
  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))

  expect(await screen.findByText('Identifier associated with Camera asset.')).toBeInTheDocument()
  expect(onAssociated).toHaveBeenCalledTimes(1)
  expect(String(fetchMock.mock.calls.find(([path]) => String(path) === '/api/v1/assets/asset-camera/identifiers')?.[0])).toBe('/api/v1/assets/asset-camera/identifiers')
  const associateCall = fetchMock.mock.calls.find(([path]) => String(path) === '/api/v1/assets/asset-camera/identifiers')
  expect(associateCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'camera-csrf' })
  expect(JSON.parse(String(associateCall?.[1]?.body))).toMatchObject({ symbology: 'qr', value: 'CAMERA-QR-VALUE' })
  expect(stop).toHaveBeenCalledTimes(1)
})

test('keeps camera frames local and stops every media track on stop, cancellation, and unmount', async () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  const firstStop = vi.fn()
  const secondStop = vi.fn()
  const thirdStop = vi.fn()
  const streams = [firstStop, secondStop, thirdStop].map((stop) => ({ getTracks: () => [{ stop }] }))
  const getUserMedia = vi.fn(async () => streams.shift() as unknown as MediaStream)
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { mediaDevices: { getUserMedia } }))
  vi.stubGlobal('BarcodeDetector', class {
    detect = vi.fn(async () => [])
  })
  vi.stubGlobal('requestAnimationFrame', vi.fn(() => 17))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

  const rendered = render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={vi.fn(async () => undefined)} selectedAsset={null} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))

  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))
  expect(await screen.findByText(/Frames stay in this browser/)).toBeInTheDocument()
  expect(getUserMedia).toHaveBeenCalledWith({ video: { facingMode: { ideal: 'environment' } }, audio: false })
  fireEvent.click(screen.getByRole('button', { name: 'Stop camera' }))
  expect(firstStop).toHaveBeenCalledTimes(1)

  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))
  expect(await screen.findByText(/Frames stay in this browser/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Cancel scanning' }))
  expect(secondStop).toHaveBeenCalledTimes(1)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))
  expect(await screen.findByText(/Frames stay in this browser/)).toBeInTheDocument()
  rendered.unmount()
  expect(thirdStop).toHaveBeenCalledTimes(1)
  expect(fetchMock).not.toHaveBeenCalled()
})

test('captures manufacturer serial, model, and internal asset tag then finds the matching asset', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path.startsWith('/api/v1/assets?q=')) {
      const query = new URL(path, 'https://stewardmesh.test').searchParams.get('q')
      if (query === 'PF41ER5H' || query === '43188') {
        return jsonResponse({ items: [{ id: 'asset-laptop', name: 'Wayne laptop', assetTag: '43188', serialNumber: 'PF41ER5H' }] })
      }
      return jsonResponse({ items: [] })
    }
    return jsonResponse({ error: { message: 'No visible match.' } }, 404)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onResolveAsset = vi.fn(async () => undefined)
  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={onResolveAsset} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'labels' } })
  fireEvent.change(screen.getByLabelText('Manufacturer serial'), { target: { value: 'PF41ER5H' } })
  fireEvent.change(screen.getByLabelText('Asset tag'), { target: { value: '43188' } })
  fireEvent.change(screen.getByLabelText('Model number'), { target: { value: '20W5S51T00' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find matching asset' }))

  expect(await screen.findByText(/Matched Wayne laptop/)).toBeInTheDocument()
  expect(onResolveAsset).toHaveBeenCalledWith('asset-laptop')
  expect(fetchMock.mock.calls.some(([path]) => String(path).includes('/api/v1/assets?q=PF41ER5H'))).toBe(true)
})

test('classifies a keyboard-wedge manufacturer label into identity fields during find', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/v1/asset-identifiers/resolve') return jsonResponse({ error: { message: 'No visible match.' } }, 404)
    if (String(input).startsWith('/api/v1/assets?q=43188')) {
      return jsonResponse({ items: [{ id: 'asset-tag', name: 'Tagged laptop', assetTag: '43188' }] })
    }
    return jsonResponse({ items: [] })
  })
  vi.stubGlobal('fetch', fetchMock)
  const onResolveAsset = vi.fn(async () => undefined)
  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={onResolveAsset} selectedAsset={null} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  const input = screen.getByLabelText('Scanned or entered value')
  fireEvent.change(input, { target: { value: '43188' } })
  fireEvent.keyDown(input, { key: 'Enter' })

  expect(await screen.findByText(/Matched Tagged laptop/)).toBeInTheDocument()
  expect(screen.getByLabelText('Asset tag')).toHaveValue('43188')
  expect(onResolveAsset).toHaveBeenCalledWith('asset-tag')
})

test('shows multiple captured barcodes for mapping and highlights existing serial, tag, and model matches', async () => {
  const laptop = { id: 'asset-laptop', name: 'Wayne laptop', assetTag: '43188', serialNumber: 'PF41ER5H' }
  const catalogModel = {
    id: 'model-t14', manufacturer: 'Lenovo', name: 'ThinkPad T14 Gen 3', modelNumber: '20W5S51T00', status: 'active',
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path.startsWith('/api/v1/assets?q=')) {
      const query = new URL(path, 'https://stewardmesh.test').searchParams.get('q')
      if (query === 'PF41ER5H' || query === '43188') return jsonResponse({ items: [laptop] })
      return jsonResponse({ items: [] })
    }
    if (path.startsWith('/api/v1/asset-models')) {
      const query = new URL(path, 'https://stewardmesh.test').searchParams.get('q')
      if (query === '20W5S51T00') return jsonResponse({ items: [catalogModel] })
      return jsonResponse({ items: [] })
    }
    if (path === '/api/v1/asset-identifiers/resolve') return jsonResponse({ error: { message: 'No visible match.' } }, 404)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onResolveAsset = vi.fn(async (_assetId: string) => undefined)
  const onLinkAssetModel = vi.fn(async () => undefined)
  const onSetupScannedModel = vi.fn()
  function Harness() {
    const [selected, setSelected] = useState<{ id: string; name: string; assetTag?: string; serialNumber?: string; modelNumber?: string } | null>(null)
    return <AtlasScanner
      canWrite
      csrfToken="csrf"
      onAssociated={vi.fn()}
      onClearAsset={() => setSelected(null)}
      onLinkAssetModel={onLinkAssetModel}
      onResolveAsset={async (assetId) => {
        await onResolveAsset(assetId)
        setSelected({ id: assetId, name: 'Wayne laptop', assetTag: '43188', serialNumber: 'PF41ER5H' })
      }}
      onSetupScannedModel={onSetupScannedModel}
      selectedAsset={selected}
    />
  }
  render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'labels' } })
  const input = screen.getByLabelText('Scanned or entered value')
  fireEvent.change(input, { target: { value: '20W5S51T00' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find matching asset' }))
  expect(await screen.findByText(/Captured the model number/)).toBeInTheDocument()

  fireEvent.change(input, { target: { value: 'PF41ER5H' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find matching asset' }))
  expect(await screen.findByText('Map scanned values')).toBeInTheDocument()
  expect(await screen.findAllByText(/Matches existing manufacturer serial on Wayne laptop/)).not.toHaveLength(0)
  expect(await screen.findAllByText(/Matches existing model number on Lenovo ThinkPad T14 Gen 3/)).not.toHaveLength(0)
  expect(onResolveAsset).toHaveBeenCalledWith('asset-laptop')
  expect(await screen.findByText('Check the scanned model number')).toBeInTheDocument()
  expect(screen.getByText(/has no catalog model number/)).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Enter this model number on the item' }))
  await waitFor(() => expect(onLinkAssetModel).toHaveBeenCalledWith('model-t14'))
})

test('lets the operator send a scanned model number to model setup or clear a wrong item', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path.startsWith('/api/v1/assets?q=')) {
      return jsonResponse({ items: [{ id: 'asset-laptop', name: 'Wayne laptop', serialNumber: 'PF41ER5H' }] })
    }
    if (path.startsWith('/api/v1/asset-models')) return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-identifiers/resolve') return jsonResponse({ error: { message: 'No visible match.' } }, 404)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onSetupScannedModel = vi.fn()
  const onClearAsset = vi.fn()
  function Harness() {
    const [selected, setSelected] = useState<{ id: string; name: string; serialNumber?: string; modelNumber?: string } | null>({
      id: 'asset-laptop', name: 'Wayne laptop', serialNumber: 'PF41ER5H', modelNumber: 'T14-G3',
    })
    return <AtlasScanner
      canWrite
      csrfToken="csrf"
      onAssociated={vi.fn()}
      onClearAsset={() => { onClearAsset(); setSelected(null) }}
      onResolveAsset={async () => undefined}
      onSetupScannedModel={onSetupScannedModel}
      selectedAsset={selected}
    />
  }
  render(<Harness />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Workflow'), { target: { value: 'labels' } })
  fireEvent.change(screen.getByLabelText('Manufacturer serial'), { target: { value: 'PF41ER5H' } })
  fireEvent.change(screen.getByLabelText('Model number'), { target: { value: '20W5S51T00' } })
  expect(await screen.findByText('Check the scanned model number')).toBeInTheDocument()
  expect(screen.getByText(/does not match Wayne laptop's catalog model number T14-G3/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Associate with model setup' }))
  expect(onSetupScannedModel).toHaveBeenCalledWith({ modelNumber: '20W5S51T00', modelId: undefined })

  fireEvent.change(screen.getByLabelText('Model number'), { target: { value: '21N1S3N400' } })
  expect(await screen.findByText('Check the scanned model number')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'This is the wrong item' }))
  expect(onClearAsset).toHaveBeenCalled()
  expect(screen.queryByText('Check the scanned model number')).not.toBeInTheDocument()
})

test('keeps several camera barcodes on the mapping surface instead of auto-finding the first code', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path.startsWith('/api/v1/assets?q=') || path.startsWith('/api/v1/asset-models')) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const stop = vi.fn()
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), {
    mediaDevices: { getUserMedia: vi.fn(async () => ({ getTracks: () => [{ stop }] }) as unknown as MediaStream) },
  }))
  vi.stubGlobal('BarcodeDetector', class {
    detect = vi.fn(async () => [
      { format: 'code_39', rawValue: '20W5S51T00' },
      { format: 'code_128', rawValue: 'PF41ER5H' },
      { format: 'code_39', rawValue: '43188' },
    ])
  })
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    queueMicrotask(() => callback(1))
    return 19
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  const onResolveAsset = vi.fn(async () => undefined)

  render(<AtlasScanner canWrite csrfToken="csrf" onAssociated={vi.fn()} onResolveAsset={onResolveAsset} selectedAsset={null} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.click(screen.getByRole('button', { name: 'Use camera' }))

  expect(await screen.findByText('Map scanned values')).toBeInTheDocument()
  expect(screen.getByLabelText('Manufacturer serial')).toHaveValue('PF41ER5H')
  expect(screen.getByLabelText('Asset tag')).toHaveValue('43188')
  expect(screen.getByLabelText('Model number')).toHaveValue('20W5S51T00')
  expect(onResolveAsset).not.toHaveBeenCalled()
})

