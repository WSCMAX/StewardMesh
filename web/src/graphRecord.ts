import type { WorkspaceAreaID } from './WorkspaceShell'
import { workspaceHash } from './WorkspaceShell'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type GraphRecordRef = {
  id: string
  kind: string
}

export type WorkspaceRecordFocus = {
  area: WorkspaceAreaID
  kind: string
  recordId: string
  nonce: number
}

export type AtlasAssetScope = {
  assetIds: readonly string[]
  label: string
  fiscalYearStartMonth: number
  nonce: number
}

export type WorkspaceRecordTarget = {
  area: WorkspaceAreaID
  name: string
}

const targetsByKind: Record<string, WorkspaceRecordTarget> = {
  site: { area: 'people', name: 'People' },
  building: { area: 'people', name: 'People' },
  room: { area: 'people', name: 'People' },
  department: { area: 'people', name: 'People' },
  person: { area: 'people', name: 'People' },
  shared: { area: 'people', name: 'People' },
  public: { area: 'people', name: 'People' },
  lab: { area: 'people', name: 'People' },
  group: { area: 'people', name: 'People' },
  subject: { area: 'people', name: 'People' },
  asset: { area: 'atlas', name: 'Atlas' },
  model: { area: 'atlas', name: 'Atlas' },
  vendor: { area: 'ledger', name: 'Ledger' },
  purchase_order: { area: 'ledger', name: 'Ledger' },
  contract: { area: 'ledger', name: 'Ledger' },
  budget: { area: 'ledger', name: 'Ledger' },
  commitment: { area: 'ledger', name: 'Ledger' },
  product: { area: 'stack', name: 'Stack' },
  version: { area: 'stack', name: 'Stack' },
  license: { area: 'stack', name: 'Stack' },
  label: { area: 'threads', name: 'Tags' },
  goal: { area: 'threads', name: 'Tags' },
  document: { area: 'vault', name: 'Vault' },
  plan: { area: 'horizon', name: 'Horizon' },
}

export function recordIDFromNode(node: GraphRecordRef) {
  const prefix = `${node.kind}:`
  return node.id.startsWith(prefix) ? node.id.slice(prefix.length) : node.id
}

export function workspaceRecordTarget(kind: string): WorkspaceRecordTarget | null {
  return targetsByKind[kind] ?? null
}

export function workspaceRecordHref(kind: string) {
  const target = workspaceRecordTarget(kind)
  return target ? workspaceHash(target.area) : ''
}

export function openRecordLabel(kind: string, canWrite: boolean) {
  const target = workspaceRecordTarget(kind)
  if (!target) return ''
  if (kind === 'asset' && canWrite) return `Edit this asset in ${target.name}`
  if (kind === 'model' && canWrite) return `Edit this model in ${target.name}`
  return `Open in ${target.name}`
}

export function newWorkspaceRecordFocus(node: GraphRecordRef, nonce = Date.now()): WorkspaceRecordFocus | null {
  const target = workspaceRecordTarget(node.kind)
  if (!target) return null
  return { area: target.area, kind: node.kind, recordId: recordIDFromNode(node), nonce }
}
