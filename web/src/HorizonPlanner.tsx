import { type FormEvent, type ReactNode, useEffect, useId, useMemo, useRef, useState } from 'react'
import type { Asset, AssetModel } from './AtlasInventory'
import { isAssetModel } from './AtlasInventory'
import DataGrid from './grid/DataGrid'
import { calendarText, dollarsFromMinor, type GridColumn } from './grid/columns'
import type { GridIdentity } from './grid/viewState'
import Drawer from './grid/Drawer'
import { addCalendarMonthsUTC, fiscalMonthInYear, fiscalYearForDate, fiscalYearStartISO, groupByDeployment, replacementDateFromPlan } from './horizonPlanning'
import { calendarDateText, lifecyclePercent, modelPastLifecycle } from './lifecyclePlanning'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import HorizonForecastCharts from './HorizonForecastCharts'
import HorizonReplacementPlans from './HorizonReplacementPlans'
import HorizonSectionNav, { type HorizonSection } from './HorizonSectionNav'
import { forecastViewGroupBy, otherBucket, resolveBucketLabel, type ForecastView, type ForecastViewItem } from './horizonForecastViews'
import { ProductHeader, buttonClass, cx, inputClass, panelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

type LifecycleStage = 'planned' | 'in_service' | 'refresh_due' | 'approved' | 'retired'
type GroupBy = 'fiscal_year' | 'department' | 'site' | 'tag' | 'goal' | 'asset_class' | 'manufacturer' | 'building'
type AmountKind = 'planned' | 'actual' | 'estimated' | 'committed' | 'normalized_real' | 'tco'

type HorizonPlan = {
  id: string
  assetId: string
  scenario: string
  expectedUsefulLifeMonths: number
  replacementDate?: string
  derivedReplacementDate?: string
  lifecycleStage: LifecycleStage
  replacementCostMinor: number
  currency: string
  effectiveFrom: string
  revision: Revision
}

type HorizonPlanVersion = Omit<HorizonPlan, 'id'> & {
  planId: string
  recordedAt: string
  actorId: string
}

type ForecastGroup = {
  key: string
  label: string
  scenario: string
  plannedReplacementMinor: number
  assetCount: number
  amountsByKindMinor: Record<string, number>
}

type ForecastGroupAsset = {
  planId: string
  assetId: string
  assetName: string
  lifecycleStage: LifecycleStage
  expectedUsefulLifeMonths: number
  replacementDate?: string
  derivedReplacementDate?: string
  fiscalYear: number
  replacementCostMinor: number
  currency: string
  revision: Revision
}

type ForecastGroupAssets = {
  scenario: string
  groupKey: string
  label: string
  groupBy: GroupBy
  currency: string
  items: ForecastGroupAsset[]
}

type ForecastAmountItem = {
  assetId: string
  assetName: string
  planId?: string
  costId?: string
  description: string
  fiscalYear: number
  fiscalPeriod?: string
  scenario: string
  amountMinor: number
  currency: string
  kind: AmountKind
}

type ForecastAmountBreakdown = {
  amountKind: AmountKind
  scenario?: string
  groupKey?: string
  label: string
  groupBy: GroupBy
  currency: string
  totalMinor: number
  itemCount: number
  items: ForecastAmountItem[]
}

type HorizonForecast = {
  asOf: string
  groupBy: GroupBy
  currency: string
  scenarios: string[]
  plannedReplacementMinor: number
  assetCount: number
  totalsByKindMinor: Record<string, number>
  groups: ForecastGroup[]
  items?: ForecastViewItem[]
}

type KindDefault = {
  organizationId: string
  assetKind: string
  scenario: string
  expectedUsefulLifeMonths: number
  replacementModelId?: string
  revision: Revision
  createdAt: string
  updatedAt: string
}

type DueNowRow = {
  plan: HorizonPlan
  asset: Asset
  linkedModel?: AssetModel
  reason: string
}

type HorizonPlannerProps = {
  csrfToken: string
  permissions: readonly string[]
  assets: readonly Asset[]
  identity?: GridIdentity | null
  onOpenHelp?: () => void
  onOpenAtlasInventory?: (assetIds: readonly string[], label: string, fiscalYearStartMonth: number) => void
}

const lifecycleStages: LifecycleStage[] = ['planned', 'in_service', 'refresh_due', 'approved', 'retired']
const groupOptions: GroupBy[] = ['fiscal_year', 'department', 'site', 'tag', 'goal', 'asset_class', 'manufacturer', 'building']
const forecastViews: { id: ForecastView; label: string }[] = [
  { id: 'fiscal_year', label: 'Year' },
  { id: 'department', label: 'Department' },
  { id: 'asset_class', label: 'Type' },
  { id: 'manufacturer', label: 'Manufacturer' },
  { id: 'building', label: 'Building' },
  { id: 'department_type', label: 'Department + type' },
]
const costKinds = ['actual', 'estimated', 'committed', 'normalized_real', 'tco'] as const
const amountKinds: readonly AmountKind[] = ['planned', ...costKinds]
const maximumReplacementCost = '90071992547409.91'
const initialYear = new Date().getFullYear()
const initialAsOf = localDateTimeValue(new Date())


function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isSafeNonNegativeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function isLifecycleStage(value: unknown): value is LifecycleStage {
  return typeof value === 'string' && lifecycleStages.includes(value as LifecycleStage)
}

function isGroupBy(value: unknown): value is GroupBy {
  return typeof value === 'string' && groupOptions.includes(value as GroupBy)
}

function isAmounts(value: unknown): value is Record<string, number> {
  return isObject(value) && Object.values(value).every(isSafeNonNegativeInteger)
}

function isPlan(value: unknown): value is HorizonPlan {
  if (!isObject(value)) return false
  return typeof value.id === 'string' && value.id.length > 0
    && typeof value.assetId === 'string' && value.assetId.length > 0
    && typeof value.scenario === 'string' && value.scenario.length > 0
    && isSafeNonNegativeInteger(value.expectedUsefulLifeMonths) && value.expectedUsefulLifeMonths >= 1 && value.expectedUsefulLifeMonths <= 1200
    && (value.replacementDate === undefined || typeof value.replacementDate === 'string')
    && (value.derivedReplacementDate === undefined || typeof value.derivedReplacementDate === 'string')
    && isLifecycleStage(value.lifecycleStage)
    && isSafeNonNegativeInteger(value.replacementCostMinor)
    && typeof value.currency === 'string' && /^[A-Z]{3}$/.test(value.currency)
    && typeof value.effectiveFrom === 'string' && value.effectiveFrom.length > 0
    && isRevision(value.revision)
}

function isPlanVersion(value: unknown): value is HorizonPlanVersion {
  if (!isObject(value)) return false
  return typeof value.planId === 'string' && value.planId.length > 0
    && typeof value.assetId === 'string' && value.assetId.length > 0
    && typeof value.scenario === 'string' && value.scenario.length > 0
    && isSafeNonNegativeInteger(value.expectedUsefulLifeMonths) && value.expectedUsefulLifeMonths >= 1 && value.expectedUsefulLifeMonths <= 1200
    && (value.replacementDate === undefined || typeof value.replacementDate === 'string')
    && (value.derivedReplacementDate === undefined || typeof value.derivedReplacementDate === 'string')
    && isLifecycleStage(value.lifecycleStage)
    && isSafeNonNegativeInteger(value.replacementCostMinor)
    && typeof value.currency === 'string' && /^[A-Z]{3}$/.test(value.currency)
    && typeof value.effectiveFrom === 'string' && value.effectiveFrom.length > 0
    && isRevision(value.revision)
    && typeof value.recordedAt === 'string' && value.recordedAt.length > 0
    && typeof value.actorId === 'string' && value.actorId.length > 0
}

function responseItems(value: unknown, keys: string[]): unknown[] {
  if (Array.isArray(value)) return value
  if (!isObject(value)) return []
  for (const key of keys) {
    if (Array.isArray(value[key])) return value[key]
  }
  return []
}

function namedValue(names: ReadonlyMap<string, string>, id?: string) {
  if (!id) return ''
  return names.get(id) ?? id
}

function modelLabel(model: AssetModel) {
  return `${model.manufacturer} ${model.name}`.trim()
}

function successorLabel(model: AssetModel, models: readonly AssetModel[]) {
  const successor = models.find((item) => item.id === model.replacementModelId)
  return successor ? modelLabel(successor) : model.replacementModelId ?? 'Unknown'
}

function dueNowColumns(
  models: readonly AssetModel[],
  siteNames: ReadonlyMap<string, string>,
  buildingNames: ReadonlyMap<string, string>,
  roomNames: ReadonlyMap<string, string>,
  departmentNames: ReadonlyMap<string, string>,
  formatMoney: (minor: number, currency: string) => string,
): GridColumn<DueNowRow>[] {
  return [
    { key: 'name', header: 'Asset', kind: 'text', width: 16, text: (row) => row.asset.name },
    {
      key: 'model', header: 'Model', kind: 'text', width: 14,
      text: (row) => row.linkedModel ? modelLabel(row.linkedModel) : row.asset.modelId ?? '',
      display: (row) => {
        const name = row.linkedModel ? modelLabel(row.linkedModel) : row.asset.modelId ?? ''
        if (!name) return ''
        return row.linkedModel?.status === 'retired' ? `${name} · retired model` : name
      },
    },
    { key: 'criticality', header: 'Criticality', kind: 'number', width: 8, text: (row) => row.asset.criticalityScore ? String(row.asset.criticalityScore) : '' },
    { key: 'reason', header: 'Reason', kind: 'text', width: 22, text: (row) => row.reason },
    {
      key: 'successor', header: 'Successor', kind: 'text', width: 14,
      text: (row) => row.linkedModel?.replacementModelId ? successorLabel(row.linkedModel, models) : '',
    },
    {
      key: 'replacementCost', header: 'Replacement cost', kind: 'money', align: 'right', width: 10,
      text: (row) => dollarsFromMinor(row.plan.replacementCostMinor),
      display: (row) => formatMoney(row.plan.replacementCostMinor, row.plan.currency),
    },
    { key: 'assetId', header: 'Asset ID', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.asset.id },
    { key: 'assetTag', header: 'Asset tag', kind: 'text', width: 10, hiddenByDefault: true, text: (row) => row.asset.assetTag ?? '' },
    { key: 'serialNumber', header: 'Serial number', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.asset.serialNumber ?? '' },
    { key: 'kind', header: 'Kind', kind: 'text', width: 8, hiddenByDefault: true, text: (row) => row.asset.kind },
    { key: 'status', header: 'Status', kind: 'text', width: 8, hiddenByDefault: true, text: (row) => row.asset.status },
    { key: 'hostname', header: 'Hostname', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => row.asset.hostname ?? '' },
    { key: 'site', header: 'Site', kind: 'text', width: 10, hiddenByDefault: true, text: (row) => namedValue(siteNames, row.asset.siteId) },
    { key: 'building', header: 'Building', kind: 'text', width: 10, hiddenByDefault: true, text: (row) => namedValue(buildingNames, row.asset.buildingId) },
    { key: 'room', header: 'Room', kind: 'text', width: 10, hiddenByDefault: true, text: (row) => namedValue(roomNames, row.asset.roomId) },
    { key: 'department', header: 'Department', kind: 'text', width: 12, hiddenByDefault: true, text: (row) => namedValue(departmentNames, row.asset.departmentId) },
    { key: 'purchaseDate', header: 'Purchase date', kind: 'date', width: 9, hiddenByDefault: true, text: (row) => calendarText(row.asset.purchaseDate) },
    { key: 'lifecycleStartDate', header: 'Lifecycle start', kind: 'date', width: 9, hiddenByDefault: true, text: (row) => calendarText(row.asset.lifecycleStartDate) },
    { key: 'installedDate', header: 'Installed date', kind: 'date', width: 9, hiddenByDefault: true, text: (row) => calendarText(row.asset.installedDate) },
    {
      key: 'unitCost', header: 'Unit cost', kind: 'money', align: 'right', width: 8, hiddenByDefault: true,
      text: (row) => dollarsFromMinor(row.asset.unitCostMinor),
      display: (row) => row.asset.unitCostMinor ? formatMoney(row.asset.unitCostMinor, row.asset.currency ?? row.plan.currency) : '',
    },
    { key: 'deploymentNotes', header: 'Deployment notes', kind: 'text', width: 16, hiddenByDefault: true, text: (row) => row.asset.deploymentNotes ?? '' },
    { key: 'lifecycleStage', header: 'Plan stage', kind: 'text', width: 10, hiddenByDefault: true, text: (row) => row.plan.lifecycleStage },
    { key: 'usefulLife', header: 'Useful life', kind: 'number', width: 8, hiddenByDefault: true, text: (row) => String(row.plan.expectedUsefulLifeMonths) },
    {
      key: 'replacementDate', header: 'Replacement date', kind: 'date', width: 10, hiddenByDefault: true,
      text: (row) => calendarText(row.plan.replacementDate ?? row.plan.derivedReplacementDate),
    },
  ]
}

function namedRecordMap(value: unknown) {
  const map = new Map<string, string>()
  for (const item of responseItems(value, ['items'])) {
    if (isObject(item) && typeof item.id === 'string' && typeof item.name === 'string' && item.name.trim()) {
      map.set(item.id, item.name)
    }
  }
  return map
}

function parsePlans(value: unknown): HorizonPlan[] {
  const items = responseItems(value, ['items', 'plans'])
  if (!items.every(isPlan)) throw new Error('invalid Horizon plans response')
  return items
}

function parseSavedPlan(value: unknown): HorizonPlan {
  const candidate = isObject(value) && value.plan !== undefined ? value.plan : value
  if (!isPlan(candidate)) throw new Error('invalid Horizon plan response')
  return candidate
}

function parseHistory(value: unknown): HorizonPlanVersion[] {
  const items = responseItems(value, ['items', 'versions'])
  if (!items.every(isPlanVersion)) throw new Error('invalid Horizon history response')
  return items
}

function isKindDefault(value: unknown): value is KindDefault {
  if (!isObject(value)) return false
  return typeof value.assetKind === 'string' && typeof value.scenario === 'string'
    && isSafeNonNegativeInteger(value.expectedUsefulLifeMonths) && value.expectedUsefulLifeMonths >= 1
    && isRevision(value.revision)
}

function parseKindDefaults(value: unknown): KindDefault[] {
  const items = responseItems(value, ['items'])
  if (!items.every(isKindDefault)) throw new Error('invalid Horizon kind defaults response')
  return items
}

function parseForecast(value: unknown): HorizonForecast {
  if (!isObject(value) || typeof value.asOf !== 'string' || value.asOf.length === 0
    || !isGroupBy(value.groupBy) || typeof value.currency !== 'string'
    || !Array.isArray(value.scenarios) || !value.scenarios.every((item) => typeof item === 'string' && item.length > 0)
    || !isSafeNonNegativeInteger(value.plannedReplacementMinor) || !isSafeNonNegativeInteger(value.assetCount)
    || !isAmounts(value.totalsByKindMinor) || !Array.isArray(value.groups)) {
    throw new Error('invalid Horizon forecast response')
  }
  const groups = value.groups
  if (!groups.every((item) => isObject(item) && typeof item.key === 'string' && typeof item.label === 'string'
    && typeof item.scenario === 'string' && item.scenario.length > 0
    && isSafeNonNegativeInteger(item.plannedReplacementMinor)
    && isSafeNonNegativeInteger(item.assetCount) && isAmounts(item.amountsByKindMinor))) {
    throw new Error('invalid Horizon forecast response')
  }
  if (value.items !== undefined && (!Array.isArray(value.items) || !value.items.every(isForecastViewItem))) {
    throw new Error('invalid Horizon forecast response')
  }
  return value as HorizonForecast
}

function isForecastViewItem(value: unknown): value is ForecastViewItem {
  if (!isObject(value)) return false
  return typeof value.planId === 'string' && typeof value.assetId === 'string' && typeof value.assetName === 'string'
    && typeof value.scenario === 'string' && Number.isInteger(value.fiscalYear)
    && isSafeNonNegativeInteger(value.replacementCostMinor) && typeof value.currency === 'string'
    && typeof value.department === 'string' && typeof value.kind === 'string'
    && typeof value.manufacturer === 'string' && typeof value.building === 'string'
}

function isForecastGroupAsset(value: unknown): value is ForecastGroupAsset {
  if (!isObject(value)) return false
  return typeof value.planId === 'string' && typeof value.assetId === 'string' && typeof value.assetName === 'string'
    && isLifecycleStage(value.lifecycleStage) && isSafeNonNegativeInteger(value.expectedUsefulLifeMonths)
    && (value.replacementDate === undefined || typeof value.replacementDate === 'string')
    && (value.derivedReplacementDate === undefined || typeof value.derivedReplacementDate === 'string')
    && Number.isInteger(value.fiscalYear) && isSafeNonNegativeInteger(value.replacementCostMinor)
    && typeof value.currency === 'string' && isRevision(value.revision)
}

function parseForecastGroupAssets(value: unknown): ForecastGroupAssets {
  if (!isObject(value) || typeof value.scenario !== 'string' || typeof value.groupKey !== 'string'
    || typeof value.label !== 'string' || !isGroupBy(value.groupBy) || typeof value.currency !== 'string'
    || !Array.isArray(value.items) || !value.items.every(isForecastGroupAsset)) {
    throw new Error('invalid Horizon forecast group assets response')
  }
  return value as ForecastGroupAssets
}

function isAmountKind(value: unknown): value is AmountKind {
  return typeof value === 'string' && (amountKinds as readonly string[]).includes(value)
}

function isForecastAmountItem(value: unknown): value is ForecastAmountItem {
  if (!isObject(value)) return false
  return typeof value.assetId === 'string' && typeof value.assetName === 'string'
    && (value.planId === undefined || typeof value.planId === 'string')
    && (value.costId === undefined || typeof value.costId === 'string')
    && typeof value.description === 'string' && Number.isInteger(value.fiscalYear)
    && (value.fiscalPeriod === undefined || typeof value.fiscalPeriod === 'string')
    && typeof value.scenario === 'string' && isSafeNonNegativeInteger(value.amountMinor)
    && typeof value.currency === 'string' && isAmountKind(value.kind)
}

function parseForecastAmountBreakdown(value: unknown): ForecastAmountBreakdown {
  if (!isObject(value) || !isAmountKind(value.amountKind)
    || (value.scenario !== undefined && typeof value.scenario !== 'string')
    || (value.groupKey !== undefined && typeof value.groupKey !== 'string')
    || typeof value.label !== 'string' || !isGroupBy(value.groupBy) || typeof value.currency !== 'string'
    || !isSafeNonNegativeInteger(value.totalMinor) || !isSafeNonNegativeInteger(value.itemCount)
    || !Array.isArray(value.items) || !value.items.every(isForecastAmountItem)) {
    throw new Error('invalid Horizon forecast amount breakdown response')
  }
  return value as ForecastAmountBreakdown
}

function label(value: string) {
  return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function money(minor: number, currency: string) {
  if (!currency) return `${minor.toLocaleString()} minor units`
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(minor / 100)
  } catch {
    return `${currency} ${(minor / 100).toFixed(2)}`
  }
}

function minorUnits(value: FormDataEntryValue | null) {
  const normalized = String(value ?? '').trim()
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) throw new Error('Enter a non-negative replacement cost with at most two decimal places.')
  const [whole, fraction = ''] = normalized.split('.')
  const amount = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(amount)) throw new Error('The replacement cost is too large.')
  return amount
}

function timestampValue(value: FormDataEntryValue | null) {
  const normalized = String(value ?? '').trim()
  const timestamp = new Date(normalized)
  if (!normalized || Number.isNaN(timestamp.getTime())) throw new Error('Enter a valid effective date and time.')
  return timestamp.toISOString()
}

function dateTimestampValue(value: FormDataEntryValue | null) {
  const normalized = String(value ?? '').trim()
  if (!/^\d{4}-\d{2}-\d{2}$/.test(normalized)) throw new Error('Enter a valid effective date.')
  return `${normalized}T00:00:00Z`
}

function localDateTimeValue(value: string | Date) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

function displayDate(value: string, includeTime = false) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  if (!includeTime) return date.toLocaleDateString(undefined, { timeZone: 'UTC' })
  return date.toLocaleString()
}

function primaryScenario(value: string) {
  return value.split(',').map((item) => item.trim().toLowerCase()).find(Boolean) ?? 'baseline'
}

function reportPath(base: '/api/v1/horizon/forecast' | '/api/v1/horizon/forecast/assets' | '/api/v1/horizon/forecast/amounts' | '/api/v1/horizon/export.csv', scenarios: string, asOf: string, fromYear: number, toYear: number, fiscalYearStartMonth: number, groupBy: GroupBy) {
  const parameters = new URLSearchParams()
  parameters.set('scenarios', scenarios.split(',').map((item) => item.trim().toLowerCase()).filter(Boolean).join(','))
  parameters.set('asOf', timestampValue(asOf))
  parameters.set('fromYear', String(fromYear))
  parameters.set('toYear', String(toYear))
  parameters.set('fiscalYearStartMonth', String(fiscalYearStartMonth))
  parameters.set('groupBy', groupBy)
  if (base === '/api/v1/horizon/forecast') parameters.set('includeItems', 'true')
  return `${base}?${parameters.toString()}`
}

function validateReportInputs(scenarios: string, asOf: string, fromYear: number, toYear: number, fiscalYearStartMonth: number) {
  const values = scenarios.split(',').map((item) => item.trim().toLowerCase()).filter(Boolean)
  if (values.length < 1 || values.length > 5 || values.some((value) => !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(value))) throw new Error('Enter one through five valid comma-separated scenarios.')
  timestampValue(asOf)
  if (!Number.isInteger(fromYear) || !Number.isInteger(toYear) || fromYear < 1970 || toYear > 9999 || fromYear > toYear || toYear - fromYear > 50) throw new Error('Enter a valid inclusive forecast range of no more than 50 years.')
  if (!Number.isInteger(fiscalYearStartMonth) || fiscalYearStartMonth < 1 || fiscalYearStartMonth > 12) throw new Error('Fiscal year start month must be from 1 through 12.')
}

function validReportPath(base: '/api/v1/horizon/forecast' | '/api/v1/horizon/export.csv', scenarios: string, asOf: string, fromYear: number, toYear: number, fiscalYearStartMonth: number, groupBy: GroupBy) {
  try {
    validateReportInputs(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth)
    return reportPath(base, scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy)
  } catch {
    return null
  }
}

function forecastGroupAssetsPath(scenarios: string, asOf: string, fromYear: number, toYear: number, fiscalYearStartMonth: number, groupBy: GroupBy, scenario: string, groupKey: string) {
  const parameters = new URLSearchParams(reportPath('/api/v1/horizon/forecast/assets', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy).split('?')[1])
  parameters.set('scenario', scenario)
  parameters.set('groupKey', groupKey)
  return `/api/v1/horizon/forecast/assets?${parameters.toString()}`
}

function forecastAmountBreakdownPath(scenarios: string, asOf: string, fromYear: number, toYear: number, fiscalYearStartMonth: number, groupBy: GroupBy, amountKind: AmountKind, group?: Pick<ForecastGroup, 'scenario' | 'key'>) {
  const parameters = new URLSearchParams(reportPath('/api/v1/horizon/forecast/amounts', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy).split('?')[1])
  parameters.set('amountKind', amountKind)
  if (group) {
    parameters.set('scenario', group.scenario)
    parameters.set('groupKey', group.key)
  }
  return `/api/v1/horizon/forecast/amounts?${parameters.toString()}`
}

function planReplacementDate(item: Pick<ForecastGroupAsset, 'replacementDate' | 'derivedReplacementDate'>) {
  return replacementDateFromPlan(item)
}

function simulateFiscalYearShift(
  groups: readonly ForecastGroup[],
  scenario: string,
  selected: readonly ForecastGroupAsset[],
  shiftMonths: number,
  fiscalYearStartMonth: number,
) {
  const before = new Map<string, number>()
  for (const group of groups) {
    if (group.scenario === scenario) before.set(group.key, group.plannedReplacementMinor)
  }
  const after = new Map(before)
  for (const item of selected) {
    const fromKey = `FY${item.fiscalYear}`
    after.set(fromKey, (after.get(fromKey) ?? 0) - item.replacementCostMinor)
    const replacement = planReplacementDate(item)
    if (!replacement) continue
    const toKey = `FY${fiscalYearForDate(addCalendarMonthsUTC(replacement, shiftMonths), fiscalYearStartMonth)}`
    after.set(toKey, (after.get(toKey) ?? 0) + item.replacementCostMinor)
  }
  return { before, after }
}


function forecastGroupLabel(
  forecast: HorizonForecast | null,
  departmentNames: ReadonlyMap<string, string>,
  buildingNames: ReadonlyMap<string, string>,
  group: ForecastGroup,
) {
  if (forecast?.groupBy === 'department') return resolveBucketLabel(group.key, departmentNames)
  if (forecast?.groupBy === 'building') return resolveBucketLabel(group.key, buildingNames)
  if (forecast?.groupBy === 'asset_class') return group.key === otherBucket ? otherBucket : label(group.key)
  return group.label
}

const amountBreakdownColumns: GridColumn<ForecastAmountItem>[] = [
  {
    key: 'asset', header: 'Asset', kind: 'text', width: 16, wrap: true,
    text: (item) => `${item.assetName} ${item.assetId}`,
    display: (item) => <><strong>{item.assetName}</strong><span className="mt-1 block text-xs text-steward-mist-muted">{item.assetId}</span></>,
  },
  { key: 'description', header: 'Description', kind: 'text', width: 18, wrap: true, text: (item) => item.description },
  { key: 'fiscalYear', header: 'Fiscal year', kind: 'text', width: 10, text: (item) => item.fiscalPeriod || `FY${item.fiscalYear}` },
  { key: 'scenario', header: 'Scenario', kind: 'text', width: 10, text: (item) => label(item.scenario) },
  { key: 'amount', header: 'Amount', kind: 'money', width: 10, text: (item) => money(item.amountMinor, item.currency) },
]

const historyColumns: GridColumn<HorizonPlanVersion>[] = [
  { key: 'revision', header: 'Revision', kind: 'number', width: 8, text: (version) => String(version.revision) },
  { key: 'effectiveFrom', header: 'Effective from', kind: 'date', width: 12, text: (version) => version.effectiveFrom, display: (version) => displayDate(version.effectiveFrom) },
  { key: 'recordedAt', header: 'Recorded', kind: 'instant', width: 14, text: (version) => version.recordedAt, display: (version) => displayDate(version.recordedAt, true) },
  { key: 'stage', header: 'Stage', kind: 'text', width: 10, text: (version) => label(version.lifecycleStage) },
  { key: 'usefulLife', header: 'Useful life', kind: 'text', width: 10, text: (version) => `${version.expectedUsefulLifeMonths} months` },
  {
    key: 'replacementDate', header: 'Replacement date', kind: 'date', width: 14,
    text: (version) => version.replacementDate ?? version.derivedReplacementDate ?? '',
    display: (version) => version.replacementDate ? displayDate(version.replacementDate) : version.derivedReplacementDate ? `${displayDate(version.derivedReplacementDate)} (derived)` : 'Not dated',
  },
  { key: 'cost', header: 'Cost', kind: 'money', width: 10, text: (version) => money(version.replacementCostMinor, version.currency) },
  { key: 'actor', header: 'Actor', kind: 'text', width: 12, text: (version) => version.actorId },
]

export default function HorizonPlanner({ csrfToken, permissions, assets, identity, onOpenHelp, onOpenAtlasInventory }: HorizonPlannerProps) {
  const canRead = permissions.includes('planning.read')
  const canWrite = permissions.includes('planning.write')
  const [plans, setPlans] = useState<HorizonPlan[]>([])
  const [forecast, setForecast] = useState<HorizonForecast | null>(null)
  const [scenarios, setScenarios] = useState('baseline')
  const [asOf, setAsOf] = useState(initialAsOf)
  const [fromYear, setFromYear] = useState(initialYear)
  const [toYear, setToYear] = useState(initialYear + 10)
  const [fiscalYearStartMonth, setFiscalYearStartMonth] = useState(1)
  const [groupBy, setGroupBy] = useState<GroupBy>('fiscal_year')
  const [forecastView, setForecastView] = useState<ForecastView>('fiscal_year')
  const [departmentNames, setDepartmentNames] = useState<ReadonlyMap<string, string>>(() => new Map())
  const [buildingNames, setBuildingNames] = useState<ReadonlyMap<string, string>>(() => new Map())
  const [siteNames, setSiteNames] = useState<ReadonlyMap<string, string>>(() => new Map())
  const [roomNames, setRoomNames] = useState<ReadonlyMap<string, string>>(() => new Map())
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<HorizonPlan | null>(null)
  const [historyPlan, setHistoryPlan] = useState<HorizonPlan | null>(null)
  const [history, setHistory] = useState<HorizonPlanVersion[]>([])
  const [kindDefaults, setKindDefaults] = useState<KindDefault[]>([])
  const [models, setModels] = useState<AssetModel[]>([])
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [selectedGroup, setSelectedGroup] = useState<ForecastGroup | null>(null)
  const [groupAssets, setGroupAssets] = useState<ForecastGroupAssets | null>(null)
  const [selectedAmount, setSelectedAmount] = useState<{ kind: AmountKind; group?: ForecastGroup } | null>(null)
  const [amountBreakdown, setAmountBreakdown] = useState<ForecastAmountBreakdown | null>(null)
  const [selectedPlanIDs, setSelectedPlanIDs] = useState<readonly string[]>([])
  const [shiftMonths, setShiftMonths] = useState(12)
  const [section, setSection] = useState<HorizonSection>('queue')
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  useEffect(() => {
    if (!canRead) return
    let active = true
    const forecastURL = reportPath('/api/v1/horizon/forecast', 'baseline', initialAsOf, initialYear, initialYear + 10, 1, 'fiscal_year')
    Promise.all([
      requestJSON('/api/v1/horizon/plans?scenario=baseline'),
      requestJSON(forecastURL),
      requestJSON('/api/v1/horizon/kind-defaults?scenario=baseline'),
      requestJSON('/api/v1/asset-models?limit=100&includeRetired=true'),
    ]).then(([plansValue, forecastValue, kindDefaultsValue, modelsValue]) => {
      if (!active) return
      setPlans(parsePlans(plansValue))
      setForecast(parseForecast(forecastValue))
      setKindDefaults(parseKindDefaults(kindDefaultsValue))
      const modelItems = responseItems(modelsValue, ['items'])
      setModels(modelItems.filter(isAssetModel))
    }).catch(() => {
      if (active) showError('Horizon plans and forecast could not be loaded.')
    })
    return () => { active = false }
  }, [canRead])

  useEffect(() => {
    if (!canRead) return
    let active = true
    Promise.all([
      requestJSON('/api/v1/departments').catch(() => ({ items: [] })),
      requestJSON('/api/v1/buildings').catch(() => ({ items: [] })),
      requestJSON('/api/v1/sites').catch(() => ({ items: [] })),
      requestJSON('/api/v1/rooms').catch(() => ({ items: [] })),
    ]).then(([departmentsValue, buildingsValue, sitesValue, roomsValue]) => {
      if (!active) return
      setDepartmentNames(namedRecordMap(departmentsValue))
      setBuildingNames(namedRecordMap(buildingsValue))
      setSiteNames(namedRecordMap(sitesValue))
      setRoomNames(namedRecordMap(roomsValue))
    })
    return () => { active = false }
  }, [canRead])

  function showError(value: string) {
    setMessage('')
    setError(value)
    queueMicrotask(() => errorRef.current?.focus())
  }

  async function loadCurrentReport() {
    validateReportInputs(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth)
    const [plansValue, forecastValue] = await Promise.all([
      requestJSON(`/api/v1/horizon/plans?scenario=${encodeURIComponent(primaryScenario(scenarios))}`),
      requestJSON(reportPath('/api/v1/horizon/forecast', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy)),
    ])
    setPlans(parsePlans(plansValue))
    setForecast(parseForecast(forecastValue))
  }

  async function refreshReport() {
    setBusy('refresh')
    setError('')
    setMessage('')
    try {
      await loadCurrentReport()
      setMessage('Horizon forecast refreshed.')
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Horizon forecast could not be refreshed.')
    } finally {
      setBusy('')
    }
  }

  function applyForecastView(view: ForecastView) {
    const nextGroup = forecastViewGroupBy(view)
    setForecastView(view)
    setGroupBy(nextGroup)
    setBusy('refresh')
    setError('')
    setMessage('')
    try {
      validateReportInputs(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth)
    } catch (cause) {
      setBusy('')
      showError(cause instanceof Error ? cause.message : 'Forecast controls are invalid.')
      return
    }
    void requestJSON(reportPath('/api/v1/horizon/forecast', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, nextGroup))
      .then((forecastValue) => {
        setForecast(parseForecast(forecastValue))
        setMessage(`Showing forecast by ${view === 'department_type' ? 'department and type' : label(nextGroup).toLowerCase()}.`)
      })
      .catch((cause) => {
        showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Horizon forecast could not be refreshed.')
      })
      .finally(() => setBusy(''))
  }

  function closeGroupDrawer() {
    setSelectedGroup(null)
    setGroupAssets(null)
    setSelectedPlanIDs([])
  }

  function closeAmountDrawer() {
    setSelectedAmount(null)
    setAmountBreakdown(null)
  }

  async function openForecastGroup(group: ForecastGroup) {
    closeAmountDrawer()
    setBusy(`group-${group.scenario}-${group.key}`)
    setError('')
    setMessage('')
    setSelectedGroup(group)
    setGroupAssets(null)
    setSelectedPlanIDs([])
    try {
      validateReportInputs(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth)
      const value = await requestJSON(forecastGroupAssetsPath(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy, group.scenario, group.key))
      const loaded = parseForecastGroupAssets(value)
      setGroupAssets(loaded)
      setSelectedPlanIDs(loaded.items.map((item) => item.planId))
    } catch (cause) {
      closeGroupDrawer()
      if (cause instanceof ApiRequestError && cause.status === 404) {
        showError('Forecast group drill-down is unavailable on this API server. Restart dev services to load the latest Horizon changes.')
      } else {
        showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Forecast group assets could not be loaded.')
      }
    } finally {
      setBusy('')
    }
  }

  async function openAmountBreakdown(kind: AmountKind, group?: ForecastGroup) {
    closeGroupDrawer()
    setBusy(`amount-${kind}-${group?.scenario ?? 'all'}-${group?.key ?? 'all'}`)
    setError('')
    setMessage('')
    setSelectedAmount({ kind, group })
    setAmountBreakdown(null)
    try {
      validateReportInputs(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth)
      const value = await requestJSON(forecastAmountBreakdownPath(scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy, kind, group ? { scenario: group.scenario, key: group.key } : undefined))
      setAmountBreakdown(parseForecastAmountBreakdown(value))
    } catch (cause) {
      closeAmountDrawer()
      if (cause instanceof ApiRequestError && cause.status === 404) {
        showError('Forecast amount breakdown is unavailable on this API server. Restart dev services to load the latest Horizon changes.')
      } else {
        showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Forecast amount items could not be loaded.')
      }
    } finally {
      setBusy('')
    }
  }

  function openAmountInAtlas() {
    if (!amountBreakdown || !onOpenAtlasInventory) return
    const ids = [...new Set(amountBreakdown.items.map((item) => item.assetId))]
    onOpenAtlasInventory(ids, `${label(amountBreakdown.amountKind)} · ${amountBreakdown.label}`, fiscalYearStartMonth)
    closeAmountDrawer()
  }

  function openGroupInAtlas(assetIds?: readonly string[], scopeLabel?: string) {
    if (!groupAssets || !onOpenAtlasInventory) return
    const ids = assetIds ?? groupAssets.items.map((item) => item.assetId)
    const scope = scopeLabel ?? `${groupAssets.label} · ${label(groupAssets.scenario)}`
    onOpenAtlasInventory(ids, scope, fiscalYearStartMonth)
    closeGroupDrawer()
  }

  function toggleDeploymentSelection(planIDs: readonly string[]) {
    const allSelected = planIDs.length > 0 && planIDs.every((planID) => selectedPlanIDs.includes(planID))
    setSelectedPlanIDs((current) => allSelected
      ? current.filter((planID) => !planIDs.includes(planID))
      : [...new Set([...current, ...planIDs])])
  }

  function selectedForecastFiscalYear() {
    const match = selectedGroup?.key.match(/^FY(\d{4})$/)
    return match ? Number(match[1]) : null
  }

  async function applyReplacementUpdates(updates: { item: ForecastGroupAsset; replacementDate: string }[], successMessage: string) {
    if (!groupAssets || !canWrite || updates.length === 0) return
    setBusy('normalize')
    setError('')
    setMessage('')
    try {
      const effectiveFrom = `${new Date().toISOString().slice(0, 10)}T00:00:00Z`
      for (const { item, replacementDate } of updates) {
        await requestJSON(`/api/v1/horizon/plans/${encodeURIComponent(item.planId)}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify({
            assetId: item.assetId,
            scenario: groupAssets.scenario,
            expectedUsefulLifeMonths: item.expectedUsefulLifeMonths,
            replacementDate,
            lifecycleStage: item.lifecycleStage,
            replacementCostMinor: item.replacementCostMinor,
            currency: item.currency,
            effectiveFrom,
            revision: item.revision,
          }),
        })
      }
      await loadCurrentReport()
      closeGroupDrawer()
      setMessage(successMessage)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Replacement dates could not be updated.')
    } finally {
      setBusy('')
    }
  }

  async function applyShiftToItems(items: readonly ForecastGroupAsset[], months: number) {
    const updates = items.flatMap((item) => {
      const replacement = planReplacementDate(item)
      if (!replacement) return []
      return [{ item, replacementDate: addCalendarMonthsUTC(replacement, months) }]
    })
    if (updates.length === 0) throw new Error('Selected assets have no replacement date to shift.')
    await applyReplacementUpdates(updates, `Shifted ${updates.length} replacement ${updates.length === 1 ? 'date' : 'dates'} by ${months > 0 ? '+' : ''}${months} months.`)
  }

  async function alignDeploymentGroup(items: readonly ForecastGroupAsset[]) {
    const fiscalYear = selectedForecastFiscalYear()
    if (fiscalYear === null) throw new Error('Align to FY requires a fiscal year forecast row.')
    const target = fiscalYearStartISO(fiscalYear, fiscalYearStartMonth)
    const updates = items.flatMap((item) => planReplacementDate(item) ? [{ item, replacementDate: target }] : [])
    if (updates.length === 0) throw new Error('This deployment group has no dated replacements to align.')
    await applyReplacementUpdates(updates, `Aligned ${updates.length} replacement ${updates.length === 1 ? 'date' : 'dates'} to the start of ${selectedGroup?.label ?? `FY${fiscalYear}`}.`)
  }

  async function applyCycleShift() {
    if (!groupAssets || shiftMonths === 0) return
    const selected = groupAssets.items.filter((item) => selectedPlanIDs.includes(item.planId))
    await applyShiftToItems(selected, shiftMonths)
  }

  function openCreate() {
    setEditing(null)
    setFormOpen(true)
    setSection('plans')
    setError('')
    setMessage('')
  }

  function openEdit(plan: HorizonPlan) {
    setEditing(plan)
    setFormOpen(true)
    setSection('plans')
    setError('')
    setMessage('')
  }

  function closeForm() {
    setEditing(null)
    setFormOpen(false)
  }

  async function savePlan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('save')
    setError('')
    setMessage('')
    try {
      const replacementDate = String(values.get('replacementDate') ?? '').trim()
      const usefulLifeRaw = String(values.get('expectedUsefulLifeMonths') ?? '').trim()
      const usefulLife = usefulLifeRaw === '' ? 0 : Number(usefulLifeRaw)
      if (usefulLifeRaw !== '' && (!Number.isInteger(usefulLife) || usefulLife < 1 || usefulLife > 1200)) {
        throw new Error('Expected useful life must be from 1 through 1200 months.')
      }
      const currency = String(values.get('currency') ?? '').trim().toUpperCase()
      if (!/^[A-Z]{3}$/.test(currency)) throw new Error('Enter a three-letter ISO currency code.')
      const payload: Record<string, unknown> = {
        assetId: String(values.get('assetId') ?? '').trim(),
        scenario: String(values.get('scenario') ?? '').trim().toLowerCase(),
        lifecycleStage: String(values.get('lifecycleStage') ?? ''),
        replacementCostMinor: minorUnits(values.get('replacementCost')),
        currency,
        effectiveFrom: dateTimestampValue(values.get('effectiveFrom')),
      }
      if (usefulLifeRaw !== '') payload.expectedUsefulLifeMonths = usefulLife
      if (replacementDate) payload.replacementDate = `${replacementDate}T00:00:00Z`
      if (editing) payload.revision = editing.revision
      const savedValue = await requestJSON(editing ? `/api/v1/horizon/plans/${encodeURIComponent(editing.id)}` : '/api/v1/horizon/plans', {
        method: editing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      const saved = parseSavedPlan(savedValue)
      setPlans((current) => [...current.filter((plan) => plan.id !== saved.id), saved].sort((left, right) => assetName(assets, left.assetId).localeCompare(assetName(assets, right.assetId))))
      setEditing(null)
      setFormOpen(false)
      setMessage(editing ? 'Lifecycle plan updated.' : 'Lifecycle plan created.')
      const forecastValue = await requestJSON(reportPath('/api/v1/horizon/forecast', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy))
      setForecast(parseForecast(forecastValue))
      form.reset()
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The lifecycle plan could not be saved.')
    } finally {
      setBusy('')
    }
  }

  async function saveKindDefault(event: FormEvent<HTMLFormElement>, assetKind: string, revision?: Revision) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setBusy(`kind-${assetKind}`)
    setError('')
    setMessage('')
    try {
      const usefulLife = Number(values.get('expectedUsefulLifeMonths'))
      if (!Number.isInteger(usefulLife) || usefulLife < 1 || usefulLife > 1200) throw new Error('Expected useful life must be from 1 through 1200 months.')
      const payload: Record<string, unknown> = {
        assetKind,
        scenario: 'baseline',
        expectedUsefulLifeMonths: usefulLife,
      }
      if (revision) payload.revision = revision
      const savedValue = await requestJSON('/api/v1/horizon/kind-defaults', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      if (!isKindDefault(savedValue)) throw new Error('invalid kind default response')
      setKindDefaults((current) => [...current.filter((item) => item.assetKind !== savedValue.assetKind || item.scenario !== savedValue.scenario), savedValue].sort((left, right) => left.assetKind.localeCompare(right.assetKind)))
      setMessage(`Lifecycle defaults saved for ${label(assetKind)} assets.`)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Kind defaults could not be saved.')
    } finally {
      setBusy('')
    }
  }

  async function toggleHistory(plan: HorizonPlan) {
    if (historyPlan?.id === plan.id) {
      setHistoryPlan(null)
      setHistory([])
      return
    }
    setBusy(`history-${plan.id}`)
    setError('')
    setMessage('')
    try {
      const value = await requestJSON(`/api/v1/horizon/plans/${encodeURIComponent(plan.id)}/history`)
      setHistory(parseHistory(value))
      setHistoryPlan(plan)
      setMessage(`Version history loaded for ${assetName(assets, plan.assetId)}.`)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Plan history could not be loaded.')
    } finally {
      setBusy('')
    }
  }

  const assetsById = useMemo(() => new Map(assets.map((item) => [item.id, item])), [assets])
  const deploymentGroups = useMemo(() => {
    if (!groupAssets) return []
    return groupByDeployment(groupAssets.items, (assetId) => assetsById.get(assetId))
  }, [assetsById, groupAssets])


  const forecastColumns = useMemo((): GridColumn<ForecastGroup>[] => [
    {
      key: 'group', header: 'Group', kind: 'text', width: 16, wrap: true,
      text: (group) => forecastGroupLabel(forecast, departmentNames, buildingNames, group),
      display: (group) => <button className="rounded-md text-left font-medium text-[#a9c7ff] underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-steward-blue" disabled={busy !== ''} onClick={() => void openForecastGroup(group)} type="button">{forecastGroupLabel(forecast, departmentNames, buildingNames, group)}</button>,
    },
    { key: 'scenario', header: 'Scenario', kind: 'text', width: 10, text: (group) => label(group.scenario) },
    { key: 'assets', header: 'Assets', kind: 'number', width: 8, text: (group) => String(group.assetCount) },
    {
      key: 'need', header: 'Replacement need', kind: 'money', width: 12,
      text: (group) => money(group.plannedReplacementMinor, forecast?.currency ?? ''),
      display: (group) => <AmountButton disabled={busy !== ''} label={`${forecastGroupLabel(forecast, departmentNames, buildingNames, group)} replacement need`} onClick={() => void openAmountBreakdown('planned', group)} value={money(group.plannedReplacementMinor, forecast?.currency ?? '')} />,
    },
    ...costKinds.map((kind): GridColumn<ForecastGroup> => ({
      key: kind, header: label(kind), kind: 'money', width: 11,
      text: (group) => money(group.amountsByKindMinor[kind] ?? 0, forecast?.currency ?? ''),
      display: (group) => <AmountButton disabled={busy !== ''} label={`${forecastGroupLabel(forecast, departmentNames, buildingNames, group)} ${label(kind).toLowerCase()}`} onClick={() => void openAmountBreakdown(kind, group)} value={money(group.amountsByKindMinor[kind] ?? 0, forecast?.currency ?? '')} />,
    })),
  ], [buildingNames, busy, departmentNames, forecast])

  const deploymentAssetColumns = useMemo((): GridColumn<ForecastGroupAsset>[] => [
    {
      key: 'asset', header: 'Asset', kind: 'text', width: 16, wrap: true,
      text: (item) => `${item.assetName} ${item.assetId}`,
      display: (item) => <><strong>{item.assetName}</strong><span className="mt-1 block text-xs text-steward-mist-muted">{item.assetId}</span></>,
    },
    { key: 'stage', header: 'Stage', kind: 'text', width: 10, text: (item) => label(item.lifecycleStage) },
    { key: 'usefulLife', header: 'Useful life', kind: 'text', width: 10, text: (item) => `${item.expectedUsefulLifeMonths} months` },
    {
      key: 'replacementDate', header: 'Replacement date', kind: 'date', width: 14,
      text: (item) => item.replacementDate ?? item.derivedReplacementDate ?? '',
      display: (item) => item.replacementDate ? displayDate(item.replacementDate) : item.derivedReplacementDate ? `${displayDate(item.derivedReplacementDate)} (derived)` : 'Not dated',
    },
    {
      key: 'replacementFy', header: 'Replacement FY', kind: 'text', width: 10,
      text: (item) => { const replacement = planReplacementDate(item); return replacement ? `FY${fiscalYearForDate(replacement, fiscalYearStartMonth)}` : '—' },
    },
    {
      key: 'monthInFy', header: 'Month in FY', kind: 'number', width: 8,
      text: (item) => { const replacement = planReplacementDate(item); return replacement ? String(fiscalMonthInYear(replacement, fiscalYearStartMonth)) : '—' },
    },
    { key: 'cost', header: 'Replacement cost', kind: 'money', width: 12, text: (item) => money(item.replacementCostMinor, item.currency) },
  ], [fiscalYearStartMonth])

  const planColumns = useMemo((): GridColumn<HorizonPlan>[] => [
    {
      key: 'asset', header: 'Asset', kind: 'text', width: 16, wrap: true,
      text: (plan) => `${assetName(assets, plan.assetId)} ${plan.assetId}`,
      display: (plan) => <><strong>{assetName(assets, plan.assetId)}</strong><span className="mt-1 block text-xs text-steward-mist-muted">{plan.assetId}</span></>,
    },
    { key: 'scenario', header: 'Scenario', kind: 'text', width: 10, text: (plan) => label(plan.scenario) },
    { key: 'stage', header: 'Stage', kind: 'text', width: 10, text: (plan) => label(plan.lifecycleStage) },
    { key: 'usefulLife', header: 'Useful life', kind: 'text', width: 10, text: (plan) => `${plan.expectedUsefulLifeMonths} months` },
    { key: 'replacementDate', header: 'Replacement date', kind: 'text', width: 14, text: (plan) => replacementDate(plan) },
    { key: 'cost', header: 'Replacement cost', kind: 'money', width: 12, text: (plan) => money(plan.replacementCostMinor, plan.currency) },
    { key: 'revision', header: 'Revision', kind: 'number', width: 8, text: (plan) => String(plan.revision) },
    {
      key: 'actions', header: 'Actions', kind: 'text', width: 18, wrap: true,
      text: () => 'Edit plan View history',
      display: (plan) => (
        <div className="flex min-w-56 flex-wrap gap-2">
          {canWrite && <button aria-label={`Edit plan for ${assetName(assets, plan.assetId)}`} className={secondaryButtonClass} onClick={() => openEdit(plan)} type="button">Edit plan</button>}
          <button aria-expanded={historyPlan?.id === plan.id} aria-label={`${busy === `history-${plan.id}` ? 'Loading history' : historyPlan?.id === plan.id ? 'Hide history' : 'View history'} for ${assetName(assets, plan.assetId)}`} className={secondaryButtonClass} disabled={busy !== ''} onClick={() => void toggleHistory(plan)} type="button">{busy === `history-${plan.id}` ? 'Loading…' : historyPlan?.id === plan.id ? 'Hide history' : 'View history'}</button>
        </div>
      ),
    },
  ], [assets, busy, canWrite, historyPlan])

  if (!canRead) {
    return (
      <section aria-labelledby="horizon-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="lifecycle.planning" data-requirement="REQ-HORIZON-001">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div><h2 className="text-2xl font-semibold" id="horizon-heading">Horizon — Lifecycle planning</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view lifecycle plans and forecasts.</p></div>
          {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Horizon help</button>}
        </div>
      </section>
    )
  }

  const exportURL = validReportPath('/api/v1/horizon/export.csv', scenarios, asOf, fromYear, toYear, fiscalYearStartMonth, groupBy)
  const maxReplacement = forecast ? Math.max(1, ...forecast.groups.map((group) => group.plannedReplacementMinor)) : 1
  const nonAdditive = groupBy === 'tag' || groupBy === 'goal'
  const pastLifecycleModels = models.filter((model) => modelPastLifecycle(model))
  const lineageModels = models.filter((model) => model.replacementModelId)
  const assetKinds = ['desktop', 'laptop', 'tablet', 'peripheral', 'server', 'other']
  const kindDefaultFor = (assetKind: string) => kindDefaults.find((item) => item.assetKind === assetKind && item.scenario === 'baseline')
  const upgradeRecommendations = plans
    .filter((plan) => plan.lifecycleStage === 'refresh_due')
    .flatMap((plan) => {
      const asset = assets.find((item) => item.id === plan.assetId)
      if (!asset || asset.status !== 'active') return []
      const linkedModel = asset.modelId ? models.find((item) => item.id === asset.modelId) : undefined
      return [{ plan, asset, linkedModel, reason: upgradeReason(asset, linkedModel) }]
    })
    .sort((left, right) => (right.asset.criticalityScore ?? 0) - (left.asset.criticalityScore ?? 0) || left.asset.name.localeCompare(right.asset.name))
  const dueNowGridColumns = dueNowColumns(models, siteNames, buildingNames, roomNames, departmentNames, money)

  const selectedGroupAssets = groupAssets?.items.filter((item) => selectedPlanIDs.includes(item.planId)) ?? []
  const canViewAtlas = Boolean(onOpenAtlasInventory && permissions.includes('assets.read'))
  const normalizationPreview = forecast && selectedGroup && groupBy === 'fiscal_year' && selectedGroupAssets.length > 0 && shiftMonths !== 0
    ? simulateFiscalYearShift(forecast.groups, selectedGroup.scenario, selectedGroupAssets, shiftMonths, fiscalYearStartMonth)
    : null
  const previewYears = normalizationPreview
    ? [...new Set([...normalizationPreview.before.keys(), ...normalizationPreview.after.keys()])].sort((left, right) => left.localeCompare(right, undefined, { numeric: true }))
    : []
  const previewMaximum = normalizationPreview
    ? Math.max(1, ...previewYears.map((key) => Math.max(normalizationPreview.before.get(key) ?? 0, normalizationPreview.after.get(key) ?? 0)))
    : 1
  const forecastItems = forecast?.items ?? []
  const departmentLabel = (key: string) => resolveBucketLabel(key, departmentNames)
  const kindLabel = (key: string) => key === otherBucket ? otherBucket : label(key)

  return (
    <section aria-labelledby="horizon-heading" className={`${panelClass} min-w-0 p-4 sm:p-5`} data-feature="lifecycle.planning" data-requirement="REQ-HORIZON-001">
      <ProductHeader
        actions={<>
          {canWrite && <button className={buttonClass} onClick={openCreate} type="button">Add lifecycle plan</button>}
          {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Horizon help</button>}
          <a className={secondaryButtonClass} href="#workspace-mesh">Open Mesh graph</a>
        </>}
        description="Start with what is due, then forecast replacement spend, then edit the plans and defaults that drive those answers."
        headingId="horizon-heading"
        kicker="Horizon"
        title="Lifecycle planning and forecasting"
      />

      {error && <div className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-4 rounded-lg border border-steward-success/50 bg-steward-success/15 p-3 text-[#aaf0c6]" role="status">{message}</p>}

      {canWrite && formOpen && <PlanEditor assets={assets} busy={busy === 'save'} editing={editing} onCancel={closeForm} onSubmit={savePlan} />}

      <div className="mt-6">
        <HorizonSectionNav active={section} onChange={setSection} />
      </div>

      <div hidden={section !== 'queue'} id="horizon-panel-queue" role="tabpanel" aria-labelledby="horizon-tab-queue">
        <div className="mt-4 grid gap-3 sm:grid-cols-3" aria-label="Work due now">
          <Metric label="Assets due now" value={upgradeRecommendations.length.toLocaleString()} />
          <Metric label="Models past lifecycle" value={pastLifecycleModels.length.toLocaleString()} />
          <Metric label="Planned replacement" value={forecast ? money(forecast.plannedReplacementMinor, forecast.currency) : '—'} />
        </div>

        <section aria-labelledby="horizon-upgrade-heading" className="mt-6 min-w-0">
          <h3 className="text-lg font-semibold" id="horizon-upgrade-heading">Upgrade and retire recommendations</h3>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Active assets with baseline plans in refresh due — past useful life or linked to a retired catalog model. Use the gear to add Atlas asset columns. Sorted by criticality.</p>
          <div className="mt-4">
            <DataGrid
              columns={dueNowGridColumns}
              emptyMessage="No active assets currently need replacement under the baseline scenario."
              identity={identity}
              label="Due now assets"
              onOpenRow={canViewAtlas ? (row) => onOpenAtlasInventory?.([row.asset.id], row.asset.name, fiscalYearStartMonth) : undefined}
              rowId={(row) => row.plan.id}
              rowLabel={(row) => row.asset.name}
              rows={upgradeRecommendations}
              viewId="horizon-due-now"
            />
          </div>
        </section>

        <section aria-labelledby="horizon-past-models-heading" className={`${subpanelClass} mt-6 p-4`}>
          <h3 className="text-lg font-semibold" id="horizon-past-models-heading">Models past lifecycle</h3>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Models with a last effective date on or before today are no longer expected to continue operation. Search retired models and adjust lineage in Atlas → Models.</p>
          {pastLifecycleModels.length === 0
            ? <p className="mt-3 text-sm text-steward-mist-muted">No catalog models are past their last effective date.</p>
            : <ul className="mt-4 grid gap-3 sm:grid-cols-2">{pastLifecycleModels.map((model) => <li className="rounded-lg border border-steward-warning/40 bg-steward-warning/10 p-3 text-sm" key={model.id}><strong>{modelLabel(model)}</strong>{model.status === 'retired' && <span className="ml-2">· retired</span>}<p className="mt-1 text-steward-mist-muted">{label(model.kind)} · last effective {calendarDateText(model.lastEffectiveDate)} · {model.usefulLifeMonths || '—'} month useful life{model.replacementModelId ? ` · successor ${successorLabel(model, models)}` : ''}</p></li>)}</ul>}
        </section>
      </div>

      <div hidden={section !== 'forecast'} id="horizon-panel-forecast" role="tabpanel" aria-labelledby="horizon-tab-forecast">
      <section aria-labelledby="horizon-forecast-controls-heading" className={`${subpanelClass} mt-6 p-4`}>
        <h3 className="text-lg font-semibold" id="horizon-forecast-controls-heading">Forecast controls</h3>
        <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Use comma-separated scenarios. Years are inclusive; month 1 is January. Missing department, type, manufacturer, or building is Other.</p>
        <div className="mt-4" role="group" aria-label="Forecast views">
          <div className="flex flex-wrap gap-1">
            {forecastViews.map((view) => (
              <button
                aria-pressed={forecastView === view.id}
                className={cx(
                  'rounded-md px-3 py-1.5 text-sm font-medium',
                  forecastView === view.id ? 'bg-white/[0.08] text-steward-mist' : 'text-steward-mist-muted hover:bg-white/[0.04] hover:text-steward-mist',
                )}
                disabled={busy !== ''}
                key={view.id}
                onClick={() => applyForecastView(view.id)}
                type="button"
              >
                {view.label}
              </button>
            ))}
          </div>
        </div>
        <div className="mt-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
          <Field id="horizon-scenarios" label="Scenarios"><input className={inputClass} id="horizon-scenarios" onChange={(event) => setScenarios(event.target.value)} value={scenarios} /></Field>
          <Field id="horizon-as-of" label="As of"><input className={inputClass} id="horizon-as-of" onChange={(event) => setAsOf(event.target.value)} type="datetime-local" value={asOf} /></Field>
          <Field id="horizon-from-year" label="From year"><input className={inputClass} id="horizon-from-year" max={9999} min={1970} onChange={(event) => setFromYear(Number(event.target.value))} type="number" value={fromYear} /></Field>
          <Field id="horizon-to-year" label="To year"><input className={inputClass} id="horizon-to-year" max={9999} min={1970} onChange={(event) => setToYear(Number(event.target.value))} type="number" value={toYear} /></Field>
          <Field id="horizon-fiscal-month" label="Fiscal year start month"><input className={inputClass} id="horizon-fiscal-month" max={12} min={1} onChange={(event) => setFiscalYearStartMonth(Number(event.target.value))} type="number" value={fiscalYearStartMonth} /></Field>
          <Field id="horizon-group-by" label="Group by"><select className={inputClass} id="horizon-group-by" onChange={(event) => { const next = event.target.value as GroupBy; setGroupBy(next); setForecastView(next) }} value={groupBy}>{groupOptions.map((option) => <option key={option} value={option}>{option === 'asset_class' ? 'Type' : option === 'fiscal_year' ? 'Year' : label(option)}</option>)}</select></Field>
        </div>
        <div className="mt-4 flex flex-wrap gap-3">
          <button className={buttonClass} disabled={busy !== ''} onClick={() => void refreshReport()} type="button">{busy === 'refresh' ? 'Refreshing…' : 'Refresh forecast'}</button>
          {exportURL
            ? <a className={secondaryButtonClass} href={exportURL}>Export CSV</a>
            : <button className={secondaryButtonClass} disabled type="button">Export CSV</button>}
        </div>
      </section>

      {forecast ? <section aria-labelledby="horizon-forecast-heading" className="mt-6 min-w-0">
        <div className="flex flex-wrap items-end justify-between gap-3"><div><h3 className="text-lg font-semibold" id="horizon-forecast-heading">Forecast results</h3><p className="mt-1 text-sm text-steward-mist-muted">As of {displayDate(forecast.asOf, true)} · {forecast.scenarios.map(label).join(', ')}. Actual, estimated, committed, normalized real, and TCO come from Ledger costs on the same asset, scenario, and replacement fiscal year.</p></div><p className="text-sm font-semibold text-steward-mist-muted">Currency: {forecast.currency || 'No monetary rows'}</p></div>
        <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-7" aria-label="Forecast totals">
          <Metric disabled={busy !== ''} label="Planned replacement" onClick={() => void openAmountBreakdown('planned')} value={money(forecast.plannedReplacementMinor, forecast.currency)} />
          <Metric label="Unique assets" value={forecast.assetCount.toLocaleString()} />
          {costKinds.map((kind) => <Metric disabled={busy !== ''} key={kind} label={label(kind)} onClick={() => void openAmountBreakdown(kind)} value={money(forecast.totalsByKindMinor[kind] ?? 0, forecast.currency)} />)}
        </div>

        {forecastView === 'department_type' && <HorizonForecastCharts
          currency={forecast.currency}
          departmentLabel={departmentLabel}
          items={forecastItems}
          kindLabel={kindLabel}
          money={money}
        />}

        <div className={`${subpanelClass} mt-5 p-4`}>
          <h4 className="font-semibold">Replacement need overview</h4>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Supplemental compact bars. Exact values and asset counts are in the authoritative table below.</p>
          <ul className="mt-4 grid gap-3">
            {forecast.groups.map((group) => <li className="grid gap-2 sm:grid-cols-[minmax(9rem,0.6fr)_minmax(10rem,1fr)_auto] sm:items-center" key={`${group.scenario}-${group.key}`}>
              <button className="rounded-md text-left text-sm font-medium text-[#a9c7ff] underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-steward-blue" disabled={busy !== ''} onClick={() => void openForecastGroup(group)} type="button">{forecastGroupLabel(forecast, departmentNames, buildingNames, group)} · {label(group.scenario)}</button>
              <span aria-hidden="true" className="h-3 overflow-hidden rounded-sm bg-steward-ink-800"><span className="block h-full rounded-sm bg-steward-blue" style={{ width: `${group.plannedReplacementMinor === 0 ? 0 : Math.max(2, Math.round(group.plannedReplacementMinor / maxReplacement * 100))}%` }} /></span>
              <AmountButton disabled={busy !== ''} label={`${forecastGroupLabel(forecast, departmentNames, buildingNames, group)} planned replacement`} onClick={() => void openAmountBreakdown('planned', group)} value={money(group.plannedReplacementMinor, forecast.currency)} />
            </li>)}
            {forecast.groups.length === 0 && <li className="text-sm text-steward-mist-muted">No replacement needs match these forecast controls.</li>}
          </ul>
        </div>

        {nonAdditive && <p className="mt-4 rounded-lg border border-steward-warning/60 bg-steward-warning/10 p-3 text-sm leading-6 text-[#ffd596]"><strong>Non-additive grouping:</strong> one asset can appear in multiple {groupBy} rows. Do not add these rows together as an organization total.</p>}
        <p className="mt-5 text-sm text-steward-mist-muted" id="horizon-forecast-table-heading">Authoritative forecast values by {label(forecast.groupBy).toLowerCase()} and scenario.</p>
        <div className="mt-3">
          <DataGrid
            columns={forecastColumns}
            emptyMessage="No forecast rows match these controls."
            identity={identity}
            label={`Authoritative forecast values by ${label(forecast.groupBy).toLowerCase()} and scenario.`}
            rowId={(group) => `${group.scenario}-${group.key}`}
            rowLabel={(group) => forecastGroupLabel(forecast, departmentNames, buildingNames, group)}
            rows={forecast.groups}
            viewId="horizon-forecast-groups"
          />
        </div>
      </section> : <p className="mt-6 text-steward-mist-muted" role="status">Loading Horizon forecast…</p>}
      </div>

      <Drawer
        actions={groupAssets && canViewAtlas
          ? <button className={buttonClass} disabled={busy !== ''} onClick={() => openGroupInAtlas()} type="button">View in Atlas</button>
          : undefined}
        description={selectedGroup ? `${selectedGroup.assetCount} assets · ${money(selectedGroup.plannedReplacementMinor, forecast?.currency ?? '')} planned replacement` : undefined}
        footer={groupBy === 'fiscal_year' && canWrite && groupAssets && selectedPlanIDs.length > 0 && shiftMonths !== 0
          ? <button className={buttonClass} disabled={busy !== ''} onClick={() => void applyCycleShift()} type="button">{busy === 'normalize' ? 'Applying…' : `Apply ${shiftMonths > 0 ? '+' : ''}${shiftMonths}-month shift to ${selectedPlanIDs.length} selected`}</button>
          : undefined}
        kicker="Forecast group"
        onClose={closeGroupDrawer}
        open={selectedGroup !== null}
        title={selectedGroup ? `${selectedGroup.label} · ${label(selectedGroup.scenario)}` : 'Forecast group'}
        wide
      >
        {!groupAssets
          ? <p className="text-sm text-steward-mist-muted" role="status">Loading assets in this refresh cycle…</p>
          : <>
            <p className="text-sm leading-6 text-steward-mist-muted">Assets are grouped by deployment — typically a lab room or office — so you can shift or align an entire installation together. Select assets to preview cycle normalization below.</p>
            <div className="mt-4 space-y-5">
              {deploymentGroups.map((deployment) => {
                const planIDs = deployment.items.map((item) => item.planId)
                const groupSelected = planIDs.length > 0 && planIDs.every((planID) => selectedPlanIDs.includes(planID))
                return <section className="rounded-xl border border-white/10 bg-white/[0.025]" key={deployment.key}>
                  <div className="flex flex-wrap items-start justify-between gap-3 border-b border-white/10 px-4 py-3">
                    <div>
                      <h4 className="font-semibold">{deployment.label}</h4>
                      <p className="mt-1 text-sm text-steward-mist-muted">{deployment.items.length} assets · {money(deployment.totalCostMinor, groupAssets.currency)}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {canWrite && groupBy === 'fiscal_year' && <>
                        <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => toggleDeploymentSelection(planIDs)} type="button">{groupSelected ? 'Deselect group' : 'Select group'}</button>
                        <button className={secondaryButtonClass} disabled={busy !== '' || shiftMonths === 0} onClick={() => void applyShiftToItems(deployment.items, shiftMonths).catch((cause) => showError(cause instanceof Error ? cause.message : 'Deployment shift failed.'))} type="button">Shift group {shiftMonths > 0 ? '+' : ''}{shiftMonths} mo</button>
                        {selectedForecastFiscalYear() !== null && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => void alignDeploymentGroup(deployment.items).catch((cause) => showError(cause instanceof Error ? cause.message : 'Deployment align failed.'))} type="button">Align group to {selectedGroup?.label ?? 'FY start'}</button>}
                      </>}
                      {canViewAtlas && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => openGroupInAtlas(deployment.items.map((item) => item.assetId), `${groupAssets.label} · ${deployment.label}`)} type="button">View in Atlas</button>}
                    </div>
                  </div>
                  <div className="min-w-0">
                    <DataGrid
                      columns={deploymentAssetColumns}
                      emptyMessage="No assets match this forecast group."
                      identity={identity}
                      label={`${deployment.label} assets`}
                      maximumBodyHeight="24rem"
                      onSelectedRowIdsChange={setSelectedPlanIDs}
                      rowId={(item) => item.planId}
                      rowLabel={(item) => item.assetName}
                      rows={deployment.items}
                      selectable={groupBy === 'fiscal_year' && canWrite}
                      selectedRowIds={selectedPlanIDs}
                      viewId={`horizon-forecast-deployment-${deployment.key}`}
                    />
                  </div>
                </section>
              })}
              {deploymentGroups.length === 0 && <p className="text-sm text-steward-mist-muted">No assets match this forecast group.</p>}
            </div>
            {groupBy === 'fiscal_year' && groupAssets.items.length > 0 && <>
              <h4 className="mt-8 font-semibold">Normalize replacement cycles</h4>
              <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Preview how moving selected assets earlier or later would redistribute replacement spend across fiscal years. Use deployment group actions above to shift a whole lab or office, or apply the selection here.</p>
              <div className="mt-4 grid gap-4 sm:grid-cols-[minmax(12rem,0.5fr)_minmax(0,1fr)] sm:items-end">
                <Field id="horizon-shift-months" label="Shift selected by (months)">
                  <select className={inputClass} id="horizon-shift-months" onChange={(event) => setShiftMonths(Number(event.target.value))} value={shiftMonths}>
                    <option value={-24}>24 months earlier</option>
                    <option value={-12}>12 months earlier</option>
                    <option value={-6}>6 months earlier</option>
                    <option value={6}>6 months later</option>
                    <option value={12}>12 months later</option>
                    <option value={24}>24 months later</option>
                  </select>
                </Field>
                <p className="text-sm text-steward-mist-muted">{selectedPlanIDs.length} of {groupAssets.items.length} assets selected · {shiftMonths === 0 ? 'Choose a shift to preview redistribution.' : `Preview moves ${selectedPlanIDs.length} selected assets ${shiftMonths > 0 ? 'later' : 'earlier'}.`}</p>
              </div>
              {normalizationPreview && previewYears.length > 0 && <div className="mt-5 rounded-xl border border-white/10 bg-white/[0.025] p-4">
                <h5 className="font-semibold">Before and after by fiscal year</h5>
                <ul className="mt-4 grid gap-4">
                  {previewYears.map((yearKey) => {
                    const before = normalizationPreview.before.get(yearKey) ?? 0
                    const after = normalizationPreview.after.get(yearKey) ?? 0
                    return <li className="grid gap-2" key={yearKey}>
                      <div className="flex flex-wrap items-center justify-between gap-2 text-sm"><span className="font-medium">{yearKey}</span><span className="tabular-nums text-steward-mist-muted">{money(before, groupAssets.currency)} → {money(after, groupAssets.currency)}</span></div>
                      <div className="grid gap-1 sm:grid-cols-2">
                        <span aria-hidden="true" className="h-2 overflow-hidden rounded-sm bg-steward-ink-800"><span className="block h-full rounded-sm bg-steward-blue/70" style={{ width: `${before === 0 ? 0 : Math.max(2, Math.round(before / previewMaximum * 100))}%` }} /></span>
                        <span aria-hidden="true" className="h-2 overflow-hidden rounded-sm bg-steward-ink-800"><span className="block h-full rounded-sm bg-steward-teal" style={{ width: `${after === 0 ? 0 : Math.max(2, Math.round(after / previewMaximum * 100))}%` }} /></span>
                      </div>
                    </li>
                  })}
                </ul>
                <p className="mt-3 text-xs text-steward-mist-muted">Blue bars show the current forecast; teal bars show the simulated distribution after shifting selected assets.</p>
              </div>}
            </>}
          </>}
      </Drawer>

      <Drawer
        actions={amountBreakdown && amountBreakdown.items.length > 0 && canViewAtlas
          ? <button className={buttonClass} disabled={busy !== ''} onClick={() => openAmountInAtlas()} type="button">View in Atlas</button>
          : undefined}
        description={selectedAmount
          ? `${amountBreakdown ? `${amountBreakdown.itemCount} ${amountBreakdown.itemCount === 1 ? 'item' : 'items'} · ` : ''}${money(amountBreakdown?.totalMinor ?? (selectedAmount.group
            ? selectedAmount.kind === 'planned' ? selectedAmount.group.plannedReplacementMinor : selectedAmount.group.amountsByKindMinor[selectedAmount.kind] ?? 0
            : selectedAmount.kind === 'planned' ? forecast?.plannedReplacementMinor ?? 0 : forecast?.totalsByKindMinor[selectedAmount.kind] ?? 0), forecast?.currency ?? '')}`
          : undefined}
        kicker="Amount breakdown"
        onClose={closeAmountDrawer}
        open={selectedAmount !== null}
        title={selectedAmount
          ? selectedAmount.group
            ? `${label(selectedAmount.kind)} · ${forecastGroupLabel(forecast, departmentNames, buildingNames, selectedAmount.group)}`
            : label(selectedAmount.kind)
          : 'Amount breakdown'}
        wide
      >
        {!amountBreakdown
          ? <p className="text-sm text-steward-mist-muted" role="status">Loading items that make up this amount…</p>
          : <>
            <p className="text-sm leading-6 text-steward-mist-muted">
              {amountBreakdown.amountKind === 'planned'
                ? 'Each row is a lifecycle plan whose replacement cost is included in this planned replacement total.'
                : 'Each row is a Ledger cost on the same asset, scenario, and replacement fiscal year as a matching lifecycle plan.'}
            </p>
            <div className="mt-4 min-w-0">
              <DataGrid
                columns={amountBreakdownColumns}
                emptyMessage="No items make up this amount for the current forecast controls."
                identity={identity}
                label="Amount breakdown items"
                maximumBodyHeight="24rem"
                rowId={(item) => item.costId || item.planId || `${item.assetId}-${item.kind}-${item.fiscalYear}-${item.amountMinor}`}
                rowLabel={(item) => item.assetName}
                rows={amountBreakdown.items}
                viewId="horizon-amount-breakdown"
              />
              {amountBreakdown.items.length > 0 && <p className="mt-3 text-sm font-semibold tabular-nums">Total {money(amountBreakdown.totalMinor, amountBreakdown.currency)}</p>}
            </div>
          </>}
      </Drawer>

      <div hidden={section !== 'plans'} id="horizon-panel-plans" role="tabpanel" aria-labelledby="horizon-tab-plans">
      <HorizonReplacementPlans
        canWrite={canWrite}
        csrfToken={csrfToken}
        fiscalYearStartMonth={fiscalYearStartMonth}
        onError={showError}
        onMessage={(value) => { setError(''); setMessage(value) }}
        onOpenAtlasInventory={canViewAtlas ? onOpenAtlasInventory : undefined}
      />
      <section aria-labelledby="horizon-plans-heading" className="mt-6 min-w-0">
        <h3 className="text-lg font-semibold" id="horizon-plans-heading">Asset assumptions</h3>
        <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Per-asset useful life, replacement date, and cost for scenario {primaryScenario(scenarios)}. These still drive the forecast. Earlier effective assumptions remain available in version history.</p>
        <div className="mt-3 min-w-0">
          <DataGrid
            columns={planColumns}
            emptyMessage={`No lifecycle plans match ${primaryScenario(scenarios)}.`}
            identity={identity}
            label="Scrollable lifecycle plans table"
            rowId={(plan) => plan.id}
            rowLabel={(plan) => assetName(assets, plan.assetId)}
            rows={plans}
            viewId="horizon-asset-assumptions"
          />
        </div>
      </section>

      {historyPlan && <section aria-labelledby="horizon-history-heading" className={`${subpanelClass} mt-6 min-w-0 p-4`}>
        <h3 className="text-lg font-semibold" id="horizon-history-heading">Version history for {assetName(assets, historyPlan.assetId)}</h3>
        <p className="mt-1 text-sm text-steward-mist-muted">Immutable assumptions ordered by the service.</p>
        <div className="mt-3 min-w-0">
          <DataGrid
            columns={historyColumns}
            emptyMessage="No earlier plan versions are available."
            identity={identity}
            label={`Scrollable version history for ${assetName(assets, historyPlan.assetId)}`}
            maximumBodyHeight="24rem"
            rowId={(version) => `${version.planId}-${version.revision}`}
            rowLabel={(version) => `Revision ${version.revision}`}
            rows={history}
            viewId="horizon-plan-history"
          />
        </div>
      </section>}
      </div>

      <div hidden={section !== 'defaults'} id="horizon-panel-defaults" role="tabpanel" aria-labelledby="horizon-tab-defaults">
        <section aria-labelledby="horizon-kind-defaults-heading" className={`${subpanelClass} mt-6 p-4`}>
          <h3 className="text-lg font-semibold" id="horizon-kind-defaults-heading">Lifecycle plan by asset type</h3>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Baseline expected useful life applies when a per-asset plan leaves useful life unset. Useful life also comes from the linked Atlas model when present. Replacement models are configured in the Atlas model catalog lineage field.</p>
          <div className="mt-4 grid gap-4 xl:grid-cols-2">
            {assetKinds.map((assetKind) => {
              const current = kindDefaultFor(assetKind)
              return (
                <form className="rounded-xl border border-white/10 bg-white/[0.025] p-4" key={assetKind} onSubmit={(event) => void saveKindDefault(event, assetKind, current?.revision)}>
                  <h4 className="font-semibold">{label(assetKind)}</h4>
                  <div className="mt-3">
                    <Field id={`kind-life-${assetKind}`} label="Expected useful life (months)"><input className={inputClass} defaultValue={current?.expectedUsefulLifeMonths ?? ''} id={`kind-life-${assetKind}`} max={1200} min={1} name="expectedUsefulLifeMonths" placeholder="From model" required type="number" /></Field>
                  </div>
                  {canWrite && <button className={`${secondaryButtonClass} mt-4`} disabled={busy !== ''} type="submit">{busy === `kind-${assetKind}` ? 'Saving…' : current ? 'Update defaults' : 'Save defaults'}</button>}
                </form>
              )
            })}
          </div>
        </section>

        <section aria-labelledby="horizon-model-lineage-heading" className={`${subpanelClass} mt-6 p-4`}>
          <h3 className="text-lg font-semibold" id="horizon-model-lineage-heading">Atlas model replacement lineage</h3>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Horizon resolves replacement targets from asset overrides, then the linked model&apos;s lineage in Atlas. Edit lineage in Atlas → Models.</p>
          {lineageModels.length === 0
            ? <p className="mt-3 text-sm text-steward-mist-muted">No catalog models define a replacement successor yet.</p>
            : <ul className="mt-4 grid gap-3 sm:grid-cols-2">{lineageModels.map((model) => <li className="rounded-lg border border-white/10 bg-white/[0.025] p-3 text-sm" key={model.id}><strong>{modelLabel(model)}</strong>{model.status === 'retired' && <span className="ml-2 text-steward-warning">retired</span>}<p className="mt-1 text-steward-mist-muted">{label(model.kind)} · replaces with {successorLabel(model, models)}</p></li>)}</ul>}
        </section>
      </div>
    </section>
  )
}

function PlanEditor({ assets, busy, editing, onCancel, onSubmit }: { assets: readonly Asset[]; busy: boolean; editing: HorizonPlan | null; onCancel: () => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  const replacementDateID = useId()
  const usefulLifeID = useId()
  const scenarioID = useId()
  const stageID = useId()
  const costID = useId()
  const currencyID = useId()
  const effectiveID = useId()
  const assetID = useId()
  const selectedAssetMissing = editing && !assets.some((asset) => asset.id === editing.assetId)
  return (
    <section aria-labelledby="horizon-plan-editor-heading" className={`${subpanelClass} mt-6 border-steward-teal/40 p-4 sm:p-5`}>
      <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-lg font-semibold" id="horizon-plan-editor-heading">{editing ? `Edit lifecycle plan for ${assetName(assets, editing.assetId)}` : 'Add lifecycle plan'}</h3><p className="mt-1 text-sm leading-6 text-steward-mist-muted">Useful life resolves from the asset model, then asset-type defaults. Replacement targets resolve from asset overrides or Atlas model lineage. Replacement dates derive from lifecycle start, installed, or purchase date.</p></div><button className={secondaryButtonClass} onClick={onCancel} type="button">Cancel</button></div>
      <form className="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-3" key={editing?.id ?? 'new-plan'} onSubmit={onSubmit}>
        <Field id={assetID} label="Atlas asset"><select className={inputClass} defaultValue={editing?.assetId ?? ''} disabled={Boolean(editing)} id={assetID} name="assetId" required><option value="">{assets.length === 0 ? 'No Atlas assets available' : 'Select an asset'}</option>{selectedAssetMissing && <option value={editing.assetId}>{editing.assetId}</option>}{assets.map((asset) => <option key={asset.id} value={asset.id}>{asset.name} · {label(asset.kind)}</option>)}</select>{editing && <input name="assetId" type="hidden" value={editing.assetId} />}</Field>
        <Field id={scenarioID} label="Scenario"><input className={inputClass} defaultValue={editing?.scenario ?? 'baseline'} id={scenarioID} maxLength={64} name="scenario" pattern="[A-Za-z0-9][A-Za-z0-9._\-]{0,63}" required /></Field>
        <Field id={stageID} label="Lifecycle stage"><select className={inputClass} defaultValue={editing?.lifecycleStage ?? 'planned'} id={stageID} name="lifecycleStage" required>{lifecycleStages.map((stage) => <option key={stage} value={stage}>{label(stage)}</option>)}</select></Field>
        <Field id={usefulLifeID} label="Expected useful life (months)"><input className={inputClass} defaultValue={editing?.expectedUsefulLifeMonths ?? ''} id={usefulLifeID} max={1200} min={1} name="expectedUsefulLifeMonths" placeholder="Auto from model or asset type" type="number" /></Field>
        <Field help="Optional. Leave blank to derive from lifecycle start, installed, or purchase date." id={replacementDateID} label="Manual replacement date"><input aria-describedby={`${replacementDateID}-help`} className={inputClass} defaultValue={editing?.replacementDate?.slice(0, 10) ?? ''} id={replacementDateID} name="replacementDate" type="date" /></Field>
        <Field id={effectiveID} label="Effective from"><input className={inputClass} defaultValue={editing?.effectiveFrom.slice(0, 10) ?? new Date().toISOString().slice(0, 10)} id={effectiveID} name="effectiveFrom" required type="date" /></Field>
        <Field help={`Maximum ${maximumReplacementCost} major currency units so every minor unit remains exact in the browser.`} id={costID} label="Replacement cost"><input aria-describedby={`${costID}-help`} className={inputClass} defaultValue={editing ? (editing.replacementCostMinor / 100).toFixed(2) : '0.00'} id={costID} inputMode="decimal" max={maximumReplacementCost} name="replacementCost" required /></Field>
        <Field id={currencyID} label="Currency"><input className={inputClass} defaultValue={editing?.currency ?? 'USD'} id={currencyID} maxLength={3} name="currency" pattern="[A-Za-z]{3}" required /></Field>
        <div className="flex flex-wrap items-end gap-3 md:col-span-2 xl:col-span-1"><button className={buttonClass} disabled={busy || (!editing && assets.length === 0)} type="submit">{busy ? 'Saving…' : editing ? 'Update lifecycle plan' : 'Create lifecycle plan'}</button><button className={secondaryButtonClass} onClick={onCancel} type="button">Cancel</button></div>
      </form>
    </section>
  )
}

function Field({ id, label: fieldLabel, help, children }: { id: string; label: string; help?: string; children: ReactNode }) {
  return <div><label className="block text-sm font-semibold text-steward-mist-muted" htmlFor={id}>{fieldLabel}</label>{help && <p className="mt-1 text-xs leading-5 text-steward-mist-muted" id={`${id}-help`}>{help}</p>}{children}</div>
}

const amountLinkClass = 'rounded-md text-left font-medium tabular-nums text-[#a9c7ff] underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-steward-blue disabled:text-steward-mist disabled:no-underline'

function Metric({ label: metricLabel, value, onClick, disabled }: { label: string; value: string; onClick?: () => void; disabled?: boolean }) {
  return <div className="rounded-md border border-steward-ink-800 bg-steward-ink-950/40 p-3">
    <p className="text-xs font-medium text-steward-mist-muted">{metricLabel}</p>
    {onClick
      ? <button aria-label={`Show ${metricLabel.toLowerCase()} items totaling ${value}`} className={`${amountLinkClass} mt-1 text-xl font-semibold`} disabled={disabled} onClick={onClick} type="button">{value}</button>
      : <p className="mt-1 text-xl font-semibold tabular-nums">{value}</p>}
  </div>
}

function AmountButton({ label: amountLabel, value, onClick, disabled }: { label: string; value: string; onClick: () => void; disabled?: boolean }) {
  return <button aria-label={`Show ${amountLabel} items totaling ${value}`} className={amountLinkClass} disabled={disabled} onClick={onClick} type="button">{value}</button>
}

function assetName(assets: readonly Asset[], assetID: string) {
  return assets.find((asset) => asset.id === assetID)?.name ?? assetID
}

function replacementDate(plan: HorizonPlan) {
  if (plan.replacementDate) return displayDate(plan.replacementDate)
  if (plan.derivedReplacementDate) return `${displayDate(plan.derivedReplacementDate)} (derived)`
  return 'Not dated'
}

function upgradeReason(asset: Asset | undefined, linkedModel: AssetModel | undefined) {
  if (!asset) return 'Refresh due'
  if (linkedModel && modelPastLifecycle(linkedModel)) return 'Catalog model retired — upgrade recommended'
  const percent = lifecyclePercent(asset, new Date(), linkedModel)
  if (percent !== null && percent >= 100) return 'Past expected useful life — retire or replace'
  if (percent !== null && percent >= 75) return 'Approaching end of useful life'
  return 'Refresh due under baseline planning'
}
