import { type FormEvent, useEffect, useMemo, useState } from 'react'
import type { Asset } from './AtlasInventory'
import { ApiRequestError, requestJSON } from './api'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import { buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, subpanelClass } from './ui'
import {
  isAssignmentOverlap,
  isBulkCheckout,
  isCheckoutCandidate,
  isCheckoutGroup,
  overlapFromError,
  type AssignmentOverlap,
  type CheckoutCandidate,
  type CheckoutGroup,
} from './peopleCheckout'
import { loadLabelDefinitionsFor, type LabelDefinition } from './labelsGrid'

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

type CheckoutDeskProps = {
  assets: readonly Asset[]
  identities: readonly { id: string; displayName: string; status?: string }[]
  csrfToken: string
  canWrite: boolean
}

function readItems<T>(value: unknown, validator: (item: unknown) => item is T): T[] {
  if (typeof value !== 'object' || value === null) throw new Error('invalid collection response')
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items) || !items.every(validator)) throw new Error('invalid collection response')
  return items
}

function todayInputDate() {
  return new Date().toISOString().slice(0, 10)
}

function localDateToISO(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return `${trimmed}T00:00:00Z`
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString()
}

function overlapSummary(overlaps: AssignmentOverlap[]) {
  if (overlaps.length === 0) return 'Available'
  return overlaps.map((item) => {
    const when = `${formatDate(item.effectiveFrom)}${item.dueAt ? `–${formatDate(item.dueAt)}` : ''}`
    const event = item.eventSummary ? ` · ${item.eventSummary}` : ''
    return `${item.purpose === 'reservation' ? 'Reserved' : 'Checked out'} by ${item.assigneeLabel} ${when}${event}`
  }).join('; ')
}

export default function CheckoutDesk({ assets, identities, csrfToken, canWrite }: CheckoutDeskProps) {
  const [definitions, setDefinitions] = useState<LabelDefinition[]>([])
  const [groups, setGroups] = useState<CheckoutGroup[]>([])
  const [candidates, setCandidates] = useState<CheckoutCandidate[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [search, setSearch] = useState({ definitionId: '', value: '', from: todayInputDate(), to: '', quantity: '50' })
  const [preferredModelIds, setPreferredModelIds] = useState<string[]>([])
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [pendingPolicy, setPendingPolicy] = useState<{ kind: string; conflicts: AssignmentOverlap[]; retry: (policy: string) => Promise<void> } | null>(null)
  const [assignee, setAssignee] = useState<SearchableRecord[]>([])
  const [groupMembers, setGroupMembers] = useState<SearchableRecord[]>([])

  useEffect(() => {
    let cancelled = false
    Promise.all([
      loadLabelDefinitionsFor('atlas.asset'),
      requestJSON('/api/v1/people/checkout-groups'),
    ]).then(([loadedDefinitions, groupsBody]) => {
      if (cancelled) return
      setDefinitions(loadedDefinitions)
      setGroups(readItems(groupsBody, isCheckoutGroup))
    }).catch(() => {
      if (!cancelled) setError('Checkout pools could not be loaded.')
    })
    return () => { cancelled = true }
  }, [])

  const models = useMemo(() => {
    const seen = new Map<string, string>()
    for (const asset of assets) {
      if (!asset.modelId) continue
      const label = asset.modelContext
        ? `${asset.modelContext.manufacturer} ${asset.modelContext.name}`.trim()
        : asset.modelId
      if (!seen.has(asset.modelId)) seen.set(asset.modelId, label || asset.modelId)
    }
    return [...seen.entries()].map(([id, name]) => ({ id, name }))
  }, [assets])

  async function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('search')
    setError('')
    setStatus('')
    setPendingPolicy(null)
    try {
      const body = {
        definitionId: String(values.get('poolTag') ?? ''),
        value: String(values.get('poolValue') ?? ''),
        from: localDateToISO(String(values.get('from') ?? '')),
        to: localDateToISO(String(values.get('to') ?? '')),
        preferredModelIds,
        quantity: Number(String(values.get('quantity') ?? '0')) || 0,
      }
      const response = await requestJSON('/api/v1/people/checkout-availability', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      })
      const items = readItems(response, isCheckoutCandidate)
      setCandidates(items)
      const take = Number(String(values.get('quantity') ?? '0')) || items.length
      setSelected(items.slice(0, Math.max(0, take)).map((item) => item.assetId))
      setSearch({
        definitionId: String(values.get('poolTag') ?? ''),
        value: String(values.get('poolValue') ?? ''),
        from: String(values.get('from') ?? ''),
        to: String(values.get('to') ?? ''),
        quantity: String(values.get('quantity') ?? '50'),
      })
      setStatus(`${items.length} tagged assets ranked for these dates.`)
    } catch (searchError) {
      setError(searchError instanceof Error ? searchError.message : 'Availability could not be loaded.')
    } finally {
      setBusy('')
    }
  }

  async function submitCheckout(assetIds: string[], values: FormData, policy = '') {
    const purpose = String(values.get('purpose') ?? 'checkout')
    const groupId = String(values.get('groupId') ?? '').trim()
    const person = assignee[0]
    const assigneeKind = groupId ? 'group' : 'identity'
    const assigneeId = groupId || person?.id || ''
    const body: Record<string, unknown> = {
      assigneeKind,
      assigneeId,
      purpose,
      effectiveFrom: localDateToISO(search.from),
      dueAt: localDateToISO(search.to),
      eventSummary: String(values.get('eventSummary') ?? ''),
      assetIds,
      labelDefinitionId: search.definitionId,
      labelValue: search.value,
    }
    if (policy) body.conflictPolicy = policy
    try {
      const response = await requestJSON('/api/v1/people/bulk-checkouts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      })
      if (typeof response !== 'object' || response === null || !isBulkCheckout((response as Record<string, unknown>).bulkCheckout)) {
        throw new Error('invalid bulk checkout response')
      }
      setPendingPolicy(null)
      setStatus(purpose === 'reservation' ? 'Reservation created for the selected assets.' : 'Bulk checkout created for the selected assets.')
    } catch (mutationError) {
      const overlap = overlapFromError(mutationError)
      if (overlap) {
        setPendingPolicy({
          kind: overlap.kind,
          conflicts: overlap.conflicts.filter(isAssignmentOverlap),
          retry: (nextPolicy) => submitCheckout(assetIds, values, nextPolicy),
        })
        setError(overlap.message)
        return
      }
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The checkout could not be created.')
    }
  }

  async function handleBulk(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite || selected.length === 0) return
    setBusy('bulk')
    setError('')
    try {
      await submitCheckout(selected, new FormData(event.currentTarget))
    } finally {
      setBusy('')
    }
  }

  async function checkoutOne(assetId: string, form: HTMLFormElement) {
    if (!canWrite) return
    setBusy(`one-${assetId}`)
    setError('')
    try {
      await submitCheckout([assetId], new FormData(form))
    } finally {
      setBusy('')
    }
  }

  async function handleCreateGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite) return
    const values = new FormData(event.currentTarget)
    const memberIds = groupMembers.map((item) => item.id)
    setBusy('group')
    setError('')
    try {
      const created = await requestJSON('/api/v1/people/checkout-groups', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('groupName') ?? ''),
          description: String(values.get('groupDescription') ?? ''),
          memberIds,
        }),
      })
      if (!isCheckoutGroup(created)) throw new Error('invalid checkout group response')
      setGroups((current) => current.some((item) => item.id === created.id) ? current : [...current, created])
      event.currentTarget.reset()
      setStatus(`Created checkout group ${created.name}.`)
    } catch (mutationError) {
      setError(mutationError instanceof Error ? mutationError.message : 'The checkout group could not be created.')
    } finally {
      setBusy('')
    }
  }

  function toggleSelected(assetId: string) {
    setSelected((current) => current.includes(assetId) ? current.filter((item) => item !== assetId) : [...current, assetId])
  }

  return (
    <div className="mt-4 grid gap-6">
      {error && <div className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert">{error}</div>}
      <p className="sr-only" aria-live="polite" role="status">{status}</p>

      <section className={panelClass} aria-labelledby="checkout-desk-heading">
        <h2 className="text-lg font-semibold text-steward-mist" id="checkout-desk-heading">Tagged bulk checkout</h2>
        <p className="mt-2 text-sm text-steward-mist-muted">
          Use a Tags grouping on Atlas assets, then rank what is free for an event. Assets already reserved or checked out stay in the list with a lower rating so you can still pick them.
        </p>
        <form className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3" onSubmit={handleSearch}>
          <label className={labelClass}>
            Checkout tag
            <select className={inputClass} name="poolTag" required>
              <option value="">Select a tag</option>
              {definitions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}
            </select>
          </label>
          <label className={labelClass}>
            Tag value (optional)
            <input className={inputClass} name="poolValue" placeholder="Cart A" />
          </label>
          <label className={labelClass}>
            Quantity
            <input className={inputClass} defaultValue={search.quantity} min={1} name="quantity" type="number" />
          </label>
          <label className={labelClass}>
            From
            <input className={inputClass} defaultValue={todayInputDate()} name="from" required type="date" />
          </label>
          <label className={labelClass}>
            Through
            <input className={inputClass} name="to" required type="date" />
          </label>
          <label className={`${labelClass} sm:col-span-2 lg:col-span-3`}>
            Prefer models
            <select className={inputClass} multiple onChange={(event) => setPreferredModelIds([...event.currentTarget.selectedOptions].map((option) => option.value))} value={preferredModelIds}>
              {models.map((model) => <option key={model.id} value={model.id}>{model.name}</option>)}
            </select>
          </label>
          <div className="sm:col-span-2 lg:col-span-3">
            <button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'search' ? 'Ranking…' : 'Find available assets'}</button>
          </div>
        </form>
      </section>

      {candidates.length > 0 && (
        <form className={panelClass} onSubmit={handleBulk}>
          <h3 className="text-base font-semibold text-steward-mist">Ranked pool</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">Higher ratings are free preferred models. Future reservations lower the score but remain selectable.</p>
          <ul className="mt-4 space-y-2">
            {candidates.map((candidate) => (
              <li className={`${subpanelClass} flex flex-wrap items-start gap-3 p-3`} key={candidate.assetId}>
                <label className="flex items-start gap-2 text-sm text-steward-mist">
                  <input checked={selected.includes(candidate.assetId)} onChange={() => toggleSelected(candidate.assetId)} type="checkbox" />
                  <span>
                    <span className="font-medium">{candidate.name}</span>
                    {candidate.assetTag ? <span className="text-steward-mist-muted"> · {candidate.assetTag}</span> : null}
                    <span className="block text-xs text-steward-mist-muted">
                      Rating {candidate.rating}
                      {candidate.modelName ? ` · ${candidate.modelName}` : ''}
                      {` · ${overlapSummary(candidate.overlaps)}`}
                    </span>
                  </span>
                </label>
                {canWrite && (
                  <button className={secondaryButtonClass} disabled={busy !== ''} onClick={(event) => {
                    event.preventDefault()
                    void checkoutOne(candidate.assetId, event.currentTarget.form ?? event.currentTarget.closest('form') as HTMLFormElement)
                  }} type="button">
                    {busy === `one-${candidate.assetId}` ? 'Saving…' : 'Check out this asset'}
                  </button>
                )}
              </li>
            ))}
          </ul>
          {canWrite && (
            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              <label className={labelClass}>
                Purpose
                <select className={inputClass} defaultValue="checkout" name="purpose">
                  <option value="checkout">Checkout now</option>
                  <option value="reservation">Reserve for later</option>
                </select>
              </label>
              <div>
                <RecordSearchPicker
                  kind="identity"
                  label="Person"
                  multiple={false}
                  onChange={setAssignee}
                  options={identities.map((item) => ({ id: item.id, label: item.displayName }))}
                  selected={assignee}
                />
              </div>
              <label className={labelClass}>
                Or checkout group
                <select className={inputClass} defaultValue="" name="groupId">
                  <option value="">Use the selected person</option>
                  {groups.map((group) => (
                    <option key={group.id} value={group.id}>{group.name}</option>
                  ))}
                </select>
              </label>
              <label className={labelClass}>
                Event description
                <input className={inputClass} name="eventSummary" placeholder="Fall faculty conference" />
              </label>
              <div className="sm:col-span-2 flex flex-wrap gap-2">
                <button className={buttonClass} disabled={busy !== '' || selected.length === 0} type="submit">
                  {busy === 'bulk' ? 'Saving…' : `Check out ${selected.length} selected`}
                </button>
              </div>
            </div>
          )}
          {pendingPolicy && (
            <div className={`${subpanelClass} mt-4 p-3`} role="alertdialog">
              <p className="font-medium text-steward-mist">This period overlaps another checkout.</p>
              <ul className="mt-2 list-disc pl-5 text-sm text-steward-mist-muted">
                {pendingPolicy.conflicts.map((conflict) => (
                  <li key={conflict.assignmentId}>
                    {conflict.assigneeLabel}: {overlapSummary([conflict])}
                  </li>
                ))}
              </ul>
              <div className="mt-3 flex flex-wrap gap-2">
                {pendingPolicy.kind === 'checkout' ? (
                  <>
                    <button className={buttonClass} onClick={() => void pendingPolicy.retry('replace')} type="button">Replace current user</button>
                    <button className={secondaryButtonClass} onClick={() => void pendingPolicy.retry('group')} type="button">Make group checkout</button>
                  </>
                ) : (
                  <button className={buttonClass} onClick={() => void pendingPolicy.retry('proceed')} type="button">Continue anyway</button>
                )}
                <button className={secondaryButtonClass} onClick={() => setPendingPolicy(null)} type="button">Cancel</button>
              </div>
            </div>
          )}
        </form>
      )}

      {canWrite && (
        <section className={panelClass} aria-labelledby="checkout-groups-heading">
          <h2 className="text-lg font-semibold text-steward-mist" id="checkout-groups-heading">Checkout groups</h2>
          <p className="mt-2 text-sm text-steward-mist-muted">Shared checkouts keep both people as members of a group assigned to the asset.</p>
          {groups.length > 0 && (
            <ul className="mt-3 space-y-2">
              {groups.map((group) => (
                <li className={subpanelClass + ' p-3'} key={group.id}>
                  <p className="font-medium text-steward-mist">{group.name}</p>
                  <p className="text-sm text-steward-mist-muted">{group.memberIds.length} members{group.description ? ` · ${group.description}` : ''}</p>
                </li>
              ))}
            </ul>
          )}
          <form className="mt-4 grid gap-3 sm:grid-cols-2" onSubmit={handleCreateGroup}>
            <label className={labelClass}>
              Group name
              <input className={inputClass} name="groupName" required />
            </label>
            <label className={labelClass}>
              Description
              <input className={inputClass} name="groupDescription" />
            </label>
            <div className="sm:col-span-2">
              <RecordSearchPicker
                kind="identity"
                label="Members"
                multiple
                onChange={setGroupMembers}
                options={identities.map((item) => ({ id: item.id, label: item.displayName }))}
                selected={groupMembers}
              />
            </div>
            <div>
              <button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'group' ? 'Creating…' : 'Create group'}</button>
            </div>
          </form>
        </section>
      )}
    </div>
  )
}
