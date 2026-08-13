import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

type PatternFieldType = 'text' | 'number' | 'date' | 'money' | 'enum' | 'attachment' | 'reference'

type PatternField = {
  key: string
  label: string
  help?: string
  type: PatternFieldType
  required: boolean
  allowHolding?: boolean
  referenceType?: string
  options?: string[]
  currencyField?: string
  accessibleLabel: string
  csvHeader: string
}

type PatternTemplate = {
  id: string
  recordType: string
  name: string
  description?: string
  version: number
  builtIn: boolean
  status: 'active' | 'retired'
  fields: PatternField[]
}

type DraftField = {
  key: string
  label: string
  help: string
  type: PatternFieldType
  required: boolean
  allowHolding: boolean
  referenceType: string
  options: string
  currencyField: string
}

const fieldTypes: PatternFieldType[] = ['text', 'number', 'date', 'money', 'enum', 'attachment', 'reference']

function emptyField(): DraftField {
  return { key: '', label: '', help: '', type: 'text', required: false, allowHolding: false, referenceType: '', options: '', currencyField: '' }
}

function isPatternField(value: unknown): value is PatternField {
  if (typeof value !== 'object' || value === null) return false
  const field = value as Record<string, unknown>
  return typeof field.key === 'string' && typeof field.label === 'string' && fieldTypes.includes(field.type as PatternFieldType)
    && typeof field.required === 'boolean' && typeof field.accessibleLabel === 'string' && typeof field.csvHeader === 'string'
}

function isPatternTemplate(value: unknown): value is PatternTemplate {
  if (typeof value !== 'object' || value === null) return false
  const template = value as Record<string, unknown>
  return typeof template.id === 'string' && typeof template.recordType === 'string' && typeof template.name === 'string'
    && typeof template.version === 'number' && typeof template.builtIn === 'boolean'
    && (template.status === 'active' || template.status === 'retired')
    && Array.isArray(template.fields) && template.fields.every(isPatternField)
}

function readTemplates(value: unknown): PatternTemplate[] {
  if (typeof value !== 'object' || value === null) throw new Error('invalid Patterns response')
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items) || !items.every(isPatternTemplate)) throw new Error('invalid Patterns response')
  return items
}

export default function PatternsManager({ csrfToken }: { csrfToken: string }) {
  const [templates, setTemplates] = useState<PatternTemplate[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [fields, setFields] = useState<DraftField[]>([emptyField()])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const selected = templates.find((template) => template.id === selectedID) ?? templates[0]

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const loadTemplates = useCallback(async (signal?: AbortSignal) => {
    const items = readTemplates(await requestJSON('/api/v1/templates', { signal }))
    setTemplates(items)
    setSelectedID((current) => current || items[0]?.id || '')
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    loadTemplates(controller.signal)
      .catch((loadError: unknown) => {
        if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) setError(loadError instanceof ApiRequestError ? loadError.message : 'Templates could not be loaded.')
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [loadTemplates])

  function changeField(index: number, change: Partial<DraftField>) {
    setFields((current) => current.map((field, fieldIndex) => fieldIndex === index ? { ...field, ...change } : field))
  }

  async function createTemplate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('create')
    setError('')
    setStatus('')
    try {
      const response = await requestJSON('/api/v1/templates', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('patternName') ?? '').trim(),
          description: String(values.get('patternDescription') ?? '').trim(),
          recordType: String(values.get('patternRecordType') ?? '').trim(),
          fields: fields.map((field) => ({
            key: field.key.trim(), label: field.label.trim(), help: field.help.trim(), type: field.type, required: field.required,
            allowHolding: (field.type === 'reference' || field.type === 'attachment') && field.allowHolding,
            referenceType: field.type === 'reference' || field.type === 'attachment' ? field.referenceType.trim() : undefined,
            options: field.type === 'enum' ? field.options.split(',').map((option) => option.trim()).filter(Boolean) : undefined,
            currencyField: field.type === 'money' ? field.currencyField.trim() : undefined,
          })),
        }),
      })
      if (!isPatternTemplate(response)) throw new Error('invalid created template')
      await loadTemplates()
      setSelectedID(response.id)
      setFields([emptyField()])
      form.reset()
      setStatus('Custom template version 1 created.')
    } catch (mutationError) {
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The custom template could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function copyTemplate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected) return
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('copy')
    setError('')
    setStatus('')
    try {
      const response = await requestJSON(`/api/v1/templates/${encodeURIComponent(selected.id)}/copy?version=${selected.version}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ name: String(values.get('patternCopyName') ?? '').trim() }),
      })
      if (!isPatternTemplate(response)) throw new Error('invalid copied template')
      await loadTemplates()
      setSelectedID(response.id)
      form.reset()
      setStatus('Editable template copy created.')
    } catch (mutationError) {
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The template copy could not be created.')
    } finally {
      setBusy('')
    }
  }

  return <section aria-labelledby="patterns-heading" className={`${panelClass} p-5 sm:p-7`} data-feature="templates.schemas" data-requirement="REQ-PATTERNS-001">
    <p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Patterns</p>
    <h2 className="mt-2 text-2xl font-semibold" id="patterns-heading">Versioned record templates</h2>
    <p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">Use built-in schemas as form, API, and CSV contracts. Custom versions are append-only, so imports and future Exchange packages can name the exact schema they used.</p>
    <p className="mt-2 text-sm text-steward-mist-muted">A missing reference can become a visible holding record only when the field allows it; it is never silently accepted.</p>
    {error && <div aria-live="assertive" className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/10 p-4 text-sm text-[#ffbdc3]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
    {status && <p aria-live="polite" className="mt-4 rounded-xl border border-steward-success/35 bg-steward-success/10 p-4 text-sm text-[#98eab9]" role="status">{status}</p>}
    {loading ? <p className="mt-5 text-sm text-steward-mist-muted">Loading templates…</p> : <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(15rem,0.7fr)_minmax(0,1.3fr)]">
      <div>
        <label className={labelClass} htmlFor="patternTemplate">Template and version</label>
        <select className={inputClass} id="patternTemplate" onChange={(event) => setSelectedID(event.target.value)} value={selected?.id ?? ''}>
          {templates.map((template) => <option key={template.id} value={template.id}>{template.name} · v{template.version}{template.builtIn ? ' · built in' : ''}</option>)}
        </select>
        {selected && <div className={`${subpanelClass} mt-4 p-4`}>
          <h3 className="font-semibold">{selected.name}</h3>
          <p className="mt-1 break-all font-mono text-xs text-steward-teal">{selected.recordType} · {selected.id} · v{selected.version}</p>
          <p className="mt-2 text-sm leading-6 text-steward-mist-muted">{selected.description || 'No description provided.'}</p>
          <a className={`${secondaryButtonClass} mt-4`} href={`/api/v1/templates/${encodeURIComponent(selected.id)}/template.csv?version=${selected.version}`}>Download CSV template</a>
          <form className="mt-5" onSubmit={copyTemplate}>
            <label className={labelClass} htmlFor="patternCopyName">Name for editable copy</label>
            <input className={inputClass} id="patternCopyName" maxLength={160} name="patternCopyName" required />
            <button className={`${secondaryButtonClass} mt-3`} disabled={busy !== ''} type="submit">{busy === 'copy' ? 'Copying…' : 'Copy this version'}</button>
          </form>
        </div>}
      </div>
      <div>
        <h3 className="text-lg font-semibold">Field contract</h3>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {selected?.fields.map((field) => <article className={`${subpanelClass} p-4`} key={field.key}>
            <div className="flex flex-wrap items-start justify-between gap-2"><h4 className="font-semibold">{field.label}</h4><span className="rounded-md border border-steward-teal/35 px-2 py-1 font-mono text-xs text-steward-teal">{field.type}</span></div>
            <p className="mt-2 font-mono text-xs text-steward-mist-muted">{field.key} · CSV: {field.csvHeader}</p>
            <p className="mt-2 text-sm leading-6 text-steward-mist-muted">{field.help || `Accessible label: ${field.accessibleLabel}.`}</p>
            <p className="mt-2 text-xs text-steward-mist-muted">{field.required ? 'Required' : 'Optional'}{field.allowHolding ? ' · Missing references may be held visibly' : ''}{field.options ? ` · ${field.options.join(', ')}` : ''}</p>
          </article>)}
        </div>
      </div>
    </div>}

    <details className={`${subpanelClass} mt-6 p-5`}>
      <summary className="cursor-pointer text-base font-semibold">Create a custom template</summary>
      <form className="mt-5 grid gap-5" onSubmit={createTemplate}>
        <div className="grid gap-4 md:grid-cols-2">
          <div><label className={labelClass} htmlFor="patternName">Template name</label><input className={inputClass} id="patternName" maxLength={160} name="patternName" required /></div>
          <div><label className={labelClass} htmlFor="patternRecordType">Record type</label><input className={inputClass} id="patternRecordType" name="patternRecordType" pattern="[a-z][a-z0-9.-]{1,79}" placeholder="example.record" required /></div>
          <div className="md:col-span-2"><label className={labelClass} htmlFor="patternDescription">Description <span className="font-normal">(optional)</span></label><textarea className={`${inputClass} min-h-20`} id="patternDescription" maxLength={1000} name="patternDescription" /></div>
        </div>
        <fieldset>
          <legend className="text-base font-semibold">Fields</legend>
          <p className="mt-1 text-sm text-steward-mist-muted">Money fields point to a text or enum currency field. References and attachments require a target record type.</p>
          <div className="mt-4 grid gap-4">
            {fields.map((field, index) => <div className={`${subpanelClass} grid gap-4 p-4 md:grid-cols-2`} key={index}>
              <div><label className={labelClass} htmlFor={`patternFieldKey-${index}`}>Field key</label><input className={inputClass} id={`patternFieldKey-${index}`} onChange={(event) => changeField(index, { key: event.target.value })} pattern="[A-Za-z][A-Za-z0-9_.-]{0,63}" required value={field.key} /></div>
              <div><label className={labelClass} htmlFor={`patternFieldLabel-${index}`}>Field label</label><input className={inputClass} id={`patternFieldLabel-${index}`} maxLength={120} onChange={(event) => changeField(index, { label: event.target.value })} required value={field.label} /></div>
              <div><label className={labelClass} htmlFor={`patternFieldType-${index}`}>Field type</label><select className={inputClass} id={`patternFieldType-${index}`} onChange={(event) => changeField(index, { type: event.target.value as PatternFieldType })} value={field.type}>{fieldTypes.map((type) => <option key={type}>{type}</option>)}</select></div>
              <div><label className={labelClass} htmlFor={`patternFieldHelp-${index}`}>Help text <span className="font-normal">(optional)</span></label><input className={inputClass} id={`patternFieldHelp-${index}`} maxLength={500} onChange={(event) => changeField(index, { help: event.target.value })} value={field.help} /></div>
              {field.type === 'enum' && <div><label className={labelClass} htmlFor={`patternFieldOptions-${index}`}>Options, comma separated</label><input className={inputClass} id={`patternFieldOptions-${index}`} onChange={(event) => changeField(index, { options: event.target.value })} required value={field.options} /></div>}
              {(field.type === 'reference' || field.type === 'attachment') && <div><label className={labelClass} htmlFor={`patternFieldReference-${index}`}>Referenced record type</label><input className={inputClass} id={`patternFieldReference-${index}`} onChange={(event) => changeField(index, { referenceType: event.target.value })} pattern="[a-z][a-z0-9.-]{1,79}" required value={field.referenceType} /></div>}
              {field.type === 'money' && <div><label className={labelClass} htmlFor={`patternFieldCurrency-${index}`}>Currency field key</label><input className={inputClass} id={`patternFieldCurrency-${index}`} onChange={(event) => changeField(index, { currencyField: event.target.value })} required value={field.currencyField} /></div>}
              <div className="flex flex-wrap items-center gap-5 md:col-span-2">
                <label className="flex min-h-11 items-center gap-2 text-sm"><input checked={field.required} className="size-4 accent-steward-teal" onChange={(event) => changeField(index, { required: event.target.checked })} type="checkbox" />Required</label>
                {(field.type === 'reference' || field.type === 'attachment') && <label className="flex min-h-11 items-center gap-2 text-sm"><input checked={field.allowHolding} className="size-4 accent-steward-teal" onChange={(event) => changeField(index, { allowHolding: event.target.checked })} type="checkbox" />Allow visible holding record</label>}
                {fields.length > 1 && <button className={secondaryButtonClass} onClick={() => setFields((current) => current.filter((_, fieldIndex) => fieldIndex !== index))} type="button">Remove field</button>}
              </div>
            </div>)}
          </div>
          <button className={`${secondaryButtonClass} mt-4`} disabled={fields.length >= 64} onClick={() => setFields((current) => [...current, emptyField()])} type="button">Add another field</button>
        </fieldset>
        <div><button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'create' ? 'Creating…' : 'Create custom template'}</button></div>
      </form>
    </details>
  </section>
}
