import { useState } from 'react'
import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, test } from 'vitest'
import { RelatedRecordModeChooser, RelatedRecordWorkflowFrame, useRelatedRecordWorkflow, type RelatedRecordMode } from './RelatedRecordWorkflow'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

const boundaries = {
  source: { label: 'Source', owner: 'Source feature', api: 'POST /api/v1/source', authorization: 'source.write' },
  related: { label: 'Related', owner: 'Related feature', api: 'GET|POST /api/v1/related', authorization: 'related.read; related.write to create' },
}

function WorkflowHarness({ canCreate = false, createOperation, retryable = true }: { canCreate?: boolean; createOperation?: () => Promise<string>; retryable?: boolean }) {
  const [draft, setDraft] = useState('')
  const [mode, setMode] = useState<RelatedRecordMode>('select')
  const [selected, setSelected] = useState('')
  const workflow = useRelatedRecordWorkflow<string>({
    cancellationMessage: 'Workflow cancelled and its draft was cleared. Any related record already created remains available.',
    onReset: () => {
      setDraft('')
      setMode('select')
      setSelected('')
    },
  })
  return (
    <RelatedRecordWorkflowFrame boundaries={boundaries} busy={workflow.busy} description="Reusable workflow" failure={workflow.failure} failureRef={workflow.failureRef} headingId="workflow-heading" kicker="Guided task" onRetry={workflow.retry} status={workflow.status} step={workflow.step} title="Connect records">
      {workflow.step === 'intro' ? <button onClick={workflow.start} type="button">Start</button> : null}
      {workflow.step === 'source' ? (
        <form onSubmit={(event) => {
          event.preventDefault()
          if (!draft.trim()) {
            workflow.failValidation('Source: enter a value.')
            return
          }
          workflow.moveTo('related')
        }}>
          <h4>Source step</h4>
          <label>Source draft<input onChange={(event) => setDraft(event.target.value)} value={draft} /></label>
          <button type="submit">Continue</button>
          <button onClick={workflow.cancel} type="button">Cancel</button>
        </form>
      ) : null}
      {workflow.step === 'related' ? (
        <div>
          <h4>Related step</h4>
          <RelatedRecordModeChooser canCreate={canCreate} createLabel="Create related" fallbackMessage="Creation unavailable. Select an existing record." legend="Related path" mode={mode} name="relatedMode" onChange={setMode} selectLabel="Select existing" />
          {mode === 'select' ? <label>Existing record<select onChange={(event) => setSelected(event.target.value)} value={selected}><option value="">Choose</option><option value="existing">Existing record</option></select></label> : null}
          <button onClick={() => {
            if (mode === 'select') {
              if (!selected) workflow.failValidation('Related: select a record.')
              else workflow.selectRelated(selected)
              return
            }
            void workflow.createRelated(createOperation ?? (async () => 'created'), () => 'Related: creation failed.', () => retryable)
          }} type="button">{mode === 'create' ? 'Create' : 'Review'}</button>
          <button onClick={() => workflow.moveTo('source')} type="button">Back to source</button>
          <button onClick={workflow.cancel} type="button">Cancel</button>
        </div>
      ) : null}
      {workflow.step === 'confirm' ? (
        <div>
          <h4>Confirm step</h4>
          <p>Related record: {workflow.related}</p>
          <button onClick={() => void workflow.confirm(async (related) => `Confirmed ${related}.`, () => 'Confirmation failed.')} type="button">Confirm</button>
          <button onClick={() => workflow.moveTo('source')} type="button">Edit source</button>
          <button onClick={workflow.cancel} type="button">Cancel</button>
        </div>
      ) : null}
    </RelatedRecordWorkflowFrame>
  )
}

test('preserves and validates the source, falls back to selection, returns, and confirms', async () => {
  const { container } = render(<WorkflowHarness />)
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.getByRole('alert')).toHaveTextContent('Source: enter a value.')

  fireEvent.change(screen.getByLabelText('Source draft'), { target: { value: 'Preserved draft' } })
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  expect(screen.getByText('Creation unavailable. Select an existing record.')).toBeInTheDocument()
  expect(screen.queryByLabelText('Create related')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Existing record'), { target: { value: 'existing' } })
  fireEvent.click(screen.getByRole('button', { name: 'Review' }))
  fireEvent.click(screen.getByRole('button', { name: 'Edit source' }))
  expect(screen.getByLabelText('Source draft')).toHaveValue('Preserved draft')

  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  fireEvent.click(screen.getByRole('button', { name: 'Review' }))
  expect(screen.getByText('Related record: existing')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Confirmed existing.'))
  expect(screen.getByText('Owner: Source feature')).toBeInTheDocument()
  expect(screen.getByText(/POST \/api\/v1\/source/)).toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('exposes loading, failure, retry, and explicit cancellation without losing retry input', async () => {
  let attempts = 0
  let release: (() => void) | undefined
  const createOperation = async () => {
    attempts += 1
    if (attempts === 1) throw new Error('temporary failure')
    await new Promise<void>((resolve) => { release = resolve })
    return 'created'
  }
  render(<WorkflowHarness canCreate createOperation={createOperation} />)
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  fireEvent.change(screen.getByLabelText('Source draft'), { target: { value: 'Draft for retry' } })
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  fireEvent.click(screen.getByLabelText('Create related'))
  fireEvent.click(screen.getByRole('button', { name: 'Create' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Related: creation failed.')

  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByRole('status')).toHaveTextContent('Creating the related record…')
  release?.()
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Confirm step' })).toBeInTheDocument())
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.getByRole('status')).toHaveTextContent('Workflow cancelled and its draft was cleared.')
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  expect(screen.getByLabelText('Source draft')).toHaveValue('')
})

test('does not retry feature-owned authorization or validation failures', async () => {
  render(<WorkflowHarness canCreate createOperation={async () => { throw new Error('forbidden') }} retryable={false} />)
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  fireEvent.change(screen.getByLabelText('Source draft'), { target: { value: 'Authorized source' } })
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  fireEvent.click(screen.getByLabelText('Create related'))
  fireEvent.click(screen.getByRole('button', { name: 'Create' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Related: creation failed.')
  expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
})
