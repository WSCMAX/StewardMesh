import axe from 'axe-core'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import ExchangeManager, { exchangeMediaType, exchangeRecordTypeDescription, maximumExchangePackageBytes, parseExchangeExcludedRecordTypes, parseExchangeImport, parseExchangePackages, parseExchangeProviderStatus, parseExchangeRecords } from './ExchangeManager'

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #8, #9.

const checksum = 'a'.repeat(64)
const recordChecksum = 'b'.repeat(64)
const records = [
  { type: 'stack.product', id: 'product-one', revision: 2, templateId: 'builtin-stack-product', templateVersion: 1, dependencies: [], hasFile: false },
  { type: 'stack.version', id: 'version-one', revision: 1, templateId: 'builtin-stack-version', templateVersion: 1, dependencies: [{ type: 'stack.product', id: 'product-one' }], hasFile: false },
  { type: 'vault.blob', id: '0123456789abcdef0123456789abcdef', revision: 1, templateId: 'builtin-vault-blob', templateVersion: 1, dependencies: [], hasFile: true },
  { type: 'bridge.oauth-client', id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', revision: 2, templateId: 'builtin-bridge-oauth-client', templateVersion: 2, dependencies: [], hasFile: false },
]
const excludedRecordTypes = [
  { type: 'guard.account', reason: 'accounts and authentication credentials are destination-owned security state' },
  { type: 'reach.message', reason: 'queued and delivered messages are operational state whose import could replay delivery' },
]
const portableRecordTypes = ['stack.product', 'stack.version', 'stack.installation', 'stack.license', 'stack.assignment', 'vault.blob', 'bridge.oauth-client']
const registeredRecordTypes = portableRecordTypes.slice(0, 6)
const recordCollection = { items: records, excludedRecordTypes, portableRecordTypes, registeredRecordTypes, providerRegistryComplete: false }

const completedPackage = {
  packageId: 'package-export', direction: 'export', schemaVersion: '1.1', sourceSystemId: 'example-org', archiveSha256: checksum,
  sizeBytes: 512, fileMode: 'include', status: 'completed', recordCount: 1, fileCount: 0,
  createdCount: 0, unchangedCount: 1, holdingCount: 0,
  records: [{ type: 'stack.product', id: 'product-one', revision: 2, checksum: recordChecksum, status: 'unchanged', missingDependencies: [], writeLocked: false }],
  createdAt: '2026-08-13T12:00:00Z', updatedAt: '2026-08-13T12:00:00Z',
} as const

const holdingPackage = {
  packageId: 'package-import', direction: 'import', schemaVersion: '1.1', sourceSystemId: 'remote-one', archiveSha256: checksum,
  sizeBytes: 1024, fileMode: 'metadata', status: 'holding', recordCount: 2, fileCount: 1,
  createdCount: 1, unchangedCount: 0, holdingCount: 1,
  records: [
    { type: 'stack.product', id: 'product-two', revision: 1, checksum: recordChecksum, status: 'created', missingDependencies: [], writeLocked: true },
    { type: 'vault.blob', id: 'fedcba9876543210fedcba9876543210', revision: 1, checksum: checksum, status: 'holding', missingDependencies: [{ type: 'atlas.asset', id: 'asset-missing' }], writeLocked: false },
  ],
  createdAt: '2026-08-13T13:00:00Z', updatedAt: '2026-08-13T13:00:01Z',
} as const

const partialFailedPackage = {
  ...holdingPackage, packageId: 'package-partial-failure', status: 'failed', recordCount: 2, fileCount: 0,
  createdCount: 1, holdingCount: 0, records: [holdingPackage.records[0]], errorCode: 'import_failed',
} as const

function jsonResponse(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('renders selectable records and visible holding outcomes accessibly on a narrow-safe surface', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('/records') ? jsonResponse(recordCollection) : jsonResponse({ items: [holdingPackage] })))
  const { container } = render(<ExchangeManager csrfToken="csrf" permissions={['integrations.read']} />)

  expect(await screen.findByText('product-one')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Prepare export' })).toBeDisabled()
  expect(screen.getAllByText('Requires integrations.write')).toHaveLength(2)
  expect(screen.getAllByText('Holding')).toHaveLength(2)
  expect(screen.getByText('builtin-stack-product')).toBeInTheDocument()
	expect(screen.getByText(exchangeRecordTypeDescription('bridge.oauth-client'))).toHaveTextContent('OAuth grants, credentials, and authorization transactions are excluded')
  expect(screen.getAllByText('version 1').length).toBeGreaterThan(0)
  fireEvent.click(screen.getByText(/Import ·/))
  expect(screen.getByText('Write locked until claimed')).toBeInTheDocument()
  expect(screen.getByText(/atlas.asset · asset-missing/)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Guard' })).toHaveAttribute('href', '#workspace-guard')
  expect(screen.getByRole('region', { name: 'Portable records' })).toHaveClass('overflow-x-auto')
  expect(screen.getByRole('region', { name: 'Outcomes for package package-import' })).toHaveClass('overflow-x-auto')
  expect(screen.getByRole('region', { name: 'Exchange package workflow' })).toBeInTheDocument()
  expect(screen.getByText('guard.account')).toBeInTheDocument()
  expect(screen.getByText(/destination-owned security state/)).toBeInTheDocument()
  expect(screen.getByText(/6 of 7 portable record families/)).toBeInTheDocument()
  expect(screen.getByText(/does not satisfy the complete phase-one Exchange provider gate/)).toBeInTheDocument()
  expect(container.firstElementChild).toHaveClass('min-w-0')
  expect((await axe.run(container)).violations).toEqual([])
})

test('exports the selected dependency-aware scope and prepares a fixed .openinventory download', async () => {
  const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:exchange-package')
  const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/exchange/records') return jsonResponse(recordCollection)
    if (path === '/api/v1/exchange/packages?limit=25') return jsonResponse({ items: init ? [] : [completedPackage] })
    if (path === '/api/v1/exchange/export' && init?.method === 'POST') return new Response(new Uint8Array([80, 75, 3, 4]), {
      status: 200,
      headers: { 'Content-Type': exchangeMediaType, 'X-Exchange-Package-ID': 'package-export', 'X-Content-SHA256': checksum },
    })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const view = render(<ExchangeManager csrfToken="csrf-token" permissions={['integrations.read', 'integrations.write']} />)

  await screen.findByText('product-one')
  fireEvent.click(screen.getByLabelText('Select stack.version · version-one'))
  fireEvent.click(screen.getByLabelText('Include file bytes'))
  fireEvent.click(screen.getByRole('button', { name: 'Prepare export' }))

  const download = await screen.findByRole('link', { name: 'Download package-export.openinventory' })
  expect(download).toHaveAttribute('download', 'package-export.openinventory')
  expect(download).toHaveAttribute('href', 'blob:exchange-package')
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/exchange/export' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'Content-Type': 'application/json', 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toEqual({
    selection: [{ type: 'stack.version', id: 'version-one' }], includeDependencies: true, fileMode: 'include',
  })
  expect(request?.[1]?.headers).not.toHaveProperty('Authorization')
  expect(createObjectURL).toHaveBeenCalledOnce()
  view.unmount()
  expect(revokeObjectURL).toHaveBeenCalledWith('blob:exchange-package')
})

test('uploads a bounded package with CSRF and exposes idempotent holding details', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/exchange/records') return jsonResponse(recordCollection)
    if (path === '/api/v1/exchange/packages?limit=25') return jsonResponse({ items: [] })
    if (path === '/api/v1/exchange/import' && init?.method === 'POST') return jsonResponse({ package: holdingPackage, replay: true })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<ExchangeManager csrfToken="csrf-token" permissions={['integrations.read', 'integrations.write']} />)
  await screen.findByText('product-one')

  const file = new File([new Uint8Array([80, 75, 3, 4])], 'portable.openinventory', { type: exchangeMediaType })
  const input = screen.getByLabelText('.openinventory package')
  fireEvent.change(input, { target: { files: [file] } })
  const form = screen.getByRole('button', { name: 'Import package' }).closest('form')
  if (!form) throw new Error('Exchange import form missing')
  fireEvent.submit(form)

  expect(await screen.findByText(/Package replay verified: 1 record placed in holding/)).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/exchange/import' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'Content-Type': exchangeMediaType, 'X-CSRF-Token': 'csrf-token' })
  expect(request?.[1]?.body).toBe(file)
  fireEvent.click(screen.getByText(/Import ·/))
  const outcomes = screen.getByRole('region', { name: 'Outcomes for package package-import' })
  expect(within(outcomes).getByText(/atlas.asset · asset-missing/)).toBeInTheDocument()
  expect(within(outcomes).getByText('Write locked until claimed')).toBeInTheDocument()
})

test('rejects oversized and incorrectly named files before network upload', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => String(input).includes('/records') ? jsonResponse(recordCollection) : jsonResponse({ items: [] }))
  vi.stubGlobal('fetch', fetchMock)
  render(<ExchangeManager csrfToken="csrf" permissions={['integrations.read', 'integrations.write']} />)
  await screen.findByText('product-one')

  const oversized = new File([new Uint8Array([1])], 'oversized.openinventory', { type: exchangeMediaType })
  Object.defineProperty(oversized, 'size', { value: maximumExchangePackageBytes + 1 })
  const input = screen.getByLabelText('.openinventory package')
  const form = screen.getByRole('button', { name: 'Import package' }).closest('form')
  if (!form) throw new Error('Exchange import form missing')
  fireEvent.change(input, { target: { files: [oversized] } })
  fireEvent.submit(form)
  expect(screen.getByRole('alert')).toHaveTextContent('Packages cannot exceed 32.0 MiB')

  const wrongName = new File([new Uint8Array([1])], 'package.zip', { type: 'application/zip' })
  fireEvent.change(input, { target: { files: [wrongName] } })
  fireEvent.submit(form)
  expect(screen.getByRole('alert')).toHaveTextContent('.openinventory extension')
  expect(fetchMock.mock.calls.some(([path, init]) => path === '/api/v1/exchange/import' && init?.method === 'POST')).toBe(false)
})

test('does not request protected collections without integrations.read', () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  render(<ExchangeManager csrfToken="csrf" permissions={[]} />)
  expect(screen.getByRole('heading', { name: 'Exchange — Migration packages' })).toBeInTheDocument()
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
  expect(fetchMock).not.toHaveBeenCalled()
})

test('rejects malformed and internally inconsistent Exchange responses', () => {
	expect(parseExchangePackages({ items: [partialFailedPackage] })).toEqual([partialFailedPackage])
	expect(parseExchangeExcludedRecordTypes(recordCollection)).toEqual(excludedRecordTypes)
	expect(parseExchangeProviderStatus(recordCollection)).toEqual({ portableRecordTypes, registeredRecordTypes, complete: false })
	expect(parseExchangeProviderStatus({
		...recordCollection,
		registeredRecordTypes: portableRecordTypes,
		providerRegistryComplete: true,
	})).toEqual({ portableRecordTypes, registeredRecordTypes: portableRecordTypes, complete: true })
	expect(() => parseExchangeProviderStatus({ ...recordCollection, providerRegistryComplete: true })).toThrow('invalid Exchange provider status response')
	expect(() => parseExchangeExcludedRecordTypes({ ...recordCollection, excludedRecordTypes: [...excludedRecordTypes, excludedRecordTypes[0]] })).toThrow('invalid Exchange exclusion response')
	expect(() => parseExchangeRecords({ items: [{ ...records[0], revision: 0 }] })).toThrow('invalid Exchange record response')
	expect(() => parseExchangeRecords({ items: [{ ...records[0], templateId: '' }] })).toThrow('invalid Exchange record response')
	expect(() => parseExchangePackages({ items: [{ ...holdingPackage, holdingCount: 0 }] })).toThrow('invalid Exchange package response')
	expect(() => parseExchangePackages({ items: [{ ...partialFailedPackage, createdCount: 0 }] })).toThrow('invalid Exchange package response')
  expect(() => parseExchangePackages({ items: [{ ...completedPackage, archiveSha256: 'not-a-checksum' }] })).toThrow('invalid Exchange package response')
  expect(() => parseExchangeImport({ package: completedPackage, replay: false })).toThrow('invalid Exchange import response')
})
