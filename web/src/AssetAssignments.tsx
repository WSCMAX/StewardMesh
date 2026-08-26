import { type FormEvent, useEffect, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import { overlapFromError } from './peopleCheckout'
import { buttonClass, inputClass, labelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-ATLAS-001, REQ-PEOPLE-001. Features: inventory.assets, identity.directory.

type AssignmentPurpose = 'checkout' | 'reservation'
type AssigneeKind = 'identity' | 'department' | 'group'
type AssignmentRole = 'primary' | 'user' | 'department'

export type PeopleAssetAssignment = {
  id: string
  assetId: string
  assigneeKind: AssigneeKind
  assigneeId: string
  assigneeLabel?: string
  role: AssignmentRole
  purpose?: AssignmentPurpose
  eventSummary?: string
  effectiveFrom: string
  dueAt?: string
  effectiveTo?: string
}

type AssetAssignmentsProps = {
  assetId: string
  assetName: string
  csrfToken: string
  canWrite: boolean
}

const roleLabels: Record<AssignmentRole, string> = {
  primary: 'Primary assignee',
  user: 'Additional user',
  department: 'Responsible department',
}

function isAssignment(value: unknown): value is PeopleAssetAssignment {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return typeof record.id === 'string' && typeof record.assetId === 'string'
    && typeof record.assigneeId === 'string' && typeof record.effectiveFrom === 'string'
    && (record.role === 'primary' || record.role === 'user' || record.role === 'department')
}

function readAssignments(value: unknown): PeopleAssetAssignment[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items)) return []
  return items.filter(isAssignment)
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString()
}

function assignmentIsActive(assignment: PeopleAssetAssignment) {
  return !assignment.effectiveTo
}

function assignmentIsOverdue(assignment: PeopleAssetAssignment) {
  if (!assignmentIsActive(assignment) || !assignment.dueAt) return false
  return new Date(assignment.dueAt).getTime() < Date.now()
}

function localDateToISO(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return trimmed.includes('T') ? new Date(trimmed).toISOString() : `${trimmed}T00:00:00Z`
}

export default function AssetAssignments({ assetId, assetName, csrfToken, canWrite }: AssetAssignmentsProps) {
  const [assignments, setAssignments] = useState<PeopleAssetAssignment[]>([])
  const [assignee, setAssignee] = useState<SearchableRecord[]>([])
  const [purpose, setPurpose] = useState<AssignmentPurpose>('checkout')
  const [role, setRole] = useState<AssignmentRole>('user')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [pendingPolicy, setPendingPolicy] = useState<{ conflicts: { assignmentId: string; assigneeLabel: string }[]; retry: (policy: string) => Promise<void> } | null>(null)

  useEffect(() => {
    let cancelled = false
    requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/assignments`)
      .then((body) => { if (!cancelled) setAssignments(readAssignments(body)) })
      .catch((loadError: unknown) => {
        if (!cancelled) setError(loadError instanceof ApiRequestError ? loadError.message : 'Assignment history could not be loaded.')
      })
    return () => { cancelled = true }
  }, [assetId])

  async function reload() {
    const body = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/assignments`)
    setAssignments(readAssignments(body))
  }

  async function createAssignment(policy = '') {
    const person = assignee[0]
    if (!person) return
    const form = document.getElementById(`asset-assignment-${assetId}`) as HTMLFormElement | null
    const values = form ? new FormData(form) : new FormData()
    const body: Record<string, unknown> = {
      assigneeKind: 'identity',
      assigneeId: person.id,
      role,
      purpose,
      eventSummary: String(values.get('eventSummary') ?? ''),
    }
    const from = localDateToISO(String(values.get('effectiveFrom') ?? ''))
    const due = localDateToISO(String(values.get('dueAt') ?? ''))
    if (from) body.effectiveFrom = from
    if (due) body.dueAt = due
    if (policy) body.conflictPolicy = policy
    const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/assignments`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify(body),
    })
    if (!isAssignment(response)) throw new Error('invalid assignment response')
    await reload()
    setAssignee([])
    setPendingPolicy(null)
    setStatus(`${purpose === 'reservation' ? 'Reserved' : 'Checked out'} ${assetName}.`)
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy('create')
    setError('')
    setStatus('')
    try {
      await createAssignment()
    } catch (createError) {
      const overlap = overlapFromError(createError)
      if (overlap) {
        setPendingPolicy({
          conflicts: overlap.conflicts,
          retry: async (policy) => {
            setBusy('create')
            try {
              await createAssignment(policy)
            } catch (retryError) {
              setError(retryError instanceof ApiRequestError ? retryError.message : 'Checkout could not be saved.')
            } finally {
              setBusy('')
            }
          },
        })
      } else {
        setError(createError instanceof ApiRequestError ? createError.message : 'Checkout could not be saved.')
      }
    } finally {
      setBusy('')
    }
  }

  async function handleReturn(assignmentId: string) {
    setBusy(`return-${assignmentId}`)
    setError('')
    try {
      await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/assignments/${encodeURIComponent(assignmentId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ effectiveTo: new Date().toISOString() }),
      })
      await reload()
      setStatus('Assignment marked returned.')
    } catch (returnError) {
      setError(returnError instanceof ApiRequestError ? returnError.message : 'The assignment could not be returned.')
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-labelledby={`asset-assignments-${assetId}`} className="mt-6">
      <h4 className="font-semibold" id={`asset-assignments-${assetId}`}>Checkout and assignments</h4>
      <p className="mt-1 text-sm text-steward-mist-muted">Effective-dated checkouts live with this asset. Atlas Users is the current directory reference; this history is the loan record.</p>
      {error && <p className="mt-3 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" role="alert">{error}</p>}
      {status && <p className="mt-3 text-sm text-steward-mist-muted" role="status">{status}</p>}
      {assignments.length === 0 ? (
        <p className="mt-3 text-sm text-steward-mist-muted">No checkouts or reservations yet.</p>
      ) : (
        <ol className="mt-3 space-y-3">
          {assignments.map((assignment) => (
            <li className="rounded-xl border border-steward-ink-800 p-3" key={assignment.id}>
              <p className="font-medium text-steward-mist">
                {assignment.assigneeLabel || assignment.assigneeId}
                <span className="ml-2 text-sm font-normal text-steward-mist-muted">{roleLabels[assignment.role]}</span>
              </p>
              <p className="mt-1 text-sm text-steward-mist-muted">
                {assignment.purpose === 'reservation' ? 'Reserved' : 'Checked out'} {formatDate(assignment.effectiveFrom)}
                {assignment.dueAt ? ` · return by ${formatDate(assignment.dueAt)}` : ' · open-ended'}
                {assignment.effectiveTo ? ` · returned ${formatDate(assignment.effectiveTo)}` : assignmentIsOverdue(assignment) ? ' · overdue' : ''}
                {assignment.eventSummary ? ` · ${assignment.eventSummary}` : ''}
              </p>
              {assignmentIsActive(assignment) && canWrite && (
                <button className={`${secondaryButtonClass} mt-2`} disabled={busy !== ''} onClick={() => void handleReturn(assignment.id)} type="button">
                  {busy === `return-${assignment.id}` ? 'Returning…' : 'Mark returned'}
                </button>
              )}
            </li>
          ))}
        </ol>
      )}
      {canWrite && (
        <form className={`${subpanelClass} mt-4 grid gap-3 p-3 sm:grid-cols-2`} id={`asset-assignment-${assetId}`} onSubmit={handleCreate}>
          <div className="sm:col-span-2">
            <RecordSearchPicker kind="identity" label="Assignee" multiple={false} onChange={setAssignee} selected={assignee} />
          </div>
          <label className={labelClass}>
            Purpose
            <select className={inputClass} onChange={(event) => setPurpose(event.target.value as AssignmentPurpose)} value={purpose}>
              <option value="checkout">Checkout</option>
              <option value="reservation">Reservation</option>
            </select>
          </label>
          <label className={labelClass}>
            Relationship
            <select className={inputClass} onChange={(event) => setRole(event.target.value as AssignmentRole)} value={role}>
              <option value="user">Additional user</option>
              <option value="primary">Primary assignee</option>
            </select>
          </label>
          <label className={labelClass}>
            From (optional)
            <input className={inputClass} name="effectiveFrom" type="datetime-local" />
          </label>
          <label className={labelClass}>
            Return by (optional)
            <input className={inputClass} name="dueAt" type="datetime-local" />
          </label>
          <label className={`${labelClass} sm:col-span-2`}>
            Event (optional)
            <input className={inputClass} name="eventSummary" placeholder="Classroom cart for Friday lab" />
          </label>
          <div className="sm:col-span-2">
            <button className={buttonClass} disabled={busy !== '' || assignee.length === 0} type="submit">
              {busy === 'create' ? 'Saving…' : purpose === 'reservation' ? 'Reserve asset' : 'Check out asset'}
            </button>
          </div>
        </form>
      )}
      {pendingPolicy && (
        <div className={`${subpanelClass} mt-3 p-3`} role="alertdialog">
          <p className="font-medium text-steward-mist">This period overlaps another checkout.</p>
          <ul className="mt-2 list-disc pl-5 text-sm text-steward-mist-muted">
            {pendingPolicy.conflicts.map((conflict) => (
              <li key={conflict.assignmentId}>{conflict.assigneeLabel}</li>
            ))}
          </ul>
          <div className="mt-3 flex flex-wrap gap-2">
            <button className={buttonClass} onClick={() => void pendingPolicy.retry('replace')} type="button">Replace current user</button>
            <button className={secondaryButtonClass} onClick={() => void pendingPolicy.retry('group')} type="button">Make group checkout</button>
            <button className={secondaryButtonClass} onClick={() => setPendingPolicy(null)} type="button">Cancel</button>
          </div>
        </div>
      )}
    </section>
  )
}
