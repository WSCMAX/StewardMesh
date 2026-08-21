import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import type { Asset } from './AtlasInventory'
import { ProductHeader, buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-LABELS-001, REQ-THREADS-001. Features: identity.labels, goals.tags.

type ValueKind = 'flag' | 'text' | 'select' | 'multiselect'

type TagDefinition = {
  id: string
  organizationId: string
  name: string
  description?: string
  valueKind: ValueKind
  applicableRecordTypes: string[]
  options?: string[]
  parentId?: string
  goalId?: string
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

type Goal = {
  id: string
  organizationId: string
  name: string
  description?: string
  parentId?: string
  revision: Revision
}

type GoalLink = {
  goalId: string
  targetType: 'asset' | 'purchase'
  targetId: string
}

type DefinitionRef = {
  id: string
  name: string
}

type AffectedAssignment = {
  definitionId: string
  definitionName: string
  recordType: string
  recordId: string
}

type DeleteDefinitionImpact = {
  definitionsRemoved: DefinitionRef[]
  assignmentsRemoved: AffectedAssignment[]
}

type DeleteDefinitionPreview = {
  definition: DefinitionRef
  childDefinitions: DefinitionRef[]
  hasChildren: boolean
  orphanChildrenOption: DeleteDefinitionImpact
  cascadeChildrenOption: DeleteDefinitionImpact
}

type DeleteDefinitionMode = 'strict' | 'orphan_children' | 'cascade_children'

type TagsManagerProps = {
  assets: readonly Asset[]
  csrfToken: string
  permissions: readonly string[]
  roles?: readonly string[]
  onOpenHelp?: () => void
}

const valueKinds: ValueKind[] = ['flag', 'text', 'select', 'multiselect']

const recordTypeOptions = [
  'people.identity', 'people.department', 'people.site', 'people.building', 'people.room', 'people.assignment',
  'atlas.asset', 'atlas.model', 'stack.product', 'stack.assignment', 'ledger.vendor', 'ledger.purchase-order',
  'horizon.plan', 'vault.blob',
]

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
    && (item.parentId === undefined || typeof item.parentId === 'string')
    && (item.goalId === undefined || typeof item.goalId === 'string')
}

function isDescendant(definitions: readonly TagDefinition[], ancestorID: string, candidateID: string): boolean {
  let current = definitions.find((item) => item.id === candidateID)
  while (current?.parentId) {
    if (current.parentId === ancestorID) return true
    current = definitions.find((item) => item.id === current!.parentId)
  }
  return false
}

function definitionTree(definitions: readonly TagDefinition[]): { definition: TagDefinition; depth: number }[] {
  const byParent = new Map<string | undefined, TagDefinition[]>()
  for (const definition of definitions) {
    const key = definition.parentId || undefined
    const group = byParent.get(key) ?? []
    group.push(definition)
    byParent.set(key, group)
  }
  for (const group of byParent.values()) {
    group.sort((left, right) => left.name.localeCompare(right.name))
  }
  const result: { definition: TagDefinition; depth: number }[] = []
  function walk(parentID: string | undefined, depth: number) {
    for (const definition of byParent.get(parentID) ?? []) {
      result.push({ definition, depth })
      walk(definition.id, depth + 1)
    }
  }
  walk(undefined, 0)
  const listed = new Set(result.map((item) => item.definition.id))
  for (const definition of definitions) {
    if (!listed.has(definition.id)) result.push({ definition, depth: 0 })
  }
  return result
}

function isTagAssignment(value: unknown): value is TagAssignment {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.definitionId === 'string' && typeof item.recordType === 'string' && typeof item.recordId === 'string' && isRevision(item.revision)
}

function isGoal(value: unknown): value is Goal {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string' && typeof item.name === 'string'
    && isRevision(item.revision) && (item.parentId === undefined || typeof item.parentId === 'string')
}

function isGoalLink(value: unknown): value is GoalLink {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.goalId === 'string' && ['asset', 'purchase'].includes(String(item.targetType)) && typeof item.targetId === 'string'
}

function isDefinitionRef(value: unknown): value is DefinitionRef {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.name === 'string'
}

function isDeleteDefinitionImpact(value: unknown): value is DeleteDefinitionImpact {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return Array.isArray(item.definitionsRemoved) && item.definitionsRemoved.every(isDefinitionRef)
    && Array.isArray(item.assignmentsRemoved) && item.assignmentsRemoved.every((entry) => {
      if (typeof entry !== 'object' || entry === null) return false
      const assignment = entry as Record<string, unknown>
      return typeof assignment.definitionId === 'string' && typeof assignment.definitionName === 'string'
        && typeof assignment.recordType === 'string' && typeof assignment.recordId === 'string'
    })
}

function isDeleteDefinitionPreview(value: unknown): value is DeleteDefinitionPreview {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return isDefinitionRef(item.definition) && Array.isArray(item.childDefinitions) && item.childDefinitions.every(isDefinitionRef)
    && typeof item.hasChildren === 'boolean' && isDeleteDefinitionImpact(item.orphanChildrenOption)
    && isDeleteDefinitionImpact(item.cascadeChildrenOption)
}

function assignmentDisplay(assignment: TagAssignment, definition?: TagDefinition): string {
  const value = assignment.values?.length ? assignment.values.join(', ') : assignment.valueText
  const name = definition?.name ?? assignment.definitionId
  return value ? `${name}: ${value}` : name
}

export default function TagsManager({ assets, csrfToken, permissions, roles = [], onOpenHelp }: TagsManagerProps) {
  const [definitions, setDefinitions] = useState<TagDefinition[]>([])
  const [goals, setGoals] = useState<Goal[]>([])
  const [selectedDefinitionID, setSelectedDefinitionID] = useState('')
  const [recordType, setRecordType] = useState('atlas.asset')
  const [recordID, setRecordID] = useState('')
  const [assignments, setAssignments] = useState<TagAssignment[]>([])
  const [goalLinks, setGoalLinks] = useState<GoalLink[]>([])
  const [draftName, setDraftName] = useState('')
  const [draftDescription, setDraftDescription] = useState('')
  const [draftValueKind, setDraftValueKind] = useState<ValueKind>('flag')
  const [draftRecordTypes, setDraftRecordTypes] = useState<string[]>(['atlas.asset', 'people.identity'])
  const [draftOptions, setDraftOptions] = useState('')
  const [draftParentID, setDraftParentID] = useState('')
  const [draftGoalID, setDraftGoalID] = useState('')
  const [assignmentText, setAssignmentText] = useState('')
  const [assignmentValues, setAssignmentValues] = useState<string[]>([])
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [deletePreview, setDeletePreview] = useState<DeleteDefinitionPreview | null>(null)
  const [deleteMode, setDeleteMode] = useState<DeleteDefinitionMode>('strict')
  const [deleteConfirmed, setDeleteConfirmed] = useState(false)
  const errorRef = useRef<HTMLDivElement>(null)

  const hasAdministratorRole = roles.some((role) => role.toLowerCase() === 'administrator')
  const canReadTags = permissions.includes('labels.read') || hasAdministratorRole
  const canWriteTags = permissions.includes('labels.write') || hasAdministratorRole
  const canReadGoals = permissions.includes('goals.read')
  const canWriteGoals = permissions.includes('goals.write')
  const canRead = canReadTags || canReadGoals

  const selectedDefinition = useMemo(
    () => definitions.find((item) => item.id === selectedDefinitionID) ?? null,
    [definitions, selectedDefinitionID],
  )
  const applicableDefinitions = useMemo(
    () => definitions.filter((item) => item.status === 'active' && item.applicableRecordTypes.includes(recordType)),
    [definitions, recordType],
  )
  const assignmentByDefinition = useMemo(
    () => new Map(assignments.map((item) => [item.definitionId, item])),
    [assignments],
  )
  const goalByID = useMemo(() => new Map(goals.map((goal) => [goal.id, goal])), [goals])
  const definitionTreeItems = useMemo(() => definitionTree(definitions), [definitions])
  const parentOptions = useMemo(() => {
    if (!selectedDefinition) return definitions
    return definitions.filter((item) => item.id !== selectedDefinition.id && !isDescendant(definitions, selectedDefinition.id, item.id))
  }, [definitions, selectedDefinition])
  const selectedAsset = useMemo(() => assets.find((asset) => asset.id === recordID) ?? null, [assets, recordID])
  const deleteImpact = useMemo(() => {
    if (!deletePreview) return null
    if (deletePreview.hasChildren) {
      return deleteMode === 'cascade_children' ? deletePreview.cascadeChildrenOption : deletePreview.orphanChildrenOption
    }
    return deletePreview.orphanChildrenOption
  }, [deleteMode, deletePreview])

  useEffect(() => {
    if (!canReadTags) return
    let active = true
    requestJSON('/api/v1/labels/definitions')
      .then((response) => {
        const next = items(response)
        if (!next.every(isTagDefinition)) throw new Error('invalid tag definitions response')
        if (active) setDefinitions(next)
      })
      .catch((loadError) => {
        if (active) {
          setError(loadError instanceof ApiRequestError ? loadError.message : 'Tag definitions could not be loaded.')
        }
      })
    return () => { active = false }
  }, [canReadTags])

  useEffect(() => {
    if (!canReadGoals) return
    let active = true
    requestJSON('/api/v1/goals')
      .then((response) => {
        const next = items(response)
        if (!next.every(isGoal)) throw new Error('invalid goals response')
        if (active) setGoals(next)
      })
      .catch(() => {
        if (active) setError('Goals could not be loaded.')
      })
    return () => { active = false }
  }, [canReadGoals])

  useEffect(() => {
    if (!recordID && assets.length > 0 && recordType === 'atlas.asset') setRecordID(assets[0].id)
    if (recordID && recordType === 'atlas.asset' && !assets.some((asset) => asset.id === recordID)) {
      setRecordID(assets[0]?.id ?? '')
    }
  }, [assets, recordID, recordType])

  useEffect(() => {
    if (!canReadTags || recordType.trim() === '' || recordID.trim() === '') {
      setAssignments([])
      return
    }
    let active = true
    requestJSON(`/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordID)}/assignments`)
      .then((response) => {
        const next = items(response)
        if (!next.every(isTagAssignment)) throw new Error('invalid tag assignments response')
        if (active) setAssignments(next)
      })
      .catch(() => {
        if (active) setAssignments([])
      })
    return () => { active = false }
  }, [canReadTags, recordID, recordType])

  useEffect(() => {
    if (!canReadGoals || recordType !== 'atlas.asset' || recordID.trim() === '') {
      setGoalLinks([])
      return
    }
    let active = true
    requestJSON(`/api/v1/threads/asset/${encodeURIComponent(recordID)}/goals`)
      .then((response) => {
        const next = items(response)
        if (!next.every(isGoalLink)) throw new Error('invalid goal links response')
        if (active) setGoalLinks(next)
      })
      .catch(() => {
        if (active) setGoalLinks([])
      })
    return () => { active = false }
  }, [canReadGoals, recordID, recordType])

  useEffect(() => {
    if (!selectedDefinition) {
      setAssignmentText('')
      setAssignmentValues([])
      return
    }
    const existing = assignmentByDefinition.get(selectedDefinition.id)
    setAssignmentText(existing?.valueText ?? '')
    setAssignmentValues(existing?.values ?? [])
  }, [assignmentByDefinition, selectedDefinition])

  useEffect(() => {
    if (applicableDefinitions.length === 0) {
      setSelectedDefinitionID('')
      return
    }
    if (!applicableDefinitions.some((item) => item.id === selectedDefinitionID)) {
      setSelectedDefinitionID(applicableDefinitions[0].id)
    }
  }, [applicableDefinitions, selectedDefinitionID])

  function loadDefinitionIntoDraft(definition: TagDefinition) {
    setSelectedDefinitionID(definition.id)
    setDraftName(definition.name)
    setDraftDescription(definition.description ?? '')
    setDraftValueKind(definition.valueKind)
    setDraftRecordTypes(definition.applicableRecordTypes)
    setDraftOptions(definition.options?.join(', ') ?? '')
    setDraftParentID(definition.parentId ?? '')
    setDraftGoalID(definition.goalId ?? '')
  }

  async function handleCreateDefinition(event: FormEvent) {
    event.preventDefault()
    if (!canWriteTags) return
    setBusy('create-definition')
    setError('')
    setMessage('')
    try {
      const created = await requestJSON('/api/v1/labels/definitions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: draftName.trim(),
          description: draftDescription.trim(),
          valueKind: draftValueKind,
          applicableRecordTypes: draftRecordTypes,
          options: draftOptions.split(',').map((option) => option.trim()).filter(Boolean),
          parentId: draftParentID.trim(),
          goalId: draftGoalID.trim(),
        }),
      })
      if (!isTagDefinition(created)) throw new Error('invalid tag definition response')
      setDefinitions((current) => [...current, created].sort((left, right) => left.name.localeCompare(right.name)))
      loadDefinitionIntoDraft(created)
      setMessage(`Created tag “${created.name}”.`)
    } catch (createError) {
      setError(createError instanceof ApiRequestError ? createError.message : 'Tag definition could not be created.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function handleUpdateDefinition(event: FormEvent) {
    event.preventDefault()
    if (!canWriteTags || !selectedDefinition) return
    setBusy('update-definition')
    setError('')
    setMessage('')
    try {
      const updated = await requestJSON(`/api/v1/labels/definitions/${encodeURIComponent(selectedDefinition.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: draftName.trim(),
          description: draftDescription.trim(),
          valueKind: draftValueKind,
          applicableRecordTypes: draftRecordTypes,
          options: draftOptions.split(',').map((option) => option.trim()).filter(Boolean),
          parentId: draftParentID.trim(),
          goalId: draftGoalID.trim(),
          status: selectedDefinition.status,
          revision: selectedDefinition.revision,
        }),
      })
      if (!isTagDefinition(updated)) throw new Error('invalid tag definition response')
      setDefinitions((current) => current.map((item) => item.id === updated.id ? updated : item))
      loadDefinitionIntoDraft(updated)
      setMessage(`Updated tag “${updated.name}”.`)
    } catch (updateError) {
      setError(updateError instanceof ApiRequestError ? updateError.message : 'Tag definition could not be updated.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function openDeletePreview() {
    if (!canWriteTags || !selectedDefinition) return
    setBusy('delete-preview')
    setError('')
    setMessage('')
    setDeleteConfirmed(false)
    try {
      const preview = await requestJSON(`/api/v1/labels/definitions/${encodeURIComponent(selectedDefinition.id)}/delete-preview`)
      if (!isDeleteDefinitionPreview(preview)) throw new Error('invalid delete preview response')
      setDeletePreview(preview)
      setDeleteMode(preview.hasChildren ? 'orphan_children' : 'strict')
    } catch (previewError) {
      setError(previewError instanceof ApiRequestError ? previewError.message : 'Tag delete preview could not be loaded.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function handleDeleteDefinition() {
    if (!canWriteTags || !selectedDefinition || !deletePreview || !deleteConfirmed) return
    setBusy('delete-definition')
    setError('')
    setMessage('')
    try {
      await requestJSON(`/api/v1/labels/definitions/${encodeURIComponent(selectedDefinition.id)}`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          revision: selectedDefinition.revision,
          mode: deletePreview.hasChildren ? deleteMode : 'strict',
          confirm: true,
        }),
      })
      setDefinitions((current) => {
        const removed = new Set(deleteImpact?.definitionsRemoved.map((entry) => entry.id) ?? [])
        return current
          .filter((item) => !removed.has(item.id))
          .map((item) => item.parentId === selectedDefinition.id ? { ...item, parentId: undefined } : item)
      })
      if (canReadTags && recordType.trim() !== '' && recordID.trim() !== '') {
        const response = await requestJSON(`/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordID)}/assignments`)
        const next = items(response)
        if (next.every(isTagAssignment)) setAssignments(next)
        else setAssignments([])
      }
      setSelectedDefinitionID('')
      setDraftName('')
      setDraftDescription('')
      setDraftValueKind('flag')
      setDraftRecordTypes(['atlas.asset', 'people.identity'])
      setDraftOptions('')
      setDraftParentID('')
      setDraftGoalID('')
      setDeletePreview(null)
      setDeleteConfirmed(false)
      setMessage(`Deleted tag “${deletePreview.definition.name}”.`)
    } catch (deleteError) {
      setError(deleteError instanceof ApiRequestError ? deleteError.message : 'Tag could not be deleted.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function handleSaveAssignment(event: FormEvent) {
    event.preventDefault()
    if (!canWriteTags || !selectedDefinition || recordID.trim() === '') return
    setBusy('save-assignment')
    setError('')
    setMessage('')
    const existing = assignmentByDefinition.get(selectedDefinition.id)
    try {
      const saved = await requestJSON(
        `/api/v1/labels/records/${encodeURIComponent(recordType)}/${encodeURIComponent(recordID)}/assignments/${encodeURIComponent(selectedDefinition.id)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify({
            definitionId: selectedDefinition.id,
            recordType,
            recordId: recordID.trim(),
            valueText: selectedDefinition.valueKind === 'multiselect' ? '' : assignmentText.trim(),
            values: selectedDefinition.valueKind === 'multiselect' ? assignmentValues : undefined,
            revision: existing?.revision ?? 0,
          }),
        },
      )
      if (!isTagAssignment(saved)) throw new Error('invalid tag assignment response')
      setAssignments((current) => {
        const next = current.filter((item) => item.definitionId !== saved.definitionId)
        return [...next, saved]
      })
      setMessage(`Connected “${selectedDefinition.name}” to this record.`)
    } catch (saveError) {
      setError(saveError instanceof ApiRequestError ? saveError.message : 'Tag could not be connected.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function createGoal(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWriteGoals) return
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('goal-create')
    setError('')
    setMessage('')
    try {
      await requestJSON('/api/v1/goals', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('goalName') ?? ''),
          description: String(values.get('goalDescription') ?? ''),
          parentId: String(values.get('goalParent') ?? ''),
        }),
      })
      const response = await requestJSON('/api/v1/goals')
      const next = items(response)
      if (!next.every(isGoal)) throw new Error('invalid goals response')
      setGoals(next)
      form.reset()
      setMessage('Goal created.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be created.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function linkGoal(goalID: string) {
    if (!canWriteGoals || recordType !== 'atlas.asset' || !recordID) return
    setBusy('goal-link')
    setError('')
    setMessage('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(recordID)}/goals/${encodeURIComponent(goalID)}`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken },
      })
      const response = await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(recordID)}/goals`)
      const next = items(response)
      if (!next.every(isGoalLink)) throw new Error('invalid goal links response')
      setGoalLinks(next)
      setMessage('Goal linked to this asset.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be linked.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  async function unlinkGoal(goalID: string) {
    if (!canWriteGoals || recordType !== 'atlas.asset' || !recordID) return
    setBusy(`goal-${goalID}`)
    setError('')
    setMessage('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(recordID)}/goals/${encodeURIComponent(goalID)}`, {
        method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken },
      })
      setGoalLinks((current) => current.filter((link) => link.goalId !== goalID))
      setMessage('Goal unlinked from this asset.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be unlinked.')
      errorRef.current?.focus()
    } finally {
      setBusy('')
    }
  }

  if (!canRead) {
    return (
      <section className={panelClass}>
        <h2 className="text-lg font-semibold text-white">Tags</h2>
        <p className="mt-2 text-sm text-steward-mist-muted">Tags or goals read permission is required.</p>
      </section>
    )
  }

  return (
    <section aria-labelledby="tags-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="identity.labels goals.tags" data-requirement="REQ-LABELS-001 REQ-THREADS-001">
      <ProductHeader
        actions={onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Tags help</button> : undefined}
        description="Define configurable tags once, then connect them to people, assets, licenses, and anything else in scope. Each tag can be a simple flag, free text, a single choice, or a multi-select from options you configure. Strategic goals remain available for longer-lived asset and purchase relationships."
        headingId="tags-heading"
        kicker="Connect and classify records"
        title="Tags"
      />

      {error && <div className="mt-4 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-4 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
      {!canReadTags && canReadGoals && (
        <p className="mt-4 rounded-lg border border-amber-400/40 bg-amber-950/30 p-3 text-sm text-amber-100">
          Tag configuration is hidden because your session does not include <code className="text-amber-50">labels.read</code>.
          Refresh the page after the server update, or sign out and back in if Configure tags still does not appear.
        </p>
      )}

      {canReadTags && <div className="mt-6 grid gap-6 xl:grid-cols-2">
        <div className={`${subpanelClass} p-5`}>
          <h3 className="text-lg font-semibold">Configure tags</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">Set what each tag means, which record types it applies to, and allowed values.</p>
          {definitions.length > 0 && (
            <ul className="mt-4 grid gap-2">{definitionTreeItems.map(({ definition, depth }) => {
              const parent = definition.parentId ? definitions.find((item) => item.id === definition.parentId) : undefined
              const goal = definition.goalId ? goalByID.get(definition.goalId) : undefined
              return (
              <li key={definition.id} style={{ marginLeft: `${depth * 1.25}rem` }}>
                <button className={`${secondaryButtonClass} w-full justify-start text-left`} onClick={() => loadDefinitionIntoDraft(definition)} type="button">
                  <span className="font-medium">{definition.name}</span>
                  <span className="ml-2 text-steward-mist-muted">({definition.valueKind})</span>
                  {parent && <span className="mt-1 block text-xs text-steward-mist-muted">Under {parent.name}</span>}
                  {goal && <span className="mt-1 block text-xs text-steward-mist-muted">Goal: {goal.name}</span>}
                  <span className="mt-1 block text-xs text-steward-mist-muted">{definition.applicableRecordTypes.join(', ')}</span>
                </button>
              </li>
            )})}</ul>
          )}
          {canWriteTags && (
            <form className="mt-4 grid gap-4" onSubmit={selectedDefinition ? handleUpdateDefinition : handleCreateDefinition}>
              <div><label className={labelClass} htmlFor="tag-name">Name</label><input className={inputClass} id="tag-name" maxLength={100} onChange={(event) => setDraftName(event.target.value)} required value={draftName} /></div>
              <div><label className={labelClass} htmlFor="tag-description">Description</label><input className={inputClass} id="tag-description" maxLength={500} onChange={(event) => setDraftDescription(event.target.value)} value={draftDescription} /></div>
              <div><label className={labelClass} htmlFor="tag-value-kind">Value kind</label><select className={inputClass} id="tag-value-kind" onChange={(event) => setDraftValueKind(event.target.value as ValueKind)} value={draftValueKind}>{valueKinds.map((kind) => <option key={kind}>{kind}</option>)}</select></div>
              <fieldset className="grid gap-2"><legend className={labelClass}>Connects to record types</legend>{recordTypeOptions.map((option) => (
                <label className="flex items-center gap-2 text-sm" key={option}>
                  <input checked={draftRecordTypes.includes(option)} className="size-4 accent-steward-teal" onChange={(event) => {
                    setDraftRecordTypes((current) => event.target.checked ? [...current, option].sort() : current.filter((item) => item !== option))
                  }} type="checkbox" />
                  {option}
                </label>
              ))}</fieldset>
              {(draftValueKind === 'select' || draftValueKind === 'multiselect') && (
                <div><label className={labelClass} htmlFor="tag-options">Allowed values, comma separated</label><input className={inputClass} id="tag-options" onChange={(event) => setDraftOptions(event.target.value)} required value={draftOptions} /></div>
              )}
              <div>
                <label className={labelClass} htmlFor="tag-parent">Parent tag (optional)</label>
                <select className={inputClass} id="tag-parent" onChange={(event) => setDraftParentID(event.target.value)} value={draftParentID}>
                  <option value="">No parent</option>
                  {parentOptions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}
                </select>
              </div>
              {canReadGoals && (
                <div>
                  <label className={labelClass} htmlFor="tag-goal">Strategic goal (optional)</label>
                  <select className={inputClass} id="tag-goal" onChange={(event) => setDraftGoalID(event.target.value)} value={draftGoalID}>
                    <option value="">No goal</option>
                    {goals.map((goal) => <option key={goal.id} value={goal.id}>{goal.name}</option>)}
                  </select>
                </div>
              )}
              <div className="flex flex-wrap gap-3">
                <button className={buttonClass} disabled={busy !== ''} type="submit">{selectedDefinition ? 'Update tag' : 'Create tag'}</button>
                {selectedDefinition && (
                  <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => { void openDeletePreview() }} type="button">Delete tag</button>
                )}
              </div>
            </form>
          )}
          {deletePreview && selectedDefinition && (
            <div aria-labelledby="delete-tag-heading" className="mt-4 rounded-lg border border-amber-400/40 bg-amber-950/30 p-4" role="dialog">
              <h4 className="text-base font-semibold text-amber-100" id="delete-tag-heading">Delete “{deletePreview.definition.name}”?</h4>
              {deletePreview.hasChildren && (
                <div className="mt-4 grid gap-3">
                  <p className="text-sm text-steward-mist-muted">This tag has child tags. Choose what happens to them:</p>
                  <label className="flex items-start gap-2 text-sm">
                    <input checked={deleteMode === 'orphan_children'} className="mt-1 size-4 accent-steward-teal" name="delete-mode" onChange={() => setDeleteMode('orphan_children')} type="radio" />
                    <span><strong>Keep child tags</strong> — remove only this tag and clear its parent link from {deletePreview.childDefinitions.length} child tag{deletePreview.childDefinitions.length === 1 ? '' : 's'}.</span>
                  </label>
                  <label className="flex items-start gap-2 text-sm">
                    <input checked={deleteMode === 'cascade_children'} className="mt-1 size-4 accent-steward-teal" name="delete-mode" onChange={() => setDeleteMode('cascade_children')} type="radio" />
                    <span><strong>Delete child tags too</strong> — remove this tag and {deletePreview.childDefinitions.map((child) => child.name).join(', ')}.</span>
                  </label>
                </div>
              )}
              {deleteImpact && deleteImpact.assignmentsRemoved.length > 0 ? (
                <div className="mt-4">
                  <p className="text-sm font-semibold text-amber-100">These tag connections will be removed from records:</p>
                  <ul className="mt-2 max-h-40 space-y-1 overflow-y-auto text-sm text-steward-mist-muted">
                    {deleteImpact.assignmentsRemoved.map((assignment) => (
                      <li key={`${assignment.definitionId}:${assignment.recordType}:${assignment.recordId}`}>
                        {assignment.definitionName} on {assignment.recordType} / {assignment.recordId}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : (
                <p className="mt-4 text-sm text-steward-mist-muted">No records currently have the tags that will be removed.</p>
              )}
              <label className="mt-4 flex items-start gap-2 text-sm">
                <input checked={deleteConfirmed} className="mt-1 size-4 accent-steward-teal" onChange={(event) => setDeleteConfirmed(event.target.checked)} type="checkbox" />
                <span>I understand these tags will be removed from the listed records.</span>
              </label>
              <div className="mt-4 flex flex-wrap gap-3">
                <button className={buttonClass} disabled={busy !== '' || !deleteConfirmed} onClick={() => { void handleDeleteDefinition() }} type="button">
                  {busy === 'delete-definition' ? 'Deleting…' : 'Confirm delete'}
                </button>
                <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => { setDeletePreview(null); setDeleteConfirmed(false) }} type="button">Cancel</button>
              </div>
            </div>
          )}
        </div>

        <div className={`${subpanelClass} p-5`}>
          <h3 className="text-lg font-semibold">Connect tags to records</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">Pick a record, choose a tag, and save the value that ties them together.</p>
          <div className="mt-4 grid gap-4">
            <div><label className={labelClass} htmlFor="tag-record-type">Record type</label><select className={inputClass} id="tag-record-type" onChange={(event) => setRecordType(event.target.value)} value={recordType}>{recordTypeOptions.map((option) => <option key={option}>{option}</option>)}</select></div>
            {recordType === 'atlas.asset' ? (
              <div><label className={labelClass} htmlFor="tag-record-asset">Asset</label><select className={inputClass} id="tag-record-asset" onChange={(event) => setRecordID(event.target.value)} value={recordID}><option value="">Select an asset</option>{assets.map((asset) => <option key={asset.id} value={asset.id}>{asset.name}</option>)}</select></div>
            ) : (
              <div><label className={labelClass} htmlFor="tag-record-id">Record ID</label><input className={inputClass} id="tag-record-id" onChange={(event) => setRecordID(event.target.value)} pattern="[A-Za-z0-9][A-Za-z0-9._:-]{0,127}" value={recordID} /></div>
            )}
            <div><label className={labelClass} htmlFor="tag-definition">Tag</label><select className={inputClass} disabled={applicableDefinitions.length === 0} id="tag-definition" onChange={(event) => setSelectedDefinitionID(event.target.value)} value={selectedDefinitionID}>{applicableDefinitions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}</select></div>
          </div>
          {selectedDefinition && canWriteTags && recordID.trim() !== '' && (
            <form className="mt-4 grid gap-4" onSubmit={handleSaveAssignment}>
              {selectedDefinition.valueKind === 'flag' && <p className="text-sm text-steward-mist-muted">This tag is a simple connection with no separate value.</p>}
              {selectedDefinition.valueKind === 'text' && <div><label className={labelClass} htmlFor="tag-value-text">Value</label><input className={inputClass} id="tag-value-text" maxLength={500} onChange={(event) => setAssignmentText(event.target.value)} required value={assignmentText} /></div>}
              {selectedDefinition.valueKind === 'select' && <div><label className={labelClass} htmlFor="tag-value-select">Value</label><select className={inputClass} id="tag-value-select" onChange={(event) => setAssignmentText(event.target.value)} required value={assignmentText}>{(selectedDefinition.options ?? []).map((option) => <option key={option}>{option}</option>)}</select></div>}
              {selectedDefinition.valueKind === 'multiselect' && <fieldset className="grid gap-2"><legend className={labelClass}>Values</legend>{(selectedDefinition.options ?? []).map((option) => (
                <label className="flex items-center gap-2 text-sm" key={option}>
                  <input checked={assignmentValues.includes(option)} className="size-4 accent-steward-teal" onChange={(event) => {
                    setAssignmentValues((current) => event.target.checked ? [...current, option].sort() : current.filter((item) => item !== option))
                  }} type="checkbox" />
                  {option}
                </label>
              ))}</fieldset>}
              <button className={buttonClass} disabled={busy !== '' || (selectedDefinition.valueKind === 'multiselect' && assignmentValues.length === 0)} type="submit">Connect tag</button>
            </form>
          )}
          {assignments.length > 0 && (
            <ul aria-label="Connected tags" className="mt-4 grid gap-2 text-sm">{assignments.map((assignment) => {
              const definition = definitions.find((item) => item.id === assignment.definitionId)
              return <li className="rounded-lg border border-white/10 px-3 py-2" key={assignment.definitionId}>{assignmentDisplay(assignment, definition)}</li>
            })}</ul>
          )}
          {recordType === 'atlas.asset' && selectedAsset && assignments.length === 0 && (
            <p className="mt-4 text-sm text-steward-mist-muted">No tags connected to {selectedAsset.name} yet.</p>
          )}
        </div>
      </div>}

      {canReadGoals && <div className="mt-6 grid gap-6 lg:grid-cols-2">
        {canWriteGoals && (
          <form aria-label="Create goal" className={`${subpanelClass} p-5`} onSubmit={createGoal}>
            <h3 className="text-lg font-semibold">Strategic goals</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Goals tie assets and purchases to longer-lived outcomes.</p>
            <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Goal name<input className={inputClass} maxLength={160} name="goalName" required /></label>
            <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Description (optional)<textarea className={inputClass} maxLength={2000} name="goalDescription" rows={2} /></label>
            <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Parent goal (optional)<select className={inputClass} name="goalParent"><option value="">No parent</option>{goals.map((goal) => <option key={goal.id} value={goal.id}>{goal.name}</option>)}</select></label>
            <button className={`${buttonClass} mt-4`} disabled={busy === 'goal-create'} type="submit">{busy === 'goal-create' ? 'Creating goal…' : 'Create goal'}</button>
          </form>
        )}

        <div className={`${subpanelClass} p-5`}>
          <h3 className="text-lg font-semibold">Goals on this asset</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">{recordType === 'atlas.asset' && recordID ? 'Manage goal links for the asset selected above.' : 'Select an atlas.asset above to link goals.'}</p>
          {canWriteGoals && recordType === 'atlas.asset' && recordID && (
            <GoalLinker busy={busy} goalLinks={goalLinks} goals={goals} onLink={linkGoal} />
          )}
          {goalLinks.length === 0 ? (
            <p className="mt-4 rounded-lg border border-dashed border-steward-ink-800 p-4 text-sm text-steward-mist-muted">No goals linked here.</p>
          ) : (
            <ul aria-label="Linked goals" className="mt-4 space-y-3">{goalLinks.map((link) => {
              const goal = goalByID.get(link.goalId)
              return (
                <li className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-steward-ink-800 bg-steward-ink-950/55 p-4" key={link.goalId}>
                  <span><strong>{goal?.name ?? link.goalId}</strong>{goal?.description && <span className="mt-1 block text-sm text-steward-mist-muted">{goal.description}</span>}</span>
                  {canWriteGoals && <button className={secondaryButtonClass} disabled={busy === `goal-${link.goalId}`} onClick={() => void unlinkGoal(link.goalId)} type="button">Unlink</button>}
                </li>
              )
            })}</ul>
          )}
        </div>
      </div>}
    </section>
  )
}

function GoalLinker({ goals, goalLinks, busy, onLink }: { goals: Goal[]; goalLinks: GoalLink[]; busy: string; onLink: (goalID: string) => Promise<void> }) {
  const [goalID, setGoalID] = useState('')
  const linked = new Set(goalLinks.map((link) => link.goalId))
  const available = goals.filter((goal) => !linked.has(goal.id))
  return (
    <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-end">
      <label className="flex-1 text-sm font-semibold text-steward-mist-muted">Goal
        <select className={inputClass} onChange={(event) => setGoalID(event.target.value)} value={goalID}>
          <option value="">Select a goal</option>
          {available.map((goal) => <option key={goal.id} value={goal.id}>{goal.name}</option>)}
        </select>
      </label>
      <button className={buttonClass} disabled={!goalID || busy === 'goal-link'} onClick={() => { void onLink(goalID); setGoalID('') }} type="button">Link goal</button>
    </div>
  )
}
