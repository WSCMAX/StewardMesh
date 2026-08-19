import { type ChangeEvent, type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { parsePatternCsv, serializePatternCsv } from './patternCsv'
import { ProductHeader, buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

type PatternFieldType = 'text' | 'number' | 'date' | 'money' | 'enum' | 'attachment' | 'reference' | 'tag'

type PatternField = {
  key: string
  label: string
  help?: string
  type: PatternFieldType
  required: boolean
  allowHolding?: boolean
  referenceType?: string
  tagDefinitionId?: string
  options?: string[]
  currencyField?: string
  minimum?: number
  maximum?: number
  maximumLength?: number
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

type PatternValidationResult = {
  status: 'valid' | 'holding' | 'invalid'
  normalizedValues: Record<string, unknown>
  errors: { field: string, code: string, message: string }[]
  holdingReferences: { field: string, referenceType: string, value?: string }[]
}

type DraftField = {
  key: string
  label: string
  help: string
  type: PatternFieldType
  required: boolean
  allowHolding: boolean
  referenceType: string
  tagDefinitionId: string
  options: string
  currencyField: string
  accessibleLabel: string
  csvHeader: string
  minimum: string
  maximum: string
  maximumLength: string
}

const fieldTypes: PatternFieldType[] = ['text', 'number', 'date', 'money', 'enum', 'attachment', 'reference', 'tag']

function emptyField(): DraftField {
  return { key: '', label: '', help: '', type: 'text', required: false, allowHolding: false, referenceType: '', tagDefinitionId: '', options: '', currencyField: '', accessibleLabel: '', csvHeader: '', minimum: '', maximum: '', maximumLength: '' }
}

function selectionKey(template: Pick<PatternTemplate, 'id' | 'version'>): string {
  return `${template.id}\u0000${template.version}`
}

function draftFromField(field: PatternField): DraftField {
  return {
    key: field.key, label: field.label, help: field.help ?? '', type: field.type, required: field.required,
    allowHolding: field.allowHolding ?? false, referenceType: field.referenceType ?? '', tagDefinitionId: field.tagDefinitionId ?? '', options: field.options?.join(', ') ?? '',
    currencyField: field.currencyField ?? '', accessibleLabel: field.accessibleLabel, csvHeader: field.csvHeader,
    minimum: field.minimum === undefined ? '' : String(field.minimum), maximum: field.maximum === undefined ? '' : String(field.maximum),
    maximumLength: field.maximumLength === undefined ? '' : String(field.maximumLength),
  }
}

function optionalNumber(value: string): number | undefined {
  if (value.trim() === '') return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

function fieldPayload(field: DraftField): PatternField {
  return {
    key: field.key.trim(), label: field.label.trim(), help: field.help.trim(), type: field.type, required: field.required,
    allowHolding: (field.type === 'reference' || field.type === 'attachment') && field.allowHolding,
    referenceType: field.type === 'reference' || field.type === 'attachment' ? field.referenceType.trim() : undefined,
    tagDefinitionId: field.type === 'tag' ? field.tagDefinitionId.trim() : undefined,
    options: field.type === 'enum' ? field.options.split(',').map((option) => option.trim()).filter(Boolean) : undefined,
    currencyField: field.type === 'money' ? field.currencyField.trim() : undefined,
    accessibleLabel: field.accessibleLabel.trim(), csvHeader: field.csvHeader.trim(),
    minimum: field.type === 'number' || field.type === 'money' ? optionalNumber(field.minimum) : undefined,
    maximum: field.type === 'number' || field.type === 'money' ? optionalNumber(field.maximum) : undefined,
    maximumLength: field.type === 'text' && field.maximumLength.trim() !== '' ? Number(field.maximumLength) : undefined,
  }
}

function DraftFieldsEditor({ idPrefix, labelPrefix = '', fields, setFields, labelDefinitions }: {
  idPrefix: string
  labelPrefix?: string
  fields: DraftField[]
  setFields: (updater: (current: DraftField[]) => DraftField[]) => void
  labelDefinitions: { id: string, name: string }[]
}) {
  const change = (index: number, update: Partial<DraftField>) => setFields((current) => current.map((field, fieldIndex) => fieldIndex === index ? { ...field, ...update } : field))
  return <>
    <div className="mt-4 grid gap-4">
      {fields.map((field, index) => <div className={`${subpanelClass} grid gap-4 p-4 md:grid-cols-2`} key={index}>
        <div><label className={labelClass} htmlFor={`${idPrefix}Key-${index}`}>{labelPrefix}Field key</label><input className={inputClass} id={`${idPrefix}Key-${index}`} onChange={(event) => change(index, { key: event.target.value })} pattern="[A-Za-z][A-Za-z0-9_.\-]{0,63}" required value={field.key} /></div>
        <div><label className={labelClass} htmlFor={`${idPrefix}Label-${index}`}>{labelPrefix}Field label</label><input className={inputClass} id={`${idPrefix}Label-${index}`} maxLength={120} onChange={(event) => change(index, { label: event.target.value })} required value={field.label} /></div>
        <div><label className={labelClass} htmlFor={`${idPrefix}Type-${index}`}>{labelPrefix}Field type</label><select className={inputClass} id={`${idPrefix}Type-${index}`} onChange={(event) => change(index, { type: event.target.value as PatternFieldType })} value={field.type}>{fieldTypes.map((type) => <option key={type}>{type}</option>)}</select></div>
        <div><label className={labelClass} htmlFor={`${idPrefix}Help-${index}`}>{labelPrefix}Help text <span className="font-normal">(optional)</span></label><input className={inputClass} id={`${idPrefix}Help-${index}`} maxLength={500} onChange={(event) => change(index, { help: event.target.value })} value={field.help} /></div>
        <div><label className={labelClass} htmlFor={`${idPrefix}Accessible-${index}`}>{labelPrefix}Accessible label <span className="font-normal">(defaults to label)</span></label><input className={inputClass} id={`${idPrefix}Accessible-${index}`} maxLength={160} onChange={(event) => change(index, { accessibleLabel: event.target.value })} value={field.accessibleLabel} /></div>
        <div><label className={labelClass} htmlFor={`${idPrefix}Csv-${index}`}>{labelPrefix}CSV header <span className="font-normal">(defaults to key)</span></label><input className={inputClass} id={`${idPrefix}Csv-${index}`} maxLength={160} onChange={(event) => change(index, { csvHeader: event.target.value })} value={field.csvHeader} /></div>
        {field.type === 'enum' && <div><label className={labelClass} htmlFor={`${idPrefix}Options-${index}`}>{labelPrefix}Options, comma separated</label><input className={inputClass} id={`${idPrefix}Options-${index}`} onChange={(event) => change(index, { options: event.target.value })} required value={field.options} /></div>}
        {(field.type === 'reference' || field.type === 'attachment') && <div><label className={labelClass} htmlFor={`${idPrefix}Reference-${index}`}>{labelPrefix}Referenced record type</label><input className={inputClass} id={`${idPrefix}Reference-${index}`} onChange={(event) => change(index, { referenceType: event.target.value })} pattern="[a-z][a-z0-9.\-]{1,79}" required value={field.referenceType} /></div>}
        {field.type === 'tag' && <div><label className={labelClass} htmlFor={`${idPrefix}TagDefinition-${index}`}>{labelPrefix}Label definition</label><select className={inputClass} id={`${idPrefix}TagDefinition-${index}`} onChange={(event) => change(index, { tagDefinitionId: event.target.value })} required value={field.tagDefinitionId}><option value="">Choose a label definition</option>{labelDefinitions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}</select></div>}
        {field.type === 'money' && <div><label className={labelClass} htmlFor={`${idPrefix}Currency-${index}`}>{labelPrefix}Currency field key</label><input className={inputClass} id={`${idPrefix}Currency-${index}`} onChange={(event) => change(index, { currencyField: event.target.value })} required value={field.currencyField} /></div>}
        {(field.type === 'number' || field.type === 'money') && <>
          <div><label className={labelClass} htmlFor={`${idPrefix}Minimum-${index}`}>{labelPrefix}Minimum <span className="font-normal">(optional)</span></label><input className={inputClass} id={`${idPrefix}Minimum-${index}`} onChange={(event) => change(index, { minimum: event.target.value })} step="any" type="number" value={field.minimum} /></div>
          <div><label className={labelClass} htmlFor={`${idPrefix}Maximum-${index}`}>{labelPrefix}Maximum <span className="font-normal">(optional)</span></label><input className={inputClass} id={`${idPrefix}Maximum-${index}`} onChange={(event) => change(index, { maximum: event.target.value })} step="any" type="number" value={field.maximum} /></div>
        </>}
        {field.type === 'text' && <div><label className={labelClass} htmlFor={`${idPrefix}MaximumLength-${index}`}>{labelPrefix}Maximum length <span className="font-normal">(optional)</span></label><input className={inputClass} id={`${idPrefix}MaximumLength-${index}`} min="1" onChange={(event) => change(index, { maximumLength: event.target.value })} step="1" type="number" value={field.maximumLength} /></div>}
        <div className="flex flex-wrap items-center gap-5 md:col-span-2">
          <label className="flex min-h-11 items-center gap-2 text-sm"><input checked={field.required} className="size-4 accent-steward-teal" onChange={(event) => change(index, { required: event.target.checked })} type="checkbox" />Required</label>
          {(field.type === 'reference' || field.type === 'attachment') && <label className="flex min-h-11 items-center gap-2 text-sm"><input checked={field.allowHolding} className="size-4 accent-steward-teal" onChange={(event) => change(index, { allowHolding: event.target.checked })} type="checkbox" />Allow visible holding record</label>}
          {fields.length > 1 && <button className={secondaryButtonClass} onClick={() => setFields((current) => current.filter((_, fieldIndex) => fieldIndex !== index))} type="button">Remove field</button>}
        </div>
      </div>)}
    </div>
    <button className={`${secondaryButtonClass} mt-4`} disabled={fields.length >= 64} onClick={() => setFields((current) => [...current, emptyField()])} type="button">Add another field</button>
  </>
}

function isPatternField(value: unknown): value is PatternField {
  if (typeof value !== 'object' || value === null) return false
  const field = value as Record<string, unknown>
  const finiteOptional = (key: string) => field[key] === undefined || (typeof field[key] === 'number' && Number.isFinite(field[key]))
  const lengthOptional = field.maximumLength === undefined || (Number.isInteger(field.maximumLength) && Number(field.maximumLength) >= 0)
  return typeof field.key === 'string' && typeof field.label === 'string' && fieldTypes.includes(field.type as PatternFieldType)
    && typeof field.required === 'boolean' && typeof field.accessibleLabel === 'string' && typeof field.csvHeader === 'string'
    && (field.tagDefinitionId === undefined || typeof field.tagDefinitionId === 'string')
    && finiteOptional('minimum') && finiteOptional('maximum') && lengthOptional
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

function isPatternValidationResult(value: unknown): value is PatternValidationResult {
  if (typeof value !== 'object' || value === null) return false
  const result = value as Record<string, unknown>
  return (result.status === 'valid' || result.status === 'holding' || result.status === 'invalid')
    && typeof result.normalizedValues === 'object' && result.normalizedValues !== null
    && Array.isArray(result.errors) && Array.isArray(result.holdingReferences)
}

function valuesForValidation(template: PatternTemplate, values: Record<string, string>): Record<string, string | number> {
  const result: Record<string, string | number> = {}
  template.fields.forEach((field) => {
    const value = values[field.key] ?? ''
    if (value === '') return
    if (field.type === 'money') {
      result[field.key] = /^-?(0|[1-9]\d*)$/.test(value) && Number.isSafeInteger(Number(value)) ? Number(value) : value
      return
    }
    if (field.type === 'number') {
      const parsed = Number(value)
      result[field.key] = Number.isFinite(parsed) ? parsed : value
      return
    }
    result[field.key] = value
  })
  return result
}

export default function PatternsManager({ csrfToken, permissions = [] }: { csrfToken: string, permissions?: readonly string[] }) {
  const [templates, setTemplates] = useState<PatternTemplate[]>([])
  const [labelDefinitions, setLabelDefinitions] = useState<{ id: string, name: string }[]>([])
  const [selectedKey, setSelectedKey] = useState('')
  const [fields, setFields] = useState<DraftField[]>([emptyField()])
  const [versionFields, setVersionFields] = useState<DraftField[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [recordValues, setRecordValues] = useState<Record<string, string>>({})
  const [missingReferences, setMissingReferences] = useState<Record<string, boolean>>({})
  const [csvText, setCsvText] = useState('')
  const [csvDownload, setCsvDownload] = useState('')
  const [validation, setValidation] = useState<PatternValidationResult | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const fieldRefs = useRef<Record<string, HTMLInputElement | HTMLSelectElement | null>>({})
  const selected = templates.find((template) => selectionKey(template) === selectedKey) ?? templates[0]
  const selectedIsLatest = selected !== undefined && !templates.some((template) => template.id === selected.id && template.version > selected.version)

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  useEffect(() => {
    setRecordValues({})
    setMissingReferences({})
    setCsvText('')
    setCsvDownload('')
    setValidation(null)
    setVersionFields(selected?.fields.map(draftFromField) ?? [])
  }, [selected?.id, selected?.version])

  const loadTemplates = useCallback(async (signal?: AbortSignal) => {
    const items = readTemplates(await requestJSON('/api/v1/templates?includeVersions=true', { signal }))
    setTemplates(items)
    setSelectedKey((current) => items.some((item) => selectionKey(item) === current) ? current : items[0] ? selectionKey(items[0]) : '')
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

  useEffect(() => {
    if (!permissions.includes('labels.read')) return
    let active = true
    requestJSON('/api/v1/labels/definitions')
      .then((response) => {
        if (typeof response !== 'object' || response === null || !Array.isArray((response as Record<string, unknown>).items)) return
        const items = ((response as Record<string, unknown>).items as unknown[])
          .filter((item): item is { id: string, name: string } => typeof item === 'object' && item !== null && typeof (item as Record<string, unknown>).id === 'string' && typeof (item as Record<string, unknown>).name === 'string')
        if (active) setLabelDefinitions(items)
      })
      .catch(() => {})
    return () => { active = false }
  }, [permissions])

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
          fields: fields.map(fieldPayload),
        }),
      })
      if (!isPatternTemplate(response)) throw new Error('invalid created template')
      await loadTemplates()
      setSelectedKey(selectionKey(response))
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
      setSelectedKey(selectionKey(response))
      form.reset()
      setStatus('Editable template copy created.')
    } catch (mutationError) {
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The template copy could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function appendVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || selected.builtIn) return
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('version')
    setError('')
    setStatus('')
    try {
      const response = await requestJSON(`/api/v1/templates/${encodeURIComponent(selected.id)}/versions`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ description: String(values.get('patternVersionDescription') ?? '').trim(), fields: versionFields.map(fieldPayload) }),
      })
      if (!isPatternTemplate(response)) throw new Error('invalid versioned template')
      await loadTemplates()
      setSelectedKey(selectionKey(response))
      form.reset()
      setStatus(`Custom template version ${response.version} appended.`)
    } catch (mutationError) {
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The new template version could not be appended.')
    } finally {
      setBusy('')
    }
  }

  async function validateRecord(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected) return
    setBusy('validate')
    setError('')
    setStatus('')
    setValidation(null)
    try {
      const response = await requestJSON(`/api/v1/templates/${encodeURIComponent(selected.id)}/validate?version=${selected.version}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          values: valuesForValidation(selected, recordValues),
          missingReferences: selected.fields.filter((field) => missingReferences[field.key]).map((field) => field.key),
          allowHoldingRecord: true,
        }),
      })
      if (!isPatternValidationResult(response)) throw new Error('invalid validation response')
      setValidation(response)
      const normalized: Record<string, string> = {}
      Object.entries(response.normalizedValues).forEach(([key, value]) => { normalized[key] = String(value) })
      setRecordValues((current) => ({ ...current, ...normalized }))
      setStatus(response.status === 'valid' ? `${selected.name} version ${selected.version} is valid.`
        : response.status === 'holding' ? 'The row is valid as a visible holding record.'
          : 'The server found fields that need attention.')
      if (response.status === 'invalid') {
        const firstField = response.errors.find((item) => selected.fields.some((field) => field.key === item.field))?.field
        if (firstField) requestAnimationFrame(() => fieldRefs.current[firstField]?.focus())
      }
    } catch (validationError) {
      setError(validationError instanceof ApiRequestError ? validationError.message : 'The record could not be validated.')
    } finally {
      setBusy('')
    }
  }

  function importCsvRow() {
    if (!selected) return
    setError('')
    setStatus('')
    setValidation(null)
    try {
      const parsed = parsePatternCsv(selected, csvText)
      setRecordValues(Object.fromEntries(Object.entries(parsed).map(([key, value]) => [key, String(value)])))
      setCsvDownload('')
      setStatus(`One CSV row was loaded against ${selected.id} version ${selected.version}. Validate it before use.`)
    } catch (csvError) {
      setError(csvError instanceof Error ? csvError.message : 'The CSV row could not be imported.')
    }
  }

  function exportCsvRow() {
    if (!selected) return
    setError('')
    setStatus('')
    try {
      const csv = serializePatternCsv(selected, valuesForValidation(selected, recordValues))
      setCsvDownload(`data:text/csv;charset=utf-8,${encodeURIComponent(csv)}`)
      setStatus(`The current row is ready to download for ${selected.id} version ${selected.version}.`)
    } catch (csvError) {
      setCsvDownload('')
      setError(csvError instanceof Error ? csvError.message : 'The CSV row could not be exported.')
    }
  }

  return <section aria-labelledby="patterns-heading" className={`${panelClass} min-w-0 max-w-full overflow-hidden p-5 sm:p-6`} data-feature="templates.schemas" data-requirement="REQ-PATTERNS-001">
    <ProductHeader
      description="Use built-in schemas as form, API, CSV, and Exchange contracts. Custom versions are append-only, and every operation pins the exact template ID and version."
      headingId="patterns-heading"
      kicker="Patterns"
      title="Versioned record templates"
    />
    <p className="mt-2 text-sm text-steward-mist-muted">A missing reference can become a visible holding record only when the field allows it; it is never silently accepted.</p>
    {error && <div aria-live="assertive" className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/10 p-4 text-sm text-[#ffbdc3]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
    {status && <p aria-live="polite" className="mt-4 rounded-xl border border-steward-success/35 bg-steward-success/10 p-4 text-sm text-[#98eab9]" role="status">{status}</p>}
    {loading ? <p className="mt-5 text-sm text-steward-mist-muted">Loading templates…</p> : <div className="mt-6 grid min-w-0 gap-6 xl:grid-cols-[minmax(15rem,0.7fr)_minmax(0,1.3fr)]">
      <div className="min-w-0">
        <label className={labelClass} htmlFor="patternTemplate">Template and version</label>
        <select className={inputClass} id="patternTemplate" onChange={(event) => setSelectedKey(event.target.value)} value={selected ? selectionKey(selected) : ''}>
          {templates.map((template) => <option key={selectionKey(template)} value={selectionKey(template)}>{template.name} · v{template.version}{template.builtIn ? ' · built in' : ''}</option>)}
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
          {!selected.builtIn && !selectedIsLatest && <p className="mt-5 border-t border-steward-mist/10 pt-4 text-sm text-steward-mist-muted">Select the latest version of this template to append its next immutable version.</p>}
          {!selected.builtIn && selectedIsLatest && <details className="mt-5 border-t border-steward-mist/10 pt-4">
            <summary className="cursor-pointer font-semibold">Append a custom version</summary>
            <form className="mt-4 grid gap-4" onSubmit={appendVersion}>
              <div><label className={labelClass} htmlFor="patternVersionDescription">Version description <span className="font-normal">(optional)</span></label><textarea className={`${inputClass} min-h-20`} id="patternVersionDescription" maxLength={1000} name="patternVersionDescription" placeholder={selected.description} /></div>
              <fieldset>
                <legend className="font-semibold">Version fields</legend>
                <p className="mt-1 text-sm text-steward-mist-muted">Start from this exact version, then edit the next immutable field contract.</p>
                <DraftFieldsEditor fields={versionFields} idPrefix="patternVersionField" labelDefinitions={labelDefinitions} labelPrefix="Version " setFields={setVersionFields} />
              </fieldset>
              <div><button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'version' ? 'Appending…' : 'Append next immutable version'}</button></div>
            </form>
          </details>}
        </div>}
      </div>
      <div className="min-w-0">
        <h3 className="text-lg font-semibold">Field contract</h3>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {selected?.fields.map((field) => <article className={`${subpanelClass} p-4`} key={field.key}>
            <div className="flex flex-wrap items-start justify-between gap-2"><h4 className="font-semibold">{field.label}</h4><span className="rounded-md border border-steward-teal/35 px-2 py-1 font-mono text-xs text-steward-teal">{field.type}</span></div>
            <p className="mt-2 font-mono text-xs text-steward-mist-muted">{field.key} · CSV: {field.csvHeader}</p>
            <p className="mt-2 text-sm leading-6 text-steward-mist-muted">{field.help || `Accessible label: ${field.accessibleLabel}.`}</p>
            <p className="mt-2 text-xs text-steward-mist-muted">{field.required ? 'Required' : 'Optional'}{field.allowHolding ? ' · Missing references may be held visibly' : ''}{field.options ? ` · ${field.options.join(', ')}` : ''}{field.tagDefinitionId ? ` · Label: ${field.tagDefinitionId}` : ''}</p>
          </article>)}
        </div>
      </div>
    </div>}

    {selected && <section aria-labelledby="pattern-workbench-heading" className={`${subpanelClass} mt-6 p-5`}>
      <h3 className="text-lg font-semibold" id="pattern-workbench-heading">Generated record workbench</h3>
      <p className="mt-2 text-sm leading-6 text-steward-mist-muted">
        This form and its CSV row are pinned to <span className="font-mono text-steward-teal">{selected.id} · v{selected.version}</span>.
        The server remains authoritative for typed validation.
      </p>
      <form className="mt-5 grid gap-5" onSubmit={validateRecord}>
        <div className="grid gap-4 md:grid-cols-2">
          {selected.fields.map((field) => {
            const inputID = `patternValue-${field.key}`
            const helpID = `${inputID}-help`
            const fieldErrors = validation?.errors.filter((item) => item.field === field.key) ?? []
            const errorIDs = fieldErrors.map((_, index) => `${inputID}-error-${index}`)
            const shared = {
              'aria-describedby': [helpID, ...errorIDs].join(' '),
              'aria-invalid': fieldErrors.length > 0 || undefined,
              className: inputClass,
              id: inputID,
              name: field.key,
              onChange: (event: ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setRecordValues((current) => ({ ...current, [field.key]: event.target.value })),
              ref: (node: HTMLInputElement | HTMLSelectElement | null) => { fieldRefs.current[field.key] = node },
              required: field.required,
              value: recordValues[field.key] ?? '',
            }
            return <div key={field.key}>
              <label className={labelClass} htmlFor={inputID}>{field.accessibleLabel}{field.required ? ' (required)' : ''}</label>
              {field.type === 'enum'
                ? <select {...shared}><option value="">Choose…</option>{field.options?.map((option) => <option key={option} value={option}>{option}</option>)}</select>
                : field.type === 'tag'
                  ? <input {...shared} type="text" />
                : <input {...shared}
                  inputMode={field.type === 'number' || field.type === 'money' ? 'decimal' : undefined}
                  max={field.type === 'number' || field.type === 'money' ? field.maximum : undefined}
                  maxLength={field.type === 'text' || field.type === 'attachment' || field.type === 'reference' ? field.maximumLength : undefined}
                  min={field.type === 'number' || field.type === 'money' ? field.minimum : undefined}
                  step={field.type === 'money' ? '1' : field.type === 'number' ? 'any' : undefined}
                  type={field.type === 'date' ? 'date' : field.type === 'number' || field.type === 'money' ? 'number' : 'text'}
                />}
              <p className="mt-1 text-xs leading-5 text-steward-mist-muted" id={helpID}>
                {field.help || (field.type === 'reference' || field.type === 'attachment'
                  ? `Enter a stable ${field.referenceType} identifier.`
                  : field.type === 'tag'
                    ? `Enter a value validated against label definition ${field.tagDefinitionId}. For multi-select labels, separate values with commas.`
                  : field.type === 'money' ? 'Enter an exact integer amount in minor units.'
                    : `CSV column: ${field.csvHeader}.`)}
              </p>
              {fieldErrors.map((item, index) =>
                <p className="mt-1 text-sm text-[#ffbdc3]" id={errorIDs[index]} key={`${item.code}-${index}`}>{item.message}</p>)}
              {(field.type === 'reference' || field.type === 'attachment') && field.allowHolding &&
                <label className="mt-2 flex min-h-11 items-center gap-2 text-sm">
                  <input checked={missingReferences[field.key] ?? false} className="size-4 accent-steward-teal"
                    onChange={(event) => setMissingReferences((current) => ({ ...current, [field.key]: event.target.checked }))} type="checkbox" />
                  Mark {field.accessibleLabel.toLowerCase()} as unresolved
                </label>}
            </div>
          })}
        </div>
        {validation && <div aria-live="polite" className="rounded-xl border border-steward-teal/30 p-4 text-sm">
          <p className="font-semibold">Server result: {validation.status}</p>
          {validation.errors.length > 0 && <ul className="mt-2 list-disc space-y-1 pl-5">
            {validation.errors.map((item) => <li key={`${item.field}-${item.code}`}>{item.message}</li>)}
          </ul>}
          {validation.holdingReferences.length > 0 && <p className="mt-2">Holding references: {validation.holdingReferences.map((item) => item.field).join(', ')}</p>}
        </div>}
        <div><button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'validate' ? 'Validating…' : 'Validate exact version'}</button></div>
      </form>

      <div className="mt-7 border-t border-steward-mist/10 pt-6">
        <h4 className="font-semibold">Bounded CSV row</h4>
        <p className="mt-1 text-sm leading-6 text-steward-mist-muted">
          Paste exactly one header row and one data row (maximum 128 KiB). Headers must exactly match this version; text that begins with a spreadsheet formula character is rejected.
        </p>
        <label className={labelClass} htmlFor="patternCsvRow">CSV header and row</label>
        <textarea className={`${inputClass} min-h-32 font-mono text-xs`} id="patternCsvRow" onChange={(event) => setCsvText(event.target.value)} spellCheck={false} value={csvText} />
        <div className="mt-3 flex flex-wrap gap-3">
          <button className={secondaryButtonClass} onClick={importCsvRow} type="button">Import CSV row</button>
          <button className={secondaryButtonClass} onClick={exportCsvRow} type="button">Prepare current row CSV</button>
          {csvDownload && <a className={secondaryButtonClass} download={`${selected.recordType}-v${selected.version}.csv`} href={csvDownload}>Download current CSV row</a>}
        </div>
      </div>
    </section>}

    <details className={`${subpanelClass} mt-6 p-5`}>
      <summary className="cursor-pointer text-base font-semibold">Create a custom template</summary>
      <form className="mt-5 grid gap-5" onSubmit={createTemplate}>
        <div className="grid gap-4 md:grid-cols-2">
          <div><label className={labelClass} htmlFor="patternName">Template name</label><input className={inputClass} id="patternName" maxLength={160} name="patternName" required /></div>
          <div><label className={labelClass} htmlFor="patternRecordType">Record type</label><input className={inputClass} id="patternRecordType" name="patternRecordType" pattern="[a-z][a-z0-9.\-]{1,79}" placeholder="example.record" required /></div>
          <div className="md:col-span-2"><label className={labelClass} htmlFor="patternDescription">Description <span className="font-normal">(optional)</span></label><textarea className={`${inputClass} min-h-20`} id="patternDescription" maxLength={1000} name="patternDescription" /></div>
        </div>
        <fieldset>
          <legend className="text-base font-semibold">Fields</legend>
          <p className="mt-1 text-sm text-steward-mist-muted">Money fields point to a text or enum currency field. References and attachments require a target record type.</p>
          <DraftFieldsEditor fields={fields} idPrefix="patternField" labelDefinitions={labelDefinitions} setFields={setFields} />
        </fieldset>
        <div><button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'create' ? 'Creating…' : 'Create custom template'}</button></div>
      </form>
    </details>
  </section>
}
