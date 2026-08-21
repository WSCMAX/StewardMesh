import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import axe from 'axe-core'
import { beforeEach, expect, test, vi } from 'vitest'
import type { Asset } from './AtlasInventory'
import HorizonPlanner from './HorizonPlanner'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

const asset: Asset = {
  id: 'asset-1',
  organizationId: 'organization-1',
  name: 'Core server',
  kind: 'server',
  status: 'active',
  purchaseDate: '2027-06-30T00:00:00Z',
  revision: 1,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const plan = {
  id: 'plan-1',
  assetId: asset.id,
  scenario: 'baseline',
  expectedUsefulLifeMonths: 60,
  derivedReplacementDate: '2032-06-30',
  lifecycleStage: 'in_service',
  replacementCostMinor: 500000,
  currency: 'USD',
  effectiveFrom: '2026-08-11T12:00:00Z',
  revision: 1,
}

const forecast = {
  asOf: '2026-08-11T13:00:00Z',
  groupBy: 'fiscal_year',
  currency: 'USD',
  scenarios: ['baseline'],
  plannedReplacementMinor: 500000,
  assetCount: 1,
  totalsByKindMinor: { actual: 125000, estimated: 500000, committed: 75000, normalized_real: 510000, tco: 650000 },
  groups: [{
    key: 'FY2032',
    label: 'FY2032',
    scenario: 'baseline',
    plannedReplacementMinor: 500000,
    assetCount: 1,
    amountsByKindMinor: { actual: 125000, estimated: 500000, committed: 75000, normalized_real: 510000, tco: 650000 },
  }],
}

const history = [{
  planId: plan.id,
  assetId: asset.id,
  scenario: 'baseline',
  expectedUsefulLifeMonths: 48,
  derivedReplacementDate: '2031-06-30T00:00:00Z',
  lifecycleStage: 'planned',
  replacementCostMinor: 450000,
  currency: 'USD',
  effectiveFrom: '2026-01-01T00:00:00Z',
  revision: 1,
  recordedAt: '2026-01-01T00:01:00Z',
  actorId: 'account-1',
}]

function response(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

function ancillaryFetch(path: string) {
  if (path.startsWith('/api/v1/horizon/kind-defaults')) return response({ items: [] })
  if (path.startsWith('/api/v1/asset-models')) return response({ items: [] })
  return null
}

const labAssetOne: Asset = {
  ...asset,
  id: 'lab-asset-1',
  name: 'Architecture Design Lab Station 001',
  roomId: 'room-lab-1',
  deploymentNotes: 'Adobe Creative Cloud lab',
}

const labAssetTwo: Asset = {
  ...asset,
  id: 'lab-asset-2',
  name: 'Architecture Design Lab Station 002',
  roomId: 'room-lab-1',
  deploymentNotes: 'Adobe Creative Cloud lab',
}

const groupAssets = {
  scenario: 'baseline',
  groupKey: 'FY2032',
  label: 'FY2032',
  groupBy: 'fiscal_year',
  currency: 'USD',
  items: [{
    planId: plan.id,
    assetId: asset.id,
    assetName: asset.name,
    lifecycleStage: plan.lifecycleStage,
    expectedUsefulLifeMonths: plan.expectedUsefulLifeMonths,
    derivedReplacementDate: plan.derivedReplacementDate,
    fiscalYear: 2032,
    replacementCostMinor: plan.replacementCostMinor,
    currency: plan.currency,
    revision: plan.revision,
  }],
}

const groupAssetsMulti = {
  ...groupAssets,
  items: [
    { ...groupAssets.items[0], planId: 'plan-1', assetId: labAssetOne.id, assetName: labAssetOne.name },
    { ...groupAssets.items[0], planId: 'plan-2', assetId: labAssetTwo.id, assetName: labAssetTwo.name, replacementCostMinor: 250000 },
  ],
}

function initialFetch(plans = [plan]) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    const ancillary = ancillaryFetch(url)
    if (ancillary) return ancillary
    if (url.startsWith('/api/v1/horizon/forecast/assets')) return response(groupAssets)
    if (url.startsWith('/api/v1/horizon/forecast')) return response(forecast)
    return response({ items: plans })
  })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('explains the planning permission gate without loading private data', () => {
  const fetchMock = vi.fn()
  const onOpenHelp = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" onOpenHelp={onOpenHelp} permissions={[]} />)
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Add lifecycle plan' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Horizon help' }))
  expect(onOpenHelp).toHaveBeenCalledOnce()
  expect(fetchMock).not.toHaveBeenCalled()
})

test('loads plans and an authoritative forecast without accessibility violations', async () => {
  const fetchMock = initialFetch()
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read']} />)

  expect(await screen.findByText('Core server')).toBeInTheDocument()
  expect(screen.getAllByText('FY2032').length).toBeGreaterThan(0)
  expect(screen.getByText('60 months')).toBeInTheDocument()
  expect(screen.getByText(/derived/)).toBeInTheDocument()
  expect(screen.getByRole('table', { name: /Authoritative forecast values/ })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Add lifecycle plan' })).not.toBeInTheDocument()
  const exportLink = screen.getByRole('link', { name: 'Export CSV' })
  expect(exportLink).toHaveAttribute('href', expect.stringContaining('/api/v1/horizon/export.csv?scenarios=baseline'))
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/horizon/plans?scenario=baseline', expect.anything())
  expect(fetchMock.mock.calls.some(([path]) => String(path).includes(`fromYear=${new Date().getFullYear()}`))).toBe(true)
  expect((await axe.run(container)).violations).toEqual([])
})

test('disables export without crashing while forecast controls are temporarily invalid', async () => {
  vi.stubGlobal('fetch', initialFetch())
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read']} />)
  await screen.findByText('Core server')

  fireEvent.change(screen.getByLabelText('As of'), { target: { value: '' } })

  expect(screen.getByRole('heading', { name: 'Lifecycle planning and forecasting' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled()
})

test('renders replacement timestamps as calendar dates without local timezone drift', async () => {
  const dateSpy = vi.spyOn(Date.prototype, 'toLocaleDateString').mockImplementation((_locales, options) => options?.timeZone === 'UTC' ? 'UTC calendar date' : 'local calendar date')
  vi.stubGlobal('fetch', initialFetch([{ ...plan, derivedReplacementDate: '2032-06-30T00:00:00Z' }]))

  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read']} />)

  expect(await screen.findByText('UTC calendar date (derived)')).toBeInTheDocument()
  expect(dateSpy).toHaveBeenCalledWith(undefined, { timeZone: 'UTC' })
})

test('creates an exact-minor-unit plan with the in-memory CSRF token', async () => {
  const created = { ...plan, replacementDate: '2032-06-30', derivedReplacementDate: undefined, expectedUsefulLifeMonths: 72, lifecycleStage: 'approved', replacementCostMinor: 123456 }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const ancillary = ancillaryFetch(path)
    if (ancillary) return ancillary
    if (path === '/api/v1/horizon/plans' && init?.method === 'POST') return response(created, 201)
    if (path.startsWith('/api/v1/horizon/forecast')) return response(forecast)
    if (path === '/api/v1/horizon/plans?scenario=baseline') return response({ items: [] })
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read', 'planning.write']} />)
  await screen.findByText(/No lifecycle plans match/)

  fireEvent.click(screen.getByRole('button', { name: 'Add lifecycle plan' }))
  const form = screen.getByRole('button', { name: 'Create lifecycle plan' }).closest('form')
  if (!form) throw new Error('plan form missing')
  const scoped = within(form)
  expect(scoped.getByLabelText('Scenario')).toHaveAttribute('pattern', '[A-Za-z0-9][A-Za-z0-9._\\-]{0,63}')
  fireEvent.change(scoped.getByLabelText('Atlas asset'), { target: { value: asset.id } })
  fireEvent.change(scoped.getByLabelText('Expected useful life (months)'), { target: { value: '72' } })
  fireEvent.change(scoped.getByLabelText('Manual replacement date'), { target: { value: '2032-06-30' } })
  fireEvent.change(scoped.getByLabelText('Lifecycle stage'), { target: { value: 'approved' } })
  fireEvent.change(scoped.getByLabelText('Replacement cost'), { target: { value: '90071992547409.92' } })
  fireEvent.change(scoped.getByLabelText('Effective from'), { target: { value: '2026-08-11' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create lifecycle plan' }))

  expect(await screen.findByText('The replacement cost is too large.')).toBeInTheDocument()
  expect(fetchMock.mock.calls.some(([path, init]) => path === '/api/v1/horizon/plans' && init?.method === 'POST')).toBe(false)
  fireEvent.change(scoped.getByLabelText('Replacement cost'), { target: { value: '1234.56' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create lifecycle plan' }))

  expect(await screen.findByText('Lifecycle plan created.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/horizon/plans' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
    assetId: asset.id,
    scenario: 'baseline',
    expectedUsefulLifeMonths: 72,
    replacementDate: '2032-06-30T00:00:00Z',
    lifecycleStage: 'approved',
    replacementCostMinor: 123456,
    currency: 'USD',
  })
  expect(JSON.parse(String(request?.[1]?.body)).effectiveFrom).toBe('2026-08-11T00:00:00Z')
})

test('updates a plan with its revision and discloses immutable history', async () => {
  const updated = { ...plan, expectedUsefulLifeMonths: 84, revision: 2 }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const ancillary = ancillaryFetch(path)
    if (ancillary) return ancillary
    if (path === '/api/v1/horizon/plans/plan-1' && init?.method === 'PUT') return response(updated)
    if (path === '/api/v1/horizon/plans/plan-1/history') return response({ items: history })
    if (path.startsWith('/api/v1/horizon/forecast')) return response(forecast)
    if (path === '/api/v1/horizon/plans?scenario=baseline') return response({ items: [plan] })
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read', 'planning.write']} />)
  await screen.findByText('Core server')

  fireEvent.click(screen.getByRole('button', { name: 'Edit plan for Core server' }))
  const form = screen.getByRole('button', { name: 'Update lifecycle plan' }).closest('form')
  if (!form) throw new Error('edit form missing')
  fireEvent.change(within(form).getByLabelText('Expected useful life (months)'), { target: { value: '84' } })
  fireEvent.click(within(form).getByRole('button', { name: 'Update lifecycle plan' }))
  expect(await screen.findByText('Lifecycle plan updated.')).toBeInTheDocument()
  const updateRequest = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/horizon/plans/plan-1' && init?.method === 'PUT')
  expect(JSON.parse(String(updateRequest?.[1]?.body))).toMatchObject({ assetId: asset.id, expectedUsefulLifeMonths: 84, revision: 1 })

  fireEvent.click(screen.getByRole('button', { name: 'View history for Core server' }))
  expect(await screen.findByRole('heading', { name: 'Version history for Core server' })).toBeInTheDocument()
  expect(screen.getByText('account-1')).toBeInTheDocument()
  expect(screen.getByText('48 months')).toBeInTheDocument()
  expect(screen.getByText(`${new Date(history[0].derivedReplacementDate).toLocaleDateString(undefined, { timeZone: 'UTC' })} (derived)`)).toBeInTheDocument()
  expect(screen.getByText(new Date(history[0].effectiveFrom).toLocaleDateString(undefined, { timeZone: 'UTC' }))).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/horizon/plans/plan-1/history', expect.anything())
})

test('refreshes grouped scenarios and warns that tag rows are non-additive', async () => {
  const tagForecast = { ...forecast, groupBy: 'tag', scenarios: ['baseline', 'optimistic'], groups: [{ ...forecast.groups[0], key: 'critical', label: 'Critical systems' }] }
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    const ancillary = ancillaryFetch(path)
    if (ancillary) return ancillary
    if (path.includes('groupBy=tag')) return response(tagForecast)
    if (path.startsWith('/api/v1/horizon/forecast')) return response(forecast)
    return response({ items: [plan] })
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" permissions={['planning.read']} />)
  await screen.findByText('Core server')

  fireEvent.change(screen.getByLabelText('Scenarios'), { target: { value: 'baseline, optimistic' } })
  fireEvent.change(screen.getByLabelText('Group by'), { target: { value: 'tag' } })
  fireEvent.click(screen.getByRole('button', { name: 'Refresh forecast' }))
  expect(await screen.findByText(/Non-additive grouping/)).toBeInTheDocument()
  expect(screen.getAllByText('Critical systems').length).toBeGreaterThan(0)
  await waitFor(() => expect(fetchMock.mock.calls.some(([path]) => String(path).includes('scenarios=baseline%2Coptimistic') && String(path).includes('groupBy=tag'))).toBe(true))
})

test('groups forecast assets by deployment and supports per-group Atlas view', async () => {
  const onOpenAtlasInventory = vi.fn()
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    const ancillary = ancillaryFetch(url)
    if (ancillary) return ancillary
    if (url.startsWith('/api/v1/horizon/forecast/assets')) return response(groupAssetsMulti)
    if (url.startsWith('/api/v1/horizon/forecast')) return response(forecast)
    return response({ items: [plan] })
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<HorizonPlanner assets={[labAssetOne, labAssetTwo]} csrfToken="csrf-token" onOpenAtlasInventory={onOpenAtlasInventory} permissions={['planning.read', 'planning.write', 'assets.read']} />)
  await screen.findByRole('button', { name: 'FY2032' })

  fireEvent.click(screen.getByRole('button', { name: 'FY2032' }))
  expect(await screen.findByText('Architecture Design Lab')).toBeInTheDocument()
  fireEvent.click(screen.getAllByRole('button', { name: 'View in Atlas' })[1])
  expect(onOpenAtlasInventory).toHaveBeenCalledWith(['lab-asset-1', 'lab-asset-2'], 'FY2032 · Architecture Design Lab', 1)
})

test('opens a forecast group drawer with assets in the refresh cycle', async () => {
  const onOpenAtlasInventory = vi.fn()
  vi.stubGlobal('fetch', initialFetch())
  render(<HorizonPlanner assets={[asset]} csrfToken="csrf-token" onOpenAtlasInventory={onOpenAtlasInventory} permissions={['planning.read', 'planning.write', 'assets.read']} />)
  await screen.findByText('Core server')

  fireEvent.click(screen.getByRole('button', { name: 'FY2032' }))

  expect(await screen.findByRole('dialog')).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'FY2032 · Baseline' })).toBeInTheDocument()
  expect(screen.getAllByText('Core server').length).toBeGreaterThan(1)
  expect(screen.getByText('Normalize replacement cycles')).toBeInTheDocument()
  const atlasButtons = screen.getAllByRole('button', { name: 'View in Atlas' })
  expect(atlasButtons.length).toBeGreaterThan(0)
  fireEvent.click(atlasButtons[0])
  expect(onOpenAtlasInventory).toHaveBeenCalledWith([asset.id], 'FY2032 · Baseline', 1)
})
