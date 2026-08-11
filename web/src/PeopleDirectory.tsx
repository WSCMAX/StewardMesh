import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Asset } from './AtlasInventory'
import { ApiRequestError, requestJSON } from './api'

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001, A11Y-001, DOC-001, DOC-002.
// Feature: identity.directory.

type RecordStatus = 'active' | 'inactive'
type IdentityKind = 'person' | 'shared' | 'public' | 'lab'
type AssigneeKind = 'identity' | 'department'
type AssignmentRole = 'primary' | 'user' | 'department'

type SiteAddress = {
  line1?: string
  line2?: string
  city?: string
  region?: string
  postalCode?: string
  country?: string
}

type Site = {
  id: string
  organizationId: string
  name: string
  address?: SiteAddress
  status: RecordStatus
  revision: number
  createdAt: string
  updatedAt: string
}

type Building = {
  id: string
  organizationId: string
  siteId: string
  name: string
  status: RecordStatus
  revision: number
  createdAt: string
  updatedAt: string
}

type Room = {
  id: string
  organizationId: string
  siteId: string
  buildingId: string
  number: string
  name?: string
  status: RecordStatus
  revision: number
  createdAt: string
  updatedAt: string
}

type Department = {
  id: string
  organizationId: string
  name: string
  siteId?: string
  status: RecordStatus
  revision: number
  createdAt: string
  updatedAt: string
}

type Identity = {
  id: string
  organizationId: string
  kind: IdentityKind
  displayName: string
  email?: string
  departmentId?: string
  siteId?: string
  status: RecordStatus
  provider?: string
  providerSubject?: string
  revision: number
  createdAt: string
  updatedAt: string
}

type AssetAssignment = {
  id: string
  organizationId: string
  assetId: string
  assigneeKind: AssigneeKind
  assigneeId: string
  role: AssignmentRole
  effectiveFrom: string
  effectiveTo?: string
  createdBy: string
  createdAt: string
}

type Filters = {
  search: string
  kind: '' | IdentityKind
  status: '' | RecordStatus
  departmentId: string
  siteId: string
}

type PeopleDirectoryProps = {
  assets: readonly Asset[]
  csrfToken: string
  issuesUrl: string
  permissions: readonly string[]
}

const peopleHelpUrl = 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/people.md'
const emptyFilters: Filters = { search: '', kind: '', status: '', departmentId: '', siteId: '' }
const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2.5 text-steward-mist transition hover:border-steward-blue disabled:cursor-not-allowed disabled:opacity-60'
const labelClass = 'block text-sm font-semibold text-steward-mist-muted'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2.5 font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const secondaryButtonClass = 'min-h-11 rounded-lg border border-steward-ink-800 bg-steward-ink-900 px-4 py-2.5 font-semibold text-steward-mist transition hover:border-steward-blue hover:bg-steward-ink-800 disabled:cursor-wait disabled:opacity-60'

const kindLabels: Record<IdentityKind, string> = {
  person: 'Person',
  shared: 'Shared identity',
  public: 'Public users',
  lab: 'Computer lab users',
}

const roleLabels: Record<AssignmentRole, string> = {
  primary: 'Primary assignee',
  user: 'Additional user',
  department: 'Responsible department',
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}

function isBaseRecord(value: unknown): value is Record<string, unknown> {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return isString(record.id) && record.id.length > 0
    && isString(record.organizationId) && record.organizationId.length > 0
    && typeof record.revision === 'number' && record.revision > 0
    && isString(record.createdAt) && isString(record.updatedAt)
}

function isStatus(value: unknown): value is RecordStatus {
  return value === 'active' || value === 'inactive'
}

function isOptionalString(value: unknown): value is string | undefined {
  return value === undefined || isString(value)
}

function isSiteAddress(value: unknown): value is SiteAddress {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const address = value as Record<string, unknown>
  return isOptionalString(address.line1)
    && isOptionalString(address.line2)
    && isOptionalString(address.city)
    && isOptionalString(address.region)
    && isOptionalString(address.postalCode)
    && isOptionalString(address.country)
}

function isSite(value: unknown): value is Site {
  return isBaseRecord(value) && isString(value.name) && value.name.length > 0 && isStatus(value.status)
    && (value.address === undefined || isSiteAddress(value.address))
}

function isBuilding(value: unknown): value is Building {
  return isBaseRecord(value) && isString(value.siteId) && value.siteId.length > 0
    && isString(value.name) && value.name.length > 0 && isStatus(value.status)
}

function isRoom(value: unknown): value is Room {
  return isBaseRecord(value) && isString(value.siteId) && value.siteId.length > 0
    && isString(value.buildingId) && value.buildingId.length > 0
    && isString(value.number) && value.number.length > 0
    && isOptionalString(value.name) && isStatus(value.status)
}

function isDepartment(value: unknown): value is Department {
  return isBaseRecord(value) && isString(value.name) && value.name.length > 0 && isStatus(value.status)
    && (value.siteId === undefined || isString(value.siteId))
}

function isIdentity(value: unknown): value is Identity {
  return isBaseRecord(value) && isString(value.displayName) && value.displayName.length > 0
    && ['person', 'shared', 'public', 'lab'].includes(String(value.kind))
    && isStatus(value.status)
    && (value.email === undefined || isString(value.email))
    && (value.departmentId === undefined || isString(value.departmentId))
    && (value.siteId === undefined || isString(value.siteId))
}

function isAssignment(value: unknown): value is AssetAssignment {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return isString(record.id) && isString(record.organizationId) && isString(record.assetId)
    && (record.assigneeKind === 'identity' || record.assigneeKind === 'department')
    && isString(record.assigneeId)
    && (record.role === 'primary' || record.role === 'user' || record.role === 'department')
    && isString(record.effectiveFrom) && (record.effectiveTo === undefined || isString(record.effectiveTo))
    && isString(record.createdBy) && isString(record.createdAt)
}

function readCollection<T>(value: unknown, validator: (item: unknown) => item is T): T[] {
  if (typeof value !== 'object' || value === null) throw new Error('invalid collection response')
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items) || !items.every(validator)) throw new Error('invalid collection response')
  return items
}

function readRecord<T>(value: unknown, validator: (item: unknown) => item is T): T {
  if (!validator(value)) throw new Error('invalid record response')
  return value
}

function filtersToQuery(filters: Filters) {
  const query = new URLSearchParams()
  if (filters.search.trim()) query.set('q', filters.search.trim())
  if (filters.kind) query.set('kind', filters.kind)
  if (filters.status) query.set('status', filters.status)
  if (filters.departmentId) query.set('departmentId', filters.departmentId)
  if (filters.siteId) query.set('siteId', filters.siteId)
  query.set('limit', '100')
  return query
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown date'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function localDateTimeToISO(value: string) {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) throw new Error('Enter a valid effective date and time.')
  return date.toISOString()
}

function formatSiteAddress(address?: SiteAddress) {
  if (!address) return []
  const locality = [address.city, address.region].filter(Boolean).join(', ')
  const postalCountry = [address.postalCode, address.country].filter(Boolean).join(' ')
  return [address.line1, address.line2, locality, postalCountry].filter((line): line is string => Boolean(line))
}

function siteAddressFromForm(values: FormData): SiteAddress | undefined {
  const address: Required<SiteAddress> = {
    line1: String(values.get('siteAddressLine1') ?? '').trim(),
    line2: String(values.get('siteAddressLine2') ?? '').trim(),
    city: String(values.get('siteAddressCity') ?? '').trim(),
    region: String(values.get('siteAddressRegion') ?? '').trim(),
    postalCode: String(values.get('siteAddressPostalCode') ?? '').trim(),
    country: String(values.get('siteAddressCountry') ?? '').trim().toUpperCase(),
  }
  if (!Object.values(address).some(Boolean)) return undefined
  if (!address.line1 || !address.city || !/^[A-Z]{2}$/.test(address.country)) {
    throw new Error('An address needs address line 1, city, and a two-letter country code.')
  }
  return address
}

export default function PeopleDirectory({ assets, csrfToken, issuesUrl, permissions }: PeopleDirectoryProps) {
  const [sites, setSites] = useState<Site[]>([])
  const [buildings, setBuildings] = useState<Building[]>([])
  const [rooms, setRooms] = useState<Room[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [identities, setIdentities] = useState<Identity[]>([])
  const [assignments, setAssignments] = useState<AssetAssignment[]>([])
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [identityKind, setIdentityKind] = useState<IdentityKind>('person')
  const [assigneeKind, setAssigneeKind] = useState<AssigneeKind>('identity')
  const [assignmentRole, setAssignmentRole] = useState<AssignmentRole>('user')
  const [selectedAssetId, setSelectedAssetId] = useState('')
  const [loading, setLoading] = useState(true)
  const [assignmentsLoading, setAssignmentsLoading] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

  const canWriteDirectory = permissions.includes('directory.write')
  const canAssignAssets = canWriteDirectory && permissions.includes('assets.write')
  const departmentNames = useMemo(() => new Map(departments.map((department) => [department.id, department.name])), [departments])
  const siteNames = useMemo(() => new Map(sites.map((site) => [site.id, site.name])), [sites])
  const identityNames = useMemo(() => new Map(identities.map((identity) => [identity.id, identity.displayName])), [identities])

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const loadDirectory = useCallback(async (activeFilters: Filters, signal?: AbortSignal) => {
    const query = filtersToQuery(activeFilters)
    const [siteResponse, buildingResponse, roomResponse, departmentResponse, identityResponse] = await Promise.all([
      requestJSON('/api/v1/sites', { signal }),
      requestJSON('/api/v1/buildings', { signal }),
      requestJSON('/api/v1/rooms', { signal }),
      requestJSON('/api/v1/departments', { signal }),
      requestJSON(`/api/v1/identities?${query.toString()}`, { signal }),
    ])
    const nextSites = readCollection(siteResponse, isSite)
    const nextBuildings = readCollection(buildingResponse, isBuilding)
    const nextRooms = readCollection(roomResponse, isRoom)
    const nextDepartments = readCollection(departmentResponse, isDepartment)
    const nextIdentities = readCollection(identityResponse, isIdentity)
    setSites(nextSites)
    setBuildings(nextBuildings)
    setRooms(nextRooms)
    setDepartments(nextDepartments)
    setIdentities(nextIdentities)
  }, [])

  const loadAssignments = useCallback(async (assetId: string, signal?: AbortSignal) => {
    if (!assetId) {
      setAssignments([])
      return
    }
    setAssignmentsLoading(true)
    try {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/assignments`, { signal })
      setAssignments(readCollection(response, isAssignment))
    } finally {
      setAssignmentsLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    loadDirectory(emptyFilters, controller.signal)
      .catch((loadError: unknown) => {
        if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) {
          setError(loadError instanceof ApiRequestError ? loadError.message : 'The People directory could not be loaded.')
        }
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [loadDirectory])

  useEffect(() => {
    if (selectedAssetId && assets.some((asset) => asset.id === selectedAssetId)) return
    setSelectedAssetId(assets[0]?.id ?? '')
  }, [assets, selectedAssetId])

  useEffect(() => {
    const controller = new AbortController()
    loadAssignments(selectedAssetId, controller.signal).catch((loadError: unknown) => {
      if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) {
        setError(loadError instanceof ApiRequestError ? loadError.message : 'Assignment history could not be loaded.')
      }
    })
    return () => controller.abort()
  }, [loadAssignments, selectedAssetId])

  function reportMutationError(mutationError: unknown, fallback: string) {
    setStatus('')
    setError(mutationError instanceof ApiRequestError ? mutationError.message : mutationError instanceof Error ? mutationError.message : fallback)
  }

  async function refreshAfterMutation(message: string) {
    await loadDirectory(filters)
    if (selectedAssetId) await loadAssignments(selectedAssetId)
    setStatus(message)
  }

  async function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setLoading(true)
    setError('')
    setStatus('')
    try {
      await loadDirectory(filters)
      setStatus(`${identities.length === 1 ? 'Directory result updated.' : 'Directory results updated.'}`)
    } catch (searchError) {
      reportMutationError(searchError, 'The directory search could not be completed.')
    } finally {
      setLoading(false)
    }
  }

  async function clearSearch() {
    setFilters(emptyFilters)
    setLoading(true)
    setError('')
    try {
      await loadDirectory(emptyFilters)
      setStatus('Directory filters cleared.')
    } catch (searchError) {
      reportMutationError(searchError, 'The directory could not be refreshed.')
    } finally {
      setLoading(false)
    }
  }

  async function handleCreateSite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('site')
    setError('')
    try {
      const body: { name: string; status: RecordStatus; address?: SiteAddress } = {
        name: String(values.get('siteName') ?? ''),
        status: 'active',
      }
      const address = siteAddressFromForm(values)
      if (address) body.address = address
      readRecord(await requestJSON('/api/v1/sites', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      }), isSite)
      form.reset()
      await refreshAfterMutation('Site created and available to buildings and departments.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The site could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateBuilding(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('building')
    setError('')
    try {
      readRecord(await requestJSON('/api/v1/buildings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          siteId: String(values.get('buildingSiteId') ?? ''),
          name: String(values.get('buildingName') ?? ''),
          status: 'active',
        }),
      }), isBuilding)
      form.reset()
      await refreshAfterMutation('Building created beneath its site.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The building could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateRoom(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('room')
    setError('')
    try {
      const buildingId = String(values.get('roomBuildingId') ?? '')
      const building = buildings.find((candidate) => candidate.id === buildingId)
      if (!building) throw new Error('Select a visible building for this room.')
      readRecord(await requestJSON('/api/v1/rooms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          siteId: building.siteId,
          buildingId: building.id,
          number: String(values.get('roomNumber') ?? ''),
          name: String(values.get('roomName') ?? ''),
          status: 'active',
        }),
      }), isRoom)
      form.reset()
      await refreshAfterMutation('Room created beneath its building.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The room could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateDepartment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('department')
    setError('')
    try {
      const body: Record<string, string> = {
        name: String(values.get('departmentName') ?? ''),
        status: 'active',
      }
      const siteId = String(values.get('departmentSiteId') ?? '')
      if (siteId) body.siteId = siteId
      readRecord(await requestJSON('/api/v1/departments', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      }), isDepartment)
      form.reset()
      await refreshAfterMutation('Department created and available to identities.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The department could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateIdentity(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('identity')
    setError('')
    try {
      const body: Record<string, string> = {
        kind: identityKind,
        displayName: String(values.get('identityDisplayName') ?? ''),
        status: 'active',
      }
      const email = String(values.get('identityEmail') ?? '')
      const departmentId = String(values.get('identityDepartmentId') ?? '')
      const siteId = String(values.get('identitySiteId') ?? '')
      if (email) body.email = email
      if (departmentId) body.departmentId = departmentId
      if (siteId) body.siteId = siteId
      readRecord(await requestJSON('/api/v1/identities', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      }), isIdentity)
      form.reset()
      setIdentityKind('person')
      await refreshAfterMutation('Directory identity created.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The identity could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleCreateAssignment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('assignment')
    setError('')
    try {
      const effectiveFrom = localDateTimeToISO(String(values.get('assignmentEffectiveFrom') ?? ''))
      const body: Record<string, string> = {
        assigneeKind,
        assigneeId: String(values.get('assignmentAssigneeId') ?? ''),
        role: assigneeKind === 'department' ? 'department' : assignmentRole,
      }
      if (effectiveFrom) body.effectiveFrom = effectiveFrom
      readRecord(await requestJSON(`/api/v1/assets/${encodeURIComponent(selectedAssetId)}/assignments`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      }), isAssignment)
      form.reset()
      setAssigneeKind('identity')
      setAssignmentRole('user')
      await loadAssignments(selectedAssetId)
      setStatus('Asset assignment created. Previous primary or department responsibility was retained in history.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The asset assignment could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function handleEndAssignment(assignmentId: string) {
    setBusy(`end-${assignmentId}`)
    setError('')
    try {
      readRecord(await requestJSON(`/api/v1/assets/${encodeURIComponent(selectedAssetId)}/assignments/${encodeURIComponent(assignmentId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({}),
      }), isAssignment)
      await loadAssignments(selectedAssetId)
      setStatus('Assignment ended and retained in history.')
    } catch (mutationError) {
      reportMutationError(mutationError, 'The assignment could not be ended.')
    } finally {
      setBusy('')
    }
  }

  function assignmentName(assignment: AssetAssignment) {
    return assignment.assigneeKind === 'identity'
      ? identityNames.get(assignment.assigneeId) ?? 'Identity outside the current filter'
      : departmentNames.get(assignment.assigneeId) ?? 'Department outside the current filter'
  }

  return (
    <section aria-labelledby="people-heading" className="space-y-6 rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6" data-feature="identity.directory" data-requirement="REQ-PEOPLE-001 REQ-DIRECTORY-EXPANSION-001">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-3xl">
          <p className="text-sm font-semibold text-steward-teal">People — Users, locations, departments, and assignments</p>
          <h2 id="people-heading" className="mt-2 text-2xl font-semibold">Know who uses and stewards each asset</h2>
          <p className="mt-2 leading-7 text-steward-mist-muted">Organize people and shared-use identities by department, site, building, and room. Assign one primary steward, multiple users, and a responsible department while retaining prior assignments.</p>
        </div>
        <div className="flex gap-4 text-sm">
          <a className="text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={peopleHelpUrl}>People help</a>
          <a className="text-steward-teal underline underline-offset-4 hover:text-[#58d9c7]" href={issuesUrl}>Report a People issue</a>
        </div>
      </div>

      {error && <div ref={errorRef} className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert" tabIndex={-1}>{error}</div>}
      <p className="sr-only" aria-live="polite" role="status">{status}</p>

      <aside aria-labelledby="people-guide-heading" className="rounded-xl border border-steward-teal/20 bg-steward-ink-950/20 p-4">
        <h3 id="people-guide-heading" className="font-semibold text-steward-mist">Quick guide</h3>
        <ol className="mt-2 list-decimal space-y-1 pl-5 text-sm leading-6 text-steward-mist-muted">
          <li>Create a site, then add its buildings and rooms.</li>
          <li>Create departments that optionally belong to a site.</li>
          <li>Add a person, shared identity, public-users group, or computer-lab group.</li>
          <li>Choose an asset to add primary, additional-user, and department assignments.</li>
        </ol>
      </aside>

      <form aria-label="Filter People directory" className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4" onSubmit={handleSearch} role="search">
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
          <div className="lg:col-span-2">
            <label className={labelClass} htmlFor="people-search">Search name or email</label>
            <input className={inputClass} id="people-search" maxLength={200} onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))} type="search" value={filters.search} />
          </div>
          <div>
            <label className={labelClass} htmlFor="people-kind-filter">Identity type</label>
            <select className={inputClass} id="people-kind-filter" onChange={(event) => setFilters((current) => ({ ...current, kind: event.target.value as Filters['kind'] }))} value={filters.kind}>
              <option value="">All types</option>
              {Object.entries(kindLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </div>
          <div>
            <label className={labelClass} htmlFor="people-department-filter">Department</label>
            <select className={inputClass} id="people-department-filter" onChange={(event) => setFilters((current) => ({ ...current, departmentId: event.target.value }))} value={filters.departmentId}>
              <option value="">All departments</option>
              {departments.map((department) => <option key={department.id} value={department.id}>{department.name}</option>)}
            </select>
          </div>
          <div>
            <label className={labelClass} htmlFor="people-site-filter">Site</label>
            <select className={inputClass} id="people-site-filter" onChange={(event) => setFilters((current) => ({ ...current, siteId: event.target.value }))} value={filters.siteId}>
              <option value="">All sites</option>
              {sites.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}
            </select>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-3">
          <button className={buttonClass} disabled={loading} type="submit">{loading ? 'Searching…' : 'Apply filters'}</button>
          <button className={secondaryButtonClass} disabled={loading} onClick={clearSearch} type="button">Clear filters</button>
        </div>
      </form>

      <div aria-busy={loading} aria-labelledby="people-results-heading">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h3 id="people-results-heading" className="text-lg font-semibold">Directory results</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">{loading ? 'Loading scoped records…' : `${identities.length} ${identities.length === 1 ? 'identity' : 'identities'} visible with your current access and filters.`}</p>
          </div>
          <p className="text-sm text-steward-mist-muted">{departments.length} departments · {sites.length} sites</p>
        </div>
        {identities.length === 0 && !loading ? (
          <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">No directory identities match these filters.</p>
        ) : (
          <ul className="mt-4 grid gap-3 md:grid-cols-2" aria-label="People directory identities">
            {identities.map((identity) => (
              <li className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/50 p-4" key={identity.id}>
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div><p className="font-semibold text-steward-mist">{identity.displayName}</p><p className="mt-1 text-sm text-steward-mist-muted">{kindLabels[identity.kind]} · {identity.status === 'active' ? 'Active' : 'Inactive'}</p></div>
                  <span className="rounded-full border border-steward-ink-800 px-2 py-1 text-xs text-steward-mist-muted">Revision {identity.revision}</span>
                </div>
                {identity.email && <p className="mt-3 break-all text-sm text-steward-mist-muted">{identity.email}</p>}
                <p className="mt-2 text-sm text-steward-mist-muted">Department: {identity.departmentId ? departmentNames.get(identity.departmentId) ?? 'Not visible' : 'Not assigned'}</p>
                <p className="mt-1 text-sm text-steward-mist-muted">Site: {identity.siteId ? siteNames.get(identity.siteId) ?? 'Not visible' : 'Not assigned'}</p>
              </li>
            ))}
          </ul>
        )}
      </div>

      <section aria-busy={loading} aria-labelledby="locations-heading" className="border-t border-steward-ink-800 pt-6" data-requirement="REQ-DIRECTORY-EXPANSION-001">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h3 id="locations-heading" className="text-lg font-semibold">Locations in your scope</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Buildings and rooms are grouped beneath the sites you are allowed to see.</p>
          </div>
          <p className="text-sm text-steward-mist-muted">{sites.length} sites · {buildings.length} buildings · {rooms.length} rooms</p>
        </div>
        {loading ? (
          <p className="mt-4 text-sm text-steward-mist-muted">Loading scoped locations…</p>
        ) : sites.length === 0 ? (
          <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">No sites are visible in your directory scope.</p>
        ) : (
          <ul aria-label="Visible directory locations" className="mt-4 grid gap-4 lg:grid-cols-2">
            {sites.map((site) => {
              const addressLines = formatSiteAddress(site.address)
              const siteBuildings = buildings.filter((building) => building.siteId === site.id)
              return (
                <li className="min-w-0 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4" key={site.id}>
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <h4 className="font-semibold text-steward-mist">{site.name}</h4>
                      <p className="mt-1 text-sm text-steward-mist-muted">Site · {site.status === 'active' ? 'Active' : 'Inactive'}</p>
                    </div>
                    <span className="rounded-full border border-steward-ink-800 px-2 py-1 text-xs text-steward-mist-muted">Revision {site.revision}</span>
                  </div>
                  {addressLines.length > 0 ? (
                    <address aria-label={`${site.name} address`} className="mt-3 text-sm not-italic leading-6 text-steward-mist-muted">
                      {addressLines.map((line, index) => <span className="block" key={`${line}-${index}`}>{line}</span>)}
                    </address>
                  ) : <p className="mt-3 text-sm text-steward-mist-muted">No address recorded.</p>}
                  {siteBuildings.length === 0 ? (
                    <p className="mt-4 rounded-lg border border-dashed border-steward-ink-800 p-3 text-sm text-steward-mist-muted">No buildings recorded for this site.</p>
                  ) : (
                    <ul aria-label={`Buildings at ${site.name}`} className="mt-4 space-y-3">
                      {siteBuildings.map((building) => {
                        const buildingRooms = rooms.filter((room) => room.siteId === site.id && room.buildingId === building.id)
                        return (
                          <li className="rounded-lg border border-steward-ink-800 bg-steward-ink-900/70 p-3" key={building.id}>
                            <div className="flex flex-wrap items-start justify-between gap-2">
                              <div>
                                <h5 className="font-semibold text-steward-mist-muted">{building.name}</h5>
                                <p className="mt-1 text-xs text-steward-mist-muted">Building · {building.status === 'active' ? 'Active' : 'Inactive'}</p>
                              </div>
                              <span className="text-xs text-steward-mist-muted">{buildingRooms.length} {buildingRooms.length === 1 ? 'room' : 'rooms'}</span>
                            </div>
                            {buildingRooms.length === 0 ? (
                              <p className="mt-3 text-sm text-steward-mist-muted">No rooms recorded.</p>
                            ) : (
                              <ul aria-label={`Rooms in ${building.name}`} className="mt-3 grid gap-2 sm:grid-cols-2">
                                {buildingRooms.map((room) => (
                                  <li className="min-w-0 rounded-lg bg-steward-ink-950/70 px-3 py-2 text-sm" key={room.id}>
                                    <p className="break-words font-medium text-steward-mist-muted">Room {room.number}{room.name ? ` · ${room.name}` : ''}</p>
                                    <p className="mt-1 text-xs text-steward-mist-muted">{room.status === 'active' ? 'Active' : 'Inactive'}</p>
                                  </li>
                                ))}
                              </ul>
                            )}
                          </li>
                        )
                      })}
                    </ul>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </section>

      {canWriteDirectory ? (
        <div className="space-y-6">
          <section aria-labelledby="create-locations-heading" data-requirement="REQ-DIRECTORY-EXPANSION-001">
            <h3 className="text-lg font-semibold" id="create-locations-heading">Create location records</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Create each level in order so rooms always inherit the correct site from their building.</p>
            <div className="mt-4 grid gap-4 xl:grid-cols-3">
              <details className="min-w-0 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4">
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a site</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateSite}>
                  <div>
                    <label className={labelClass} htmlFor="site-name">Site name</label>
                    <p className="mt-1 text-sm text-steward-mist-muted" id="site-name-help">Use a recognizable campus, office, data center, or region name.</p>
                    <input aria-describedby="site-name-help" className={inputClass} id="site-name" maxLength={200} name="siteName" required />
                  </div>
                  <fieldset aria-describedby="site-address-help" className="rounded-lg border border-steward-ink-800 p-3">
                    <legend className="px-1 text-sm font-semibold text-steward-mist-muted">Address (optional)</legend>
                    <p className="text-sm text-steward-mist-muted" id="site-address-help">If you add an address, line 1, city, and a two-letter country code are required.</p>
                    <div className="mt-3 grid gap-3 sm:grid-cols-2">
                      <div className="sm:col-span-2"><label className={labelClass} htmlFor="site-address-line-1">Address line 1</label><input className={inputClass} id="site-address-line-1" maxLength={200} name="siteAddressLine1" /></div>
                      <div className="sm:col-span-2"><label className={labelClass} htmlFor="site-address-line-2">Address line 2 (optional)</label><input className={inputClass} id="site-address-line-2" maxLength={200} name="siteAddressLine2" /></div>
                      <div><label className={labelClass} htmlFor="site-address-city">City</label><input className={inputClass} id="site-address-city" maxLength={100} name="siteAddressCity" /></div>
                      <div><label className={labelClass} htmlFor="site-address-region">State or region (optional)</label><input className={inputClass} id="site-address-region" maxLength={100} name="siteAddressRegion" /></div>
                      <div><label className={labelClass} htmlFor="site-address-postal-code">Postal code (optional)</label><input className={inputClass} id="site-address-postal-code" maxLength={32} name="siteAddressPostalCode" /></div>
                      <div><label className={labelClass} htmlFor="site-address-country">Country code</label><input className={inputClass} id="site-address-country" maxLength={2} name="siteAddressCountry" pattern="[A-Za-z]{2}" title="Two-letter country code, such as US" /></div>
                    </div>
                  </fieldset>
                  <button className={`${buttonClass} w-full`} disabled={busy !== ''} type="submit">{busy === 'site' ? 'Creating site…' : 'Create site'}</button>
                </form>
              </details>

              <details className="min-w-0 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4">
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a building</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateBuilding}>
                  <div>
                    <label className={labelClass} htmlFor="building-site">Building site</label>
                    <select className={inputClass} disabled={sites.length === 0} id="building-site" name="buildingSiteId" required>
                      <option value="">Select a site</option>
                      {sites.filter((site) => site.status === 'active').map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}
                    </select>
                  </div>
                  <div><label className={labelClass} htmlFor="building-name">Building name</label><input className={inputClass} id="building-name" maxLength={200} name="buildingName" required /></div>
                  <button className={`${buttonClass} w-full`} disabled={busy !== '' || sites.length === 0} type="submit">{busy === 'building' ? 'Creating building…' : 'Create building'}</button>
                </form>
              </details>

              <details className="min-w-0 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4">
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a room</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateRoom}>
                  <div>
                    <label className={labelClass} htmlFor="room-building">Room building</label>
                    <select className={inputClass} disabled={buildings.length === 0} id="room-building" name="roomBuildingId" required>
                      <option value="">Select a building</option>
                      {buildings.filter((building) => building.status === 'active').map((building) => <option key={building.id} value={building.id}>{building.name} · {siteNames.get(building.siteId) ?? 'Site not visible'}</option>)}
                    </select>
                  </div>
                  <div><label className={labelClass} htmlFor="room-number">Room number</label><input className={inputClass} id="room-number" maxLength={100} name="roomNumber" required /></div>
                  <div><label className={labelClass} htmlFor="room-name">Room name (optional)</label><input className={inputClass} id="room-name" maxLength={200} name="roomName" /></div>
                  <button className={`${buttonClass} w-full`} disabled={busy !== '' || buildings.length === 0} type="submit">{busy === 'room' ? 'Creating room…' : 'Create room'}</button>
                </form>
              </details>
            </div>
          </section>

          <section aria-labelledby="create-people-heading">
            <h3 className="text-lg font-semibold" id="create-people-heading">Create People records</h3>
            <div className="mt-4 grid gap-4 lg:grid-cols-2">
              <details className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4">
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a department</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateDepartment}>
                  <div><label className={labelClass} htmlFor="department-name">Department name</label><input className={inputClass} id="department-name" maxLength={200} name="departmentName" required /></div>
                  <div>
                    <label className={labelClass} htmlFor="department-site">Site (optional)</label>
                    <select className={inputClass} id="department-site" name="departmentSiteId"><option value="">No site</option>{sites.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}</select>
                  </div>
                  <button className={`${buttonClass} w-full`} disabled={busy !== ''} type="submit">{busy === 'department' ? 'Creating department…' : 'Create department'}</button>
                </form>
              </details>

              <details className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4">
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a directory identity</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateIdentity}>
                  <div>
                    <label className={labelClass} htmlFor="identity-kind">Identity type</label>
                    <select className={inputClass} id="identity-kind" name="identityKind" onChange={(event) => setIdentityKind(event.target.value as IdentityKind)} value={identityKind}>{Object.entries(kindLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
                  </div>
                  <div><label className={labelClass} htmlFor="identity-display-name">Display name</label><input autoComplete="name" className={inputClass} id="identity-display-name" maxLength={200} name="identityDisplayName" required /></div>
                  <div>
                    <label className={labelClass} htmlFor="identity-email">Email address {identityKind === 'person' ? '(required)' : '(optional)'}</label>
                    <input autoComplete="email" className={inputClass} id="identity-email" maxLength={320} name="identityEmail" required={identityKind === 'person'} type="email" />
                  </div>
                  <div><label className={labelClass} htmlFor="identity-department">Department (optional)</label><select className={inputClass} id="identity-department" name="identityDepartmentId"><option value="">No department</option>{departments.map((department) => <option key={department.id} value={department.id}>{department.name}</option>)}</select></div>
                  <div>
                    <label className={labelClass} htmlFor="identity-site">Site (optional)</label>
                    <p className="mt-1 text-sm text-steward-mist-muted" id="identity-site-help">Leave blank to inherit the selected department&apos;s site.</p>
                    <select aria-describedby="identity-site-help" className={inputClass} id="identity-site" name="identitySiteId"><option value="">Inherit or no site</option>{sites.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}</select>
                  </div>
                  <button className={`${buttonClass} w-full`} disabled={busy !== ''} type="submit">{busy === 'identity' ? 'Creating identity…' : 'Create identity'}</button>
                </form>
              </details>
            </div>
          </section>
        </div>
      ) : <p className="rounded-xl border border-steward-ink-800 p-4 text-sm text-steward-mist-muted">Your role can read this scoped directory and its locations but cannot create records.</p>}

      <div className="border-t border-steward-ink-800 pt-6">
        <h3 className="text-lg font-semibold" id="assignment-history-heading">Asset assignment history</h3>
        <p className="mt-1 text-sm text-steward-mist-muted">Multiple users can remain active together. Adding a new primary assignee or responsible department automatically closes the previous matching role at the new effective date.</p>
        {assets.length === 0 ? (
          <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">Add an Atlas asset before creating People assignments.</p>
        ) : (
          <>
            <div className="mt-4 max-w-xl">
              <label className={labelClass} htmlFor="assignment-asset">Asset</label>
              <select className={inputClass} id="assignment-asset" onChange={(event) => setSelectedAssetId(event.target.value)} value={selectedAssetId}>{assets.map((asset) => <option key={asset.id} value={asset.id}>{asset.name}</option>)}</select>
            </div>

            {canAssignAssets && (
              <form aria-label="Create asset assignment" className="mt-4 grid gap-4 rounded-xl border border-steward-ink-800 bg-steward-ink-950/40 p-4 md:grid-cols-2 lg:grid-cols-4" onSubmit={handleCreateAssignment}>
                <div>
                  <label className={labelClass} htmlFor="assignment-kind">Assignee type</label>
                  <select className={inputClass} id="assignment-kind" onChange={(event) => { const value = event.target.value as AssigneeKind; setAssigneeKind(value); setAssignmentRole(value === 'department' ? 'department' : 'user') }} value={assigneeKind}><option value="identity">Directory identity</option><option value="department">Department</option></select>
                </div>
                <div>
                  <label className={labelClass} htmlFor="assignment-assignee">Assignee</label>
                  <select className={inputClass} id="assignment-assignee" name="assignmentAssigneeId" required>
                    <option value="">Select an assignee</option>
                    {assigneeKind === 'identity' ? identities.filter((identity) => identity.status === 'active').map((identity) => <option key={identity.id} value={identity.id}>{identity.displayName}</option>) : departments.filter((department) => department.status === 'active').map((department) => <option key={department.id} value={department.id}>{department.name}</option>)}
                  </select>
                </div>
                <div>
                  <label className={labelClass} htmlFor="assignment-role">Relationship</label>
                  <select className={inputClass} disabled={assigneeKind === 'department'} id="assignment-role" onChange={(event) => setAssignmentRole(event.target.value as AssignmentRole)} value={assigneeKind === 'department' ? 'department' : assignmentRole}>{assigneeKind === 'department' ? <option value="department">Responsible department</option> : <><option value="user">Additional user</option><option value="primary">Primary assignee</option></>}</select>
                </div>
                <div>
                  <label className={labelClass} htmlFor="assignment-effective-from">Effective from (optional)</label>
                  <input className={inputClass} id="assignment-effective-from" name="assignmentEffectiveFrom" type="datetime-local" />
                </div>
                <div className="md:col-span-2 lg:col-span-4"><button className={buttonClass} disabled={busy !== '' || !selectedAssetId} type="submit">{busy === 'assignment' ? 'Creating assignment…' : 'Create assignment'}</button></div>
              </form>
            )}

            <div aria-busy={assignmentsLoading} aria-labelledby="assignment-history-heading" className="mt-4">
              {assignmentsLoading ? <p className="text-sm text-steward-mist-muted">Loading assignment history…</p> : assignments.length === 0 ? <p className="rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">No assignments recorded for this asset.</p> : (
                <ol className="space-y-3">
                  {assignments.map((assignment) => (
                    <li className="flex flex-wrap items-start justify-between gap-4 rounded-xl border border-steward-ink-800 p-4" key={assignment.id}>
                      <div>
                        <p className="font-semibold">{assignmentName(assignment)}</p>
                        <p className="mt-1 text-sm text-steward-mist-muted">{roleLabels[assignment.role]} · {assignment.effectiveTo ? 'Ended' : 'Active'}</p>
                        <p className="mt-1 text-sm text-steward-mist-muted">From {formatDate(assignment.effectiveFrom)}{assignment.effectiveTo ? ` to ${formatDate(assignment.effectiveTo)}` : ''}</p>
                      </div>
                      {!assignment.effectiveTo && canAssignAssets && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => handleEndAssignment(assignment.id)} type="button">{busy === `end-${assignment.id}` ? 'Ending…' : 'End assignment'}</button>}
                    </li>
                  ))}
                </ol>
              )}
            </div>
          </>
        )}
      </div>
    </section>
  )
}
