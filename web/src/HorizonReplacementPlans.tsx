import { useEffect, useMemo, useState } from 'react'
import { isAsset, type Asset } from './AtlasInventory'
import { isRevision, requestJSON, type Revision } from './api'
import DataGrid, { type StagedDraft } from './grid/DataGrid'
import { type GridColumn } from './grid/columns'
import { buildPayload } from './grid/writeQueue'
import type { CellEdit } from './grid/useCellEditing'
import { buttonClass, secondaryButtonClass } from './ui'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

export const replacementPlanGroupings = ['department', 'building', 'type', 'manufacturer', 'site', 'custom'] as const

export type ReplacementPlanGrouping = (typeof replacementPlanGroupings)[number]

export type ReplacementPlan = {
  id: string
  organizationId: string
  name: string
  grouping: ReplacementPlanGrouping
  groupKey?: string
  scenario: string
  assetCount: number
  revision: Revision
  createdAt: string
  updatedAt: string
}

const groupingLabels: Record<ReplacementPlanGrouping, string> = {
  department: 'Department',
  building: 'Building',
  type: 'Type',
  manufacturer: 'Manufacturer',
  site: 'Site',
  custom: 'Custom',
}

export function isReplacementPlan(value: unknown): value is ReplacementPlan {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.name === 'string' && typeof item.grouping === 'string'
    && replacementPlanGroupings.includes(item.grouping as ReplacementPlanGrouping)
    && typeof item.scenario === 'string' && typeof item.assetCount === 'number'
    && isRevision(item.revision) && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
}

function parseReplacementPlans(value: unknown): ReplacementPlan[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as { items?: unknown }).items
  return Array.isArray(items) ? items.filter(isReplacementPlan) : []
}

function groupingLabel(value: string) {
  return groupingLabels[value as ReplacementPlanGrouping] ?? value
}

export default function HorizonReplacementPlans({
  csrfToken,
  canWrite,
  fiscalYearStartMonth,
  onOpenAtlasInventory,
  onError,
  onMessage,
}: {
  csrfToken: string
  canWrite: boolean
  fiscalYearStartMonth: number
  onOpenAtlasInventory?: (assetIds: readonly string[], label: string, fiscalYearStartMonth: number) => void
  onError: (message: string) => void
  onMessage: (message: string) => void
}) {
  const [plans, setPlans] = useState<ReplacementPlan[]>([])
  const [selected, setSelected] = useState<ReplacementPlan | null>(null)
  const [planAssets, setPlanAssets] = useState<Asset[]>([])
  const [busy, setBusy] = useState('')

  useEffect(() => {
    let active = true
    requestJSON('/api/v1/horizon/replacement-plans').then((value) => {
      if (active) setPlans(parseReplacementPlans(value))
    }).catch(() => {
      if (active) onError('Replacement plans could not be loaded.')
    })
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!selected) {
      setPlanAssets([])
      return
    }
    let active = true
    setBusy(`assets-${selected.id}`)
    requestJSON(`/api/v1/horizon/replacement-plans/${encodeURIComponent(selected.id)}/assets`)
      .then((value) => {
        if (!active) return
        const items = typeof value === 'object' && value !== null && Array.isArray((value as { items?: unknown }).items)
          ? (value as { items: unknown[] }).items.filter(isAsset)
          : []
        setPlanAssets(items)
      })
      .catch(() => {
        if (active) onError(`Assets for ${selected.name} could not be loaded.`)
      })
      .finally(() => {
        if (active) setBusy('')
      })
    return () => { active = false }
  }, [selected])

  const columns = useMemo((): GridColumn<ReplacementPlan>[] => [
    { key: 'name', header: 'Plan name', kind: 'text', editable: canWrite, required: true, maxLength: 200, width: 16, text: (plan) => plan.name },
    {
      key: 'grouping', header: 'Grouping', kind: 'enum', editable: canWrite, required: true, options: [...replacementPlanGroupings], width: 10,
      text: (plan) => plan.grouping, display: (plan) => groupingLabel(plan.grouping),
    },
    { key: 'groupKey', header: 'Group', kind: 'text', editable: canWrite, maxLength: 128, width: 12, text: (plan) => plan.groupKey ?? '' },
    { key: 'scenario', header: 'Scenario', kind: 'text', editable: canWrite, required: true, maxLength: 64, width: 9, text: (plan) => plan.scenario },
    { key: 'assetCount', header: 'Assets', kind: 'number', width: 7, align: 'right', text: (plan) => String(plan.assetCount) },
  ], [canWrite])

  const assetColumns = useMemo((): GridColumn<Asset>[] => [
    { key: 'name', header: 'Asset', kind: 'text', width: 16, text: (asset) => asset.name, display: (asset) => <><strong>{asset.name}</strong><span className="mt-1 block text-xs text-steward-mist-muted">{asset.id}</span></> },
    { key: 'kind', header: 'Kind', kind: 'text', width: 10, text: (asset) => asset.kind },
    { key: 'status', header: 'Status', kind: 'text', width: 9, text: (asset) => asset.status },
    { key: 'assetTag', header: 'Tag', kind: 'text', width: 10, text: (asset) => asset.assetTag ?? '' },
  ], [])

  async function saveEdits(edits: readonly CellEdit[]) {
    const byRow = new Map<string, CellEdit[]>()
    for (const edit of edits) {
      const current = byRow.get(edit.rowId) ?? []
      current.push(edit)
      byRow.set(edit.rowId, current)
    }
    const saved: ReplacementPlan[] = []
    for (const [id, rowEdits] of byRow) {
      const plan = plans.find((item) => item.id === id)
      if (!plan) throw new Error('The replacement plan is no longer loaded.')
      const payload = buildPayload(rowEdits, columns, {
        name: plan.name, grouping: plan.grouping, groupKey: plan.groupKey ?? '', scenario: plan.scenario, revision: plan.revision,
      })
      const response = await requestJSON(`/api/v1/horizon/replacement-plans/${encodeURIComponent(id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      if (!isReplacementPlan(response)) throw new Error('invalid replacement plan response')
      saved.push(response)
    }
    setPlans((current) => current.map((plan) => saved.find((item) => item.id === plan.id) ?? plan))
    onMessage(saved.length === 1 ? 'Replacement plan updated.' : `${saved.length} replacement plans updated.`)
  }

  async function createRows(drafts: readonly StagedDraft[]) {
    const created: ReplacementPlan[] = []
    for (const draft of drafts) {
      const response = await requestJSON('/api/v1/horizon/replacement-plans', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: draft.values.name ?? '',
          grouping: draft.values.grouping || 'custom',
          groupKey: draft.values.groupKey ?? '',
          scenario: draft.values.scenario || 'baseline',
        }),
      })
      if (!isReplacementPlan(response)) throw new Error('invalid replacement plan response')
      created.push(response)
    }
    setPlans((current) => [...current, ...created].sort((left, right) => left.name.localeCompare(right.name)))
    onMessage(created.length === 1 ? 'Replacement plan created.' : `${created.length} replacement plans created.`)
  }

  return (
    <section aria-labelledby="horizon-replacement-plans-heading" className="mt-6 min-w-0">
      <h3 className="text-lg font-semibold" id="horizon-replacement-plans-heading">Replacement plans</h3>
      <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Named plans that group many assets. Assign a plan on each Atlas asset, then open a row to see the assets in that plan.</p>
      <div className="mt-3">
        <DataGrid
          columns={columns}
          editable={canWrite}
          emptyMessage="No replacement plans yet. Add a named plan, then assign assets from Atlas."
          label="Replacement plans"
          onCreateRows={canWrite ? createRows : undefined}
          onOpenRow={setSelected}
          onSaveEdits={canWrite ? saveEdits : undefined}
          rowId={(plan) => plan.id}
          rowLabel={(plan) => plan.name}
          rows={plans}
          viewId="horizon-replacement-plans"
        />
      </div>

      {selected && (
        <section aria-labelledby="horizon-replacement-plan-assets-heading" className="mt-6 min-w-0">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h4 className="text-base font-semibold" id="horizon-replacement-plan-assets-heading">{selected.name}</h4>
              <p className="mt-1 text-sm text-steward-mist-muted">{groupingLabel(selected.grouping)}{selected.groupKey ? ` · ${selected.groupKey}` : ''} · {selected.assetCount.toLocaleString()} assets</p>
            </div>
            <div className="flex flex-wrap gap-2">
              {onOpenAtlasInventory && planAssets.length > 0 && (
                <button
                  className={buttonClass}
                  onClick={() => onOpenAtlasInventory(planAssets.map((asset) => asset.id), selected.name, fiscalYearStartMonth)}
                  type="button"
                >
                  View in Atlas
                </button>
              )}
              <button className={secondaryButtonClass} onClick={() => setSelected(null)} type="button">Close</button>
            </div>
          </div>
          <div className="mt-3">
            <DataGrid
              columns={assetColumns}
              emptyMessage={busy === `assets-${selected.id}` ? 'Loading assets…' : 'No assets are assigned to this plan yet. Set Replacement plan on assets in Atlas.'}
              label={`Assets in ${selected.name}`}
              maximumBodyHeight="24rem"
              rowId={(asset) => asset.id}
              rowLabel={(asset) => asset.name}
              rows={planAssets}
              viewId="horizon-replacement-plan-assets"
            />
          </div>
        </section>
      )}
    </section>
  )
}
