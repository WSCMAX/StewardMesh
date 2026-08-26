import { isQueryEmpty, parseQuery, type QueryCondition, type QueryModel } from './grid/queryLanguage'

// Requirement: REQ-ATLAS-001. Feature: inventory.assets.

export const assetPageLimit = 100
export const assetListingWindow = 400
export const listingCacheLimit = 8

const kinds = new Set(['server', 'computer', 'desktop', 'laptop', 'tablet', 'phone', 'network', 'peripheral', 'virtual', 'other'])
const statuses = new Set(['draft', 'active', 'inactive', 'retired', 'disposed'])
const referenceId = /^[a-f0-9]{32}$/
const assetId = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/

export type AssetListQuery = {
  q: string
  name: string
  assetTag: string
  serialNumber: string
  hostname: string
  manufacturer: string
  kind: string
  status: string
  modelId: string
  siteId: string
  buildingId: string
  roomId: string
  departmentId: string
  userId: string
  deploymentContext: string
}

export type GridListingInput = {
  filters: Readonly<Record<string, string>>
  search: string
  encodedQuery: string
}

export type TranslatedListing = {
  query: AssetListQuery
  remoteKeys: readonly string[]
  remoteSearch: boolean
  remoteQuery: boolean
  queryPartial: boolean
}

export type CachedAssetPage<T> = {
  items: readonly T[]
  nextCursor: string
  filteredCount: number
}

export function emptyAssetListQuery(): AssetListQuery {
  return {
    q: '', name: '', assetTag: '', serialNumber: '', hostname: '', manufacturer: '',
    kind: '', status: '', modelId: '', siteId: '', buildingId: '', roomId: '', departmentId: '', userId: '', deploymentContext: '',
  }
}

export function defaultAssetListQuery(): AssetListQuery {
  return { ...emptyAssetListQuery(), status: 'active' }
}

export const remoteAssetFilterKeys = [
  'name', 'assetTag', 'serialNumber', 'hostname', 'manufacturer', 'kind', 'status',
  'modelId', 'siteId', 'buildingId', 'roomId', 'departmentId', 'users', 'userId', 'deploymentContext',
] as const

export function listingKey(query: AssetListQuery): string {
  return JSON.stringify(query)
}

export function sameAssetListQuery(left: AssetListQuery, right: AssetListQuery): boolean {
  return listingKey(left) === listingKey(right)
}

export function toAssetSearchParams(query: AssetListQuery, cursor = '', limit = assetPageLimit): URLSearchParams {
  const params = new URLSearchParams()
  params.set('limit', String(limit))
  const fields: Array<[string, string]> = [
    ['q', query.q], ['name', query.name], ['assetTag', query.assetTag], ['serialNumber', query.serialNumber],
    ['hostname', query.hostname], ['manufacturer', query.manufacturer], ['kind', query.kind], ['status', query.status],
    ['modelId', query.modelId], ['siteId', query.siteId], ['buildingId', query.buildingId], ['roomId', query.roomId],
    ['departmentId', query.departmentId], ['userId', query.userId], ['deploymentContext', query.deploymentContext],
  ]
  for (const [key, value] of fields) {
    if (value.trim()) params.set(key, value.trim())
  }
  if (cursor.trim()) params.set('cursor', cursor.trim())
  return params
}

export function listingFromGrid(input: GridListingInput): TranslatedListing {
  const query = emptyAssetListQuery()
  const remoteKeys: string[] = []
  query.q = input.search.trim().slice(0, 200)
  const remoteSearch = query.q.length > 0

  for (const [key, raw] of Object.entries(input.filters)) {
    const mapped = applyMappedFilter(query, key, raw)
    if (mapped) remoteKeys.push(key)
  }

  let remoteQuery = false
  let queryPartial = false
  const encoded = input.encodedQuery.trim()
  if (encoded) {
    const parsed = parseQuery(encoded)
    if (parsed.ok && !isQueryEmpty(parsed.model)) {
      const translated = translateQueryModel(parsed.model)
      if (translated) {
        mergeAssetListQuery(query, translated.query)
        remoteQuery = translated.complete
        queryPartial = !translated.complete
      } else {
        queryPartial = true
      }
    }
  }

  return { query, remoteKeys, remoteSearch, remoteQuery, queryPartial }
}

export class ListingCache<T> {
  private readonly pages = new Map<string, CachedAssetPage<T>>()

  get(query: AssetListQuery): CachedAssetPage<T> | undefined {
    return this.pages.get(listingKey(query))
  }

  set(query: AssetListQuery, page: CachedAssetPage<T>) {
    const key = listingKey(query)
    if (this.pages.has(key)) this.pages.delete(key)
    this.pages.set(key, page)
    while (this.pages.size > listingCacheLimit) {
      const oldest = this.pages.keys().next().value
      if (typeof oldest !== 'string') break
      this.pages.delete(oldest)
    }
  }

  clear() {
    this.pages.clear()
  }
}

function applyMappedFilter(query: AssetListQuery, key: string, raw: string): boolean {
  const value = firstFilterToken(raw).slice(0, 200)
  if (!value) return false
  switch (key) {
    case 'name':
      query.name = value.toLowerCase()
      return true
    case 'assetTag':
      query.assetTag = value.toLowerCase()
      return true
    case 'serialNumber':
      query.serialNumber = value.toLowerCase()
      return true
    case 'hostname':
      query.hostname = value.toLowerCase()
      return true
    case 'manufacturer':
      query.manufacturer = value.toLowerCase()
      return true
    case 'kind': {
      const kind = value.toLowerCase()
      if (!kinds.has(kind)) return false
      query.kind = kind
      return true
    }
    case 'status': {
      const status = value.toLowerCase()
      if (!statuses.has(status)) return false
      query.status = status
      return true
    }
    case 'modelId':
      if (!assetId.test(value)) return false
      query.modelId = value
      return true
    case 'siteId':
      if (!referenceId.test(value)) return false
      query.siteId = value
      return true
    case 'buildingId':
      if (!referenceId.test(value)) return false
      query.buildingId = value
      return true
    case 'roomId':
      if (!referenceId.test(value)) return false
      query.roomId = value
      return true
    case 'departmentId':
      if (!referenceId.test(value)) return false
      query.departmentId = value
      return true
    case 'users':
    case 'userId':
      if (!referenceId.test(value)) return false
      query.userId = value
      return true
    case 'deploymentContext':
      query.deploymentContext = value.toLowerCase()
      return true
    default:
      return false
  }
}

/**
 * Turns a purely conjunctive query into server list filters. Conditions the
 * API cannot express (negations, lists, unmapped fields) are skipped rather
 * than aborting the whole translation: the mapped filters still narrow the
 * server page, `complete` turns false, and the grid re-applies the full query
 * locally. OR structures cannot be narrowed safely and translate to nothing.
 */
function translateQueryModel(model: QueryModel): { query: AssetListQuery; complete: boolean } | null {
  if (model.groupJoin === 'OR' && model.groups.length > 1) return null
  const query = emptyAssetListQuery()
  let complete = true
  for (const group of model.groups) {
    if (group.join === 'OR' && group.conditions.length > 1) return null
    for (const condition of group.conditions) {
      const snapshot = { ...query }
      if (!applyQueryCondition(query, condition)) {
        complete = false
        continue
      }
      // A second condition on the same field overwrites the server filter, so
      // the grid must keep matching the full query locally.
      for (const key of Object.keys(query) as Array<keyof AssetListQuery>) {
        if (snapshot[key] && query[key] !== snapshot[key]) complete = false
      }
    }
  }
  return { query, complete }
}

function applyQueryCondition(query: AssetListQuery, condition: QueryCondition): boolean {
  if (condition.operator !== 'eq' && condition.operator !== 'contains') return false
  return applyMappedFilter(query, condition.field, condition.value)
}

function mergeAssetListQuery(target: AssetListQuery, extra: AssetListQuery) {
  const keys = Object.keys(target) as Array<keyof AssetListQuery>
  for (const key of keys) {
    if (!target[key] && extra[key]) target[key] = extra[key]
  }
}

function firstFilterToken(value: string) {
  const trimmed = value.trim()
  const pipe = trimmed.indexOf('|')
  if (pipe > 0) return trimmed.slice(0, pipe).trim()
  return trimmed
}
