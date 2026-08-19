import type { Asset, AssetModel, AssetModelContext } from './AtlasInventory'

export function lifecycleAnchor(asset: Asset): Date | null {
  const raw = asset.lifecycleStartDate || asset.installedDate || asset.purchaseDate
  if (!raw) return null
  const date = new Date(raw)
  return Number.isNaN(date.getTime()) ? null : normalizeDate(date)
}

export function usefulLifeMonths(asset: Asset, replacementModel?: AssetModel | null): number {
  if (asset.modelContext?.usefulLifeMonths && asset.modelContext.usefulLifeMonths > 0) {
    return asset.modelContext.usefulLifeMonths
  }
  if (replacementModel?.usefulLifeMonths && replacementModel.usefulLifeMonths > 0) {
    return replacementModel.usefulLifeMonths
  }
  return 0
}

export function lifecyclePercent(asset: Asset, asOf = new Date(), replacementModel?: AssetModel | null): number | null {
  const anchor = lifecycleAnchor(asset)
  const months = usefulLifeMonths(asset, replacementModel)
  if (!anchor || months <= 0) return null
  const end = addCalendarMonths(anchor, months)
  const today = normalizeDate(asOf)
  if (today.getTime() < anchor.getTime()) return 0
  if (today.getTime() >= end.getTime()) return 100
  const total = end.getTime() - anchor.getTime()
  if (total <= 0) return null
  const elapsed = today.getTime() - anchor.getTime()
  return Math.min(100, Math.max(0, Math.round((elapsed * 100) / total)))
}

export function modelPastLifecycle(model: Pick<AssetModel, 'lastEffectiveDate'> | Pick<AssetModelContext, 'lastEffectiveDate'>, asOf = new Date()): boolean {
  const raw = 'lastEffectiveDate' in model ? model.lastEffectiveDate : undefined
  if (!raw) return false
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return false
  return normalizeDate(date).getTime() <= normalizeDate(asOf).getTime()
}

function normalizeDate(value: Date) {
  return new Date(Date.UTC(value.getUTCFullYear(), value.getUTCMonth(), value.getUTCDate()))
}

function addCalendarMonths(value: Date, months: number) {
  const date = normalizeDate(value)
  return normalizeDate(new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + months, date.getUTCDate())))
}

export function calendarDateText(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString(undefined, { timeZone: 'UTC' })
}
