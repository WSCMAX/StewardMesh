import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import { buttonClass, emptyStateClass, inputClass, labelClass, plainButtonClass, StatusBadge, subpanelClass } from './ui'

// Requirement: REQ-LABELS-001. Feature: identity.labels.

type ValueKind = 'flag' | 'text' | 'select' | 'multiselect'

type TagDefinition = {
  id: string
  name: string
  description?: string
  valueKind: ValueKind
  applicableRecordTypes: string[]
  options?: string[]
  status: 'active' | 'retired'
  revision: Revision
}

type TagAssignment = {
  definitionId: string
  recordType: string
  recordId: string
  valueText?: string
  values?: string[]
  revision: Revision
}

type RecordTagsProps = {
  csrfToken: string
  permissions: readonly string[]
  recordId: string
  recordName: string
  recordType: 'atlas.asset' | 'atlas.model'
}

const valueKinds: ValueKind[] = ['flag', 'text', 'select', 'multiselect']

function items(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const candidate = (value as Record<string, unknown>).items
  return Array.isArray(candidate) ? candidate : []
}

function isTagDefinition(value: unknown): value is TagDefinition {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.name === 'string' && valueKinds.includes(item.valueKind as ValueKind)
    && Array.isArray(item.applicableRecordTypes) && (item.status === 'active' || item.status === 'retired') && isRevision(item.revision)
}

function isTagAssignment(value: unknown): value is TagAssignment {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.definitionId === 'string' && typeof item.recordType === 'string' && typeof item.recordId === 'string' && isRevision(item.revision)
}

function assignmentDisplay(assignment: TagAssignment, definition?: TagDefinition) {
  const value = assignment.values?.length ? assignment.values.join(', ') : assignment.valueText
  const name = definition?.name ?? assignment.definitionId
  return value ? `${name}: ${value}` : name
}

export default function RecordTags({ csrfToken, permissions, recordId, recordName, recordType }: RecordTagsProps) {
  const canRead = permissions.includes('labels.read')
  const canWrite = permissions.includes('labels.write')
  const [definitions, setDefinitions] = useState<TagDefinition[]>([])
  const [assignments, setAssignments] = useState<TagAssignment[]>([])
  const [definitionID, setDefinitionID] = useState('')
  const [assignmentText, setAssignmentText] = useState('')
  const [assignmentValues, setAssignmentValues] = useState<string[]>([])
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

  const applicableDefinitions = useMemo(
    () => definitions.filter((item) => item.status === 'active' && item.applicableRecordTypes.includes(recordType)),
    [definitions, recordType],
  )
  const assignmentByDefinition = useMemo(
    () => new Map(assignments.map((item) => [item.definitionId, item])),
    [assignments],
  )
  const selectedDefinition = applicableDefinitions.find((item) => item.id === definitionID) ?? null
  const headingID = `${recordType.replace('.', '-')}-${recordId}-tags-heading`

  useEffect(() => {
    if (!canRead) return
    let active = true
    requestJSON('/api/v1/labels/definitions')
      .then((response) => {
        const next = items(response)
        if (!next.every(isTagDefinition)) throw new Error('invalid tag definitions response')
        if (active) setDefinitions(next)
      })
      .catch(() => {
        if (active) setDefinitions([])
      })
    return () => { active = false }
  }, [canRead])

  useEffect(() => {
    if (!canRead || !recordId) {
      setAssignments([])
      return
    }
    let active = true
    requestJSON(`/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordId)}/assignments`)
      .then((response) => {
        const next = items(response)
        if (!next.every(isTagAssignment)) throw new Error('invalid tag assignments response')
        if (active) setAssignments(next)
      })
      .catch(() => {
        if (active) setAssignments([])
      })
    return () => { active = false }
  }, [canRead, recordId, recordType])

  useEffect(() => {
    if (applicableDefinitions.length === 0) {
      setDefinitionID('')
      return
    }
    if (!applicableDefinitions.some((item) => item.id === definitionID)) {
      setDefinitionID(applicableDefinitions[0].id)
    }
  }, [applicableDefinitions, definitionID])

  useEffect(() => {
    if (!selectedDefinition) {
      setAssignmentText('')
      setAssignmentValues([])
      return
    }
    const existing = assignmentByDefinition.get(selectedDefinition.id)
    setAssignmentText(existing?.valueText ?? selectedDefinition.options?.[0] ?? '')
    setAssignmentValues(existing?.values ?? [])
  }, [assignmentByDefinition, selectedDefinition])

  if (!canRead) return null

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite || !selectedDefinition) return
    setBusy('save')
    setError('')
    setMessage('')
    const existing = assignmentByDefinition.get(selectedDefinition.id)
    try {
      const saved = await requestJSON(
        `/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordId)}/assignments/${encodeURIComponent(selectedDefinition.id)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify({
            definitionId: selectedDefinition.id,
            recordType,
            recordId,
            valueText: selectedDefinition.valueKind === 'multiselect' ? '' : assignmentText.trim(),
            values: selectedDefinition.valueKind === 'multiselect' ? assignmentValues : undefined,
            revision: existing?.revision ?? 0,
          }),
        },
      )
      if (!isTagAssignment(saved)) throw new Error('invalid tag assignment response')
      setAssignments((current) => [...current.filter((item) => item.definitionId !== saved.definitionId), saved])
      setMessage(`Connected “${selectedDefinition.name}” to ${recordName}.`)
    } catch (saveError) {
      setError(saveError instanceof ApiRequestError ? saveError.message : 'The tag could not be connected.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  async function handleRemove(assignment: TagAssignment) {
    if (!canWrite) return
    const definition = definitions.find((item) => item.id === assignment.definitionId)
    setBusy(`remove-${assignment.definitionId}`)
    setError('')
    setMessage('')
    try {
      await requestJSON(
        `/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordId)}/assignments/${encodeURIComponent(assignment.definitionId)}?revision=${assignment.revision}`,
        { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } },
      )
      setAssignments((current) => current.filter((item) => item.definitionId !== assignment.definitionId))
      setMessage(`Removed “${definition?.name ?? assignment.definitionId}” from ${recordName}.`)
    } catch (removeError) {
      setError(removeError instanceof ApiRequestError ? removeError.message : 'The tag could not be removed.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-labelledby={headingID} className="mt-6 border-t border-steward-ink-800 pt-5" data-feature="identity.labels" data-requirement="REQ-LABELS-001">
      <h4 className="font-semibold" id={headingID}>Tags</h4>
      <p className="mt-1 text-sm text-steward-mist-muted">Connect configured tags to {recordName}. Create or edit tag definitions in Tags.</p>
      {error && <div className="mt-3 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-3 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
      {assignments.length === 0 ? (
        <p className={`${emptyStateClass} mt-3 px-4 py-5`}>No tags are connected to this {recordType === 'atlas.model' ? 'model' : 'asset'}.</p>
      ) : (
        <ul aria-label="Connected tags" className="mt-3 space-y-2">
          {assignments.map((assignment) => {
            const definition = definitions.find((item) => item.id === assignment.definitionId)
            return (
              <li className={`${subpanelClass} flex flex-wrap items-center justify-between gap-3 px-3 py-2`} key={assignment.definitionId}>
                <span className="min-w-0 break-words text-sm">{assignmentDisplay(assignment, definition)}</span>
                <div className="flex flex-wrap items-center gap-2">
                  {definition && <StatusBadge tone="info">{definition.valueKind}</StatusBadge>}
                  {canWrite && <button className={plainButtonClass} disabled={busy === `remove-${assignment.definitionId}`} onClick={() => void handleRemove(assignment)} type="button">{busy === `remove-${assignment.definitionId}` ? 'Removing…' : 'Remove'}</button>}
                </div>
              </li>
            )
          })}
        </ul>
      )}
      {canWrite && applicableDefinitions.length === 0 && (
        <p className="mt-3 text-sm text-steward-mist-muted">No active tags apply to {recordType}. Configure one in Tags and include this record type.</p>
      )}
      {canWrite && selectedDefinition && (
        <form aria-label={`Connect a tag to ${recordName}`} className={`${subpanelClass} mt-4 grid gap-4 p-4`} onSubmit={handleSave}>
          <label className={labelClass}>Tag
            <select className={inputClass} onChange={(event) => setDefinitionID(event.target.value)} value={selectedDefinition.id}>
              {applicableDefinitions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}
            </select>
          </label>
          {selectedDefinition.valueKind === 'flag' && <p className="text-sm text-steward-mist-muted">This tag is a simple connection with no separate value.</p>}
          {selectedDefinition.valueKind === 'text' && <label className={labelClass}>Value<input className={inputClass} maxLength={500} onChange={(event) => setAssignmentText(event.target.value)} required value={assignmentText} /></label>}
          {selectedDefinition.valueKind === 'select' && <label className={labelClass}>Value<select className={inputClass} onChange={(event) => setAssignmentText(event.target.value)} required value={assignmentText}>{(selectedDefinition.options ?? []).map((option) => <option key={option}>{option}</option>)}</select></label>}
          {selectedDefinition.valueKind === 'multiselect' && (
            <fieldset className="grid gap-2">
              <legend className={labelClass}>Values</legend>
              {(selectedDefinition.options ?? []).map((option) => (
                <label className="flex min-h-11 items-center gap-2 text-sm" key={option}>
                  <input checked={assignmentValues.includes(option)} className="size-4 accent-steward-teal" onChange={(event) => {
                    setAssignmentValues((current) => event.target.checked ? [...current, option].sort() : current.filter((item) => item !== option))
                  }} type="checkbox" />
                  {option}
                </label>
              ))}
            </fieldset>
          )}
          <button className={buttonClass} disabled={busy === 'save' || (selectedDefinition.valueKind === 'multiselect' && assignmentValues.length === 0)} type="submit">{busy === 'save' ? 'Saving…' : assignmentByDefinition.has(selectedDefinition.id) ? 'Update tag' : 'Connect tag'}</button>
        </form>
      )}
    </section>
  )
}
