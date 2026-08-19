import { isRevision, requestJSON, type Revision } from './api'
import { encodeLookupText, lookupExportText, parseLookupText, type GridColumn, type LookupConfig, type LookupCreateConfig } from './grid/columns'
import type { CellEdit } from './grid/useCellEditing'

// Requirement: REQ-LABELS-001. Feature: identity.labels.

export type LabelValueKind = 'flag' | 'text' | 'select' | 'multiselect'

export type LabelDefinition = {
  id: string
  name: string
  description?: string
  valueKind: LabelValueKind
  applicableRecordTypes: string[]
  options?: string[]
  status: 'active' | 'retired'
  revision: Revision
}

export type LabelAssignment = {
  definitionId: string
  recordType: string
  recordId: string
  valueText?: string
  values?: string[]
  revision: Revision
}

export const LABEL_COLUMN_PREFIX = 'label:'

const labelValueKinds: LabelValueKind[] = ['flag', 'text', 'select', 'multiselect']

export function labelItems(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const candidate = (value as Record<string, unknown>).items
  return Array.isArray(candidate) ? candidate : []
}

export function isLabelDefinition(value: unknown): value is LabelDefinition {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.name === 'string'
    && labelValueKinds.includes(item.valueKind as LabelValueKind)
    && Array.isArray(item.applicableRecordTypes)
    && (item.status === 'active' || item.status === 'retired')
    && isRevision(item.revision)
}

export function isLabelAssignment(value: unknown): value is LabelAssignment {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.definitionId === 'string' && typeof item.recordType === 'string'
    && typeof item.recordId === 'string' && isRevision(item.revision)
}

export function isLabelColumnKey(key: string) {
  return key.startsWith(LABEL_COLUMN_PREFIX)
}

export function labelDefinitionIdFromColumnKey(key: string) {
  return key.slice(LABEL_COLUMN_PREFIX.length)
}

export function labelColumnKey(definitionId: string) {
  return `${LABEL_COLUMN_PREFIX}${definitionId}`
}

function optionLookupOptions(options: readonly string[] | undefined) {
  return (options ?? []).map((option) => ({ id: option, label: option }))
}

function assignmentForAsset(
  assignments: ReadonlyMap<string, ReadonlyMap<string, LabelAssignment>> | undefined,
  definitionId: string,
  assetId: string,
) {
  return assignments?.get(definitionId)?.get(assetId)
}

export function labelCellText(
  definition: LabelDefinition,
  assignment: LabelAssignment | undefined,
) {
  switch (definition.valueKind) {
    case 'flag':
      return assignment ? 'yes' : ''
    case 'text':
      return assignment?.valueText ?? ''
    case 'select':
      return assignment?.valueText ?? ''
    case 'multiselect':
      return encodeLookupText((assignment?.values ?? []).map((value) => ({ id: value, primary: false })))
    default:
      return ''
  }
}

export function labelCellDisplay(
  definition: LabelDefinition,
  assignment: LabelAssignment | undefined,
) {
  switch (definition.valueKind) {
    case 'flag':
      return assignment ? definition.name : ''
    case 'text':
      return assignment?.valueText ?? ''
    case 'select':
      return assignment?.valueText ?? ''
    case 'multiselect':
      return (assignment?.values ?? []).join(', ')
    default:
      return ''
  }
}

export type LabelColumnContext<T> = {
  csrfToken: string
  canWriteLabels: boolean
  definitions: readonly LabelDefinition[]
  assignments: ReadonlyMap<string, ReadonlyMap<string, LabelAssignment>>
  onDefinitionUpdated: (definition: LabelDefinition) => void
  rowId: (row: T) => string
}

function labelValueCreate(
  definition: LabelDefinition,
  context: LabelColumnContext<unknown>,
): LookupCreateConfig | undefined {
  if (!context.canWriteLabels || (definition.valueKind !== 'select' && definition.valueKind !== 'multiselect')) {
    return undefined
  }
  return {
    label: 'Add value',
    fields: [{ key: 'value', label: 'Value', required: true, placeholder: 'New option' }],
    submit: async (values) => {
      const value = values.value.trim()
      if (!value) throw new Error('value required')
      const updated = await appendLabelDefinitionOptions(definition, [value], context.csrfToken)
      context.onDefinitionUpdated(updated)
      return { id: value, label: value }
    },
  }
}

function labelLookup(
  definition: LabelDefinition,
  context: LabelColumnContext<unknown>,
): LookupConfig {
  const options = optionLookupOptions(definition.options)
  return {
    options,
    multiple: definition.valueKind === 'multiselect',
    search: async (query) => {
      const needle = query.trim().toLowerCase()
      if (!needle) return options
      return options.filter((option) => option.label.toLowerCase().includes(needle))
    },
    create: labelValueCreate(definition, context),
    browseHref: '#workspace-threads',
    browseLabel: 'Manage tags',
  }
}

export function buildLabelColumns<T>(
  context: LabelColumnContext<T>,
  editable: boolean,
): GridColumn<T>[] {
  return context.definitions.map((definition) => {
    const key = labelColumnKey(definition.id)
    const base = {
      key,
      header: definition.name,
      editable: editable && context.canWriteLabels,
      width: definition.valueKind === 'multiselect' ? 14 : 10,
      help: definition.description,
      text: (row: T) => labelCellText(definition, assignmentForAsset(context.assignments, definition.id, context.rowId(row))),
      display: (row: T) => labelCellDisplay(definition, assignmentForAsset(context.assignments, definition.id, context.rowId(row))),
    } satisfies Partial<GridColumn<T>>

    if (definition.valueKind === 'flag') {
      return {
        ...base,
        kind: 'enum',
        options: ['', 'yes'],
        exportText: (row: T) => labelCellDisplay(definition, assignmentForAsset(context.assignments, definition.id, context.rowId(row))),
      }
    }
    if (definition.valueKind === 'text') {
      return {
        ...base,
        kind: 'text',
        maxLength: 500,
        exportText: (row: T) => labelCellText(definition, assignmentForAsset(context.assignments, definition.id, context.rowId(row))),
      }
    }
    if (definition.valueKind === 'select') {
      const lookup = labelLookup(definition, context as LabelColumnContext<unknown>)
      return {
        ...base,
        kind: 'lookup',
        lookup,
        exportText: (row: T) => lookupExportText(base.text(row), lookup.options, false),
      }
    }
    const lookup = labelLookup(definition, context as LabelColumnContext<unknown>)
    return {
      ...base,
      kind: 'lookup',
      lookup,
      exportText: (row: T) => lookupExportText(base.text(row), lookup.options, true),
    }
  })
}

export async function loadAtlasLabelDefinitions() {
  return loadLabelDefinitionsFor('atlas.asset')
}

export async function loadLabelDefinitionsFor(recordType: string) {
  const response = await requestJSON('/api/v1/labels/definitions')
  return labelItems(response)
    .filter(isLabelDefinition)
    .filter((item) => item.status === 'active' && item.applicableRecordTypes.includes(recordType))
    .sort((left, right) => left.name.localeCompare(right.name))
}

export async function loadLabelAssignmentsForDefinition(definitionId: string, recordType: string) {
  const params = new URLSearchParams({ recordType })
  const response = await requestJSON(`/api/v1/labels/definitions/${encodeURIComponent(definitionId)}/assignments?${params.toString()}`)
  return labelItems(response).filter(isLabelAssignment)
}

export async function loadAtlasLabelAssignments(definitions: readonly LabelDefinition[]) {
  return loadLabelAssignments(definitions, 'atlas.asset')
}

export async function loadLabelAssignments(definitions: readonly LabelDefinition[], recordType: string) {
  const maps = new Map<string, Map<string, LabelAssignment>>()
  await Promise.all(definitions.map(async (definition) => {
    const assignments = await loadLabelAssignmentsForDefinition(definition.id, recordType)
    const byRecord = new Map<string, LabelAssignment>()
    for (const assignment of assignments) byRecord.set(assignment.recordId, assignment)
    maps.set(definition.id, byRecord)
  }))
  return maps
}

export async function appendLabelDefinitionOptions(
  definition: LabelDefinition,
  additions: readonly string[],
  csrfToken: string,
) {
  const existing = new Set(definition.options ?? [])
  const options = [...(definition.options ?? [])]
  for (const value of additions) {
    const trimmed = value.trim()
    if (!trimmed || existing.has(trimmed)) continue
    existing.add(trimmed)
    options.push(trimmed)
  }
  options.sort((left, right) => left.localeCompare(right))
  const saved = await requestJSON(`/api/v1/labels/definitions/${encodeURIComponent(definition.id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({
      name: definition.name,
      description: definition.description ?? '',
      valueKind: definition.valueKind,
      applicableRecordTypes: definition.applicableRecordTypes,
      options,
      status: definition.status,
      revision: definition.revision,
    }),
  })
  if (!isLabelDefinition(saved)) throw new Error('invalid label definition response')
  return saved
}

export async function createAtlasLabelDefinition(
  input: { id?: string; name: string; valueKind: LabelValueKind; options: readonly string[] },
  csrfToken: string,
) {
  return createLabelDefinition(input, ['atlas.asset'], csrfToken)
}

export async function createLabelDefinition(
  input: { id?: string; name: string; valueKind: LabelValueKind; options: readonly string[] },
  recordTypes: readonly string[],
  csrfToken: string,
) {
  const saved = await requestJSON('/api/v1/labels/definitions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({
      id: input.id?.trim() || undefined,
      name: input.name.trim(),
      valueKind: input.valueKind,
      applicableRecordTypes: [...recordTypes],
      options: input.valueKind === 'select' || input.valueKind === 'multiselect' ? [...input.options] : undefined,
    }),
  })
  if (!isLabelDefinition(saved)) throw new Error('invalid label definition response')
  return saved
}

function selectedValues(definition: LabelDefinition, text: string) {
  if (definition.valueKind === 'multiselect') {
    return parseLookupText(text).map((item) => item.id)
  }
  if (definition.valueKind === 'select') {
    const trimmed = text.trim()
    return trimmed ? [trimmed] : []
  }
  return []
}

async function ensureDefinitionOptions(
  definition: LabelDefinition,
  values: readonly string[],
  csrfToken: string,
  onDefinitionUpdated: (definition: LabelDefinition) => void,
) {
  const missing = values.filter((value) => !(definition.options ?? []).includes(value))
  if (missing.length === 0) return definition
  const updated = await appendLabelDefinitionOptions(definition, missing, csrfToken)
  onDefinitionUpdated(updated)
  return updated
}

export async function saveLabelCellEdit(
  recordId: string,
  definition: LabelDefinition,
  text: string,
  existing: LabelAssignment | undefined,
  csrfToken: string,
  canWriteLabels: boolean,
  onDefinitionUpdated: (definition: LabelDefinition) => void,
  recordType = 'atlas.asset',
): Promise<LabelAssignment | null> {
  if (!canWriteLabels) throw new Error('Tag write access is required.')

  const path = `/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordId)}/assignments/${encodeURIComponent(definition.id)}`

  if (definition.valueKind === 'flag') {
    if (text.trim().toLowerCase() === 'yes') {
      const saved = await requestJSON(path, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          definitionId: definition.id,
          recordType,
          recordId,
          revision: existing?.revision ?? 0,
        }),
      })
      if (!isLabelAssignment(saved)) throw new Error('invalid tag assignment response')
      return saved
    }
    if (existing) {
      await requestJSON(`${path}?revision=${existing.revision}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken },
      })
    }
    return null
  }

  if (definition.valueKind === 'text') {
    const valueText = text.trim()
    if (!valueText) {
      if (existing) {
        await requestJSON(`${path}?revision=${existing.revision}`, {
          method: 'DELETE',
          headers: { 'X-CSRF-Token': csrfToken },
        })
      }
      return null
    }
    const saved = await requestJSON(path, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
        definitionId: definition.id,
        recordType,
        recordId,
        valueText,
        revision: existing?.revision ?? 0,
      }),
    })
    if (!isLabelAssignment(saved)) throw new Error('invalid tag assignment response')
    return saved
  }

  const values = selectedValues(definition, text)
  if (values.length === 0) {
    if (existing) {
      await requestJSON(`${path}?revision=${existing.revision}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken },
      })
    }
    return null
  }

  const currentDefinition = await ensureDefinitionOptions(definition, values, csrfToken, onDefinitionUpdated)
  const saved = await requestJSON(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({
      definitionId: currentDefinition.id,
      recordType,
      recordId,
      valueText: currentDefinition.valueKind === 'select' ? values[0] : '',
      values: currentDefinition.valueKind === 'multiselect' ? values : undefined,
      revision: existing?.revision ?? 0,
    }),
  })
  if (!isLabelAssignment(saved)) throw new Error('invalid tag assignment response')
  return saved
}

export async function saveLabelEdits(
  edits: readonly CellEdit[],
  definitions: readonly LabelDefinition[],
  assignments: ReadonlyMap<string, ReadonlyMap<string, LabelAssignment>>,
  csrfToken: string,
  canWriteLabels: boolean,
  onDefinitionUpdated: (definition: LabelDefinition) => void,
  onAssignmentUpdated: (definitionId: string, recordId: string, assignment: LabelAssignment | null) => void,
  recordType = 'atlas.asset',
) {
  const definitionById = new Map(definitions.map((item) => [item.id, item]))
  for (const edit of edits) {
    if (!isLabelColumnKey(edit.columnKey)) continue
    const definitionId = labelDefinitionIdFromColumnKey(edit.columnKey)
    const definition = definitionById.get(definitionId)
    if (!definition) throw new Error(`Unknown tag column “${edit.columnKey}”.`)
    const existing = assignments.get(definitionId)?.get(edit.rowId)
    const saved = await saveLabelCellEdit(
      edit.rowId,
      definition,
      edit.text,
      existing,
      csrfToken,
      canWriteLabels,
      onDefinitionUpdated,
      recordType,
    )
    onAssignmentUpdated(definitionId, edit.rowId, saved)
  }
}
