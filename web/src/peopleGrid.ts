import { requestJSON, type Revision } from './api'
import { lookupExportText, type GridColumn, type LookupConfig, type LookupCreateConfig, type LookupOption } from './grid/columns'
import {
  buildLabelColumns,
  type LabelAssignment,
  type LabelDefinition,
} from './labelsGrid'

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001, REQ-WORKSPACE-001. Features: identity.directory, identity.labels, experience.grid.

export type RecordStatus = 'active' | 'inactive'
export type IdentityKind = 'person' | 'shared' | 'public' | 'lab'

export type SiteAddress = {
  line1?: string
  line2?: string
  city?: string
  region?: string
  postalCode?: string
  country?: string
}

export type Site = {
  id: string
  organizationId: string
  name: string
  address?: SiteAddress
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type Building = {
  id: string
  organizationId: string
  siteId: string
  name: string
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type Room = {
  id: string
  organizationId: string
  siteId: string
  buildingId: string
  number: string
  name?: string
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type Department = {
  id: string
  organizationId: string
  name: string
  siteId?: string
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type Identity = {
  id: string
  organizationId: string
  kind: IdentityKind
  displayName: string
  email?: string
  departmentId?: string
  siteId?: string
  buildingId?: string
  roomId?: string
  status: RecordStatus
  provider?: string
  providerSubject?: string
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type LocationKind = 'site' | 'building' | 'room'
export type LocationPriority = 'primary' | 'secondary'
export type LocationRelationshipKind = 'located_at' | 'uses_office' | 'teaches_in' | 'attends_class' | 'resides_in' | 'uses_lab'

export type LocationReferenceType = {
  id: string
  organizationId: string
  name: string
  description?: string
  relationshipKind: LocationRelationshipKind
  locationKind: LocationKind
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type LocationReference = {
  id: string
  organizationId: string
  identityId: string
  typeId: string
  locationKind: LocationKind
  locationId: string
  priority: LocationPriority
  status: RecordStatus
  revision: Revision
  createdAt: string
  updatedAt: string
}

export const identityKinds: IdentityKind[] = ['person', 'shared', 'public', 'lab']
export const recordStatuses: RecordStatus[] = ['active', 'inactive']
export const locationKinds: LocationKind[] = ['site', 'building', 'room']
export const locationPriorities: LocationPriority[] = ['primary', 'secondary']
export const locationRelationshipKinds: LocationRelationshipKind[] = ['located_at', 'uses_office', 'teaches_in', 'attends_class', 'resides_in', 'uses_lab']
export const kindLabels: Record<IdentityKind, string> = {
  person: 'Person',
  shared: 'Shared identity',
  public: 'Public users',
  lab: 'Computer lab users',
}
export const locationKindLabels: Record<LocationKind, string> = {
  site: 'Site',
  building: 'Building',
  room: 'Room',
}
export const locationPriorityLabels: Record<LocationPriority, string> = {
  primary: 'Primary',
  secondary: 'Secondary',
}
export const locationRelationshipLabels: Record<LocationRelationshipKind, string> = {
  located_at: 'Located at',
  uses_office: 'Uses office',
  teaches_in: 'Teaches in',
  attends_class: 'Attends class',
  resides_in: 'Resides in',
  uses_lab: 'Uses lab',
}

export const peopleRecordTypes = {
  identity: 'people.identity',
  site: 'people.site',
  building: 'people.building',
  room: 'people.room',
  department: 'people.department',
} as const

export type PeopleRecordType = (typeof peopleRecordTypes)[keyof typeof peopleRecordTypes]
export type LocationSheet = 'sites' | 'buildings' | 'rooms' | 'departments'
export type OccupancySheet = 'references' | 'types'

export const locationSheets: { id: LocationSheet; label: string; recordType: PeopleRecordType; description: string }[] = [
  { id: 'sites', label: 'Sites', recordType: peopleRecordTypes.site, description: 'Campuses and other top-level places' },
  { id: 'buildings', label: 'Buildings', recordType: peopleRecordTypes.building, description: 'Buildings grouped under a site' },
  { id: 'rooms', label: 'Rooms', recordType: peopleRecordTypes.room, description: 'Rooms grouped under a building; add a Floor tag for floors' },
  { id: 'departments', label: 'Departments', recordType: peopleRecordTypes.department, description: 'Departments optionally tied to a site' },
]

export const occupancySheets: { id: OccupancySheet; label: string; description: string }[] = [
  { id: 'references', label: 'References', description: 'Primary and secondary occupancy links. Group by room to see usage.' },
  { id: 'types', label: 'Types', description: 'Catalog of occupancy relationships such as office, instructor, class, dormitory, and lab' },
]

type DirectoryRefs = {
  sites: readonly Site[]
  buildings: readonly Building[]
  rooms: readonly Room[]
  departments: readonly Department[]
}

export type PeopleTagContext = {
  csrfToken: string
  canWriteLabels: boolean
  definitions: readonly LabelDefinition[]
  assignments: ReadonlyMap<string, ReadonlyMap<string, LabelAssignment>>
  onDefinitionUpdated: (definition: LabelDefinition) => void
}

export type PeopleColumnContext = DirectoryRefs & {
  csrfToken: string
  canWrite: boolean
  tags: PeopleTagContext
  identities?: readonly Identity[]
  locationTypes?: readonly LocationReferenceType[]
  onCreated: {
    site?: (site: Site) => void
    building?: (building: Building) => void
    room?: (room: Room) => void
    department?: (department: Department) => void
  }
}

function optionFrom(id: string, label: string, detail?: string): LookupOption {
  return { id, label, detail }
}

function siteOption(site: Site): LookupOption {
  return optionFrom(site.id, site.name)
}

function buildingOption(building: Building, sites: readonly Site[]): LookupOption {
  const site = sites.find((item) => item.id === building.siteId)
  return optionFrom(building.id, building.name, site?.name)
}

function roomOption(room: Room, buildings: readonly Building[]): LookupOption {
  const building = buildings.find((item) => item.id === room.buildingId)
  const name = room.name ? `${room.number} · ${room.name}` : room.number
  return optionFrom(room.id, name, building?.name)
}

function departmentOption(department: Department): LookupOption {
  return optionFrom(department.id, department.name)
}

async function searchCollection<T extends { id: string }>(
  path: string,
  query: string,
  read: (item: unknown) => T | null,
  toOption: (item: T) => LookupOption,
): Promise<readonly LookupOption[]> {
  const response = await requestJSON(path)
  const items = Array.isArray((response as { items?: unknown }).items) ? (response as { items: unknown[] }).items : []
  const needle = query.trim().toLowerCase()
  return items.flatMap((item) => {
    const record = read(item)
    if (!record) return []
    const option = toOption(record)
    if (!needle) return [option]
    if (option.label.toLowerCase().includes(needle) || option.id.toLowerCase().includes(needle) || (option.detail ?? '').toLowerCase().includes(needle)) return [option]
    return []
  })
}

function isNamed(value: unknown): value is { id: string; name: string; siteId?: string } {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.name === 'string'
}

async function postRecord<T>(path: string, csrfToken: string, body: Record<string, unknown>, read: (value: unknown) => T | null): Promise<T> {
  const saved = await requestJSON(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({ ...body, status: 'active' }),
  })
  const record = read(saved)
  if (!record) throw new Error('invalid directory response')
  return record
}

function siteCreate(context: PeopleColumnContext): LookupCreateConfig | undefined {
  if (!context.canWrite) return undefined
  return {
    label: 'Add site',
    fields: [{ key: 'name', label: 'Site name', required: true, placeholder: 'Campus or office' }],
    submit: async (values) => {
      const saved = await postRecord('/api/v1/sites', context.csrfToken, { name: values.name }, (item) => isNamed(item) ? item as Site : null)
      context.onCreated.site?.(saved as Site)
      return siteOption(saved as Site)
    },
  }
}

function buildingCreate(context: PeopleColumnContext, siteId?: string): LookupCreateConfig | undefined {
  if (!context.canWrite) return undefined
  return {
    label: 'Add building',
    fields: [
      ...(siteId ? [] : [{ key: 'siteId', label: 'Site', required: true, options: context.sites.map(siteOption), placeholder: 'Select a site' }]),
      { key: 'name', label: 'Building name', required: true },
    ],
    submit: async (values) => {
      const resolvedSite = siteId || values.siteId
      const saved = await postRecord('/api/v1/buildings', context.csrfToken, { siteId: resolvedSite, name: values.name }, (item) => isNamed(item) ? item as Building : null)
      context.onCreated.building?.(saved as Building)
      return buildingOption(saved as Building, context.sites)
    },
  }
}

function departmentCreate(context: PeopleColumnContext): LookupCreateConfig | undefined {
  if (!context.canWrite) return undefined
  return {
    label: 'Add department',
    fields: [
      { key: 'name', label: 'Department name', required: true },
      { key: 'siteId', label: 'Site', options: context.sites.map(siteOption), placeholder: 'No site' },
    ],
    submit: async (values) => {
      const body: Record<string, unknown> = { name: values.name }
      if (values.siteId) body.siteId = values.siteId
      const saved = await postRecord('/api/v1/departments', context.csrfToken, body, (item) => isNamed(item) ? item as Department : null)
      context.onCreated.department?.(saved as Department)
      return departmentOption(saved as Department)
    },
  }
}

function siteLookup(context: PeopleColumnContext): LookupConfig {
  return {
    options: context.sites.map(siteOption),
    search: (query) => searchCollection('/api/v1/sites', query, (item) => isNamed(item) ? item as Site : null, (item) => siteOption(item as Site)),
    create: siteCreate(context),
    browseHref: '#workspace-people',
    browseLabel: 'Open sites',
  }
}

function buildingLookup(context: PeopleColumnContext): LookupConfig {
  return {
    options: context.buildings.map((item) => buildingOption(item, context.sites)),
    search: (query) => searchCollection('/api/v1/buildings', query, (item) => isNamed(item) ? item as Building : null, (item) => buildingOption(item as Building, context.sites)),
    create: buildingCreate(context),
    browseHref: '#workspace-people',
    browseLabel: 'Open buildings',
  }
}

function roomLookup(context: PeopleColumnContext): LookupConfig {
  return {
    options: context.rooms.map((item) => roomOption(item, context.buildings)),
    search: (query) => searchCollection('/api/v1/rooms', query, (item) => isRoomRecord(item) ? item : null, (item) => roomOption(item, context.buildings)),
    create: roomCreate(context),
    browseHref: '#workspace-people',
    browseLabel: 'Open rooms',
  }
}

function isRoomRecord(value: unknown): value is Room {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.number === 'string' && typeof item.buildingId === 'string'
}

function roomCreate(context: PeopleColumnContext): LookupCreateConfig | undefined {
  if (!context.canWrite) return undefined
  return {
    label: 'Add room',
    fields: [
      { key: 'buildingId', label: 'Building', required: true, options: context.buildings.map((item) => buildingOption(item, context.sites)), placeholder: 'Select a building' },
      { key: 'number', label: 'Room number', required: true },
      { key: 'name', label: 'Room name' },
    ],
    submit: async (values) => {
      const building = context.buildings.find((item) => item.id === values.buildingId)
      const saved = await postRecord('/api/v1/rooms', context.csrfToken, {
        buildingId: values.buildingId, siteId: building?.siteId, number: values.number, name: values.name,
      }, (item) => isRoomRecord(item) ? item : null)
      context.onCreated.room?.(saved)
      return roomOption(saved, context.buildings)
    },
  }
}

function statusColumn<T extends { status: RecordStatus }>(editable: boolean): GridColumn<T> {
  return {
    key: 'status', header: 'Status', kind: 'enum', options: recordStatuses, editable, required: true, width: 8,
    text: (row) => row.status,
  }
}

function labelColumns<T extends { id: string }>(tags: PeopleTagContext, editable: boolean): GridColumn<T>[] {
  if (tags.definitions.length === 0) return []
  return buildLabelColumns({
    csrfToken: tags.csrfToken,
    canWriteLabels: tags.canWriteLabels,
    definitions: tags.definitions,
    assignments: tags.assignments,
    onDefinitionUpdated: tags.onDefinitionUpdated,
    rowId: (row) => row.id,
  }, editable)
}

export function buildIdentityColumns(context: PeopleColumnContext, editable: boolean): GridColumn<Identity>[] {
  const departmentLookup: LookupConfig = {
    options: context.departments.map(departmentOption),
    search: (query) => searchCollection('/api/v1/departments', query, (item) => isNamed(item) ? item as Department : null, (item) => departmentOption(item as Department)),
    create: departmentCreate(context),
    browseHref: '#workspace-people',
    browseLabel: 'Open departments',
  }
  return [
    { key: 'displayName', header: 'Display name', kind: 'text', editable, required: true, maxLength: 200, width: 16, text: (row) => row.displayName },
    { key: 'email', header: 'Email', kind: 'text', editable, maxLength: 320, width: 16, text: (row) => row.email ?? '' },
    {
      key: 'kind', header: 'Type', kind: 'enum', options: identityKinds, editable, required: true, width: 10,
      text: (row) => row.kind,
      display: (row) => kindLabels[row.kind] ?? row.kind,
      exportText: (row) => kindLabels[row.kind] ?? row.kind,
    },
    statusColumn<Identity>(editable),
    {
      key: 'departmentId', header: 'Department', kind: 'lookup', editable, width: 12, lookup: departmentLookup,
      text: (row) => row.departmentId ?? '',
      exportText: (row) => lookupExportText(row.departmentId ?? '', departmentLookup.options),
      display: (row) => context.departments.find((item) => item.id === row.departmentId)?.name ?? row.departmentId ?? '',
    },
    {
      key: 'siteId', header: 'Site', kind: 'lookup', editable, width: 12, lookup: siteLookup(context),
      text: (row) => row.siteId ?? '',
      exportText: (row) => lookupExportText(row.siteId ?? '', siteLookup(context).options),
      display: (row) => context.sites.find((item) => item.id === row.siteId)?.name ?? row.siteId ?? '',
    },
    {
      key: 'buildingId', header: 'Building', kind: 'lookup', editable, width: 12, lookup: buildingLookup(context),
      text: (row) => row.buildingId ?? '',
      exportText: (row) => lookupExportText(row.buildingId ?? '', buildingLookup(context).options),
      display: (row) => context.buildings.find((item) => item.id === row.buildingId)?.name ?? row.buildingId ?? '',
    },
    {
      key: 'roomId', header: 'Room', kind: 'lookup', editable, width: 12, lookup: roomLookup(context),
      text: (row) => row.roomId ?? '',
      exportText: (row) => lookupExportText(row.roomId ?? '', roomLookup(context).options),
      display: (row) => {
        const room = context.rooms.find((item) => item.id === row.roomId)
        if (!room) return row.roomId ?? ''
        return room.name ? `${room.number} · ${room.name}` : room.number
      },
    },
    ...labelColumns(context.tags, editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildSiteColumns(context: PeopleColumnContext, editable: boolean): GridColumn<Site>[] {
  return [
    { key: 'name', header: 'Site', kind: 'text', editable, required: true, maxLength: 200, width: 14, text: (row) => row.name },
    statusColumn<Site>(editable),
    { key: 'line1', header: 'Address line 1', kind: 'text', editable, maxLength: 200, width: 14, text: (row) => row.address?.line1 ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), line1: text } } },
    { key: 'line2', header: 'Address line 2', kind: 'text', editable, maxLength: 200, width: 12, text: (row) => row.address?.line2 ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), line2: text } } },
    { key: 'city', header: 'City', kind: 'text', editable, maxLength: 100, width: 10, text: (row) => row.address?.city ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), city: text } } },
    { key: 'region', header: 'Region', kind: 'text', editable, maxLength: 100, width: 8, text: (row) => row.address?.region ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), region: text } } },
    { key: 'postalCode', header: 'Postal code', kind: 'text', editable, maxLength: 32, width: 8, text: (row) => row.address?.postalCode ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), postalCode: text } } },
    { key: 'country', header: 'Country', kind: 'text', editable, maxLength: 2, width: 6, text: (row) => row.address?.country ?? '', toPayload: (draft, text) => { draft.address = { ...(draft.address as SiteAddress | undefined), country: text.toUpperCase() } } },
    ...labelColumns(context.tags, editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildBuildingColumns(context: PeopleColumnContext, editable: boolean): GridColumn<Building>[] {
  const lookup = siteLookup(context)
  return [
    { key: 'name', header: 'Building', kind: 'text', editable, required: true, maxLength: 200, width: 14, text: (row) => row.name },
    {
      key: 'siteId', header: 'Site', kind: 'lookup', editable, required: true, width: 12, lookup,
      text: (row) => row.siteId,
      exportText: (row) => lookupExportText(row.siteId, lookup.options),
      display: (row) => context.sites.find((item) => item.id === row.siteId)?.name ?? row.siteId,
    },
    statusColumn<Building>(editable),
    ...labelColumns(context.tags, editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildRoomColumns(context: PeopleColumnContext, editable: boolean): GridColumn<Room>[] {
  const buildingLookup: LookupConfig = {
    options: context.buildings.map((item) => buildingOption(item, context.sites)),
    search: (query) => searchCollection('/api/v1/buildings', query, (item) => isNamed(item) ? item as Building : null, (item) => buildingOption(item as Building, context.sites)),
    create: buildingCreate(context),
    browseHref: '#workspace-people',
    browseLabel: 'Open buildings',
  }
  return [
    { key: 'number', header: 'Room number', kind: 'text', editable, required: true, maxLength: 100, width: 10, text: (row) => row.number },
    { key: 'name', header: 'Room name', kind: 'text', editable, maxLength: 200, width: 14, text: (row) => row.name ?? '' },
    {
      key: 'buildingId', header: 'Building', kind: 'lookup', editable, required: true, width: 12, lookup: buildingLookup,
      text: (row) => row.buildingId,
      exportText: (row) => lookupExportText(row.buildingId, buildingLookup.options),
      display: (row) => context.buildings.find((item) => item.id === row.buildingId)?.name ?? row.buildingId,
    },
    {
      key: 'siteId', header: 'Site', kind: 'lookup', width: 12, lookup: siteLookup(context),
      text: (row) => row.siteId,
      exportText: (row) => lookupExportText(row.siteId, siteLookup(context).options),
      display: (row) => context.sites.find((item) => item.id === row.siteId)?.name ?? row.siteId,
    },
    statusColumn<Room>(editable),
    ...labelColumns(context.tags, editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildDepartmentColumns(context: PeopleColumnContext, editable: boolean): GridColumn<Department>[] {
  const lookup = siteLookup(context)
  return [
    { key: 'name', header: 'Department', kind: 'text', editable, required: true, maxLength: 200, width: 14, text: (row) => row.name },
    {
      key: 'siteId', header: 'Site', kind: 'lookup', editable, width: 12, lookup,
      text: (row) => row.siteId ?? '',
      exportText: (row) => lookupExportText(row.siteId ?? '', lookup.options),
      display: (row) => context.sites.find((item) => item.id === row.siteId)?.name ?? row.siteId ?? '',
    },
    statusColumn<Department>(editable),
    ...labelColumns(context.tags, editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildLocationReferenceTypeColumns(editable: boolean): GridColumn<LocationReferenceType>[] {
  return [
    { key: 'name', header: 'Type', kind: 'text', editable, required: true, maxLength: 200, width: 14, text: (row) => row.name },
    { key: 'description', header: 'Description', kind: 'text', editable, maxLength: 500, width: 18, text: (row) => row.description ?? '' },
    {
      key: 'relationshipKind', header: 'Relationship', kind: 'enum', options: locationRelationshipKinds, editable, required: true, width: 12,
      text: (row) => row.relationshipKind,
      display: (row) => locationRelationshipLabels[row.relationshipKind] ?? row.relationshipKind,
      exportText: (row) => locationRelationshipLabels[row.relationshipKind] ?? row.relationshipKind,
    },
    {
      key: 'locationKind', header: 'Location kind', kind: 'enum', options: locationKinds, editable, required: true, width: 10,
      text: (row) => row.locationKind,
      display: (row) => locationKindLabels[row.locationKind] ?? row.locationKind,
      exportText: (row) => locationKindLabels[row.locationKind] ?? row.locationKind,
    },
    statusColumn<LocationReferenceType>(editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

export function buildLocationReferenceColumns(context: PeopleColumnContext, editable: boolean): GridColumn<LocationReference>[] {
  const identities = context.identities ?? []
  const types = context.locationTypes ?? []
  const identityLookup: LookupConfig = {
    options: identities.map((item) => optionFrom(item.id, item.displayName, kindLabels[item.kind])),
    search: async (query) => {
      const needle = query.trim().toLowerCase()
      return identities.flatMap((item) => {
        if (needle && !item.displayName.toLowerCase().includes(needle) && !(item.email ?? '').toLowerCase().includes(needle)) return []
        return [optionFrom(item.id, item.displayName, kindLabels[item.kind])]
      })
    },
    browseHref: '#workspace-people',
    browseLabel: 'Open directory',
  }
  const typeLookup: LookupConfig = {
    options: types.map((item) => optionFrom(item.id, item.name, locationRelationshipLabels[item.relationshipKind])),
    search: async (query) => {
      const needle = query.trim().toLowerCase()
      return types.flatMap((item) => {
        if (needle && !item.name.toLowerCase().includes(needle) && !item.relationshipKind.includes(needle)) return []
        return [optionFrom(item.id, item.name, locationRelationshipLabels[item.relationshipKind])]
      })
    },
    browseHref: '#workspace-people',
    browseLabel: 'Open types',
  }
  const locationLookup: LookupConfig = {
    options: occupancyLocationOptions(context),
    search: async (query) => occupancyLocationOptions(context).filter((option) => {
      const needle = query.trim().toLowerCase()
      if (!needle) return true
      return option.label.toLowerCase().includes(needle) || option.id.toLowerCase().includes(needle) || (option.detail ?? '').toLowerCase().includes(needle)
    }),
    browseHref: '#workspace-people',
    browseLabel: 'Open locations',
  }
  return [
    {
      key: 'identityId', header: 'Person', kind: 'lookup', editable, required: true, width: 14, lookup: identityLookup,
      text: (row) => row.identityId,
      exportText: (row) => lookupExportText(row.identityId, identityLookup.options),
      display: (row) => identities.find((item) => item.id === row.identityId)?.displayName ?? row.identityId,
    },
    {
      key: 'typeId', header: 'Reference type', kind: 'lookup', editable, required: true, width: 14, lookup: typeLookup,
      text: (row) => row.typeId,
      exportText: (row) => lookupExportText(row.typeId, typeLookup.options),
      display: (row) => types.find((item) => item.id === row.typeId)?.name ?? row.typeId,
    },
    {
      key: 'locationKind', header: 'Location kind', kind: 'enum', options: locationKinds, editable, required: true, width: 10,
      text: (row) => row.locationKind,
      display: (row) => locationKindLabels[row.locationKind] ?? row.locationKind,
      exportText: (row) => locationKindLabels[row.locationKind] ?? row.locationKind,
    },
    {
      key: 'locationId', header: 'Location', kind: 'lookup', editable, required: true, width: 14, lookup: locationLookup,
      text: (row) => row.locationId,
      exportText: (row) => lookupExportText(row.locationId, locationLookup.options),
      display: (row) => occupancyLocationLabel(context, row.locationKind, row.locationId),
    },
    {
      key: 'priority', header: 'Priority', kind: 'enum', options: locationPriorities, editable, required: true, width: 10,
      text: (row) => row.priority,
      display: (row) => locationPriorityLabels[row.priority] ?? row.priority,
      exportText: (row) => locationPriorityLabels[row.priority] ?? row.priority,
    },
    statusColumn<LocationReference>(editable),
    { key: 'id', header: 'Record ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.id },
  ]
}

function occupancyLocationOptions(context: PeopleColumnContext): LookupOption[] {
  return [
    ...context.sites.map((item) => optionFrom(item.id, item.name, 'Site')),
    ...context.buildings.map((item) => optionFrom(item.id, item.name, 'Building')),
    ...context.rooms.map((item) => roomOption(item, context.buildings)),
  ]
}

function occupancyLocationLabel(context: PeopleColumnContext, kind: LocationKind, id: string) {
  if (kind === 'site') return context.sites.find((item) => item.id === id)?.name ?? id
  if (kind === 'building') return context.buildings.find((item) => item.id === id)?.name ?? id
  const room = context.rooms.find((item) => item.id === id)
  if (!room) return id
  return room.name ? `${room.number} · ${room.name}` : room.number
}

export function identityPayload(identity: Identity): Record<string, unknown> {
  return {
    kind: identity.kind,
    displayName: identity.displayName,
    email: identity.email ?? '',
    departmentId: identity.departmentId ?? '',
    siteId: identity.siteId ?? '',
    buildingId: identity.buildingId ?? '',
    roomId: identity.roomId ?? '',
    status: identity.status,
    revision: identity.revision,
  }
}

export function sitePayload(site: Site): Record<string, unknown> {
  return {
    name: site.name,
    status: site.status,
    revision: site.revision,
    address: { ...(site.address ?? {}) },
  }
}

export function buildingPayload(building: Building): Record<string, unknown> {
  return { name: building.name, siteId: building.siteId, status: building.status, revision: building.revision }
}

export function roomPayload(room: Room): Record<string, unknown> {
  return {
    number: room.number,
    name: room.name ?? '',
    buildingId: room.buildingId,
    siteId: room.siteId,
    status: room.status,
    revision: room.revision,
  }
}

export function departmentPayload(department: Department): Record<string, unknown> {
  return { name: department.name, siteId: department.siteId ?? '', status: department.status, revision: department.revision }
}

export function locationReferenceTypePayload(item: LocationReferenceType): Record<string, unknown> {
  return {
    name: item.name,
    description: item.description ?? '',
    relationshipKind: item.relationshipKind,
    locationKind: item.locationKind,
    status: item.status,
    revision: item.revision,
  }
}

export function locationReferencePayload(item: LocationReference): Record<string, unknown> {
  return {
    identityId: item.identityId,
    typeId: item.typeId,
    locationKind: item.locationKind,
    locationId: item.locationId,
    priority: item.priority,
    status: item.status,
    revision: item.revision,
  }
}

export function emptyTagContext(csrfToken: string, canWriteLabels: boolean): PeopleTagContext {
  return {
    csrfToken,
    canWriteLabels,
    definitions: [],
    assignments: new Map(),
    onDefinitionUpdated: () => undefined,
  }
}
