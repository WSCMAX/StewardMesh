import type { ReactNode } from 'react'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// Column definitions describe how one record field is read, displayed, edited,
// parsed from pasted spreadsheet text, and written back into a request payload.
// Screens own the field semantics; the grid stays generic.

export type GridColumnKind = 'text' | 'enum' | 'number' | 'money' | 'date' | 'instant' | 'lookup'

/** A directory or inventory record a lookup column can resolve to. */
export type LookupOption = { id: string; label: string; detail?: string }

/** One field on the inline “add a new record” form a lookup picker can open. */
export type LookupCreateField = {
  key: string
  label: string
  required?: boolean
  placeholder?: string
  /** Turns the field into a parent-record select, such as a building’s site. */
  options?: readonly LookupOption[]
}

export type LookupCreateConfig = {
  /** Button label, e.g. “Add department”. */
  label: string
  fields: readonly LookupCreateField[]
  submit: (values: Record<string, string>) => Promise<LookupOption>
}

export type LookupConfig = {
  /** Loads matching records as the operator types. */
  search: (query: string) => Promise<readonly LookupOption[]>
  /** Already-known records used to render ids as names without a round trip. */
  options?: readonly LookupOption[]
  multiple?: boolean
  /** Lets one selected record be flagged primary, shown with a badge. */
  allowPrimary?: boolean
  /** Inline create form shown from the picker’s + control. */
  create?: LookupCreateConfig
  /** Opens the owning page, e.g. People locations or Ledger. */
  browseHref?: string
  browseLabel?: string
  onBrowse?: () => void
}

export function filterLookupOptions(options: readonly LookupOption[], query: string) {
  const needle = query.trim().toLowerCase()
  if (!needle) return [...options]
  return options.filter((option) => option.label.toLowerCase().includes(needle)
    || option.id.toLowerCase().includes(needle)
    || (option.detail ?? '').toLowerCase().includes(needle))
}

export function mergeLookupOptions(primary: readonly LookupOption[], extra: readonly LookupOption[]) {
  const seen = new Set(primary.map((option) => option.id))
  return [...primary, ...extra.filter((option) => !seen.has(option.id))]
}

export type ParseResult = { ok: true; text: string } | { ok: false; error: string }

/**
 * Everything about a column that does not depend on the record type. Editing,
 * validation, and the clipboard work purely against these rules, which is what
 * lets staged rows that have no record yet flow through the same code paths.
 */
export type ColumnRules = {
  key: string
  header: string
  kind: GridColumnKind
  editable?: boolean
  /** Allowed values for `enum` columns. Parsing is case-insensitive against these. */
  options?: readonly string[]
  required?: boolean
  maxLength?: number
  minimum?: number
  /** Column width in rem. Widths drive the fixed table layout and truncation. */
  width?: number
  align?: 'left' | 'right'
  /** Overrides the kind-based parser for pasted or typed text. */
  parse?: (input: string) => ParseResult
  /** Writes canonical text into a request payload draft. Defaults to `draft[key] = text`. */
  toPayload?: (draft: Record<string, unknown>, text: string) => void
  help?: string
  /** Opens the barcode camera inside this cell's editor. */
  scannable?: boolean
  /** Replaces the plain text editor with a searchable record picker. */
  lookup?: LookupConfig
  /** Hides the column until the operator turns it on in the column chooser. */
  hiddenByDefault?: boolean
}

export type GridColumn<T> = ColumnRules & {
  /** Canonical editable text for a cell. Copy and the cell editor both use this. */
  text: (row: T) => string
  /** Optional richer read-only rendering. Falls back to the canonical text. */
  display?: (row: T) => ReactNode
  /** Human-readable value for CSV, Excel, and JSON export. Falls back to lookup labels, then canonical text. */
  exportText?: (row: T) => string
  /** Supplies a row-scoped lookup editor, such as rooms filtered by building. */
  resolveLookup?: (context: { row: T | undefined; values: Record<string, string> }) => LookupConfig | undefined
}

const calendarPattern = /^(\d{4})-(\d{2})-(\d{2})$/
const slashDatePattern = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/
const localInstantPattern = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}):(\d{2})(?::\d{2})?$/
const integerPattern = /^-?\d+$/
const defaultMaximumLength = 500

export function labelize(value: string) {
  return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

export function dollarsFromMinor(minor?: number) {
  return typeof minor === 'number' && minor !== 0 ? (minor / 100).toFixed(2) : ''
}

export function minorFromDollars(value: string) {
  const normalized = value.trim().replaceAll(',', '').replace(/^\$/, '')
  if (!normalized) return 0
  const [whole, fraction = ''] = normalized.split('.')
  return Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
}

/** Renders a stored calendar or instant value as the `YYYY-MM-DD` text a cell edits. */
export function calendarText(value?: string) {
  return value ? value.slice(0, 10) : ''
}

/** Renders a stored instant as the local `YYYY-MM-DDTHH:mm` text a cell edits. */
export function instantText(value?: string) {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

function isRealCalendarDate(year: number, month: number, day: number) {
  if (month < 1 || month > 12 || day < 1) return false
  const probe = new Date(Date.UTC(year, month - 1, day))
  return probe.getUTCFullYear() === year && probe.getUTCMonth() === month - 1 && probe.getUTCDate() === day
}

function parseCalendar(input: string): ParseResult {
  const value = input.trim()
  if (!value) return { ok: true, text: '' }
  const iso = calendarPattern.exec(value)
  if (iso) {
    const [, year, month, day] = iso
    if (!isRealCalendarDate(Number(year), Number(month), Number(day))) return { ok: false, error: 'Not a real calendar date.' }
    return { ok: true, text: value }
  }
  // Spreadsheets commonly export month/day/year, so accept it and canonicalize.
  const slashed = slashDatePattern.exec(value)
  if (slashed) {
    const [, month, day, year] = slashed
    if (!isRealCalendarDate(Number(year), Number(month), Number(day))) return { ok: false, error: 'Not a real calendar date.' }
    return { ok: true, text: `${year}-${month.padStart(2, '0')}-${day.padStart(2, '0')}` }
  }
  return { ok: false, error: 'Use YYYY-MM-DD.' }
}

function parseInstant(input: string): ParseResult {
  const value = input.trim()
  if (!value) return { ok: true, text: '' }
  const local = localInstantPattern.exec(value)
  if (local) {
    const [, day, hour, minute] = local
    const calendar = parseCalendar(day)
    if (!calendar.ok) return calendar
    if (Number(hour) > 23 || Number(minute) > 59) return { ok: false, error: 'Not a real time of day.' }
    return { ok: true, text: `${calendar.text}T${hour}:${minute}` }
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return { ok: false, error: 'Use YYYY-MM-DD HH:MM.' }
  return { ok: true, text: instantText(parsed.toISOString()) }
}

function parseMoney(input: string): ParseResult {
  const value = input.trim().replaceAll(',', '').replace(/^\$/, '')
  if (!value) return { ok: true, text: '' }
  if (!/^\d+(?:\.\d{1,2})?$/.test(value)) return { ok: false, error: 'Use a positive amount such as 1250.00.' }
  return { ok: true, text: Number(value).toFixed(2) }
}

function parseNumber(input: string, minimum?: number): ParseResult {
  const value = input.trim().replaceAll(',', '')
  if (!value) return { ok: true, text: '' }
  if (!integerPattern.test(value)) return { ok: false, error: 'Use a whole number.' }
  if (minimum !== undefined && Number(value) < minimum) return { ok: false, error: `Use ${minimum} or more.` }
  return { ok: true, text: String(Number(value)) }
}

function parseEnum(input: string, options: readonly string[]): ParseResult {
  const value = input.trim()
  if (!value) return { ok: true, text: '' }
  const matched = options.find((option) => option.toLowerCase() === value.toLowerCase() || labelize(option).toLowerCase() === value.toLowerCase())
  if (!matched) return { ok: false, error: `Use one of ${options.join(', ')}.` }
  return { ok: true, text: matched }
}

/** One selected lookup record. A trailing `*` marks it as primary. */
export type LookupSelection = { id: string; primary: boolean }

/**
 * Canonical lookup text is `id*` for a primary record and `id` otherwise,
 * joined with `|`. Copy and paste round-trip ids; the cell display maps them
 * to names through the column's known options.
 */
export function parseLookupText(input: string): LookupSelection[] {
  const seen = new Set<string>()
  const selected: LookupSelection[] = []
  for (const part of input.split('|')) {
    const trimmed = part.trim()
    if (!trimmed) continue
    const primary = trimmed.endsWith('*')
    const id = (primary ? trimmed.slice(0, -1) : trimmed).trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    selected.push({ id, primary })
  }
  if (selected.filter((item) => item.primary).length > 1) {
    let kept = false
    return selected.map((item) => {
      if (!item.primary) return item
      if (kept) return { ...item, primary: false }
      kept = true
      return item
    })
  }
  return selected
}

export function encodeLookupText(selected: readonly LookupSelection[]) {
  return selected.map((item) => (item.primary ? `${item.id}*` : item.id)).join('|')
}

export function lookupLabel(id: string, options: readonly LookupOption[] | undefined, fallback = id) {
  return options?.find((option) => option.id === id)?.label ?? fallback
}

/** Turns canonical lookup cell text into the labels an export should carry. */
export function lookupExportText(raw: string, options: readonly LookupOption[] | undefined, multiple = false) {
  const selected = parseLookupText(raw)
  if (selected.length === 0) return ''
  return selected.map((item) => {
    const label = lookupLabel(item.id, options)
    return item.primary && multiple ? `${label} (primary)` : label
  }).join(', ')
}

function parseLookup(input: string, column: ColumnRules): ParseResult {
  const value = input.trim()
  if (!value) return { ok: true, text: '' }
  const selected = parseLookupText(value)
  if (selected.length === 0) {
    // A pasted name is resolved against already-loaded options so a
    // spreadsheet of display names still lands on ids.
    const options = column.lookup?.options ?? []
    const matched = options.find((option) => option.label.toLowerCase() === value.toLowerCase() || option.id.toLowerCase() === value.toLowerCase())
    if (!matched) return { ok: false, error: `No matching ${column.header.toLowerCase()} for "${value}".` }
    return { ok: true, text: column.lookup?.allowPrimary ? `${matched.id}*` : matched.id }
  }
  if (!column.lookup?.multiple && selected.length > 1) return { ok: false, error: `${column.header} accepts one record.` }
  const names = new Map((column.lookup?.options ?? []).flatMap((option) => [
    [option.id.toLowerCase(), option.id],
    [option.label.toLowerCase(), option.id],
  ] as const))
  const resolved: LookupSelection[] = []
  const seen = new Set<string>()
  for (const item of selected) {
    const id = names.get(item.id.toLowerCase()) ?? item.id
    if (seen.has(id)) continue
    seen.add(id)
    resolved.push({ id, primary: item.primary })
  }
  if (column.lookup?.allowPrimary && resolved.length > 0 && !resolved.some((item) => item.primary)) {
    resolved[0] = { ...resolved[0], primary: true }
  }
  return { ok: true, text: encodeLookupText(resolved) }
}

/**
 * Normalizes text typed into a cell or pasted from a spreadsheet, returning
 * either canonical text for the cell or a message the grid shows on the cell.
 */
export function parseCell(column: ColumnRules, input: string): ParseResult {
  const trimmed = input.trim()
  if (column.required && !trimmed) return { ok: false, error: `${column.header} is required.` }
  const result = column.parse
    ? column.parse(input)
    : column.kind === 'enum' ? parseEnum(input, column.options ?? [])
      : column.kind === 'money' ? parseMoney(input)
        : column.kind === 'number' ? parseNumber(input, column.minimum)
          : column.kind === 'date' ? parseCalendar(input)
            : column.kind === 'instant' ? parseInstant(input)
              : column.kind === 'lookup' || column.lookup ? parseLookup(input, column)
                : { ok: true as const, text: trimmed }
  if (!result.ok) return result
  const limit = column.maxLength ?? defaultMaximumLength
  if (result.text.length > limit) return { ok: false, error: `Use ${limit} characters or fewer.` }
  return result
}

/**
 * Extra checks an operator adds to a column from inside the grid. Screens define
 * what a field means and how it parses; this is how the people entering the data
 * tighten it for their own records without waiting on a code change.
 */
export type ValidationRule = {
  required?: boolean
  /** Restricts the cell to this list. Matching is case-insensitive. */
  allowed?: readonly string[]
  /** Bounds for `number` and `money` columns. Ignored on other kinds. */
  minimum?: number
  maximum?: number
  /** Regular expression the canonical text has to match. */
  pattern?: string
  /** Replaces the generated message when the rule rejects a value. */
  message?: string
}

export function isEmptyRule(rule?: ValidationRule) {
  if (!rule) return true
  return !rule.required
    && (rule.allowed?.length ?? 0) === 0
    && rule.minimum === undefined
    && rule.maximum === undefined
    && !rule.pattern?.trim()
}

export function describeRule(rule: ValidationRule) {
  const parts: string[] = []
  if (rule.required) parts.push('required')
  if (rule.allowed?.length) parts.push(`one of ${rule.allowed.join(', ')}`)
  if (rule.minimum !== undefined) parts.push(`at least ${rule.minimum}`)
  if (rule.maximum !== undefined) parts.push(`at most ${rule.maximum}`)
  if (rule.pattern?.trim()) parts.push(`matching ${rule.pattern.trim()}`)
  return parts.join(', ')
}

function checkRule(rule: ValidationRule, text: string, column: ColumnRules) {
  const fail = (generated: string) => rule.message?.trim() || generated
  if (rule.required && !text) return fail(`${column.header} is required.`)
  if (!text) return null
  const allowed = rule.allowed ?? []
  if (allowed.length > 0 && !allowed.some((option) => option.toLowerCase() === text.toLowerCase())) {
    return fail(`Use one of ${allowed.join(', ')}.`)
  }
  if (column.kind === 'number' || column.kind === 'money') {
    const value = Number(text)
    if (rule.minimum !== undefined && value < rule.minimum) return fail(`Use ${rule.minimum} or more.`)
    if (rule.maximum !== undefined && value > rule.maximum) return fail(`Use ${rule.maximum} or less.`)
  }
  if (rule.pattern?.trim()) {
    let expression: RegExp
    try {
      expression = new RegExp(rule.pattern)
    } catch {
      // A half-typed expression must not lock every cell in the column.
      return null
    }
    if (!expression.test(text)) return fail(`${column.header} does not match ${rule.pattern}.`)
  }
  return null
}

/**
 * Wraps a column so it enforces an operator's rule after its own parser has
 * produced canonical text. The built-in rules always win: a rule can narrow a
 * column but never widen it into values the API would reject.
 */
export function withValidation<T>(column: GridColumn<T>, rule?: ValidationRule): GridColumn<T> {
  if (!rule || isEmptyRule(rule)) return column
  return {
    ...column,
    parse: (input) => {
      const parsed = parseCell(column, input)
      if (!parsed.ok) return parsed
      const failure = checkRule(rule, parsed.text, column)
      return failure ? { ok: false, error: failure } : parsed
    },
  }
}

/** Writes one canonical cell value into a request payload draft. */
export function applyCellPayload(column: ColumnRules, draft: Record<string, unknown>, text: string) {
  if (column.toPayload) {
    column.toPayload(draft, text)
    return
  }
  if (column.kind === 'money') {
    draft[column.key] = minorFromDollars(text)
    return
  }
  if (column.kind === 'number') {
    draft[column.key] = text ? Number(text) : undefined
    return
  }
  if (column.kind === 'date') {
    draft[column.key] = text ? `${text}T00:00:00Z` : undefined
    return
  }
  if (column.kind === 'instant') {
    draft[column.key] = text ? new Date(text).toISOString() : undefined
    return
  }
  draft[column.key] = text
}

// Excel and Sheets exchange rectangular selections as tab-separated values where
// a field containing a tab, newline, or quote is wrapped in double quotes and
// inner quotes are doubled. Both directions are implemented so a range copied
// out of the grid pastes back in unchanged.

export function encodeTSV(matrix: readonly (readonly string[])[]) {
  return matrix.map((row) => row.map(encodeTSVCell).join('\t')).join('\n')
}

function encodeTSVCell(value: string) {
  if (!/[\t\n\r"]/.test(value)) return value
  return `"${value.replaceAll('"', '""')}"`
}

export function decodeTSV(source: string): string[][] {
  const matrix: string[][] = []
  let row: string[] = []
  let cell = ''
  let quoted = false
  let index = 0
  while (index < source.length) {
    const token = source[index]
    if (quoted) {
      if (token === '"') {
        if (source[index + 1] === '"') {
          cell += '"'
          index += 2
          continue
        }
        quoted = false
        index += 1
        continue
      }
      cell += token
      index += 1
      continue
    }
    if (token === '"' && cell.length === 0) {
      quoted = true
      index += 1
      continue
    }
    if (token === '\t') {
      row.push(cell)
      cell = ''
      index += 1
      continue
    }
    if (token === '\n' || token === '\r') {
      row.push(cell)
      matrix.push(row)
      row = []
      cell = ''
      index += source.startsWith('\r\n', index) ? 2 : 1
      continue
    }
    cell += token
    index += 1
  }
  if (cell.length > 0 || row.length > 0 || quoted) {
    row.push(cell)
    matrix.push(row)
  }
  return matrix
}
