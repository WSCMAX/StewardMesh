import { ApiRequestError } from './api'

export type AssignmentPurpose = 'checkout' | 'reservation'

export type AssignmentOverlap = {
  assetId: string
  assignmentId: string
  assigneeKind: string
  assigneeId: string
  assigneeLabel: string
  role: string
  purpose: AssignmentPurpose
  eventSummary?: string
  effectiveFrom: string
  dueAt?: string
  effectiveTo?: string
}

export type CheckoutCandidate = {
  assetId: string
  name: string
  kind: string
  assetTag?: string
  modelId?: string
  modelName?: string
  rating: number
  overlaps: AssignmentOverlap[]
}

export type CheckoutGroup = {
  id: string
  organizationId: string
  name: string
  description?: string
  memberIds: string[]
  status: string
  revision: number
  createdAt: string
  updatedAt: string
}

export type BulkCheckout = {
  id: string
  organizationId: string
  assigneeKind: string
  assigneeId: string
  purpose: AssignmentPurpose
  effectiveFrom: string
  dueAt?: string
  eventSummary?: string
  requestedCount: number
  createdBy: string
  createdAt: string
}

export function isAssignmentOverlap(value: unknown): value is AssignmentOverlap {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return typeof record.assetId === 'string' && typeof record.assignmentId === 'string'
    && typeof record.assigneeLabel === 'string' && typeof record.effectiveFrom === 'string'
    && (record.purpose === 'checkout' || record.purpose === 'reservation')
}

export function isCheckoutCandidate(value: unknown): value is CheckoutCandidate {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return typeof record.assetId === 'string' && typeof record.name === 'string'
    && typeof record.rating === 'number' && Array.isArray(record.overlaps)
    && record.overlaps.every(isAssignmentOverlap)
}

export function isCheckoutGroup(value: unknown): value is CheckoutGroup {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return typeof record.id === 'string' && typeof record.name === 'string' && Array.isArray(record.memberIds)
}

export function isBulkCheckout(value: unknown): value is BulkCheckout {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return typeof record.id === 'string' && typeof record.assigneeId === 'string' && typeof record.requestedCount === 'number'
}

export function overlapFromError(error: unknown): { kind: string; message: string; conflicts: AssignmentOverlap[] } | null {
  if (!(error instanceof ApiRequestError) || error.status !== 409 || typeof error.body !== 'object' || error.body === null) {
    return null
  }
  const payload = (error.body as Record<string, unknown>).error
  if (typeof payload !== 'object' || payload === null) return null
  const record = payload as Record<string, unknown>
  if (record.code !== 'assignment_overlap' || !Array.isArray(record.conflicts)) return null
  return {
    kind: typeof record.conflictKind === 'string' ? record.conflictKind : 'checkout',
    message: typeof record.message === 'string' ? record.message : 'This period overlaps another checkout.',
    conflicts: record.conflicts.filter(isAssignmentOverlap),
  }
}
