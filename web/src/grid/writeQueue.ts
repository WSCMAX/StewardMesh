import { useCallback, useMemo, useState } from 'react'
import { ApiRequestError } from '../api'
import { applyCellPayload, type ColumnRules } from './columns'
import { groupEditsByRow, type CellEdit } from './useCellEditing'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// Persists pending grid edits. No module exposes a bulk-update endpoint today,
// so a pasted block fans out to one request per record with bounded concurrency
// and per-record outcomes. The transport is the only place that knows how edits
// map onto endpoints, so adding a batch route later means implementing
// `writeBatch` rather than changing the grid or the screens.

export type WriteTask = {
  rowId: string
  edits: readonly CellEdit[]
}

export type WriteFailureCode = 'conflict' | 'ownership_locked' | 'validation' | 'reference_missing' | 'error'

export type WriteOutcome = {
  rowId: string
  state: 'saved' | 'failed'
  code?: WriteFailureCode
  message?: string
}

export type WriteTransport = {
  /** Records written at once. Ignored when `writeBatch` is available. */
  concurrency?: number
  /** Writes one record's edits, resolving once the change is durable. */
  writeRecord: (task: WriteTask) => Promise<void>
  /** Single-request path for a whole batch. Preferred whenever implemented. */
  writeBatch?: (tasks: readonly WriteTask[]) => Promise<readonly WriteOutcome[]>
}

export type WriteReport = {
  outcomes: readonly WriteOutcome[]
  saved: number
  failed: number
  conflicts: number
  locked: number
}

const defaultConcurrency = 4

export function classifyWriteError(error: unknown): { code: WriteFailureCode; message: string } {
  if (error instanceof ApiRequestError) {
    if (error.status === 409) return { code: 'conflict', message: 'Another change landed first. Reload to see current values.' }
    if (error.status === 423) return { code: 'ownership_locked', message: 'Imported record is write locked until ownership is claimed.' }
    if (error.status === 422) return { code: 'reference_missing', message: error.message }
    if (error.status === 400) return { code: 'validation', message: error.message }
    return { code: 'error', message: error.message }
  }
  return { code: 'error', message: 'The change could not be saved.' }
}

/** Builds one request payload from a record's pending edits. */
export function buildPayload(edits: readonly CellEdit[], columns: readonly ColumnRules[], base: Record<string, unknown> = {}) {
  const draft: Record<string, unknown> = { ...base }
  for (const edit of edits) {
    const column = columns.find((candidate) => candidate.key === edit.columnKey)
    if (column) applyCellPayload(column, draft, edit.text)
  }
  return draft
}

export function tasksFromEdits(edits: readonly CellEdit[]): WriteTask[] {
  return [...groupEditsByRow(edits)].map(([rowId, rowEdits]) => ({ rowId, edits: rowEdits }))
}

async function runPool<I>(items: readonly I[], limit: number, worker: (item: I) => Promise<void>) {
  let cursor = 0
  const runners = Array.from({ length: Math.max(1, Math.min(limit, items.length)) }, async () => {
    while (cursor < items.length) {
      const index = cursor
      cursor += 1
      await worker(items[index])
    }
  })
  await Promise.all(runners)
}

export function summarizeReport(report: WriteReport) {
  const total = report.saved + report.failed
  if (report.failed === 0) return `${report.saved} of ${total} ${total === 1 ? 'record' : 'records'} saved.`
  const reasons: string[] = []
  if (report.conflicts > 0) reasons.push(`${report.conflicts} conflicted with a newer change`)
  if (report.locked > 0) reasons.push(`${report.locked} write locked pending ownership`)
  const other = report.failed - report.conflicts - report.locked
  if (other > 0) reasons.push(`${other} rejected`)
  return `${report.saved} of ${total} records saved. ${reasons.join(', ')}. Reload to see current values.`
}

export async function runWriteQueue(
  tasks: readonly WriteTask[],
  transport: WriteTransport,
  onProgress?: (rowId: string, state: 'saving' | 'saved' | 'failed', message?: string) => void,
): Promise<WriteReport> {
  if (tasks.length === 0) return { outcomes: [], saved: 0, failed: 0, conflicts: 0, locked: 0 }

  let outcomes: WriteOutcome[]
  if (transport.writeBatch) {
    for (const task of tasks) onProgress?.(task.rowId, 'saving')
    try {
      outcomes = [...await transport.writeBatch(tasks)]
    } catch (error) {
      const failure = classifyWriteError(error)
      outcomes = tasks.map((task) => ({ rowId: task.rowId, state: 'failed' as const, ...failure }))
    }
    for (const outcome of outcomes) onProgress?.(outcome.rowId, outcome.state, outcome.message)
  } else {
    const collected: WriteOutcome[] = []
    await runPool(tasks, transport.concurrency ?? defaultConcurrency, async (task) => {
      onProgress?.(task.rowId, 'saving')
      try {
        await transport.writeRecord(task)
        collected.push({ rowId: task.rowId, state: 'saved' })
        onProgress?.(task.rowId, 'saved')
      } catch (error) {
        const failure = classifyWriteError(error)
        collected.push({ rowId: task.rowId, state: 'failed', ...failure })
        onProgress?.(task.rowId, 'failed', failure.message)
      }
    })
    outcomes = collected
  }

  return {
    outcomes,
    saved: outcomes.filter((outcome) => outcome.state === 'saved').length,
    failed: outcomes.filter((outcome) => outcome.state === 'failed').length,
    conflicts: outcomes.filter((outcome) => outcome.code === 'conflict').length,
    locked: outcomes.filter((outcome) => outcome.code === 'ownership_locked').length,
  }
}

export type RowWriteState = { state: 'saving' | 'saved' | 'failed'; message?: string }

/** React binding that exposes per-row save state for the grid to render. */
export function useWriteQueue() {
  const [states, setStates] = useState<ReadonlyMap<string, RowWriteState>>(new Map())
  const [report, setReport] = useState<WriteReport | null>(null)
  const [running, setRunning] = useState(false)

  const run = useCallback(async (tasks: readonly WriteTask[], transport: WriteTransport) => {
    setRunning(true)
    setStates(new Map())
    setReport(null)
    try {
      const result = await runWriteQueue(tasks, transport, (rowId, state, message) => {
        setStates((current) => {
          const next = new Map(current)
          next.set(rowId, { state, message })
          return next
        })
      })
      setReport(result)
      return result
    } finally {
      setRunning(false)
    }
  }, [])

  const reset = useCallback(() => {
    setStates(new Map())
    setReport(null)
  }, [])

  const rowState = useCallback((rowId: string) => states.get(rowId)?.state, [states])
  const rowMessage = useCallback((rowId: string) => states.get(rowId)?.message, [states])
  const summary = useMemo(() => report ? summarizeReport(report) : '', [report])

  return { run, reset, rowState, rowMessage, report, summary, running }
}
