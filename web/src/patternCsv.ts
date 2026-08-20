// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

export const maximumPatternCsvBytes = 128 * 1024
export const maximumPatternCsvRows = 1

export type CsvPatternField = {
  key: string
  csvHeader: string
  type: 'text' | 'number' | 'date' | 'money' | 'enum' | 'attachment' | 'reference' | 'tag'
}

export type CsvPatternTemplate = {
  id: string
  version: number
  fields: CsvPatternField[]
}

function formulaUnsafe(value: string): boolean {
  return /^[=+\-@]/.test(value.trimStart())
}

function parseRows(source: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cell = ''
  let quoted = false
  let afterQuote = false
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index]
    if (quoted) {
      if (character === '"') {
        if (source[index + 1] === '"') {
          cell += '"'
          index += 1
        } else {
          quoted = false
          afterQuote = true
        }
      } else {
        cell += character
      }
      continue
    }
    if (afterQuote && character !== ',' && character !== '\r' && character !== '\n') {
      throw new Error('CSV contains characters after a closing quote.')
    }
    if (character === '"') {
      if (cell === '' && !afterQuote) {
        quoted = true
        continue
      }
      throw new Error('CSV contains a quote inside an unquoted value.')
    }
    if (character === ',') {
      row.push(cell)
      cell = ''
      afterQuote = false
      continue
    }
    if (character === '\r' || character === '\n') {
      if (character === '\r' && source[index + 1] === '\n') index += 1
      row.push(cell)
      rows.push(row)
      row = []
      cell = ''
      afterQuote = false
      continue
    }
    cell += character
  }
  if (quoted) throw new Error('CSV contains an unterminated quoted value.')
  if (cell !== '' || row.length > 0) {
    row.push(cell)
    rows.push(row)
  }
  return rows
}

function encodeCell(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value
}

function assertBounded(source: string) {
  if (new TextEncoder().encode(source).byteLength > maximumPatternCsvBytes) {
    throw new Error('CSV exceeds the 128 KiB workbench limit.')
  }
}

function typedValue(field: CsvPatternField, value: string): string | number | undefined {
  if (value === '') return undefined
  if (field.type === 'money') {
    if (!/^-?(0|[1-9]\d*)$/.test(value)) {
      throw new Error(`${field.csvHeader} must contain an exact integer amount in minor units.`)
    }
    const exact = BigInt(value)
    if (exact < BigInt(Number.MIN_SAFE_INTEGER) || exact > BigInt(Number.MAX_SAFE_INTEGER)) {
      throw new Error(`${field.csvHeader} must contain an exact integer amount in minor units.`)
    }
    return Number(exact)
  }
  if (field.type === 'number') {
    const number = Number(value)
    if (!Number.isFinite(number)) throw new Error(`${field.csvHeader} must contain a finite number.`)
    return number
  }
  if (formulaUnsafe(value)) {
    throw new Error(`${field.csvHeader} starts with a spreadsheet formula character.`)
  }
  return value
}

export function parsePatternCsv(template: CsvPatternTemplate, source: string): Record<string, string | number> {
  assertBounded(source)
  const rows = parseRows(source.replace(/^\uFEFF/, ''))
  if (rows.length !== maximumPatternCsvRows + 1) {
    throw new Error('CSV must contain exactly one header row and one data row.')
  }
  const headers = template.fields.map((field) => field.csvHeader)
  if (rows[0].length !== headers.length || rows[0].some((header, index) => header !== headers[index])) {
    throw new Error(`CSV headers must exactly match ${template.id} version ${template.version}.`)
  }
  if (rows[1].length !== headers.length) throw new Error('CSV data row does not match the template column count.')
  const values: Record<string, string | number> = {}
  template.fields.forEach((field, index) => {
    const value = typedValue(field, rows[1][index])
    if (value !== undefined) values[field.key] = value
  })
  return values
}

export function serializePatternCsv(template: CsvPatternTemplate, values: Record<string, unknown>): string {
  const headers = template.fields.map((field) => field.csvHeader)
  const cells = template.fields.map((field) => {
    const raw = values[field.key]
    if (raw === undefined || raw === null || raw === '') return ''
    if (field.type === 'money') {
      const value = typeof raw === 'number' ? String(raw) : String(raw)
      if (!/^-?(0|[1-9]\d*)$/.test(value)) throw new Error(`${field.csvHeader} must contain an exact integer amount in minor units.`)
      const exact = BigInt(value)
      if (exact < BigInt(Number.MIN_SAFE_INTEGER) || exact > BigInt(Number.MAX_SAFE_INTEGER)) {
        throw new Error(`${field.csvHeader} must contain an exact integer amount in minor units.`)
      }
      return value
    }
    if (field.type === 'number') {
      const number = typeof raw === 'number' ? raw : Number(raw)
      if (!Number.isFinite(number)) throw new Error(`${field.csvHeader} must contain a finite number.`)
      return String(number)
    }
    const value = String(raw)
    if (formulaUnsafe(value)) throw new Error(`${field.csvHeader} starts with a spreadsheet formula character.`)
    return value
  })
  const result = `${headers.map(encodeCell).join(',')}\r\n${cells.map(encodeCell).join(',')}\r\n`
  assertBounded(result)
  return result
}
