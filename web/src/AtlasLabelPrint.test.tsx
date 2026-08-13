import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import AtlasLabelPrint from './AtlasLabelPrint'

// Requirements: REQ-ATLAS-CODES-001, REQ-PATTERNS-001, A11Y-001, SEC-GUARD-001.

const assets = [{ id: 'asset-one', name: 'Lab server', assetTag: 'TAG-001' }, { id: 'asset-two', name: 'Field printer', assetTag: 'TAG-002' }]
const codeIdentifier = { id: 'identifier-code', assetId: 'asset-one', symbology: 'code128' as const, normalizedValue: 'LAB-001', displayValue: 'LAB-001', status: 'active' as const }
const qrIdentifier = { id: 'identifier-qr', assetId: 'asset-two', symbology: 'qr' as const, normalizedValue: 'secret-value', displayValue: 'QR label', status: 'active' as const }
const secondCodeIdentifier = { id: 'identifier-code-two', assetId: 'asset-two', symbology: 'code128' as const, normalizedValue: 'LAB-002', displayValue: 'LAB-002', status: 'active' as const }
const template = {
  id: 'builtin-atlas-label-code128', patternTemplateId: 'builtin-atlas-label-code128', patternVersion: 1,
  name: 'Atlas Code 128 label', version: 1, widthMm: 70, heightMm: 30, marginMm: 3, quietZoneMm: 3,
  symbology: 'code128', payloadSource: 'identifier_value', humanReadableField: 'identifier.displayValue',
  safeAssetFields: ['asset.name', 'asset.assetTag'], organizationBranding: 'StewardMesh',
}
const qrTemplate = { ...template, id: 'builtin-atlas-label-qr', patternTemplateId: 'builtin-atlas-label-qr', name: 'Atlas QR label', widthMm: 50, quietZoneMm: 2, symbology: 'qr', payloadSource: 'organization_route' }

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function svgResponse(replay = false) {
  return new Response('<svg xmlns="http://www.w3.org/2000/svg" width="70mm" height="30mm"><rect width="70" height="30"/></svg>', {
    headers: {
      'Content-Type': 'image/svg+xml', 'X-Label-Batch-ID': 'label-batch-aaaaaaaaaaaaaaaaaaaaaaaa', 'X-Label-Width-MM': '70.00',
      'X-Label-Height-MM': '30.00', 'X-Label-Item-Count': '1', 'X-Idempotent-Replay': String(replay),
    },
  })
}

function pdfResponse(itemCount = 2) {
  return new Response('%PDF-1.4\n%%EOF\n', { headers: {
    'Content-Type': 'application/pdf', 'X-Label-Batch-ID': 'label-batch-bbbbbbbbbbbbbbbbbbbbbbbb', 'X-Label-Width-MM': '70.00',
    'X-Label-Height-MM': '30.00', 'X-Label-Item-Count': String(itemCount), 'X-Idempotent-Replay': 'false',
  } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.spyOn(window, 'print').mockImplementation(() => undefined)
  vi.spyOn(window, 'open').mockReturnValue(window)
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:label-artifact') })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
})

test('previews physical vector output and requires explicit operator confirmation before printing', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/asset-label-templates')) return jsonResponse({ items: [template, qrTemplate], maximumBatchSize: 50 })
    if (String(input).endsWith('/assets/asset-one/identifiers')) return jsonResponse({ items: [codeIdentifier] })
    if (String(input).endsWith('/assets/asset-two/identifiers')) return jsonResponse({ items: [qrIdentifier] })
    if (String(input).endsWith('/asset-label-batches') && init?.method === 'POST') return svgResponse()
    throw new Error(`unexpected request: ${String(input)}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasLabelPrint assets={assets} csrfToken="csrf-token" />)

  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  expect(await screen.findByRole('combobox', { name: 'Versioned label template' })).toHaveValue(template.id)
  expect(screen.getByText(/70 × 30 mm · 3 mm margins/)).toBeInTheDocument()
  expect(screen.getByText(/Pattern builtin-atlas-label-code128, version 1/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  expect(await screen.findByAltText('Generated preview of 1 Atlas Codes label')).toBeInTheDocument()
  const printButton = screen.getByRole('button', { name: 'Open browser print dialog' })
  expect(printButton).toBeDisabled()
  expect(window.print).not.toHaveBeenCalled()
  fireEvent.click(screen.getByLabelText(/I reviewed the 70 × 30 mm dimensions/))
  fireEvent.click(printButton)
  expect(window.print).toHaveBeenCalledOnce()
  expect(await screen.findByText(/browser print dialog was opened/)).toBeInTheDocument()

  const artifactRequest = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/asset-label-batches'))
  expect(artifactRequest?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(String((artifactRequest?.[1]?.headers as Record<string, string>)['Idempotency-Key'])).toMatch(/^label-/)
  expect(JSON.parse(String(artifactRequest?.[1]?.body))).toEqual({
    templateId: template.id, templateVersion: 1, identifierIds: ['identifier-code'], output: 'svg', testPrint: true,
  })
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('blocks mixed formats, surfaces cancellation, and reuses the request key for safe retries', async () => {
  let attempts = 0
  let resolvePending!: (response: Response) => void
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/asset-label-templates')) return Promise.resolve(jsonResponse({ items: [template, qrTemplate] }))
    if (String(input).endsWith('/assets/asset-one/identifiers')) return Promise.resolve(jsonResponse({ items: [codeIdentifier] }))
    if (String(input).endsWith('/assets/asset-two/identifiers')) return Promise.resolve(jsonResponse({ items: [qrIdentifier] }))
    if (String(input).endsWith('/asset-label-batches')) {
      attempts += 1
      if (attempts === 1) return new Promise<Response>((resolve, reject) => {
        resolvePending = resolve
        init?.signal?.addEventListener('abort', () => reject(new DOMException('cancelled', 'AbortError')))
      })
      if (attempts === 2) return Promise.reject(new Error('temporary renderer error'))
      return Promise.resolve(svgResponse(true))
    }
    return Promise.reject(new Error(`unexpected request: ${String(input)}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasLabelPrint assets={assets} csrfToken="csrf-token" />)
  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  await screen.findByRole('combobox', { name: 'Versioned label template' })
  fireEvent.click(screen.getByText('QR label').closest('label')!.querySelector('input')!)
  expect(screen.getByRole('button', { name: 'Generate test-print preview' })).toBeDisabled()
  fireEvent.click(screen.getByText('QR label').closest('label')!.querySelector('input')!)
  fireEvent.change(screen.getByRole('combobox', { name: 'Output path' }), { target: { value: 'svg' } })

  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  const cancelButton = await screen.findByRole('button', { name: 'Cancel generation' })
  expect(cancelButton).toBeEnabled()
  expect(screen.getByRole('combobox', { name: 'Output path' })).toBeDisabled()
  expect(screen.getByText('LAB-001').closest('label')!.querySelector('input')).toBeDisabled()
  fireEvent.click(cancelButton)
  expect(await screen.findByText(/generation cancelled/)).toBeInTheDocument()
  resolvePending(svgResponse())

  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  expect(await screen.findByRole('button', { name: 'Retry generation' })).toBeInTheDocument()
  const firstKey = (fetchMock.mock.calls.filter(([path]) => String(path).endsWith('/asset-label-batches'))[1]?.[1]?.headers as Record<string, string>)['Idempotency-Key']
  fireEvent.click(screen.getByRole('button', { name: 'Retry generation' }))
  expect(await screen.findByText(/Test-print preview ready/)).toBeInTheDocument()
  const retryKey = (fetchMock.mock.calls.filter(([path]) => String(path).endsWith('/asset-label-batches'))[2]?.[1]?.headers as Record<string, string>)['Idempotency-Key']
  expect(retryKey).toBe(firstKey)
  await waitFor(() => expect(screen.getByText(/safe retry replay/)).toBeInTheDocument())
})

test('exports ZPL only through a reviewed operator-controlled download', async () => {
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/asset-label-templates')) return jsonResponse({ items: [template] })
    if (String(input).endsWith('/assets/asset-one/identifiers')) return jsonResponse({ items: [codeIdentifier] })
    if (String(input).endsWith('/asset-label-batches') && init?.method === 'POST') return new Response('^XA\n^FDLAB-001^FS\n^XZ\n', { headers: {
      'Content-Type': 'application/vnd.zebra-zpl', 'X-Label-Batch-ID': 'label-batch-cccccccccccccccccccccccc', 'X-Label-Width-MM': '70',
      'X-Label-Height-MM': '30', 'X-Label-Item-Count': '1', 'X-Idempotent-Replay': 'false',
    } })
    throw new Error(`unexpected request: ${String(input)}`)
  }))
  render(<AtlasLabelPrint assets={[assets[0]]} csrfToken="csrf-token" />)
  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  await screen.findByRole('combobox', { name: 'Output path' })
  fireEvent.change(screen.getByRole('combobox', { name: 'Output path' }), { target: { value: 'zpl' } })
  expect(screen.getByText(/not a direct printer connection/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  const confirmedDownload = await screen.findByRole('button', { name: 'Download confirmed ZPL' })
  expect(confirmedDownload).toBeDisabled()
  expect(click).not.toHaveBeenCalled()
  fireEvent.click(screen.getByLabelText(/I reviewed the 70 × 30 mm dimensions/))
  fireEvent.click(confirmedDownload)
  expect(click).toHaveBeenCalledOnce()
  expect(await screen.findByText(/did not contact a printer/)).toBeInTheDocument()
})

test('builds one bounded PDF batch from identifiers on multiple visible assets', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/asset-label-templates')) return jsonResponse({ items: [template] })
    if (String(input).endsWith('/assets/asset-one/identifiers')) return jsonResponse({ items: [codeIdentifier] })
    if (String(input).endsWith('/assets/asset-two/identifiers')) return jsonResponse({ items: [secondCodeIdentifier] })
    if (String(input).endsWith('/asset-label-batches') && init?.method === 'POST') return pdfResponse()
    throw new Error(`unexpected request: ${String(input)}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasLabelPrint assets={assets} csrfToken="csrf-token" />)
  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  await screen.findByText(/Field printer · TAG-002 · Code 128/)
  fireEvent.click(screen.getByText('LAB-002').closest('label')!.querySelector('input')!)
  expect(screen.getByRole('combobox', { name: 'Output path' })).toHaveValue('pdf')
  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  expect(await screen.findByText(/2 labels · 70 × 30 mm each · PDF/)).toBeInTheDocument()
  expect(screen.getByText(/confirm the dimensions, count, and test-print state below before opening/i)).toBeInTheDocument()
  const openPDF = screen.getByRole('button', { name: 'Open PDF for printing' })
  expect(openPDF).toBeDisabled()
  expect(window.open).not.toHaveBeenCalled()
  const request = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/asset-label-batches'))
  expect(JSON.parse(String(request?.[1]?.body)).identifierIds).toEqual(['identifier-code', 'identifier-code-two'])
  fireEvent.click(screen.getByLabelText(/I reviewed the 70 × 30 mm dimensions/))
  fireEvent.click(openPDF)
  expect(window.open).toHaveBeenCalledOnce()
})

test('drops a late artifact after the print panel invalidates an in-flight request', async () => {
  let resolveBatch!: (response: Response) => void
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/asset-label-templates')) return Promise.resolve(jsonResponse({ items: [template] }))
    if (String(input).endsWith('/assets/asset-one/identifiers')) return Promise.resolve(jsonResponse({ items: [codeIdentifier] }))
    if (String(input).endsWith('/asset-label-batches') && init?.method === 'POST') {
      return new Promise<Response>((resolve) => { resolveBatch = resolve })
    }
    return Promise.reject(new Error(`unexpected request: ${String(input)}`))
  }))
  render(<AtlasLabelPrint assets={[assets[0]]} csrfToken="csrf-token" />)
  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  await screen.findByRole('button', { name: 'Generate test-print preview' })
  fireEvent.click(screen.getByRole('button', { name: 'Generate test-print preview' }))
  await screen.findByRole('button', { name: 'Cancel generation' })
  fireEvent.click(screen.getByRole('button', { name: 'Close label printing' }))
  resolveBatch(svgResponse())

  fireEvent.click(screen.getByRole('button', { name: 'Print labels' }))
  await screen.findByRole('combobox', { name: 'Versioned label template' })
  await waitFor(() => expect(screen.queryByAltText(/Generated preview/)).not.toBeInTheDocument())
  expect(window.print).not.toHaveBeenCalled()
})
