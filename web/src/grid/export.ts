import { encodeTSV, type GridColumn, type GridColumnKind } from './columns'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// Builds a download from the grid's already-resolved view. The browser holds
// every visible cell, so the file matches what is on screen — current columns,
// sort, filters, grouping — instead of asking the server to re-query.

export type ExportFormat = 'csv' | 'json' | 'xlsx'

export type ExportScope = 'filtered' | 'selected' | 'highlighted'

export type ExportColumn = {
  key: string
  header: string
  kind: GridColumnKind
  width: number
}

export type ExportSheet = {
  name: string
  columns: readonly ExportColumn[]
  rows: readonly (readonly string[])[]
}

export function sheetFromGrid<T>(options: {
  name: string
  columns: readonly GridColumn<T>[]
  rows: readonly T[]
  text: (row: T, column: GridColumn<T>) => string
}): ExportSheet {
  return {
    name: options.name,
    columns: options.columns.map((column) => ({
      key: column.key,
      header: column.header,
      kind: column.kind,
      width: column.width ?? 12,
    })),
    rows: options.rows.map((row) => options.columns.map((column) => options.text(row, column))),
  }
}

export function csvFromSheet(sheet: ExportSheet) {
  return encodeTSV([
    sheet.columns.map((column) => formulaSafeText(column.header)),
    ...sheet.rows.map((row) => row.map(formulaSafeText)),
  ]).replaceAll('\t', ',')
}

export function jsonFromSheet(sheet: ExportSheet) {
  return `${JSON.stringify(sheet.rows.map((row) => {
    const record: Record<string, string> = {}
    sheet.columns.forEach((column, index) => { record[column.key] = row[index] ?? '' })
    return record
  }), null, 2)}\n`
}

export function downloadBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function filenameFor(name: string, format: ExportFormat) {
  const slug = name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'grid'
  return `${slug}.${format}`
}

/** Prevents spreadsheet apps from treating exported cell text as a formula. */
export function formulaSafeText(value: string) {
  return /^[=+\-@]/.test(value.trimStart()) ? `'${value}` : value
}
