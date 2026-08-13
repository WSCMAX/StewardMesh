import { type ReactNode, type RefObject, useEffect, useRef, useState } from 'react'
import { secondaryButtonClass } from './ui'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

export type RelatedRecordStep = 'intro' | 'source' | 'related' | 'confirm'
export type RelatedRecordMode = 'select' | 'create'
export type RelatedRecordOperation = 'related' | 'confirm'

export type RelatedRecordBoundary = {
  label: string
  owner: string
  api: string
  authorization: string
}

type WorkflowFailure = {
  message: string
  retry: (() => Promise<void>) | null
}

type RelatedRecordWorkflowOptions = {
  cancellationMessage: string
  onReset: () => void
}

export type RelatedRecordWorkflowState<TRelated> = {
  busy: RelatedRecordOperation | null
  cancel: () => void
  confirm: (operation: (related: TRelated) => Promise<string>, describeError: (error: unknown) => string, canRetry?: (error: unknown) => boolean) => Promise<void>
  createRelated: (operation: () => Promise<TRelated>, describeError: (error: unknown) => string, canRetry?: (error: unknown) => boolean) => Promise<void>
  failValidation: (message: string) => void
  failure: WorkflowFailure | null
  failureRef: RefObject<HTMLDivElement | null>
  moveTo: (step: Exclude<RelatedRecordStep, 'intro'>) => void
  related: TRelated | null
  retry: () => Promise<void>
  selectRelated: (related: TRelated) => void
  start: () => void
  status: string
  step: RelatedRecordStep
}

export function useRelatedRecordWorkflow<TRelated>({ cancellationMessage, onReset }: RelatedRecordWorkflowOptions): RelatedRecordWorkflowState<TRelated> {
  const [step, setStep] = useState<RelatedRecordStep>('intro')
  const [related, setRelated] = useState<TRelated | null>(null)
  const [busy, setBusy] = useState<RelatedRecordOperation | null>(null)
  const [failure, setFailure] = useState<WorkflowFailure | null>(null)
  const [status, setStatus] = useState('')
  const failureRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (failure) failureRef.current?.focus()
  }, [failure])

  function clearFailure() {
    setFailure(null)
  }

  function moveTo(nextStep: Exclude<RelatedRecordStep, 'intro'>) {
    clearFailure()
    setStep(nextStep)
  }

  function start() {
    setStatus('')
    moveTo('source')
  }

  function failValidation(message: string) {
    setFailure({ message, retry: null })
  }

  function selectRelated(selection: TRelated) {
    setRelated(selection)
    moveTo('confirm')
  }

  async function perform(operation: RelatedRecordOperation, action: () => Promise<void>, describeError: (error: unknown) => string, canRetry: (error: unknown) => boolean) {
    setBusy(operation)
    clearFailure()
    try {
      await action()
    } catch (error) {
      const retry = canRetry(error) ? async () => perform(operation, action, describeError, canRetry) : null
      setFailure({ message: describeError(error), retry })
    } finally {
      setBusy(null)
    }
  }

  async function createRelated(operation: () => Promise<TRelated>, describeError: (error: unknown) => string, canRetry: (error: unknown) => boolean = () => true) {
    await perform('related', async () => {
      const created = await operation()
      setRelated(created)
      setStep('confirm')
    }, describeError, canRetry)
  }

  async function confirm(operation: (selection: TRelated) => Promise<string>, describeError: (error: unknown) => string, canRetry: (error: unknown) => boolean = () => true) {
    if (!related) {
      failValidation('Review: return to the related-record step and choose a record.')
      return
    }
    await perform('confirm', async () => {
      const successMessage = await operation(related)
      onReset()
      setRelated(null)
      setStep('intro')
      setStatus(successMessage)
    }, describeError, canRetry)
  }

  async function retry() {
    await failure?.retry?.()
  }

  function cancel() {
    if (busy) return
    onReset()
    setRelated(null)
    setFailure(null)
    setStep('intro')
    setStatus(cancellationMessage)
  }

  return {
    busy,
    cancel,
    confirm,
    createRelated,
    failValidation,
    failure,
    failureRef,
    moveTo,
    related,
    retry,
    selectRelated,
    start,
    status,
    step,
  }
}

type RelatedRecordWorkflowFrameProps = {
  boundaries: {
    source: RelatedRecordBoundary
    related: RelatedRecordBoundary
  }
  busy: RelatedRecordOperation | null
  children: ReactNode
  description: string
  failure: WorkflowFailure | null
  failureRef: RefObject<HTMLDivElement | null>
  headingId: string
  kicker: string
  onRetry: () => Promise<void>
  status: string
  step: RelatedRecordStep
  title: string
}

export function RelatedRecordWorkflowFrame({ boundaries, busy, children, description, failure, failureRef, headingId, kicker, onRetry, status, step, title }: RelatedRecordWorkflowFrameProps) {
  const stepNumber = step === 'source' ? 1 : step === 'related' ? 2 : step === 'confirm' ? 3 : 0
  return (
    <section aria-busy={busy !== null} aria-labelledby={headingId} className="rounded-xl border border-steward-blue/35 bg-steward-ink-950/45 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.025)] sm:p-5" data-feature="experience.workspace" data-requirement="REQ-WORKSPACE-001">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-3xl">
          <p className="text-sm font-semibold text-steward-blue">{kicker}</p>
          <h3 className="mt-1 text-lg font-semibold" id={headingId}>{title}</h3>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">{description}</p>
        </div>
        {stepNumber > 0 && <p className="rounded-full border border-steward-ink-800 px-3 py-1 text-sm text-steward-mist-muted">Step {stepNumber} of 3</p>}
      </div>

      <details className="mt-4 rounded-lg border border-steward-ink-800 p-3 text-sm text-steward-mist-muted">
        <summary className="cursor-pointer font-semibold text-steward-mist">Record ownership and authorization</summary>
        <dl className="mt-3 grid gap-3 md:grid-cols-2">
          {[boundaries.source, boundaries.related].map((boundary) => (
            <div className="rounded-lg bg-steward-ink-900/65 p-3" key={boundary.label}>
              <dt className="font-semibold text-steward-mist">{boundary.label}</dt>
              <dd className="mt-1">Owner: {boundary.owner}</dd>
              <dd className="mt-1 break-words">API: <code>{boundary.api}</code></dd>
              <dd className="mt-1">Authorization: <code>{boundary.authorization}</code></dd>
            </div>
          ))}
        </dl>
        <p className="mt-3">Workspace preserves the draft and coordinates these calls. Each owning API still validates and authorizes its own record.</p>
      </details>

      {busy && <p className="mt-4 rounded-lg border border-steward-blue/35 bg-steward-blue/10 p-3 text-sm text-steward-mist" role="status">{busy === 'related' ? 'Creating the related record…' : 'Confirming the relationship…'}</p>}
      {failure && (
        <div className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" ref={failureRef} role="alert" tabIndex={-1}>
          <p>{failure.message}</p>
          {failure.retry && <button className={`${secondaryButtonClass} mt-3`} disabled={busy !== null} onClick={() => void onRetry()} type="button">Retry</button>}
        </div>
      )}
      {status && <div className="mt-4 rounded-lg border border-steward-teal/40 bg-steward-teal/10 p-3 text-steward-mist" role="status">{status}</div>}
      {children}
    </section>
  )
}

type RelatedRecordModeChooserProps = {
  canCreate: boolean
  createLabel: string
  fallbackMessage: string
  legend: string
  mode: RelatedRecordMode
  name: string
  onChange: (mode: RelatedRecordMode) => void
  selectLabel: string
}

export function RelatedRecordModeChooser({ canCreate, createLabel, fallbackMessage, legend, mode, name, onChange, selectLabel }: RelatedRecordModeChooserProps) {
  return (
    <fieldset className="grid gap-3 sm:grid-cols-2">
      <legend className="text-sm font-semibold text-steward-mist-muted">{legend}</legend>
      <label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg border border-steward-ink-800 p-3"><input checked={mode === 'select'} name={name} onChange={() => onChange('select')} type="radio" /> {selectLabel}</label>
      {canCreate ? (
        <label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg border border-steward-ink-800 p-3"><input checked={mode === 'create'} name={name} onChange={() => onChange('create')} type="radio" /> {createLabel}</label>
      ) : (
        <p className="rounded-lg border border-steward-ink-800 p-3 text-sm leading-6 text-steward-mist-muted">{fallbackMessage}</p>
      )}
    </fieldset>
  )
}
