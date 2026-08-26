// Requirements: REQ-ATLAS-001, REQ-ATLAS-CODES-001. Features: inventory.assets, inventory.identifiers.

// Manufacturer and property labels often print the serial, internal asset tag,
// and model/MTM as separate barcodes on the same surface. Classification is a
// best-effort hint; the operator can reassign a captured value.

export type IdentityField = 'serialNumber' | 'assetTag' | 'modelNumber'

export type DeviceIdentity = {
  serialNumber: string
  assetTag: string
  modelNumber: string
}

const emptyIdentity: DeviceIdentity = { serialNumber: '', assetTag: '', modelNumber: '' }

const lenovoFactoryPrefix = /^1S([0-9]{2}[A-Z0-9]{8})([A-Z0-9]{5,})$/i
const machineTypeModel = /^[0-9]{2}[A-Z0-9]{8}$/i
const numericAssetTag = /^[0-9]{3,12}$/
const identityKeys: Record<IdentityField, string[]> = {
  serialNumber: ['serial', 'serialnumber', 'sn', 's/n', 'ser'],
  assetTag: ['assettag', 'asset', 'tag', 'property', 'inventory'],
  modelNumber: ['modelnumber', 'model', 'mtm', 'machinetype', 'product'],
}

export function emptyDeviceIdentity(): DeviceIdentity {
  return { ...emptyIdentity }
}

export function modelIdentifierFromScan(rawValue: string) {
  const parsed = parseIdentityPayload(rawValue)
  return (parsed.modelNumber || rawValue).trim()
}

export function classifyIdentityValue(rawValue: string): IdentityField {
  const value = rawValue.trim()
  if (numericAssetTag.test(value)) return 'assetTag'
  if (machineTypeModel.test(value)) return 'modelNumber'
  return 'serialNumber'
}

export function parseIdentityPayload(rawValue: string): Partial<DeviceIdentity> {
  const value = rawValue.trim()
  if (!value) return {}
  const factory = value.match(lenovoFactoryPrefix)
  if (factory) {
    return { modelNumber: factory[1].toUpperCase(), serialNumber: factory[2].toUpperCase() }
  }
  const fromUrl = parseIdentityURL(value)
  if (fromUrl) return fromUrl
  const fromPairs = parseIdentityPairs(value)
  if (fromPairs) return fromPairs
  return { [classifyIdentityValue(value)]: value }
}

export function applyIdentityScan(current: DeviceIdentity, rawValue: string): DeviceIdentity {
  const parsed = parseIdentityPayload(rawValue)
  const next = { ...current }
  for (const field of ['serialNumber', 'assetTag', 'modelNumber'] as const) {
    const value = parsed[field]?.trim()
    if (value) next[field] = value
  }
  return next
}

export function assignIdentityField(current: DeviceIdentity, field: IdentityField, rawValue: string): DeviceIdentity {
  const value = rawValue.trim()
  const next = { ...current }
  for (const key of ['serialNumber', 'assetTag', 'modelNumber'] as const) {
    if (key !== field && next[key] === value) next[key] = ''
  }
  next[field] = value
  return next
}

export type IdentityAssignment = IdentityField | 'ignore'

export type CapturedIdentityValue = {
  id: string
  raw: string
  value: string
  suggested: IdentityField
  assigned: IdentityAssignment
}

export const identityFieldLabels: Record<IdentityField, string> = {
  serialNumber: 'Manufacturer serial',
  assetTag: 'Asset tag',
  modelNumber: 'Model number',
}

export function identityValuesEqual(left: string, right?: string) {
  return left.trim().toLowerCase() === (right ?? '').trim().toLowerCase()
}

export function capturedValuesFromScan(rawValue: string): CapturedIdentityValue[] {
  const parsed = parseIdentityPayload(rawValue)
  const entries: CapturedIdentityValue[] = []
  for (const field of ['serialNumber', 'assetTag', 'modelNumber'] as const) {
    const value = parsed[field]?.trim()
    if (!value) continue
    entries.push({
      id: `${field}:${value.toLowerCase()}`,
      raw: rawValue.trim(),
      value,
      suggested: field,
      assigned: field,
    })
  }
  return entries
}

export function mergeCapturedValues(current: CapturedIdentityValue[], incoming: CapturedIdentityValue[]): CapturedIdentityValue[] {
  const next = [...current]
  for (const item of incoming) {
    const existing = next.find((entry) => identityValuesEqual(entry.value, item.value))
    if (existing) continue
    const fieldTaken = next.some((entry) => entry.assigned === item.assigned)
    next.push(fieldTaken ? { ...item, assigned: 'ignore' } : item)
  }
  return next
}

export function identityFromCaptured(values: CapturedIdentityValue[]): DeviceIdentity {
  const next = emptyDeviceIdentity()
  for (const item of values) {
    if (item.assigned === 'ignore') continue
    next[item.assigned] = item.value
  }
  return next
}

export function assignCapturedValue(values: CapturedIdentityValue[], id: string, assigned: IdentityAssignment): CapturedIdentityValue[] {
  return values.map((item) => {
    if (item.id === id) return { ...item, assigned }
    if (assigned !== 'ignore' && item.assigned === assigned) return { ...item, assigned: 'ignore' }
    return item
  })
}

export function duplicateIdentityAssignment(values: CapturedIdentityValue[]): IdentityField | null {
  const seen = new Set<IdentityField>()
  for (const item of values) {
    if (item.assigned === 'ignore') continue
    if (seen.has(item.assigned)) return item.assigned
    seen.add(item.assigned)
  }
  return null
}

export function preferredModelIdentifier(rawValues: readonly string[]) {
  const captured = rawValues.flatMap((value) => capturedValuesFromScan(value))
  const model = captured.find((item) => item.suggested === 'modelNumber' || item.assigned === 'modelNumber')
  if (model) return model.value
  return rawValues[0] ? modelIdentifierFromScan(rawValues[0]) : ''
}

export function modelNumberDecisionNeeded(scanned: string, stored?: string) {
  const value = scanned.trim()
  if (!value) return false
  return !identityValuesEqual(value, stored ?? '')
}

function parseIdentityURL(value: string): Partial<DeviceIdentity> | null {
  if (!/^https?:\/\//i.test(value)) return null
  try {
    const url = new URL(value)
    const collected: Partial<DeviceIdentity> = {}
    for (const [key, entry] of url.searchParams.entries()) {
      const field = identityFieldForKey(key)
      if (field && entry.trim()) collected[field] = entry.trim()
    }
    const pathParts = url.pathname.split('/').map((part) => decodeURIComponent(part)).filter(Boolean)
    for (let index = 0; index < pathParts.length - 1; index += 1) {
      const field = identityFieldForKey(pathParts[index])
      const entry = pathParts[index + 1]
      if (field && entry && !collected[field]) collected[field] = entry
    }
    return Object.keys(collected).length > 0 ? collected : null
  } catch {
    return null
  }
}

function parseIdentityPairs(value: string): Partial<DeviceIdentity> | null {
  if (!/[:=]/.test(value) && !/\b(s\/n|mtm|asset)\b/i.test(value)) return null
  const collected: Partial<DeviceIdentity> = {}
  const pattern = /\b(s\/n|sn|serial(?:\s*number)?|mtm|model(?:\s*number)?|asset(?:\s*tag)?|tag)\b\s*[:=#-]?\s*([A-Z0-9][A-Z0-9._-]{1,63})/gi
  for (const match of value.matchAll(pattern)) {
    const field = identityFieldForKey(match[1])
    if (field) collected[field] = match[2]
  }
  return Object.keys(collected).length > 0 ? collected : null
}

function identityFieldForKey(key: string): IdentityField | undefined {
  const normalized = key.toLowerCase().replace(/[\s/_-]+/g, '')
  return (Object.keys(identityKeys) as IdentityField[]).find((field) => identityKeys[field].includes(normalized)
    || identityKeys[field].includes(key.toLowerCase()))
}
