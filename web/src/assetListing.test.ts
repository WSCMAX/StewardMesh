import { defaultAssetListQuery, listingFromGrid, listingKey, toAssetSearchParams } from './assetListing'
import { expect, test } from 'vitest'

test('maps column filters and search onto parameterized asset list params', () => {
  const listing = listingFromGrid({
    filters: { status: 'active', manufacturer: 'Framework', kind: 'laptop', name: 'cart' },
    search: 'SCI-101',
    encodedQuery: '',
  })
  expect(listing.query.status).toBe('active')
  expect(listing.query.manufacturer).toBe('framework')
  expect(listing.query.kind).toBe('laptop')
  expect(listing.query.name).toBe('cart')
  expect(listing.query.q).toBe('SCI-101')
  expect(listing.remoteSearch).toBe(true)
  expect(listing.remoteKeys).toEqual(expect.arrayContaining(['status', 'manufacturer', 'kind', 'name']))
  const params = toAssetSearchParams(listing.query)
  expect(params.get('status')).toBe('active')
  expect(params.get('manufacturer')).toBe('framework')
  expect(params.get('q')).toBe('SCI-101')
  expect(params.get('limit')).toBe('100')
  expect(params.has('cursor')).toBe(false)
})

test('rejects incomplete status values so the API never receives an invalid enum', () => {
  const listing = listingFromGrid({ filters: { status: 'act' }, search: '', encodedQuery: '' })
  expect(listing.query.status).toBe('')
  expect(listing.remoteKeys).toEqual([])
})

test('translates an AND query into server filters and leaves OR queries client-side', () => {
  const andQuery = listingFromGrid({
    filters: {},
    search: '',
    encodedQuery: 'status=active^manufacturerLIKEFramework',
  })
  expect(andQuery.remoteQuery).toBe(true)
  expect(andQuery.query.status).toBe('active')
  expect(andQuery.query.manufacturer).toBe('framework')

  const orQuery = listingFromGrid({
    filters: { status: 'active' },
    search: '',
    encodedQuery: 'kind=laptop^ORkind=desktop',
  })
  expect(orQuery.queryPartial).toBe(true)
  expect(orQuery.remoteQuery).toBe(false)
  expect(orQuery.query.status).toBe('active')
})

test('translates building and room conditions into server filters', () => {
  const building = 'b'.repeat(32)
  const room = 'c'.repeat(32)
  const listing = listingFromGrid({
    filters: {},
    search: '',
    encodedQuery: `buildingId=${building}^roomId=${room}`,
  })
  expect(listing.remoteQuery).toBe(true)
  expect(listing.query.buildingId).toBe(building)
  expect(listing.query.roomId).toBe(room)
  const params = toAssetSearchParams(listing.query)
  expect(params.get('buildingId')).toBe(building)
  expect(params.get('roomId')).toBe(room)
})

test('keeps the translatable filters when one AND condition cannot reach the server', () => {
  const listing = listingFromGrid({
    filters: {},
    search: '',
    encodedQuery: 'manufacturer=Lenovo^name!=cart',
  })
  // The manufacturer filter still narrows the server page; the negation is
  // applied locally, so the query stays partial rather than being dropped.
  expect(listing.query.manufacturer).toBe('lenovo')
  expect(listing.remoteQuery).toBe(false)
  expect(listing.queryPartial).toBe(true)
})

test('falls back to local matching when two conditions target the same server filter', () => {
  const listing = listingFromGrid({
    filters: {},
    search: '',
    encodedQuery: 'nameLIKElab^nameLIKEcart',
  })
  expect(listing.remoteQuery).toBe(false)
  expect(listing.queryPartial).toBe(true)
})

test('keeps default active listing distinct from an unfiltered listing', () => {
  expect(listingKey(defaultAssetListQuery())).not.toBe(listingKey({ ...defaultAssetListQuery(), status: '' }))
})
