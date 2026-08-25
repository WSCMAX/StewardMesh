// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

export const otherBucket = 'Other'

export type ForecastView = 'fiscal_year' | 'department' | 'asset_class' | 'manufacturer' | 'building' | 'department_type' | 'site' | 'tag' | 'goal'

export type ForecastViewItem = {
  planId: string
  assetId: string
  assetName: string
  scenario: string
  fiscalYear: number
  replacementCostMinor: number
  currency: string
  department: string
  kind: string
  manufacturer: string
  building: string
}

export type NamedBucket = { key: string; label: string; replacementCostMinor: number; assetCount: number }

export type StackedYear = { year: number; label: string; totalMinor: number; series: Record<string, number> }

export type DepartmentTypePie = { key: string; label: string; totalMinor: number; slices: NamedBucket[] }

export function bucketValue(value: string | undefined) {
  const trimmed = value?.trim() ?? ''
  return trimmed || otherBucket
}

export function resolveBucketLabel(value: string, names: ReadonlyMap<string, string>) {
  const key = bucketValue(value)
  if (key === otherBucket) return otherBucket
  return names.get(key) ?? key
}

export function forecastViewGroupBy(view: ForecastView) {
  return view === 'department_type' ? 'fiscal_year' : view
}

export function aggregateNamedBuckets(items: readonly ForecastViewItem[], keyOf: (item: ForecastViewItem) => string, labelOf: (key: string) => string): NamedBucket[] {
  const buckets = new Map<string, NamedBucket & { assets: Set<string> }>()
  for (const item of items) {
    const key = bucketValue(keyOf(item))
    const current = buckets.get(key) ?? { key, label: labelOf(key), replacementCostMinor: 0, assetCount: 0, assets: new Set<string>() }
    current.replacementCostMinor += item.replacementCostMinor
    current.assets.add(item.assetId)
    buckets.set(key, current)
  }
  return [...buckets.values()]
    .map((bucket) => ({ key: bucket.key, label: bucket.label, replacementCostMinor: bucket.replacementCostMinor, assetCount: bucket.assets.size }))
    .sort((left, right) => right.replacementCostMinor - left.replacementCostMinor || left.label.localeCompare(right.label))
}

export function stackByYear(items: readonly ForecastViewItem[], seriesOf: (item: ForecastViewItem) => string): { years: StackedYear[]; series: string[] } {
  const years = new Map<number, StackedYear>()
  const series = new Set<string>()
  for (const item of items) {
    const name = bucketValue(seriesOf(item))
    series.add(name)
    const current = years.get(item.fiscalYear) ?? { year: item.fiscalYear, label: `FY${item.fiscalYear}`, totalMinor: 0, series: {} }
    current.series[name] = (current.series[name] ?? 0) + item.replacementCostMinor
    current.totalMinor += item.replacementCostMinor
    years.set(item.fiscalYear, current)
  }
  return {
    years: [...years.values()].sort((left, right) => left.year - right.year),
    series: [...series].sort((left, right) => left === otherBucket ? 1 : right === otherBucket ? -1 : left.localeCompare(right)),
  }
}

export function piesByDepartment(items: readonly ForecastViewItem[], departmentLabel: (key: string) => string, kindLabel: (key: string) => string): DepartmentTypePie[] {
  const groups = new Map<string, ForecastViewItem[]>()
  for (const item of items) {
    const key = bucketValue(item.department)
    const current = groups.get(key) ?? []
    current.push(item)
    groups.set(key, current)
  }
  return [...groups.entries()]
    .map(([key, rows]) => {
      const slices = aggregateNamedBuckets(rows, (item) => item.kind, kindLabel)
      return {
        key,
        label: departmentLabel(key),
        totalMinor: slices.reduce((sum, slice) => sum + slice.replacementCostMinor, 0),
        slices,
      }
    })
    .sort((left, right) => right.totalMinor - left.totalMinor || left.label.localeCompare(right.label))
}
