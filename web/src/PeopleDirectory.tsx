import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Asset } from './AtlasInventory'
import { ApiRequestError, isRevision, requestJSON } from './api'
import DirectoryImportManager from './DirectoryImportManager'
import PeopleSectionNav, { type PeopleSection } from './PeopleSectionNav'
import type { WorkspaceRecordFocus } from './graphRecord'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import { documentationHref } from './documentation'
import { RelatedRecordModeChooser, RelatedRecordWorkflowFrame, useRelatedRecordWorkflow } from './RelatedRecordWorkflow'
import { ProductHeader, buttonClass, cx, inputClass, labelClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass } from './ui'
import DataGrid, { type StagedDraft } from './grid/DataGrid'
import Drawer from './grid/Drawer'
import type { GridColumn } from './grid/columns'
import type { CellEdit } from './grid/useCellEditing'
import { buildPayload, summarizeReport, tasksFromEdits, useWriteQueue, type WriteTransport } from './grid/writeQueue'
import type { GridIdentity } from './grid/viewState'
import {
  createLabelDefinition,
  isLabelColumnKey,
  loadLabelAssignments,
  loadLabelDefinitionsFor,
  saveLabelEdits,
  type LabelAssignment,
  type LabelDefinition,
  type LabelValueKind,
} from './labelsGrid'
import {
  buildBuildingColumns,
  buildDepartmentColumns,
  buildIdentityColumns,
  buildLocationReferenceColumns,
  buildLocationReferenceTypeColumns,
  buildRoomColumns,
  buildSiteColumns,
  buildingPayload,
  departmentPayload,
  emptyTagContext,
  identityPayload,
  kindLabels,
  occupancySheets,
  locationReferencePayload,
  locationReferenceTypePayload,
  locationSheets,
  peopleRecordTypes,
  roomPayload,
  sitePayload,
  type Building,
  type Department,
  type Identity,
  type IdentityKind,
  type LocationReference,
  type LocationReferenceType,
  type LocationSheet,
  type OccupancySheet,
  type PeopleColumnContext,
  type PeopleRecordType,
  type PeopleTagContext,
  type RecordStatus,
  type Room,
  type Site,
  type SiteAddress,
} from './peopleGrid'

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-008, REQ-WORKSPACE-001, A11Y-001, DOC-001, DOC-002.
// Features: identity.directory, identity.labels, experience.grid, experience.workspace.

type AssigneeKind = 'identity' | 'department'
type AssignmentRole = 'primary' | 'user' | 'department'

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

type GuidedPersonDraft = {
  displayName: string
  email: string
  departmentId: string
}

type GuidedLocationKind = 'site' | 'building' | 'room'

type GuidedLocationChoice = {
  key: string
  kind: GuidedLocationKind
  label: string
  siteId: string
  siteLabel: string
}

type PeopleDirectoryProps = {
  assets: readonly Asset[]
  csrfToken: string
  issuesUrl: string
  permissions: readonly string[]
  onOpenHelp?: () => void
  onReportIssue?: () => void
  focusRecord?: WorkspaceRecordFocus | null
  identity?: GridIdentity | null
}

const peopleHelpUrl = documentationHref('people')
const emptyFilters: Filters = { search: '', kind: '', status: '', departmentId: '', siteId: '' }
const emptyGuidedPerson: GuidedPersonDraft = { displayName: '', email: '', departmentId: '' }
const personLocationBoundaries = {
  source: {
    label: 'Person record',
    owner: 'People — Users, locations, departments, and assignments',
    api: 'POST /api/v1/identities',
    authorization: 'directory.write',
  },
  related: {
    label: 'Location record',
    owner: 'People — Users, locations, departments, and assignments',
    api: 'GET/POST /api/v1/sites, /buildings, /rooms',
    authorization: 'directory.read; directory.write to create',
  },
} as const

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
    && isRevision(record.revision)
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
    && (value.buildingId === undefined || isString(value.buildingId))
    && (value.roomId === undefined || isString(value.roomId))
}

function isLocationReferenceType(value: unknown): value is LocationReferenceType {
  return isBaseRecord(value) && isString(value.name) && value.name.length > 0
    && ['located_at', 'uses_office', 'teaches_in', 'attends_class', 'resides_in', 'uses_lab'].includes(String(value.relationshipKind))
    && ['site', 'building', 'room'].includes(String(value.locationKind))
    && isStatus(value.status)
    && (value.description === undefined || isString(value.description))
}

function isLocationReference(value: unknown): value is LocationReference {
  return isBaseRecord(value)
    && isString(value.identityId) && isString(value.typeId) && isString(value.locationId)
    && ['site', 'building', 'room'].includes(String(value.locationKind))
    && (value.priority === 'primary' || value.priority === 'secondary')
    && isStatus(value.status)
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

function locationChoices(sites: readonly Site[], buildings: readonly Building[], rooms: readonly Room[]) {
  const siteNames = new Map(sites.map((site) => [site.id, site.name]))
  const buildingNames = new Map(buildings.map((building) => [building.id, building.name]))
  const choices: GuidedLocationChoice[] = sites.map((site) => ({
    key: `site:${site.id}`,
    kind: 'site',
    label: `Site — ${site.name}`,
    siteId: site.id,
    siteLabel: site.name,
  }))
  for (const building of buildings) {
    const siteLabel = siteNames.get(building.siteId)
    if (!siteLabel) continue
    choices.push({
      key: `building:${building.id}`,
      kind: 'building',
      label: `Building — ${building.name} · ${siteLabel}`,
      siteId: building.siteId,
      siteLabel,
    })
  }
  for (const room of rooms) {
    const siteLabel = siteNames.get(room.siteId)
    const buildingLabel = buildingNames.get(room.buildingId)
    if (!siteLabel || !buildingLabel) continue
    choices.push({
      key: `room:${room.id}`,
      kind: 'room',
      label: `Room ${room.number}${room.name ? ` · ${room.name}` : ''} — ${buildingLabel} · ${siteLabel}`,
      siteId: room.siteId,
      siteLabel,
    })
  }
  return choices
}

type PersonLocationWorkflowProps = {
  buildings: readonly Building[]
  canWrite: boolean
  departments: readonly Department[]
  rooms: readonly Room[]
  sites: readonly Site[]
  onCreateBuilding: (siteId: string, name: string) => Promise<Building>
  onCreatePerson: (draft: GuidedPersonDraft, location: GuidedLocationChoice) => Promise<Identity>
  onCreateRoom: (building: Building, number: string, name: string) => Promise<Room>
  onCreateSite: (name: string) => Promise<Site>
}

function PersonLocationWorkflow({ buildings, canWrite, departments, rooms, sites, onCreateBuilding, onCreatePerson, onCreateRoom, onCreateSite }: PersonLocationWorkflowProps) {
  const [person, setPerson] = useState<GuidedPersonDraft>(emptyGuidedPerson)
  const [locationMode, setLocationMode] = useState<'select' | 'create'>('select')
  const [selectedLocationKey, setSelectedLocationKey] = useState('')
  const [createKind, setCreateKind] = useState<GuidedLocationKind>('site')
  const [createParentId, setCreateParentId] = useState('')
  const [createName, setCreateName] = useState('')
  const [createRoomName, setCreateRoomName] = useState('')
  const startButtonRef = useRef<HTMLButtonElement>(null)
  const stepHeadingRef = useRef<HTMLHeadingElement>(null)
  const choices = useMemo(() => locationChoices(sites, buildings, rooms), [buildings, rooms, sites])

  function resetWorkflowDraft() {
    setPerson(emptyGuidedPerson)
    setSelectedLocationKey('')
    setLocationMode('select')
    setCreateKind('site')
    setCreateParentId('')
    setCreateName('')
    setCreateRoomName('')
  }

  const workflow = useRelatedRecordWorkflow<GuidedLocationChoice>({
    cancellationMessage: 'Person workflow cancelled and its draft was cleared. Any location already created remains available in People.',
    onReset: resetWorkflowDraft,
  })
  const previousStepRef = useRef(workflow.step)

  useEffect(() => {
    const previousStep = previousStepRef.current
    previousStepRef.current = workflow.step
    if (workflow.failure) return
    if (workflow.step === 'intro') {
      if (previousStep !== 'intro') startButtonRef.current?.focus()
      return
    }
    stepHeadingRef.current?.focus()
  }, [workflow.failure, workflow.step])

  function handlePersonNext(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const displayName = person.displayName.trim()
    const email = person.email.trim()
    if (!displayName) {
      workflow.failValidation('Person details: enter a display name before continuing.')
      return
    }
    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      workflow.failValidation('Person details: enter a valid email address before continuing.')
      return
    }
    setPerson((current) => ({ ...current, displayName, email }))
    workflow.moveTo('related')
  }

  async function handleLocationNext(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (locationMode === 'select') {
      const choice = choices.find((candidate) => candidate.key === selectedLocationKey)
      if (!choice) {
        workflow.failValidation('Location: select a visible site, building, or room before continuing.')
        return
      }
      workflow.selectRelated(choice)
      return
    }

    const name = createName.trim()
    if (!name) {
      workflow.failValidation(`Location: enter a ${createKind === 'room' ? 'room number' : `${createKind} name`} before continuing.`)
      return
    }
    await workflow.createRelated(async () => {
      let choice: GuidedLocationChoice
      if (createKind === 'site') {
        const site = await onCreateSite(name)
        choice = { key: `site:${site.id}`, kind: 'site', label: `Site — ${site.name}`, siteId: site.id, siteLabel: site.name }
      } else if (createKind === 'building') {
        const site = sites.find((candidate) => candidate.id === createParentId)
        if (!site) throw new Error('Location: select a visible site for the new building.')
        const building = await onCreateBuilding(site.id, name)
        choice = { key: `building:${building.id}`, kind: 'building', label: `Building — ${building.name} · ${site.name}`, siteId: site.id, siteLabel: site.name }
      } else {
        const building = buildings.find((candidate) => candidate.id === createParentId)
        const site = building && sites.find((candidate) => candidate.id === building.siteId)
        if (!building || !site) throw new Error('Location: select a visible building for the new room.')
        const room = await onCreateRoom(building, name, createRoomName.trim())
        choice = {
          key: `room:${room.id}`,
          kind: 'room',
          label: `Room ${room.number}${room.name ? ` · ${room.name}` : ''} — ${building.name} · ${site.name}`,
          siteId: site.id,
          siteLabel: site.name,
        }
      }
      setSelectedLocationKey(choice.key)
      setCreateName('')
      setCreateRoomName('')
      return choice
    }, (creationError) => creationError instanceof ApiRequestError ? `Location: ${creationError.message}` : creationError instanceof Error ? creationError.message : 'Location: the new location could not be created.', (creationError) => !(creationError instanceof ApiRequestError) || creationError.status >= 500)
  }

  async function handleCreatePerson(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await workflow.confirm(async (selectedLocation) => {
      const identity = await onCreatePerson(person, selectedLocation)
      return `${identity.displayName} was created at ${selectedLocation.label}.`
    }, (creationError) => creationError instanceof ApiRequestError ? `Review: ${creationError.message}` : creationError instanceof Error ? `Review: ${creationError.message}` : 'Review: the person could not be created.', (creationError) => !(creationError instanceof ApiRequestError) || creationError.status >= 500)
  }

  return (
    <RelatedRecordWorkflowFrame
      boundaries={personLocationBoundaries}
      busy={workflow.busy}
      description="Keep the person draft in place while you select an existing location or create the missing site, building, or room."
      failure={workflow.failure}
      failureRef={workflow.failureRef}
      headingId="person-location-workflow-heading"
      kicker="Guided People task"
      onRetry={workflow.retry}
      status={workflow.status}
      step={workflow.step}
      title="Add a person with a location"
    >
      {!canWrite ? (
        <div className="mt-4 rounded-lg border border-steward-ink-800 p-4 text-sm leading-6 text-steward-mist-muted" role="note">
          <p>Your role can review the visible locations below but cannot add a person or create a missing location.</p>
          <p className="mt-2">Ask an administrator for the <code>directory.write</code> permission, or send them the person and location details for completion.</p>
        </div>
      ) : workflow.step === 'intro' ? (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <button className={buttonClass} onClick={workflow.start} ref={startButtonRef} type="button">Start person workflow</button>
          <p className="text-sm text-steward-mist-muted">You can go back between steps without losing entered values.</p>
        </div>
      ) : workflow.step === 'source' ? (
        <form className="mt-5 space-y-4" noValidate onSubmit={handlePersonNext}>
          <h4 className="text-base font-semibold" ref={stepHeadingRef} tabIndex={-1}>Step 1 — Person details</h4>
          <div className="grid gap-4 md:grid-cols-2">
            <div><label className={labelClass} htmlFor="guided-person-name">Person display name</label><input autoComplete="name" className={inputClass} id="guided-person-name" maxLength={200} onChange={(event) => setPerson((current) => ({ ...current, displayName: event.target.value }))} required value={person.displayName} /></div>
            <div><label className={labelClass} htmlFor="guided-person-email">Person email address</label><input autoComplete="email" className={inputClass} id="guided-person-email" maxLength={320} onChange={(event) => setPerson((current) => ({ ...current, email: event.target.value }))} required type="email" value={person.email} /></div>
            <div className="md:col-span-2"><label className={labelClass} htmlFor="guided-person-department">Person department (optional)</label><select className={inputClass} id="guided-person-department" onChange={(event) => setPerson((current) => ({ ...current, departmentId: event.target.value }))} value={person.departmentId}><option value="">No department</option>{departments.map((department) => <option key={department.id} value={department.id}>{department.name}</option>)}</select></div>
          </div>
          <div className="flex flex-wrap gap-3"><button className={buttonClass} type="submit">Continue to location</button><button className={secondaryButtonClass} onClick={workflow.cancel} type="button">Cancel workflow</button></div>
        </form>
      ) : workflow.step === 'related' ? (
        <form className="mt-5 space-y-4" noValidate onSubmit={handleLocationNext}>
          <h4 className="text-base font-semibold" ref={stepHeadingRef} tabIndex={-1}>Step 2 — Choose or create a location</h4>
          <RelatedRecordModeChooser
            canCreate={canWrite}
            createLabel="Create a missing location"
            fallbackMessage="Location creation is unavailable with your current grant. You can still select any visible existing location."
            legend="Location path"
            mode={locationMode}
            name="guidedLocationMode"
            onChange={setLocationMode}
            selectLabel="Select a visible location"
          />
          {locationMode === 'select' ? (
            <div>
              <label className={labelClass} htmlFor="guided-existing-location">Existing location</label>
              <select className={inputClass} id="guided-existing-location" onChange={(event) => setSelectedLocationKey(event.target.value)} required value={selectedLocationKey}><option value="">Select a site, building, or room</option>{choices.map((choice) => <option key={choice.key} value={choice.key}>{choice.label}</option>)}</select>
              {choices.length === 0 && <p className="mt-2 text-sm text-steward-mist-muted">No locations are visible. Choose “Create a missing location” to add the first site.</p>}
            </div>
          ) : (
            <div className="grid gap-4 rounded-lg border border-steward-ink-800 p-4 md:grid-cols-2">
              <div><label className={labelClass} htmlFor="guided-location-kind">New location type</label><select className={inputClass} id="guided-location-kind" onChange={(event) => { setCreateKind(event.target.value as GuidedLocationKind); setCreateParentId(''); setCreateName(''); setCreateRoomName('') }} value={createKind}><option value="site">Site</option><option disabled={sites.length === 0} value="building">Building</option><option disabled={buildings.length === 0} value="room">Room</option></select></div>
              {createKind === 'building' && <div><label className={labelClass} htmlFor="guided-building-site">New building site</label><select className={inputClass} id="guided-building-site" onChange={(event) => setCreateParentId(event.target.value)} required value={createParentId}><option value="">Select a site</option>{sites.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}</select></div>}
              {createKind === 'room' && <div><label className={labelClass} htmlFor="guided-room-building">New room building</label><select className={inputClass} id="guided-room-building" onChange={(event) => setCreateParentId(event.target.value)} required value={createParentId}><option value="">Select a building</option>{buildings.map((building) => <option key={building.id} value={building.id}>{building.name} · {sites.find((site) => site.id === building.siteId)?.name ?? 'Site not visible'}</option>)}</select></div>}
              <div><label className={labelClass} htmlFor="guided-location-name">{createKind === 'room' ? 'New room number' : `New ${createKind} name`}</label><input className={inputClass} id="guided-location-name" maxLength={createKind === 'room' ? 100 : 200} onChange={(event) => setCreateName(event.target.value)} required value={createName} /></div>
              {createKind === 'room' && <div><label className={labelClass} htmlFor="guided-room-name">New room name (optional)</label><input className={inputClass} id="guided-room-name" maxLength={200} onChange={(event) => setCreateRoomName(event.target.value)} value={createRoomName} /></div>}
            </div>
          )}
          <div className="flex flex-wrap gap-3">
            <button className={buttonClass} disabled={workflow.busy !== null} type="submit">{workflow.busy === 'related' ? 'Creating location…' : locationMode === 'create' ? 'Create and review' : 'Continue to review'}</button>
            <button className={secondaryButtonClass} disabled={workflow.busy !== null} onClick={() => workflow.moveTo('source')} type="button">Back to person details</button>
            <button className={secondaryButtonClass} disabled={workflow.busy !== null} onClick={workflow.cancel} type="button">Cancel workflow</button>
          </div>
        </form>
      ) : (
        <form className="mt-5 space-y-4" onSubmit={handleCreatePerson}>
          <h4 className="text-base font-semibold" ref={stepHeadingRef} tabIndex={-1}>Step 3 — Review and create</h4>
          <dl className="grid gap-4 rounded-lg border border-steward-ink-800 p-4 sm:grid-cols-2">
            <div><dt className="text-sm font-semibold text-steward-mist-muted">Person</dt><dd className="mt-1 text-steward-mist">{person.displayName}</dd><dd className="mt-1 break-all text-sm text-steward-mist-muted">{person.email}</dd></div>
            <div><dt className="text-sm font-semibold text-steward-mist-muted">Department</dt><dd className="mt-1 text-steward-mist">{person.departmentId ? departments.find((department) => department.id === person.departmentId)?.name ?? 'Department no longer visible' : 'No department'}</dd></div>
            <div className="sm:col-span-2"><dt className="text-sm font-semibold text-steward-mist-muted">Selected location</dt><dd className="mt-1 text-steward-mist">{workflow.related?.label}</dd><dd className="mt-1 text-sm text-steward-mist-muted">The person record will be linked to the containing site: {workflow.related?.siteLabel}.</dd></div>
          </dl>
          <div className="flex flex-wrap gap-3">
            <button className={buttonClass} disabled={workflow.busy !== null} type="submit">{workflow.busy === 'confirm' ? 'Creating person…' : 'Create person'}</button>
            <button className={secondaryButtonClass} disabled={workflow.busy !== null} onClick={() => workflow.moveTo('related')} type="button">Back to location</button>
            <button className={secondaryButtonClass} disabled={workflow.busy !== null} onClick={() => workflow.moveTo('source')} type="button">Edit person details</button>
            <button className={secondaryButtonClass} disabled={workflow.busy !== null} onClick={workflow.cancel} type="button">Cancel workflow</button>
          </div>
        </form>
      )}
    </RelatedRecordWorkflowFrame>
  )
}

export default function PeopleDirectory({ assets, csrfToken, issuesUrl, permissions, onOpenHelp, onReportIssue, focusRecord = null, identity = null }: PeopleDirectoryProps) {
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
  const [activeSection, setActiveSection] = useState<PeopleSection>('directory')
  const [locationSheet, setLocationSheet] = useState<LocationSheet>('sites')
  const [occupancySheet, setOccupancySheet] = useState<OccupancySheet>('references')
  const [locationReferenceTypes, setLocationReferenceTypes] = useState<LocationReferenceType[]>([])
  const [locationReferences, setLocationReferences] = useState<LocationReference[]>([])
  const [nestedSite, setNestedSite] = useState<Site | null>(null)
  const [nestedBuilding, setNestedBuilding] = useState<Building | null>(null)
  const [identityDepartment, setIdentityDepartment] = useState<SearchableRecord[]>([])
  const [identitySite, setIdentitySite] = useState<SearchableRecord[]>([])
  const [tagColumnFormOpen, setTagColumnFormOpen] = useState(false)
  const [tagColumnBusy, setTagColumnBusy] = useState(false)
  const [tagColumnRecordType, setTagColumnRecordType] = useState<PeopleRecordType>(peopleRecordTypes.identity)
  const [identityTags, setIdentityTags] = useState<PeopleTagContext>(emptyTagContext(csrfToken, false))
  const [siteTags, setSiteTags] = useState<PeopleTagContext>(emptyTagContext(csrfToken, false))
  const [buildingTags, setBuildingTags] = useState<PeopleTagContext>(emptyTagContext(csrfToken, false))
  const [roomTags, setRoomTags] = useState<PeopleTagContext>(emptyTagContext(csrfToken, false))
  const [departmentTags, setDepartmentTags] = useState<PeopleTagContext>(emptyTagContext(csrfToken, false))
  const errorRef = useRef<HTMLDivElement>(null)
  const identityWrites = useWriteQueue()
  const siteWrites = useWriteQueue()
  const buildingWrites = useWriteQueue()
  const roomWrites = useWriteQueue()
  const departmentWrites = useWriteQueue()
  const locationTypeWrites = useWriteQueue()
  const locationReferenceWrites = useWriteQueue()

  const canWriteDirectory = permissions.includes('directory.write')
  const canAssignAssets = canWriteDirectory && permissions.includes('assets.write')
  const canReadLabels = permissions.includes('labels.read')
  const canWriteLabels = permissions.includes('labels.write')
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

  const loadOccupancy = useCallback(async (signal?: AbortSignal) => {
    const [typeResponse, referenceResponse] = await Promise.all([
      requestJSON('/api/v1/location-reference-types', { signal }),
      requestJSON('/api/v1/location-references?limit=500', { signal }),
    ])
    setLocationReferenceTypes(readCollection(typeResponse, isLocationReferenceType))
    setLocationReferences(readCollection(referenceResponse, isLocationReference))
  }, [])

  const applyTagDefinition = useCallback((definition: LabelDefinition) => {
    const apply = (current: PeopleTagContext): PeopleTagContext => ({
      ...current,
      definitions: current.definitions.some((item) => item.id === definition.id)
        ? current.definitions.map((item) => item.id === definition.id ? definition : item)
        : [...current.definitions, definition].sort((left, right) => left.name.localeCompare(right.name)),
    })
    setIdentityTags(apply)
    setSiteTags(apply)
    setBuildingTags(apply)
    setRoomTags(apply)
    setDepartmentTags(apply)
  }, [])

  const applyTagAssignment = useCallback((
    setter: (value: PeopleTagContext | ((current: PeopleTagContext) => PeopleTagContext)) => void,
    definitionId: string,
    recordId: string,
    assignment: LabelAssignment | null,
  ) => {
    setter((current) => {
      const next = new Map(current.assignments)
      const byRecord = new Map(next.get(definitionId) ?? [])
      if (assignment) byRecord.set(recordId, assignment)
      else byRecord.delete(recordId)
      next.set(definitionId, byRecord)
      return { ...current, assignments: next }
    })
  }, [])

  useEffect(() => {
    if (!canReadLabels) {
      setIdentityTags(emptyTagContext(csrfToken, canWriteLabels))
      setSiteTags(emptyTagContext(csrfToken, canWriteLabels))
      setBuildingTags(emptyTagContext(csrfToken, canWriteLabels))
      setRoomTags(emptyTagContext(csrfToken, canWriteLabels))
      setDepartmentTags(emptyTagContext(csrfToken, canWriteLabels))
      return
    }
    let cancelled = false
    const load = async (recordType: string, setter: (value: PeopleTagContext) => void) => {
      const definitions = await loadLabelDefinitionsFor(recordType)
      const assignments = await loadLabelAssignments(definitions, recordType)
      if (cancelled) return
      setter({
        csrfToken,
        canWriteLabels,
        definitions,
        assignments,
        onDefinitionUpdated: applyTagDefinition,
      })
    }
    void Promise.all([
      load(peopleRecordTypes.identity, setIdentityTags),
      load(peopleRecordTypes.site, setSiteTags),
      load(peopleRecordTypes.building, setBuildingTags),
      load(peopleRecordTypes.room, setRoomTags),
      load(peopleRecordTypes.department, setDepartmentTags),
    ]).catch(() => {
      if (!cancelled) setError('Directory tags could not be loaded.')
    })
    return () => { cancelled = true }
  }, [applyTagDefinition, canReadLabels, canWriteLabels, csrfToken])

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
    if (activeSection !== 'references') return
    const controller = new AbortController()
    loadOccupancy(controller.signal).catch((loadError: unknown) => {
      if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) {
        setError(loadError instanceof ApiRequestError ? loadError.message : 'Location references could not be loaded.')
      }
    })
    return () => controller.abort()
  }, [activeSection, loadOccupancy])

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

  useEffect(() => {
    if (!focusRecord) return
    const locationKinds = new Set(['site', 'building', 'room', 'department'])
    const identityKinds = new Set(['person', 'shared', 'public', 'lab'])
    if (locationKinds.has(focusRecord.kind)) {
      setActiveSection('locations')
      if (focusRecord.kind === 'site') {
        setLocationSheet('sites')
        setNestedSite(sites.find((item) => item.id === focusRecord.recordId) ?? null)
        setNestedBuilding(null)
      }
      if (focusRecord.kind === 'building') {
        setLocationSheet('buildings')
        const building = buildings.find((item) => item.id === focusRecord.recordId) ?? null
        setNestedBuilding(building)
        setNestedSite(building ? sites.find((item) => item.id === building.siteId) ?? null : null)
      }
      if (focusRecord.kind === 'room') {
        setLocationSheet('rooms')
        const room = rooms.find((item) => item.id === focusRecord.recordId)
        const building = room ? buildings.find((item) => item.id === room.buildingId) ?? null : null
        setNestedBuilding(building)
        setNestedSite(building ? sites.find((item) => item.id === building.siteId) ?? null : null)
      }
      if (focusRecord.kind === 'department') setLocationSheet('departments')
      return
    }
    setActiveSection('directory')
    const next: Filters = {
      ...emptyFilters,
      search: focusRecord.recordId,
      kind: identityKinds.has(focusRecord.kind) ? focusRecord.kind as IdentityKind : '',
    }
    setFilters(next)
    setLoading(true)
    loadDirectory(next)
      .catch((loadError: unknown) => {
        if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) {
          setError(loadError instanceof ApiRequestError ? loadError.message : 'The People directory could not be loaded.')
        }
      })
      .finally(() => setLoading(false))
  }, [buildings, focusRecord?.kind, focusRecord?.nonce, focusRecord?.recordId, loadDirectory, rooms, sites])

  function reportMutationError(mutationError: unknown, fallback: string) {
    setStatus('')
    setError(mutationError instanceof ApiRequestError ? mutationError.message : mutationError instanceof Error ? mutationError.message : fallback)
  }

  async function refreshAfterMutation(message: string) {
    await loadDirectory(filters)
    if (selectedAssetId) await loadAssignments(selectedAssetId)
    setStatus(message)
  }

  async function createSiteRecord(name: string, address?: SiteAddress) {
    const body: { name: string; status: RecordStatus; address?: SiteAddress } = { name, status: 'active' }
    if (address) body.address = address
    return readRecord(await requestJSON('/api/v1/sites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify(body),
    }), isSite)
  }

  async function createBuildingRecord(siteId: string, name: string) {
    return readRecord(await requestJSON('/api/v1/buildings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ siteId, name, status: 'active' }),
    }), isBuilding)
  }

  async function createRoomRecord(building: Building, number: string, name: string) {
    return readRecord(await requestJSON('/api/v1/rooms', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ siteId: building.siteId, buildingId: building.id, number, name, status: 'active' }),
    }), isRoom)
  }

  async function createIdentityRecord(body: Record<string, string>) {
    return readRecord(await requestJSON('/api/v1/identities', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify(body),
    }), isIdentity)
  }

  async function handleCreateSite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('site')
    setError('')
    try {
      const address = siteAddressFromForm(values)
      await createSiteRecord(String(values.get('siteName') ?? ''), address)
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
      await createBuildingRecord(String(values.get('buildingSiteId') ?? ''), String(values.get('buildingName') ?? ''))
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
      await createRoomRecord(building, String(values.get('roomNumber') ?? ''), String(values.get('roomName') ?? ''))
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
      const departmentId = identityDepartment[0]?.id ?? String(values.get('identityDepartmentId') ?? '')
      const siteId = identitySite[0]?.id ?? String(values.get('identitySiteId') ?? '')
      if (email) body.email = email
      if (departmentId) body.departmentId = departmentId
      if (siteId) body.siteId = siteId
      await createIdentityRecord(body)
      form.reset()
      setIdentityKind('person')
      setIdentityDepartment([])
      setIdentitySite([])
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

  const columnContext = useMemo((): Omit<PeopleColumnContext, 'tags'> => ({
    sites, buildings, rooms, departments, csrfToken, canWrite: canWriteDirectory,
    identities, locationTypes: locationReferenceTypes,
    onCreated: {
      site: (site) => setSites((current) => current.some((item) => item.id === site.id) ? current : [...current, site]),
      building: (building) => setBuildings((current) => current.some((item) => item.id === building.id) ? current : [...current, building]),
      room: (room) => setRooms((current) => current.some((item) => item.id === room.id) ? current : [...current, room]),
      department: (department) => setDepartments((current) => current.some((item) => item.id === department.id) ? current : [...current, department]),
    },
  }), [buildings, canWriteDirectory, csrfToken, departments, identities, locationReferenceTypes, rooms, sites])

  const identityColumns = useMemo(() => buildIdentityColumns({ ...columnContext, tags: identityTags }, canWriteDirectory || canWriteLabels), [canWriteDirectory, canWriteLabels, columnContext, identityTags])
  const siteColumns = useMemo(() => buildSiteColumns({ ...columnContext, tags: siteTags }, canWriteDirectory || canWriteLabels), [canWriteDirectory, canWriteLabels, columnContext, siteTags])
  const buildingColumns = useMemo(() => buildBuildingColumns({ ...columnContext, tags: buildingTags }, canWriteDirectory || canWriteLabels), [buildingTags, canWriteDirectory, canWriteLabels, columnContext])
  const roomColumns = useMemo(() => buildRoomColumns({ ...columnContext, tags: roomTags }, canWriteDirectory || canWriteLabels), [canWriteDirectory, canWriteLabels, columnContext, roomTags])
  const departmentColumns = useMemo(() => buildDepartmentColumns({ ...columnContext, tags: departmentTags }, canWriteDirectory || canWriteLabels), [canWriteDirectory, canWriteLabels, columnContext, departmentTags])
  const locationTypeColumns = useMemo(() => buildLocationReferenceTypeColumns(canWriteDirectory), [canWriteDirectory])
  const locationReferenceColumns = useMemo(() => buildLocationReferenceColumns({ ...columnContext, tags: emptyTagContext(csrfToken, false) }, canWriteDirectory), [canWriteDirectory, columnContext, csrfToken])

  function reportGridError(reason: string): never {
    setError(reason)
    queueMicrotask(() => errorRef.current?.focus())
    throw new Error(reason)
  }

  async function saveDirectoryEdits<T extends { id: string }>(
    edits: readonly CellEdit[],
    rows: readonly T[],
    columns: readonly GridColumn<T>[],
    payloadFor: (row: T) => Record<string, unknown>,
    pathFor: (row: T) => string,
    read: (value: unknown) => T,
    tags: PeopleTagContext,
    recordType: string,
    writes: ReturnType<typeof useWriteQueue>,
    onSaved: (items: T[]) => void,
    onAssignment: (definitionId: string, recordId: string, assignment: LabelAssignment | null) => void,
  ) {
    setError('')
    setStatus('')
    const stored = new Map(rows.map((row) => [row.id, row]))
    const saved = new Map<string, T>()
    const transport: WriteTransport = {
      concurrency: 4,
      writeRecord: async (task) => {
        const row = stored.get(task.rowId)
        if (!row) throw new Error('The record is no longer loaded.')
        const fieldEdits = task.edits.filter((edit) => !isLabelColumnKey(edit.columnKey))
        const labelEdits = task.edits.filter((edit) => isLabelColumnKey(edit.columnKey))
        if (fieldEdits.length > 0) {
          if (!canWriteDirectory) throw new Error('Directory write access is required.')
          const payload = buildPayload(fieldEdits, columns, payloadFor(row))
          const response = await requestJSON(pathFor(row), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify(payload),
          })
          saved.set(task.rowId, read(response))
        }
        if (labelEdits.length > 0) {
          await saveLabelEdits(labelEdits, tags.definitions, tags.assignments, csrfToken, canWriteLabels, applyTagDefinition, onAssignment, recordType)
        }
      },
    }
    const report = await writes.run(tasksFromEdits(edits), transport)
    if (saved.size > 0) onSaved(rows.map((row) => saved.get(row.id) ?? row))
    if (report.failed > 0) reportGridError(summarizeReport(report))
    setStatus(summarizeReport(report))
  }

  async function createDirectoryRows(
    drafts: readonly StagedDraft[],
    path: string,
    bodyFor: (values: Record<string, string>) => Record<string, unknown>,
    read: (value: unknown) => { id: string },
    onCreated: (record: { id: string }) => void,
  ) {
    setError('')
    setStatus('')
    for (const draft of drafts) {
      const created = read(await requestJSON(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ ...bodyFor(draft.values), status: draft.values.status || 'active' }),
      }))
      onCreated(created)
    }
    setStatus(drafts.length === 1 ? 'Record created from the spreadsheet.' : `${drafts.length} records created from the spreadsheet.`)
  }

  async function handleCreateTagColumn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWriteLabels) return
    const values = new FormData(event.currentTarget)
    const name = String(values.get('name') ?? '').trim()
    const id = String(values.get('id') ?? '').trim()
    const valueKind = String(values.get('valueKind') ?? 'multiselect') as LabelValueKind
    const options = String(values.get('options') ?? '').split(',').map((option) => option.trim()).filter(Boolean)
    if (!name) {
      setError('Tag name is required.')
      return
    }
    setTagColumnBusy(true)
    setError('')
    try {
      const created = await createLabelDefinition({ id: id || undefined, name, valueKind, options }, [tagColumnRecordType], csrfToken)
      applyTagDefinition(created)
      setTagColumnFormOpen(false)
      setStatus(`Tag column “${created.name}” is available on this sheet. Use Floor as a text or select tag on rooms when you need floor numbers.`)
    } catch (createError) {
      reportMutationError(createError, 'The tag column could not be created.')
    } finally {
      setTagColumnBusy(false)
    }
  }

  const tagToolbar = (recordType: PeopleRecordType) => (canWriteLabels || canReadLabels) ? <div className="flex flex-wrap items-center gap-2">
    {canWriteLabels && <button className={plainButtonClass + ' min-h-8 px-2 py-1 text-xs'} onClick={() => { setTagColumnRecordType(recordType); setTagColumnFormOpen(true) }} type="button">Add tag column</button>}
    {canReadLabels && <a className={plainButtonClass + ' min-h-8 px-2 py-1 text-xs'} href="#workspace-threads">Manage tags</a>}
  </div> : undefined

  function openNestedBuilding(building: Building) {
    setNestedBuilding(building)
    setNestedSite(sites.find((item) => item.id === building.siteId) ?? null)
  }

  const nestedBuildings = nestedSite ? buildings.filter((building) => building.siteId === nestedSite.id) : []
  const nestedRooms = nestedBuilding ? rooms.filter((room) => room.buildingId === nestedBuilding.id) : []

  return (
    <section aria-labelledby="people-heading" className={`${panelClass} space-y-5 p-4 sm:p-5`} data-feature="identity.directory identity.labels experience.grid experience.workspace" data-requirement="REQ-PEOPLE-001 REQ-DIRECTORY-EXPANSION-001 REQ-DIRECTORY-EXPANSION-008 REQ-WORKSPACE-001">
      <ProductHeader
        actions={<>
          {onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">People help</button> : <a className={secondaryButtonClass} href={peopleHelpUrl}>People help</a>}
          {onReportIssue ? <button className={plainButtonClass} onClick={onReportIssue} type="button">Report a People issue</button> : <a className={plainButtonClass} href={issuesUrl}>Report a People issue</a>}
        </>}
        description="Organize people and shared-use identities by department, site, building, and room. Assign one primary steward, multiple users, and a responsible department while retaining prior assignments."
        headingId="people-heading"
        kicker="People — Users, locations, departments, and assignments"
        title="Know who uses and stewards each asset"
      />

      {error && <div ref={errorRef} className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" role="alert" tabIndex={-1}>{error}</div>}
      <p className="sr-only" aria-live="polite" role="status">{status}</p>

      <PeopleSectionNav active={activeSection} onChange={setActiveSection} />

      <div aria-labelledby={`people-tab-${activeSection}`} hidden={activeSection !== 'directory'} id="people-panel-directory" role="tabpanel">
      <section aria-labelledby="people-guide-heading" className={`${subpanelClass} border-steward-teal/20 p-4`}>
        <h3 id="people-guide-heading" className="font-semibold text-steward-mist">Quick guide</h3>
        <ol className="mt-2 list-decimal space-y-1 pl-5 text-sm leading-6 text-steward-mist-muted">
          <li>Edit the <strong className="font-semibold text-steward-mist">Directory</strong> spreadsheet in place: type to replace, Enter or Tab to commit, Ctrl+C / Ctrl+V with Excel, Ctrl+D to fill down.</li>
          <li>Open <strong className="font-semibold text-steward-mist">Locations</strong> for site, building, and room sheets, or drill into a nested sheet from a site or building.</li>
          <li>Use <strong className="font-semibold text-steward-mist">Location references</strong> for office, classroom, dorm, and lab occupancy, including a catalog of reference types. Group references by room for usage.</li>
          <li>Add a <strong className="font-semibold text-steward-mist">tag column</strong> for floor numbers or other labels. Relationship browsing lives in Mesh.</li>
          <li>Use <strong className="font-semibold text-steward-mist">Workflows &amp; assignments</strong> for guided creation and asset history.</li>
        </ol>
      </section>

      <p className="mt-4 text-sm leading-6 text-steward-mist-muted">
        {canWriteDirectory || canWriteLabels
          ? `Edit cells directly. Group by department, site, type, or status from the toolbar.${canWriteDirectory ? ' Use + to insert a new identity below a row.' : ''}${canReadLabels ? ' Tag columns use configured labels, including floor or other room attributes when you add them here.' : ''}`
          : 'Sort, filter, group, and copy directory records. Editing requires directory or tag write access.'}
      </p>
      <div className="mt-4">
        <DataGrid
          columns={identityColumns}
          editable={canWriteDirectory || canWriteLabels}
          emptyMessage="No directory identities match these filters."
          identity={identity}
          label="Directory identities"
          onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/identities', (values) => ({
            kind: values.kind || 'person',
            displayName: values.displayName,
            email: values.email,
            departmentId: values.departmentId,
            siteId: values.siteId,
            buildingId: values.buildingId,
            roomId: values.roomId,
          }), (value) => readRecord(value, isIdentity), (record) => setIdentities((current) => current.some((item) => item.id === record.id) ? current : [record as Identity, ...current])) : undefined}
          onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, identities, identityColumns, identityPayload, (row) => `/api/v1/identities/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isIdentity), identityTags, peopleRecordTypes.identity, identityWrites, setIdentities, (definitionId, recordId, assignment) => applyTagAssignment(setIdentityTags, definitionId, recordId, assignment)) : undefined}
          rowId={(row) => row.id}
          rowLabel={(row) => row.displayName}
          rowMessage={(row) => identityWrites.rowMessage(row.id)}
          rowState={(row) => identityWrites.rowState(row.id)}
          rows={identities}
          selectable
          toolbar={tagToolbar(peopleRecordTypes.identity)}
          viewDefaults={{ groupBy: 'departmentId', filters: { status: 'active' } }}
          viewId="people-identities"
        />
      </div>
      <p className="mt-3 text-sm text-steward-mist-muted" role="status">
        {loading ? 'Loading scoped records…' : `${identities.length} ${identities.length === 1 ? 'identity' : 'identities'} loaded · ${departments.length} departments · ${sites.length} sites`}
      </p>
      </div>

      <div aria-labelledby="people-tab-locations" hidden={activeSection !== 'locations'} id="people-panel-locations" role="tabpanel">
      <section aria-busy={loading} aria-labelledby="locations-heading" className="space-y-4" data-requirement="REQ-DIRECTORY-EXPANSION-001">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h3 id="locations-heading" className="text-lg font-semibold">Locations in your scope</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Use a sheet per level, group rows in the toolbar, or open a nested sheet from a site or building. Add a Floor tag column on rooms when you need floors without a separate record type.</p>
          </div>
          <p className="text-sm text-steward-mist-muted">{sites.length} sites · {buildings.length} buildings · {rooms.length} rooms · {departments.length} departments</p>
        </div>
        <div aria-label="Location sheets" className="flex flex-wrap gap-1 border-b border-white/10" role="tablist">
          {locationSheets.map((sheet) => {
            const selected = locationSheet === sheet.id
            return (
              <button
                aria-controls={`people-location-sheet-${sheet.id}`}
                aria-selected={selected}
                className={cx('relative shrink-0 px-3 py-2 text-sm font-medium transition', selected ? 'text-steward-mist' : `${secondaryButtonClass} min-h-0 rounded-none border-transparent bg-transparent px-3 py-2 text-steward-mist-muted`)}
                id={`people-location-tab-${sheet.id}`}
                key={sheet.id}
                onClick={() => { setLocationSheet(sheet.id); if (sheet.id !== 'sites') setNestedSite(null); if (sheet.id !== 'buildings' && sheet.id !== 'sites') setNestedBuilding(null) }}
                role="tab"
                title={sheet.description}
                type="button"
              >
                {sheet.label}
                {selected && <span aria-hidden="true" className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-steward-teal" />}
              </button>
            )
          })}
        </div>
        <p className="text-sm text-steward-mist-muted">{locationSheets.find((sheet) => sheet.id === locationSheet)?.description}</p>
        {loading ? <p className="text-sm text-steward-mist-muted">Loading scoped locations…</p> : null}
        <div hidden={locationSheet !== 'sites'} id="people-location-sheet-sites" role="tabpanel">
          <DataGrid
            columns={siteColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No sites are visible in your directory scope."
            identity={identity}
            label="Sites"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/sites', (values) => ({
              name: values.name,
              address: { line1: values.line1, line2: values.line2, city: values.city, region: values.region, postalCode: values.postalCode, country: values.country },
            }), (value) => readRecord(value, isSite), (record) => setSites((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Site])) : undefined}
            onOpenRow={(site) => { setNestedSite(site); setNestedBuilding(null) }}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, sites, siteColumns, sitePayload, (row) => `/api/v1/sites/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isSite), siteTags, peopleRecordTypes.site, siteWrites, setSites, (definitionId, recordId, assignment) => applyTagAssignment(setSiteTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name}
            rowMessage={(row) => siteWrites.rowMessage(row.id)}
            rowState={(row) => siteWrites.rowState(row.id)}
            rows={sites}
            toolbar={tagToolbar(peopleRecordTypes.site)}
            viewId="people-sites"
          />
        </div>
        <div hidden={locationSheet !== 'buildings'} id="people-location-sheet-buildings" role="tabpanel">
          <DataGrid
            columns={buildingColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No buildings are visible in your directory scope."
            identity={identity}
            label="Buildings"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/buildings', (values) => ({
              name: values.name, siteId: values.siteId || nestedSite?.id || '',
            }), (value) => readRecord(value, isBuilding), (record) => setBuildings((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Building])) : undefined}
            onOpenRow={openNestedBuilding}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, buildings, buildingColumns, buildingPayload, (row) => `/api/v1/buildings/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isBuilding), buildingTags, peopleRecordTypes.building, buildingWrites, setBuildings, (definitionId, recordId, assignment) => applyTagAssignment(setBuildingTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name}
            rowMessage={(row) => buildingWrites.rowMessage(row.id)}
            rowState={(row) => buildingWrites.rowState(row.id)}
            rows={buildings}
            toolbar={tagToolbar(peopleRecordTypes.building)}
            viewDefaults={{ groupBy: 'siteId' }}
            viewId="people-buildings"
          />
        </div>
        <div hidden={locationSheet !== 'rooms'} id="people-location-sheet-rooms" role="tabpanel">
          <DataGrid
            columns={roomColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No rooms are visible in your directory scope."
            identity={identity}
            label="Rooms"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/rooms', (values) => {
              const building = buildings.find((item) => item.id === (values.buildingId || nestedBuilding?.id))
              return { number: values.number, name: values.name, buildingId: building?.id ?? values.buildingId, siteId: building?.siteId ?? values.siteId }
            }, (value) => readRecord(value, isRoom), (record) => setRooms((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Room])) : undefined}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, rooms, roomColumns, roomPayload, (row) => `/api/v1/rooms/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isRoom), roomTags, peopleRecordTypes.room, roomWrites, setRooms, (definitionId, recordId, assignment) => applyTagAssignment(setRoomTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name ? `${row.number} · ${row.name}` : row.number}
            rowMessage={(row) => roomWrites.rowMessage(row.id)}
            rowState={(row) => roomWrites.rowState(row.id)}
            rows={rooms}
            toolbar={tagToolbar(peopleRecordTypes.room)}
            viewDefaults={{ groupBy: 'buildingId' }}
            viewId="people-rooms"
          />
        </div>
        <div hidden={locationSheet !== 'departments'} id="people-location-sheet-departments" role="tabpanel">
          <DataGrid
            columns={departmentColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No departments are visible in your directory scope."
            identity={identity}
            label="Departments"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/departments', (values) => ({
              name: values.name, siteId: values.siteId,
            }), (value) => readRecord(value, isDepartment), (record) => setDepartments((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Department])) : undefined}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, departments, departmentColumns, departmentPayload, (row) => `/api/v1/departments/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isDepartment), departmentTags, peopleRecordTypes.department, departmentWrites, setDepartments, (definitionId, recordId, assignment) => applyTagAssignment(setDepartmentTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name}
            rowMessage={(row) => departmentWrites.rowMessage(row.id)}
            rowState={(row) => departmentWrites.rowState(row.id)}
            rows={departments}
            toolbar={tagToolbar(peopleRecordTypes.department)}
            viewDefaults={{ groupBy: 'siteId' }}
            viewId="people-departments"
          />
        </div>
        {nestedSite && <section aria-label={`Nested buildings at ${nestedSite.name}`} className={`${subpanelClass} p-3`}>
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h4 className="font-semibold">Buildings at {nestedSite.name}</h4>
            <div className="flex flex-wrap gap-2">
              <button className={`${plainButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => { setLocationSheet('buildings'); setNestedSite(null); setNestedBuilding(null) }} type="button">Open as sheet</button>
              <button className={`${secondaryButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => { setNestedSite(null); setNestedBuilding(null) }} type="button">Close nested sheet</button>
            </div>
          </div>
          <DataGrid
            columns={buildingColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No buildings recorded for this site."
            identity={identity}
            label={`Buildings at ${nestedSite.name}`}
            maximumBodyHeight="18rem"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/buildings', (values) => ({
              name: values.name, siteId: nestedSite.id,
            }), (value) => readRecord(value, isBuilding), (record) => setBuildings((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Building])) : undefined}
            onOpenRow={openNestedBuilding}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, nestedBuildings, buildingColumns, buildingPayload, (row) => `/api/v1/buildings/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isBuilding), buildingTags, peopleRecordTypes.building, buildingWrites, (next) => setBuildings((current) => current.map((item) => next.find((candidate) => candidate.id === item.id) ?? item)), (definitionId, recordId, assignment) => applyTagAssignment(setBuildingTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name}
            rows={nestedBuildings}
            viewId={`people-nested-buildings-${nestedSite.id}`}
          />
          {nestedBuilding && nestedBuilding.siteId === nestedSite.id && <section aria-label={`Nested rooms in ${nestedBuilding.name}`} className="mt-3 rounded-md border border-steward-teal/30 p-3">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h5 className="font-semibold">Rooms in {nestedBuilding.name}</h5>
              <div className="flex flex-wrap gap-2">
                <button className={`${plainButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => { setLocationSheet('rooms'); setNestedSite(null); setNestedBuilding(null) }} type="button">Open as sheet</button>
                <button className={`${secondaryButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => setNestedBuilding(null)} type="button">Close rooms sheet</button>
              </div>
            </div>
            <DataGrid
              columns={roomColumns}
              editable={canWriteDirectory || canWriteLabels}
              emptyMessage="No rooms recorded for this building."
              identity={identity}
              label={`Rooms in ${nestedBuilding.name}`}
              maximumBodyHeight="16rem"
              onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/rooms', (values) => ({
                number: values.number, name: values.name, buildingId: nestedBuilding.id, siteId: nestedBuilding.siteId,
              }), (value) => readRecord(value, isRoom), (record) => setRooms((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Room])) : undefined}
              onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, nestedRooms, roomColumns, roomPayload, (row) => `/api/v1/rooms/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isRoom), roomTags, peopleRecordTypes.room, roomWrites, (next) => setRooms((current) => current.map((item) => next.find((candidate) => candidate.id === item.id) ?? item)), (definitionId, recordId, assignment) => applyTagAssignment(setRoomTags, definitionId, recordId, assignment)) : undefined}
              rowId={(row) => row.id}
              rowLabel={(row) => row.name ? `${row.number} · ${row.name}` : row.number}
              rows={nestedRooms}
              viewId={`people-nested-rooms-${nestedBuilding.id}`}
            />
          </section>}
        </section>}
        {!nestedSite && nestedBuilding && <section aria-label={`Nested rooms in ${nestedBuilding.name}`} className={`${subpanelClass} p-3`}>
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h4 className="font-semibold">Rooms in {nestedBuilding.name}</h4>
            <div className="flex flex-wrap gap-2">
              <button className={`${plainButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => { setLocationSheet('rooms'); setNestedBuilding(null) }} type="button">Open as sheet</button>
              <button className={`${secondaryButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => setNestedBuilding(null)} type="button">Close nested sheet</button>
            </div>
          </div>
          <DataGrid
            columns={roomColumns}
            editable={canWriteDirectory || canWriteLabels}
            emptyMessage="No rooms recorded for this building."
            identity={identity}
            label={`Rooms in ${nestedBuilding.name}`}
            maximumBodyHeight="16rem"
            onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/rooms', (values) => ({
              number: values.number, name: values.name, buildingId: nestedBuilding.id, siteId: nestedBuilding.siteId,
            }), (value) => readRecord(value, isRoom), (record) => setRooms((current) => current.some((item) => item.id === record.id) ? current : [...current, record as Room])) : undefined}
            onSaveEdits={canWriteDirectory || canWriteLabels ? (edits) => saveDirectoryEdits(edits, nestedRooms, roomColumns, roomPayload, (row) => `/api/v1/rooms/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isRoom), roomTags, peopleRecordTypes.room, roomWrites, (next) => setRooms((current) => current.map((item) => next.find((candidate) => candidate.id === item.id) ?? item)), (definitionId, recordId, assignment) => applyTagAssignment(setRoomTags, definitionId, recordId, assignment)) : undefined}
            rowId={(row) => row.id}
            rowLabel={(row) => row.name ? `${row.number} · ${row.name}` : row.number}
            rows={nestedRooms}
            viewId={`people-nested-rooms-${nestedBuilding.id}`}
          />
        </section>}
      </section>
      </div>

      <div aria-labelledby="people-tab-references" hidden={activeSection !== 'references'} id="people-panel-references" role="tabpanel">
        {activeSection === 'references' ? (
        <section aria-labelledby="occupancy-heading" className="space-y-4">
          <div>
            <h3 id="occupancy-heading" className="text-lg font-semibold">Location references</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">
              Typed occupancy links show how people use rooms: office, instructor, class, dormitory, and lab. Group the references sheet by location to see room usage. Primary building and room on a directory identity is the person’s home location; these references add secondary and role-specific places.
            </p>
          </div>
          <div aria-label="Location reference sheets" className="flex flex-wrap gap-1 border-b border-white/10" role="tablist">
            {occupancySheets.map((sheet) => {
              const selected = occupancySheet === sheet.id
              return (
                <button
                  aria-controls={`people-occupancy-sheet-${sheet.id}`}
                  aria-selected={selected}
                  className={cx('relative shrink-0 px-3 py-2 text-sm font-medium transition', selected ? 'text-steward-mist' : `${secondaryButtonClass} min-h-0 rounded-none border-transparent bg-transparent px-3 py-2 text-steward-mist-muted`)}
                  id={`people-occupancy-tab-${sheet.id}`}
                  key={sheet.id}
                  onClick={() => setOccupancySheet(sheet.id)}
                  role="tab"
                  title={sheet.description}
                  type="button"
                >
                  {sheet.label}
                  {selected && <span aria-hidden="true" className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-steward-teal" />}
                </button>
              )
            })}
          </div>
          <p className="text-sm text-steward-mist-muted">{occupancySheets.find((sheet) => sheet.id === occupancySheet)?.description}</p>
          <div hidden={occupancySheet !== 'references'} id="people-occupancy-sheet-references" role="tabpanel">
            <DataGrid
              columns={locationReferenceColumns}
              editable={canWriteDirectory}
              emptyMessage="No location references are recorded yet."
              identity={identity}
              label="Location references"
              onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/location-references', (values) => ({
                identityId: values.identityId,
                typeId: values.typeId,
                locationKind: values.locationKind || 'room',
                locationId: values.locationId,
                priority: values.priority || 'secondary',
              }), (value) => readRecord(value, isLocationReference), (record) => setLocationReferences((current) => current.some((item) => item.id === record.id) ? current : [record as LocationReference, ...current])) : undefined}
              onSaveEdits={canWriteDirectory ? (edits) => saveDirectoryEdits(edits, locationReferences, locationReferenceColumns, locationReferencePayload, (row) => `/api/v1/location-references/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isLocationReference), emptyTagContext(csrfToken, false), peopleRecordTypes.identity, locationReferenceWrites, setLocationReferences, () => undefined) : undefined}
              rowId={(row) => row.id}
              rowLabel={(row) => identities.find((item) => item.id === row.identityId)?.displayName ?? row.identityId}
              rowMessage={(row) => locationReferenceWrites.rowMessage(row.id)}
              rowState={(row) => locationReferenceWrites.rowState(row.id)}
              rows={locationReferences}
              selectable
              viewDefaults={{ groupBy: 'locationId', filters: { status: 'active' } }}
              viewId="people-location-references"
            />
            <p className="mt-3 text-sm text-steward-mist-muted" role="status">{locationReferences.length} {locationReferences.length === 1 ? 'reference' : 'references'} loaded (up to 500).</p>
          </div>
          <div hidden={occupancySheet !== 'types'} id="people-occupancy-sheet-types" role="tabpanel">
            <DataGrid
              columns={locationTypeColumns}
              editable={canWriteDirectory}
              emptyMessage="No location reference types are defined yet."
              identity={identity}
              label="Location reference types"
              onCreateRows={canWriteDirectory ? async (drafts) => createDirectoryRows(drafts, '/api/v1/location-reference-types', (values) => ({
                name: values.name,
                description: values.description,
                relationshipKind: values.relationshipKind || 'uses_office',
                locationKind: values.locationKind || 'room',
              }), (value) => readRecord(value, isLocationReferenceType), (record) => setLocationReferenceTypes((current) => current.some((item) => item.id === record.id) ? current : [...current, record as LocationReferenceType])) : undefined}
              onSaveEdits={canWriteDirectory ? (edits) => saveDirectoryEdits(edits, locationReferenceTypes, locationTypeColumns, locationReferenceTypePayload, (row) => `/api/v1/location-reference-types/${encodeURIComponent(row.id)}`, (value) => readRecord(value, isLocationReferenceType), emptyTagContext(csrfToken, false), peopleRecordTypes.identity, locationTypeWrites, setLocationReferenceTypes, () => undefined) : undefined}
              rowId={(row) => row.id}
              rowLabel={(row) => row.name}
              rowMessage={(row) => locationTypeWrites.rowMessage(row.id)}
              rowState={(row) => locationTypeWrites.rowState(row.id)}
              rows={locationReferenceTypes}
              selectable
              viewDefaults={{ groupBy: 'relationshipKind' }}
              viewId="people-location-reference-types"
            />
          </div>
        </section>
        ) : null}
      </div>

      <div aria-labelledby="people-tab-workflows" hidden={activeSection !== 'workflows'} id="people-panel-workflows" role="tabpanel">
      <PersonLocationWorkflow
        buildings={buildings}
        canWrite={canWriteDirectory}
        departments={departments}
        onCreateBuilding={async (siteId, name) => {
          const building = await createBuildingRecord(siteId, name)
          setBuildings((current) => [...current, building])
          return building
        }}
        onCreatePerson={async (draft, location) => {
          const body: Record<string, string> = {
            kind: 'person',
            displayName: draft.displayName,
            email: draft.email,
            siteId: location.siteId,
            status: 'active',
          }
          if (draft.departmentId) body.departmentId = draft.departmentId
          const identity = await createIdentityRecord(body)
          try {
            await loadDirectory(filters)
          } catch {
            setIdentities((current) => current.some((candidate) => candidate.id === identity.id) ? current : [identity, ...current])
          }
          setStatus('Person created from the guided location workflow.')
          return identity
        }}
        onCreateRoom={async (building, number, name) => {
          const room = await createRoomRecord(building, number, name)
          setRooms((current) => [...current, room])
          return room
        }}
        onCreateSite={async (name) => {
          const site = await createSiteRecord(name)
          setSites((current) => [...current, site])
          return site
        }}
        rooms={rooms}
        sites={sites}
      />

      {canWriteDirectory ? (
        <div className="space-y-6">
          <section aria-labelledby="create-locations-heading" data-requirement="REQ-DIRECTORY-EXPANSION-001">
            <h3 className="text-lg font-semibold" id="create-locations-heading">Create location records</h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Create each level in order so rooms always inherit the correct site from their building.</p>
            <div className="mt-4 grid gap-4 xl:grid-cols-3">
              <details className={`${subpanelClass} min-w-0 p-4`}>
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

              <details className={`${subpanelClass} min-w-0 p-4`}>
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

              <details className={`${subpanelClass} min-w-0 p-4`}>
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
              <details className={`${subpanelClass} p-4`}>
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

              <details className={`${subpanelClass} p-4`}>
                <summary className="cursor-pointer font-semibold text-[#58d9c7]">Add a directory identity</summary>
                <form className="mt-4 space-y-4" onSubmit={handleCreateIdentity}>
                  <div>
                    <label className={labelClass} htmlFor="identity-kind">Identity type</label>
                    <select className={inputClass} id="identity-kind" name="identityKind" onChange={(event) => setIdentityKind(event.target.value as IdentityKind)} value={identityKind}>{Object.entries(kindLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
                  </div>
                  <div><label className={labelClass} htmlFor="identity-display-name">Display name</label><input autoComplete="name" className={inputClass} id="identity-display-name" maxLength={200} name="identityDisplayName" required /></div>
                  <div><label className={labelClass} htmlFor="identity-email">Email address {identityKind === 'person' ? '(required)' : '(optional)'}</label>
                    <input autoComplete="email" className={inputClass} id="identity-email" maxLength={320} name="identityEmail" required={identityKind === 'person'} type="email" />
                  </div>
                  <RecordSearchPicker
                    browseLabel="Open departments"
                    create={{
                      label: 'Add department',
                      fields: [
                        { key: 'name', label: 'Department name', required: true },
                        { key: 'siteId', label: 'Site', options: sites.map((site) => ({ id: site.id, label: site.name })), placeholder: 'No site' },
                      ],
                      submit: async (values) => {
                        const body: Record<string, string> = { name: values.name, status: 'active' }
                        if (values.siteId) body.siteId = values.siteId
                        const saved = await requestJSON('/api/v1/departments', {
                          method: 'POST',
                          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
                          body: JSON.stringify(body),
                        })
                        const record = readRecord(saved, isDepartment)
                        setDepartments((current) => [...current, record])
                        return { id: record.id, label: record.name }
                      },
                    }}
                    kind="department"
                    label="Department (optional)"
                    multiple={false}
                    name="identityDepartmentId"
                    onChange={setIdentityDepartment}
                    options={departments.map((department) => ({ id: department.id, label: department.name }))}
                    selected={identityDepartment}
                  />
                  <RecordSearchPicker
                    browseLabel="Open sites"
                    create={{
                      label: 'Add site',
                      fields: [{ key: 'name', label: 'Site name', required: true }],
                      submit: async (values) => {
                        const saved = await requestJSON('/api/v1/sites', {
                          method: 'POST',
                          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
                          body: JSON.stringify({ name: values.name, status: 'active' }),
                        })
                        const record = readRecord(saved, isSite)
                        setSites((current) => [...current, record])
                        return { id: record.id, label: record.name }
                      },
                    }}
                    help="Leave blank to inherit the selected department's site."
                    kind="site"
                    label="Site (optional)"
                    multiple={false}
                    name="identitySiteId"
                    onChange={setIdentitySite}
                    options={sites.map((site) => ({ id: site.id, label: site.name }))}
                    selected={identitySite}
                  />
                  <button className={`${buttonClass} w-full`} disabled={busy !== ''} type="submit">{busy === 'identity' ? 'Creating identity…' : 'Create identity'}</button>
                </form>
              </details>
            </div>
          </section>
        </div>
      ) : <p className="rounded-xl border border-steward-ink-800 p-4 text-sm text-steward-mist-muted">Directory creation controls remain unavailable until an administrator grants <code>directory.write</code>.</p>}

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
              <form aria-label="Create asset assignment" className={`${subpanelClass} mt-4 grid gap-4 p-4 md:grid-cols-2 lg:grid-cols-4`} onSubmit={handleCreateAssignment}>
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

            <div aria-busy={assignmentsLoading} aria-labelledby="assignment-history-heading" className="mt-4" role="region">
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
      </div>

      <div aria-labelledby="people-tab-imports" hidden={activeSection !== 'imports'} id="people-panel-imports" role="tabpanel">
        <DirectoryImportManager csrfToken={csrfToken} onApplied={() => loadDirectory(filters)} permissions={permissions} />
      </div>

      <Drawer
        kicker="People tags"
        onClose={() => setTagColumnFormOpen(false)}
        open={tagColumnFormOpen && canWriteLabels}
        title="Add tag column"
      >
        <form aria-label="Add tag column" className="grid gap-4" key={tagColumnRecordType} onSubmit={handleCreateTagColumn}>
          <p className="text-sm text-steward-mist-muted">
            {tagColumnRecordType === peopleRecordTypes.room
              ? 'Create a tag column for this sheet. Use a text or select tag named Floor when rooms need a floor without a separate record type.'
              : 'Create a tag column for this sheet. The same labels can apply to other People record types from Tags.'}
          </p>
          <label className={labelClass}>Tag name
            <input className={inputClass} defaultValue={tagColumnRecordType === peopleRecordTypes.room ? 'Floor' : ''} maxLength={120} name="name" placeholder={tagColumnRecordType === peopleRecordTypes.room ? 'Floor' : 'Program'} required />
          </label>
          <label className={labelClass}>Tag id
            <span className="mt-1 block font-normal leading-5 text-steward-mist-muted">Optional stable id such as floor.</span>
            <input className={inputClass} defaultValue={tagColumnRecordType === peopleRecordTypes.room ? 'floor' : ''} maxLength={64} name="id" />
          </label>
          <label className={labelClass}>Value type
            <select className={inputClass} defaultValue={tagColumnRecordType === peopleRecordTypes.room ? 'text' : 'multiselect'} name="valueKind">
              {['flag', 'text', 'select', 'multiselect'].map((kind) => <option key={kind} value={kind}>{kind}</option>)}
            </select>
          </label>
          <label className={labelClass}>Allowed values
            <span className="mt-1 block font-normal leading-5 text-steward-mist-muted">Required for select and multi-select tags. Comma separated.</span>
            <input className={inputClass} name="options" placeholder="1, 2, 3, Basement" />
          </label>
          <div className="flex flex-wrap gap-3">
            <button className={buttonClass} disabled={tagColumnBusy} type="submit">{tagColumnBusy ? 'Creating…' : 'Create tag column'}</button>
            <button className={secondaryButtonClass} onClick={() => setTagColumnFormOpen(false)} type="button">Cancel</button>
          </div>
        </form>
      </Drawer>
    </section>
  )
}
