import { expect, test } from 'vitest'
import { encodeQuery, emptyQuery, matchQuery, parseQuery, type QueryField } from './queryLanguage'

const fields: QueryField[] = [
  { key: 'name', header: 'Name', kind: 'text' },
  { key: 'status', header: 'Status', kind: 'enum', options: ['active', 'retired'] },
  { key: 'kind', header: 'Kind', kind: 'text' },
  { key: 'seats', header: 'Seats', kind: 'number' },
]

const rows = [
  { name: 'Lab server', status: 'active', kind: 'server', seats: '8' },
  { name: 'Studio laptop', status: 'retired', kind: 'laptop', seats: '1' },
  { name: 'North cart', status: 'active', kind: 'laptop', seats: '' },
]

function textOf(row: (typeof rows)[number], field: string) {
  return String(row[field as keyof typeof row] ?? '')
}

function idsMatching(source: string) {
  const parsed = parseQuery(source)
  expect(parsed.ok).toBe(true)
  if (!parsed.ok) return []
  return rows.filter((row) => matchQuery(row, fields, parsed.model, textOf)).map((row) => row.name)
}

test('encodes empty queries as a blank string', () => {
  expect(encodeQuery(emptyQuery())).toBe('')
  expect(parseQuery('').ok).toBe(true)
})

test('parses ServiceNow-style AND, OR, and grouped queries', () => {
  expect(idsMatching('status=active')).toEqual(['Lab server', 'North cart'])
  expect(idsMatching('status=active^kind=laptop')).toEqual(['North cart'])
  expect(idsMatching('status=active^ORstatus=retired')).toEqual(['Lab server', 'Studio laptop', 'North cart'])
  expect(idsMatching('(status=active^kind=server)^NQ(status=retired)')).toEqual(['Lab server', 'Studio laptop'])
  expect(idsMatching('nameLIKElab^ORkind=laptop')).toEqual(['Lab server', 'Studio laptop', 'North cart'])
})

test('supports emptiness, lists, inequality, and quoted values', () => {
  expect(idsMatching('seatsISEMPTY')).toEqual(['North cart'])
  expect(idsMatching('kindINlaptop,desktop')).toEqual(['Studio laptop', 'North cart'])
  expect(idsMatching('seats>1')).toEqual(['Lab server'])
  expect(idsMatching('name!="Lab server"')).toEqual(['Studio laptop', 'North cart'])
  expect(idsMatching('nameSTARTSWITHStudio')).toEqual(['Studio laptop'])
})

test('round-trips an encoded query through the visual model', () => {
  const source = 'status=active^nameLIKElab^NQstatus=retired'
  const parsed = parseQuery(source)
  expect(parsed.ok).toBe(true)
  if (!parsed.ok) return
  const encoded = encodeQuery(parsed.model)
  const again = parseQuery(encoded)
  expect(again.ok).toBe(true)
  if (!again.ok) return
  expect(rows.filter((row) => matchQuery(row, fields, parsed.model, textOf))).toEqual(
    rows.filter((row) => matchQuery(row, fields, again.model, textOf)),
  )
})

test('rejects incomplete encoded queries', () => {
  const parsed = parseQuery('status=active^')
  expect(parsed.ok).toBe(false)
  if (parsed.ok) return
  expect(parsed.error).toMatch(/field name/i)
})

test('does not treat LIKE values as regular expressions or execute encoded text', () => {
  expect(idsMatching('nameLIKE.*')).toEqual([])
  expect(idsMatching('nameLIKElab')).toEqual(['Lab server'])
  expect(parseQuery('status=active;DROP TABLE assets').ok).toBe(false)
  const injected = parseQuery('status=active;DROP')
  expect(injected.ok).toBe(true)
  if (!injected.ok) return
  expect(rows.filter((row) => matchQuery(row, fields, injected.model, textOf))).toEqual([])
})

test('rejects unknown, reserved, and oversized field names instead of reading the row object', () => {
  const probes: string[] = []
  const guarded = (row: (typeof rows)[number], field: string) => {
    probes.push(field)
    if (field === 'constructor' || field === 'prototype' || field === '__proto__') {
      throw new Error(`query probed ${field}`)
    }
    return textOf(row, field)
  }
  expect(parseQuery('constructor=active', fields).ok).toBe(false)
  expect(parseQuery('missing=active', fields).ok).toBe(false)
  expect(parseQuery('__proto__=x').ok).toBe(false)
  expect(matchQuery(rows[0], fields, { groupJoin: 'AND', groups: [{ id: 'g', join: 'AND', conditions: [{ id: 'c', field: 'constructor', operator: 'eq', value: 'active' }] }] }, guarded)).toBe(false)
  expect(probes).toEqual([])
  expect(parseQuery(`name=${'a'.repeat(501)}`).ok).toBe(false)
  expect(parseQuery(`name=${'a'.repeat(4001)}`).ok).toBe(false)
})

test('binds parsed queries to the known columns', () => {
  const parsed = parseQuery('status=active^kind=server', fields)
  expect(parsed.ok).toBe(true)
  expect(parseQuery('status=active^owner=root', fields).ok).toBe(false)
})
