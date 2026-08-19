export function fiscalYearForDate(value: string, startMonth: number) {
  const date = new Date(value)
  const month = date.getUTCMonth() + 1
  const year = date.getUTCFullYear()
  if (startMonth === 1 || month < startMonth) return year
  return year + 1
}

export function fiscalMonthInYear(value: string, startMonth: number) {
  const date = new Date(value)
  const month = date.getUTCMonth() + 1
  if (startMonth === 1) return month
  return month >= startMonth ? month - startMonth + 1 : month + (12 - startMonth) + 1
}

export function replacementDateFromPlan(plan?: { replacementDate?: string; derivedReplacementDate?: string } | null) {
  return plan?.replacementDate ?? plan?.derivedReplacementDate ?? null
}

export function addCalendarMonthsUTC(value: string, months: number) {
  const date = new Date(value)
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + months, date.getUTCDate())).toISOString()
}

export function fiscalYearStartISO(fiscalYear: number, startMonth: number) {
  const year = startMonth === 1 ? fiscalYear : fiscalYear - 1
  return `${year}-${String(startMonth).padStart(2, '0')}-01T00:00:00.000Z`
}

export function monthsBetween(fromISO: string, toISO: string) {
  const from = new Date(fromISO)
  const to = new Date(toISO)
  return (to.getUTCFullYear() - from.getUTCFullYear()) * 12 + (to.getUTCMonth() - from.getUTCMonth())
}

export function inferDeploymentName(assetName: string) {
  const match = assetName.match(/^(.+?)\s+(?:Station|Workstation|Terminal|Seat|Pod|Device)\s+\d+/i)
  return match?.[1]?.trim() ?? null
}

export type DeploymentGroup<T> = {
  key: string
  label: string
  items: T[]
  totalCostMinor: number
}

type DeploymentAssetRef = {
  roomId?: string
  siteId?: string
  departmentId?: string
  deploymentNotes?: string
  name?: string
}

export function deploymentGroupKey(asset: DeploymentAssetRef | undefined) {
  if (!asset) return 'ungrouped'
  if (asset.roomId) return `room:${asset.roomId}`
  const notes = asset.deploymentNotes?.trim()
  if (notes) return `deployment:${notes.slice(0, 120)}`
  if (asset.siteId && asset.departmentId) return `office:${asset.siteId}:${asset.departmentId}`
  return 'ungrouped'
}

export function deploymentGroupLabel(key: string, sampleAsset: DeploymentAssetRef | undefined) {
  if (key === 'ungrouped') return 'Ungrouped assets'
  if (key.startsWith('room:')) {
    const inferred = sampleAsset?.name ? inferDeploymentName(sampleAsset.name) : null
    if (inferred) return inferred
    const notes = sampleAsset?.deploymentNotes?.trim()
    if (notes) return notes.split(';')[0]?.trim() ?? 'Lab deployment'
    return 'Lab deployment'
  }
  if (key.startsWith('office:')) return 'Office deployment'
  if (key.startsWith('deployment:')) return key.slice('deployment:'.length)
  return 'Deployment group'
}

export function groupByDeployment<T extends { assetId: string; replacementCostMinor: number; assetName: string }>(
  items: readonly T[],
  assetFor: (assetId: string) => DeploymentAssetRef | undefined,
) {
  const buckets = new Map<string, T[]>()
  for (const item of items) {
    const key = deploymentGroupKey(assetFor(item.assetId))
    const bucket = buckets.get(key) ?? []
    bucket.push(item)
    buckets.set(key, bucket)
  }
  return [...buckets.entries()].map(([key, groupItems]) => {
    const sorted = [...groupItems].sort((left, right) => left.assetName.localeCompare(right.assetName))
    const sample = assetFor(sorted[0]?.assetId ?? '')
    return {
      key,
      label: deploymentGroupLabel(key, sample),
      items: sorted,
      totalCostMinor: sorted.reduce((sum, item) => sum + item.replacementCostMinor, 0),
    } satisfies DeploymentGroup<T>
  }).sort((left, right) => left.label.localeCompare(right.label))
}
