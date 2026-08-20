import type { GridColumnKind } from './columns'

// Requirements: REQ-ATLAS-001, REQ-WORKSPACE-001. Feature: experience.grid.

// Encoded queries follow the ServiceNow list style: fieldOPERATORVALUE, AND with
// ^, OR with ^OR, a new OR-group with ^NQ, and parentheses for nested groups.
// The visual builder round-trips through the same encoding so Atlas reporting
// and every other grid share one query language.

export const queryOperators = [
  'eq', 'neq', 'contains', 'not_contains', 'starts_with', 'ends_with',
  'is_empty', 'is_not_empty', 'in', 'not_in', 'gt', 'gte', 'lt', 'lte',
] as const

export type QueryOperator = (typeof queryOperators)[number]

export type QueryJoin = 'AND' | 'OR'

export type QueryCondition = {
  id: string
  field: string
  operator: QueryOperator
  value: string
}

export type QueryGroup = {
  id: string
  join: QueryJoin
  conditions: QueryCondition[]
}

export type QueryModel = {
  groupJoin: QueryJoin
  groups: QueryGroup[]
}

export type QueryField = {
  key: string
  header: string
  kind?: GridColumnKind
  options?: readonly string[]
}

export type QueryParseResult =
  | { ok: true; model: QueryModel }
  | { ok: false; error: string }

/** Caps encoded queries so pasted text cannot stall the parser or the matcher. */
export const maximumEncodedQueryLength = 4000
export const maximumQueryValueLength = 500
export const maximumQueryGroups = 12
export const maximumQueryConditions = 40
export const maximumQueryNameLength = 80
export const maximumSavedQueries = 25

const fieldNamePattern = /^[A-Za-z][A-Za-z0-9_]{0,63}$/
const reservedFieldNames = new Set(['constructor', 'prototype', 'toString', 'valueOf', 'hasOwnProperty'])

const operatorTokens: ReadonlyArray<{ operator: QueryOperator; token: string }> = [
  { operator: 'starts_with', token: 'STARTSWITH' },
  { operator: 'ends_with', token: 'ENDSWITH' },
  { operator: 'is_not_empty', token: 'ISNOTEMPTY' },
  { operator: 'not_contains', token: 'NOTLIKE' },
  { operator: 'is_empty', token: 'ISEMPTY' },
  { operator: 'contains', token: 'LIKE' },
  { operator: 'not_in', token: 'NOTIN' },
  { operator: 'gte', token: '>=' },
  { operator: 'lte', token: '<=' },
  { operator: 'neq', token: '!=' },
  { operator: 'in', token: 'IN' },
  { operator: 'eq', token: '=' },
  { operator: 'gt', token: '>' },
  { operator: 'lt', token: '<' },
]

export const operatorLabels: Record<QueryOperator, string> = {
  eq: 'is',
  neq: 'is not',
  contains: 'contains',
  not_contains: 'does not contain',
  starts_with: 'starts with',
  ends_with: 'ends with',
  is_empty: 'is empty',
  is_not_empty: 'is not empty',
  in: 'is one of',
  not_in: 'is not one of',
  gt: 'greater than',
  gte: 'at least',
  lt: 'less than',
  lte: 'at most',
}

const operatorsNeedingValue = new Set<QueryOperator>([
  'eq', 'neq', 'contains', 'not_contains', 'starts_with', 'ends_with', 'in', 'not_in', 'gt', 'gte', 'lt', 'lte',
])

let queryId = 0

export function newQueryId(prefix: string) {
  queryId += 1
  return `${prefix}-${queryId}`
}

export function emptyCondition(): QueryCondition {
  return { id: newQueryId('c'), field: '', operator: 'eq', value: '' }
}

export function emptyGroup(): QueryGroup {
  return { id: newQueryId('g'), join: 'AND', conditions: [emptyCondition()] }
}

export function emptyQuery(): QueryModel {
  return { groupJoin: 'AND', groups: [emptyGroup()] }
}

export function conditionNeedsValue(operator: QueryOperator) {
  return operatorsNeedingValue.has(operator)
}

export function operatorsForKind(kind?: GridColumnKind): readonly QueryOperator[] {
  if (kind === 'number' || kind === 'money' || kind === 'date' || kind === 'instant') {
    return ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'is_empty', 'is_not_empty']
  }
  if (kind === 'enum') return ['eq', 'neq', 'in', 'not_in', 'is_empty', 'is_not_empty']
  return queryOperators
}

export function isQueryEmpty(model: QueryModel) {
  return !model.groups.some((group) => group.conditions.some(isConditionActive))
}

export function isConditionActive(condition: QueryCondition) {
  if (!condition.field.trim()) return false
  return !conditionNeedsValue(condition.operator) || condition.value.trim().length > 0
}

export function activeQuery(model: QueryModel): QueryModel {
  const groups = model.groups
    .map((group) => ({ ...group, conditions: group.conditions.filter(isConditionActive) }))
    .filter((group) => group.conditions.length > 0)
  return { groupJoin: model.groupJoin, groups }
}

function tokenFor(operator: QueryOperator) {
  return operatorTokens.find((entry) => entry.operator === operator)?.token ?? '='
}

export function isSafeFieldName(value: string) {
  return fieldNamePattern.test(value) && !reservedFieldNames.has(value)
}

export function sanitizeQueryName(value: string) {
  return value.replaceAll(/\s+/g, ' ').trim().slice(0, maximumQueryNameLength)
}

function encodeValue(value: string) {
  const safe = value.slice(0, maximumQueryValueLength)
  if (safe === '') return ''
  if (/^[A-Za-z0-9._:/-]+$/.test(safe)) return safe
  return `"${safe.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`
}

function encodeCondition(condition: QueryCondition) {
  const token = tokenFor(condition.operator)
  if (!conditionNeedsValue(condition.operator)) return `${condition.field}${token}`
  return `${condition.field}${token}${encodeValue(condition.value.trim())}`
}

function encodeGroup(group: QueryGroup) {
  const parts = group.conditions.filter(isConditionActive).map(encodeCondition)
  if (parts.length === 0) return ''
  const joiner = group.join === 'OR' ? '^OR' : '^'
  const encoded = parts.join(joiner)
  return parts.length > 1 && group.join === 'OR' ? `(${encoded})` : encoded
}

export function encodeQuery(model: QueryModel) {
  const active = activeQuery(model)
  if (active.groups.length === 0) return ''
  const parts = active.groups.map(encodeGroup).filter(Boolean)
  if (parts.length === 0) return ''
  if (parts.length === 1) return parts[0]
  const joiner = active.groupJoin === 'OR' ? '^NQ' : '^'
  const wrapped = parts.map((part) => (part.startsWith('(') ? part : `(${part})`))
  return wrapped.join(joiner)
}

class QueryScanner {
  source: string
  index = 0

  constructor(source: string) {
    this.source = source
  }

  peek() {
    return this.source[this.index] ?? ''
  }

  rest() {
    return this.source.slice(this.index)
  }

  startsWith(value: string) {
    return this.source.startsWith(value, this.index)
  }

  take(count = 1) {
    const value = this.source.slice(this.index, this.index + count)
    this.index += count
    return value
  }

  skipSpaces() {
    while (this.peek() === ' ') this.take()
  }
}

function parseEncodedValue(scanner: QueryScanner) {
  scanner.skipSpaces()
  if (scanner.peek() === '"') {
    scanner.take()
    let value = ''
    while (scanner.peek()) {
      const next = scanner.take()
      if (next === '\\') {
        value += scanner.take()
        if (value.length > maximumQueryValueLength) throw new Error('A query value is too long.')
        continue
      }
      if (next === '"') return boundQueryValue(value)
      value += next
      if (value.length > maximumQueryValueLength) throw new Error('A query value is too long.')
    }
    throw new Error('Unclosed quoted value.')
  }
  let value = ''
  while (scanner.peek() && !'^( )'.includes(scanner.peek()) && !scanner.startsWith('^OR') && !scanner.startsWith('^NQ')) {
    value += scanner.take()
    if (value.length > maximumQueryValueLength) throw new Error('A query value is too long.')
  }
  return boundQueryValue(value)
}

function boundQueryValue(value: string) {
  if (value.length > maximumQueryValueLength) throw new Error('A query value is too long.')
  return value
}

function parseOperator(scanner: QueryScanner): QueryOperator {
  for (const entry of operatorTokens) {
    if (scanner.startsWith(entry.token)) {
      scanner.take(entry.token.length)
      return entry.operator
    }
  }
  throw new Error('Expected an operator such as =, !=, LIKE, or STARTSWITH.')
}

function parseField(scanner: QueryScanner) {
  scanner.skipSpaces()
  const rest = scanner.rest()
  let best: string | null = null
  let bestAt = Number.POSITIVE_INFINITY
  for (const entry of operatorTokens) {
    const at = rest.indexOf(entry.token)
    if (at <= 0 || at >= bestAt) continue
    const field = rest.slice(0, at)
    if (!isSafeFieldName(field)) continue
    best = field
    bestAt = at
  }
  if (!best) throw new Error('Expected a field name.')
  scanner.take(best.length)
  return best
}

type ParsedNode =
  | { kind: 'condition'; condition: QueryCondition }
  | { kind: 'group'; join: QueryJoin; nodes: ParsedNode[] }

function parsePrimary(scanner: QueryScanner): ParsedNode {
  scanner.skipSpaces()
  if (scanner.peek() === '(') {
    scanner.take()
    const node = parseOr(scanner)
    scanner.skipSpaces()
    if (scanner.peek() !== ')') throw new Error('Expected a closing parenthesis.')
    scanner.take()
    return node
  }
  const field = parseField(scanner)
  const operator = parseOperator(scanner)
  const value = conditionNeedsValue(operator) ? parseEncodedValue(scanner) : ''
  return { kind: 'condition', condition: { id: newQueryId('c'), field, operator, value } }
}

function parseAnd(scanner: QueryScanner): ParsedNode {
  const nodes = [parsePrimary(scanner)]
  for (;;) {
    scanner.skipSpaces()
    if (!scanner.startsWith('^') || scanner.startsWith('^OR') || scanner.startsWith('^NQ')) break
    scanner.take()
    nodes.push(parsePrimary(scanner))
  }
  return nodes.length === 1 ? nodes[0] : { kind: 'group', join: 'AND', nodes }
}

function parseOr(scanner: QueryScanner): ParsedNode {
  const nodes = [parseAnd(scanner)]
  for (;;) {
    scanner.skipSpaces()
    if (scanner.startsWith('^OR')) {
      scanner.take(3)
      nodes.push(parseAnd(scanner))
      continue
    }
    if (scanner.startsWith('^NQ')) {
      scanner.take(3)
      nodes.push(parseAnd(scanner))
      continue
    }
    break
  }
  return nodes.length === 1 ? nodes[0] : { kind: 'group', join: 'OR', nodes }
}

function flattenNode(node: ParsedNode): QueryModel {
  if (node.kind === 'condition') {
    return { groupJoin: 'AND', groups: [{ id: newQueryId('g'), join: 'AND', conditions: [node.condition] }] }
  }
  const conditions: QueryCondition[] = []
  const nested: QueryGroup[] = []
  for (const child of node.nodes) {
    if (child.kind === 'condition') conditions.push(child.condition)
    else {
      const flattened = flattenNode(child)
      if (flattened.groups.length === 1 && flattened.groups[0].join === child.join) nested.push(flattened.groups[0])
      else nested.push(...flattened.groups.map((group) => ({ ...group, join: child.join })))
    }
  }
  if (nested.length === 0) {
    return { groupJoin: node.join, groups: [{ id: newQueryId('g'), join: node.join, conditions }] }
  }
  const groups: QueryGroup[] = []
  if (conditions.length > 0) groups.push({ id: newQueryId('g'), join: node.join, conditions })
  groups.push(...nested)
  return { groupJoin: node.join, groups: groups.length > 0 ? groups : [emptyGroup()] }
}

export function parseQuery(source: string, fields?: readonly QueryField[]): QueryParseResult {
  if (source.length > maximumEncodedQueryLength) {
    return { ok: false, error: 'The query is too long.' }
  }
  const trimmed = source.trim()
  if (!trimmed) return { ok: true, model: emptyQuery() }
  try {
    const scanner = new QueryScanner(trimmed)
    const node = parseOr(scanner)
    scanner.skipSpaces()
    if (scanner.peek()) throw new Error('Unexpected text after the query.')
    const model = flattenNode(node)
    return bindQuery(model.groups.length > 0 ? model : emptyQuery(), fields)
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : 'The query could not be parsed.' }
  }
}

/** Restricts a parsed model to known columns so unknown names cannot probe the row object. */
export function bindQuery(model: QueryModel, fields?: readonly QueryField[]): QueryParseResult {
  if (model.groups.length > maximumQueryGroups) {
    return { ok: false, error: 'The query has too many groups.' }
  }
  let conditionCount = 0
  const allowed = fields ? new Set(fields.map((field) => field.key).filter(isSafeFieldName)) : null
  for (const group of model.groups) {
    conditionCount += group.conditions.length
    if (conditionCount > maximumQueryConditions) {
      return { ok: false, error: 'The query has too many conditions.' }
    }
    for (const condition of group.conditions) {
      if (!condition.field) continue
      if (!isSafeFieldName(condition.field)) {
        return { ok: false, error: 'Expected a field name.' }
      }
      if (allowed && !allowed.has(condition.field)) {
        return { ok: false, error: `Unknown field "${condition.field}".` }
      }
      if (condition.value.length > maximumQueryValueLength) {
        return { ok: false, error: 'A query value is too long.' }
      }
    }
  }
  return { ok: true, model }
}

function numericCompare(left: string, right: string) {
  const leftNumber = Number(left.replaceAll(',', ''))
  const rightNumber = Number(right.replaceAll(',', ''))
  if (Number.isFinite(leftNumber) && Number.isFinite(rightNumber)) return leftNumber - rightNumber
  return left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' })
}

function listValues(value: string) {
  return value.split(',').map((item) => item.trim().toLowerCase()).filter(Boolean)
}

export function matchQueryValue(text: string, operator: QueryOperator, value: string, kind?: GridColumnKind) {
  void kind
  const haystack = text.trim()
  const needle = value.trim()
  const lowerHay = haystack.toLowerCase()
  const lowerNeedle = needle.toLowerCase()
  switch (operator) {
    case 'eq':
      return lowerHay === lowerNeedle
    case 'neq':
      return lowerHay !== lowerNeedle
    case 'contains':
      return lowerHay.includes(lowerNeedle)
    case 'not_contains':
      return !lowerHay.includes(lowerNeedle)
    case 'starts_with':
      return lowerHay.startsWith(lowerNeedle)
    case 'ends_with':
      return lowerHay.endsWith(lowerNeedle)
    case 'is_empty':
      return haystack.length === 0
    case 'is_not_empty':
      return haystack.length > 0
    case 'in':
      return listValues(needle).includes(lowerHay)
    case 'not_in':
      return !listValues(needle).includes(lowerHay)
    case 'gt':
      return numericCompare(haystack, needle) > 0
    case 'gte':
      return numericCompare(haystack, needle) >= 0
    case 'lt':
      return numericCompare(haystack, needle) < 0
    case 'lte':
      return numericCompare(haystack, needle) <= 0
    default:
      return false
  }
}

function matchCondition<T>(row: T, fields: readonly QueryField[], condition: QueryCondition, textOf: (row: T, field: string) => string) {
  const field = fields.find((candidate) => candidate.key === condition.field)
  if (!field || !isSafeFieldName(field.key)) return false
  return matchQueryValue(textOf(row, field.key), condition.operator, condition.value.slice(0, maximumQueryValueLength), field.kind)
}

function matchGroup<T>(row: T, fields: readonly QueryField[], group: QueryGroup, textOf: (row: T, field: string) => string) {
  const conditions = group.conditions.filter(isConditionActive)
  if (conditions.length === 0) return true
  return group.join === 'OR'
    ? conditions.some((condition) => matchCondition(row, fields, condition, textOf))
    : conditions.every((condition) => matchCondition(row, fields, condition, textOf))
}

export function matchQuery<T>(row: T, fields: readonly QueryField[], model: QueryModel, textOf: (row: T, field: string) => string) {
  const active = activeQuery(model)
  if (active.groups.length === 0) return true
  return active.groupJoin === 'OR'
    ? active.groups.some((group) => matchGroup(row, fields, group, textOf))
    : active.groups.every((group) => matchGroup(row, fields, group, textOf))
}

export function describeQuery(model: QueryModel, fields: readonly QueryField[]) {
  const active = activeQuery(model)
  if (active.groups.length === 0) return ''
  const fieldLabel = (key: string) => fields.find((field) => field.key === key)?.header ?? key
  const groups = active.groups.map((group) => {
    const parts = group.conditions.map((condition) => {
      const operator = operatorLabels[condition.operator]
      if (!conditionNeedsValue(condition.operator)) return `${fieldLabel(condition.field)} ${operator}`
      return `${fieldLabel(condition.field)} ${operator} ${condition.value}`
    })
    const joined = parts.join(group.join === 'OR' ? ' OR ' : ' AND ')
    return parts.length > 1 ? `(${joined})` : joined
  })
  return groups.join(active.groupJoin === 'OR' ? ' OR ' : ' AND ')
}
