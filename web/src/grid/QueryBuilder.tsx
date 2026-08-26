import { useEffect, useMemo, useRef, useState } from 'react'
import { compactInputClass, cx, labelClass, menuSurfaceClass, plainButtonClass, secondaryButtonClass } from '../ui'
import {
  conditionNeedsValue, emptyCondition, emptyGroup, encodeQuery, maximumEncodedQueryLength, operatorLabels, operatorsForKind,
  parseQuery, queryValueOptionLabel, type QueryCondition, type QueryField, type QueryGroup, type QueryJoin, type QueryModel,
  type QueryOperator, type QueryValueOption,
} from './queryLanguage'

// Requirements: REQ-ATLAS-001, REQ-WORKSPACE-001. Feature: experience.grid.

type QueryBuilderProps = {
  encoded: string
  error?: string
  fields: readonly QueryField[]
  model: QueryModel
  onEncodedChange: (value: string) => void
  onModelChange: (model: QueryModel) => void
  /**
   * Unique pickable values for a field, usually the distinct visible values.
   * The context is the query without the condition being edited, so the list
   * can narrow to rows the other conditions already match — pick
   * "Manufacturer is Lenovo" and the Model list only offers Lenovo models.
   */
  optionsForField?: (field: QueryField, context?: QueryModel) => readonly QueryValueOption[]
}

/** The query as it stands without the condition being edited. */
function modelWithoutCondition(model: QueryModel, conditionId: string): QueryModel {
  return {
    ...model,
    groups: model.groups.map((group) => ({
      ...group,
      conditions: group.conditions.filter((condition) => condition.id !== conditionId),
    })),
  }
}

export default function QueryBuilder({ encoded, error, fields, model, onEncodedChange, onModelChange, optionsForField }: QueryBuilderProps) {
  function updateGroup(id: string, change: (group: QueryGroup) => QueryGroup) {
    onModelChange({ ...model, groups: model.groups.map((group) => group.id === id ? change(group) : group) })
  }

  function updateCondition(groupId: string, conditionId: string, change: Partial<QueryCondition>) {
    updateGroup(groupId, (group) => ({
      ...group,
      conditions: group.conditions.map((condition) => condition.id === conditionId ? { ...condition, ...change } : condition),
    }))
  }

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-xs font-medium text-steward-mist">Match</p>
        <JoinSelect
          label="How filter groups combine"
          onChange={(groupJoin) => onModelChange({ ...model, groupJoin })}
          value={model.groupJoin}
        />
        <p className="text-xs text-steward-mist-muted">the groups below.</p>
      </div>
      {model.groups.map((group, index) => (
        <div className="rounded-md border border-white/10 bg-steward-ink-950/40 p-3" key={group.id}>
          {index > 0 && <p className="mb-2 text-xs font-medium text-steward-slate">{model.groupJoin}</p>}
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-xs text-steward-mist-muted">Match</p>
              <JoinSelect
                label={`How conditions in group ${index + 1} combine`}
                onChange={(join) => updateGroup(group.id, (current) => ({ ...current, join }))}
                value={group.join}
              />
              <p className="text-xs text-steward-mist-muted">of these conditions</p>
            </div>
            {model.groups.length > 1 && (
              <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => onModelChange({ ...model, groups: model.groups.filter((item) => item.id !== group.id) })} type="button">
                Remove group
              </button>
            )}
          </div>
          <ul className="grid gap-2">
            {group.conditions.map((condition) => {
              const field = fields.find((candidate) => candidate.key === condition.field)
              const operators = operatorsForKind(field?.kind)
              const operator = operators.includes(condition.operator) ? condition.operator : operators[0]
              const valueOptions = field
                ? optionsForField?.(field, modelWithoutCondition(model, condition.id)) ?? (field.options ?? []).map((option) => ({ value: option }))
                : []
              return (
                <li className="grid gap-2 sm:grid-cols-[minmax(8rem,1fr)_minmax(7rem,11rem)_minmax(8rem,1.4fr)_auto]" key={condition.id}>
                  <label className="min-w-0">
                    <span className="sr-only">Field</span>
                    <select className={compactInputClass} onChange={(event) => updateCondition(group.id, condition.id, { field: event.target.value, operator: 'eq', value: '' })} value={condition.field}>
                      <option value="">Choose field</option>
                      {fields.map((item) => <option key={item.key} value={item.key}>{item.header}</option>)}
                    </select>
                  </label>
                  <label className="min-w-0">
                    <span className="sr-only">Operator</span>
                    <select className={compactInputClass} onChange={(event) => updateCondition(group.id, condition.id, { operator: event.target.value as QueryOperator, value: conditionNeedsValue(event.target.value as QueryOperator) ? condition.value : '' })} value={operator}>
                      {operators.map((item) => <option key={item} value={item}>{operatorLabels[item]}</option>)}
                    </select>
                  </label>
                  {conditionNeedsValue(operator) ? (
                    operator === 'in' || operator === 'not_in' ? (
                      <ValueMultiPicker
                        key={`${condition.field}-multi`}
                        label={`Values for ${field?.header ?? 'condition'}`}
                        onChange={(value) => updateCondition(group.id, condition.id, { value })}
                        options={valueOptions}
                        value={condition.value}
                      />
                    ) : valueOptions.length > 0 && (operator === 'eq' || operator === 'neq') ? (
                      <ValueCombobox
                        key={`${condition.field}-single`}
                        label={`Value for ${field?.header ?? 'condition'}`}
                        onChange={(value) => updateCondition(group.id, condition.id, { value })}
                        options={valueOptions}
                        value={condition.value}
                      />
                    ) : (
                      <label className="min-w-0">
                        <span className="sr-only">Value</span>
                        <input className={compactInputClass} onChange={(event) => updateCondition(group.id, condition.id, { value: event.target.value })} placeholder="Value" value={condition.value} />
                      </label>
                    )
                  ) : <span className="self-center text-xs text-steward-slate">No value</span>}
                  <button
                    className={cx(plainButtonClass, 'min-h-10 px-2 py-1 text-xs')}
                    disabled={group.conditions.length === 1 && model.groups.length === 1}
                    onClick={() => updateGroup(group.id, (current) => ({ ...current, conditions: current.conditions.filter((item) => item.id !== condition.id) }))}
                    type="button"
                  >Remove</button>
                </li>
              )
            })}
          </ul>
          <button className={cx(plainButtonClass, 'mt-2 min-h-8 px-2 py-1 text-xs')} onClick={() => updateGroup(group.id, (current) => ({ ...current, conditions: [...current.conditions, emptyCondition()] }))} type="button">
            Add condition
          </button>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        <button className={cx(secondaryButtonClass, 'min-h-8 px-3 py-1 text-xs')} onClick={() => onModelChange({ ...model, groups: [...model.groups, emptyGroup()] })} type="button">
          Add group
        </button>
      </div>
      <label>
        <span className={labelClass}>Query</span>
        <input
          aria-invalid={error ? true : undefined}
          className={cx(compactInputClass, 'mt-1.5 w-full font-mono text-xs')}
          maxLength={maximumEncodedQueryLength}
          onChange={(event) => onEncodedChange(event.target.value.slice(0, maximumEncodedQueryLength))}
          onBlur={(event) => {
            const parsed = parseQuery(event.target.value, fields)
            if (parsed.ok && encodeQuery(parsed.model) !== event.target.value.trim()) onEncodedChange(encodeQuery(parsed.model) || event.target.value)
          }}
          placeholder="status=active^nameLIKElab^ORkind=server"
          spellCheck={false}
          value={encoded}
        />
      </label>
      {error ? <p className="text-xs text-[#ffccd1]" role="alert">{error}</p> : <p className="text-xs leading-5 text-steward-slate">Use =, !=, LIKE, NOTLIKE, STARTSWITH, ENDSWITH, IN, ISEMPTY, ISNOTEMPTY, &gt;, and &lt;. Combine with ^ (AND), ^OR, ^NQ (new group), or parentheses.</p>}
    </div>
  )
}

function JoinSelect({ label, onChange, value }: { label: string; onChange: (join: QueryJoin) => void; value: QueryJoin }) {
  return (
    <select aria-label={label} className={cx(compactInputClass, 'w-auto min-h-8 px-2 py-1 text-xs')} onChange={(event) => onChange(event.target.value as QueryJoin)} value={value}>
      <option value="AND">all (AND)</option>
      <option value="OR">any (OR)</option>
    </select>
  )
}

const maximumVisibleOptions = 50

function filterValueOptions(options: readonly QueryValueOption[], query: string) {
  const needle = query.trim().toLowerCase()
  if (!needle) return options.slice(0, maximumVisibleOptions)
  return options
    .filter((option) => queryValueOptionLabel(option).toLowerCase().includes(needle) || option.value.toLowerCase().includes(needle))
    .slice(0, maximumVisibleOptions)
}

/** Closes a picker dropdown when the pointer lands outside of it. */
function useOutsideClose(open: boolean, ref: React.RefObject<HTMLElement | null>, onClose: () => void) {
  useEffect(() => {
    if (!open) return undefined
    function handlePointerDown(event: globalThis.PointerEvent) {
      if (!ref.current?.contains(event.target as Node)) onClose()
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    return () => document.removeEventListener('pointerdown', handlePointerDown, true)
  })
}

function OptionRow({ option, onPick, picked }: { option: QueryValueOption; onPick: () => void; picked?: boolean }) {
  return (
    <button
      aria-pressed={picked}
      className={cx(plainButtonClass, 'flex min-h-8 w-full items-center justify-between gap-2 px-2 py-1 text-left text-xs', picked && 'text-steward-teal')}
      onClick={onPick}
      role="option"
      aria-selected={picked ?? false}
      type="button"
    >
      <span className="min-w-0 truncate">{picked !== undefined && <span aria-hidden="true" className="mr-1.5">{picked ? '✓' : '+'}</span>}{queryValueOptionLabel(option)}</span>
      {typeof option.count === 'number' && option.count > 0 && <span className="shrink-0 text-[11px] text-steward-slate">{option.count}</span>}
    </button>
  )
}

/**
 * The value editor for "is" and "is not": a text box that suggests the unique
 * values the operator can pick from. Choosing a suggestion stores its canonical
 * value (a record id for lookup fields) while the box keeps showing the label.
 */
function ValueCombobox({ label, onChange, options, value }: {
  label: string
  onChange: (value: string) => void
  options: readonly QueryValueOption[]
  value: string
}) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<string | null>(null)
  // Values picked from the list keep their label even after the option list
  // recomputes without them, e.g. when a refetch leaves no matching rows.
  const [lastPick, setLastPick] = useState<QueryValueOption | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  useOutsideClose(open, ref, () => { setOpen(false); setDraft(null) })
  const selected = options.find((option) => option.value === value)
    ?? (lastPick?.value === value ? lastPick : undefined)
  const display = draft ?? (selected ? queryValueOptionLabel(selected) : value)
  const filtered = filterValueOptions(options, draft ?? '')
  return (
    <div className="relative min-w-0" ref={ref}>
      <label className="block min-w-0">
        <span className="sr-only">{label}</span>
        <input
          aria-expanded={open}
          className={compactInputClass}
          onChange={(event) => {
            setDraft(event.target.value)
            onChange(event.target.value)
            setOpen(true)
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(event) => {
            if (event.key === 'Escape') { setOpen(false); setDraft(null) }
            if (event.key === 'Enter') { setOpen(false); setDraft(null); event.preventDefault() }
          }}
          placeholder="Value"
          role="combobox"
          value={display}
        />
      </label>
      {open && filtered.length > 0 && (
        <div className={cx(menuSurfaceClass, 'absolute left-0 top-full z-20 mt-1 max-h-56 w-full min-w-44 max-w-80 overflow-y-auto p-1 steward-scrollbar')} role="listbox">
          {filtered.map((option) => (
            <OptionRow
              key={option.value}
              onPick={() => {
                onChange(option.value)
                setLastPick(option)
                setDraft(null)
                setOpen(false)
              }}
              option={option}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function splitListValue(value: string) {
  const seen = new Set<string>()
  const values: string[] = []
  for (const part of value.split(',')) {
    const trimmed = part.trim()
    if (!trimmed || seen.has(trimmed.toLowerCase())) continue
    seen.add(trimmed.toLowerCase())
    values.push(trimmed)
  }
  return values
}

/**
 * The value editor for "is one of" and "is not one of": selected values render
 * as removable chips, and the + control opens a searchable unique list.
 */
function ValueMultiPicker({ label, onChange, options, value }: {
  label: string
  onChange: (value: string) => void
  options: readonly QueryValueOption[]
  value: string
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  // Labels of picked options, kept so chips stay readable even after the
  // option list recomputes without those values.
  const pickedLabels = useRef(new Map<string, string>())
  useOutsideClose(open, ref, () => setOpen(false))
  useEffect(() => {
    if (open) searchRef.current?.focus()
  }, [open])
  const values = useMemo(() => splitListValue(value), [value])
  const labelFor = (item: string) => {
    const option = options.find((candidate) => candidate.value === item)
    return option ? queryValueOptionLabel(option) : pickedLabels.current.get(item) ?? item
  }
  const has = (item: string) => values.some((candidate) => candidate.toLowerCase() === item.toLowerCase())
  function toggle(item: string, label?: string) {
    if (label) pickedLabels.current.set(item, label)
    const next = has(item) ? values.filter((candidate) => candidate.toLowerCase() !== item.toLowerCase()) : [...values, item]
    onChange(next.join(', '))
  }
  const filtered = filterValueOptions(options, search)
  const custom = search.replaceAll(',', ' ').replaceAll(/\s+/g, ' ').trim()
  const customIsNew = custom.length > 0
    && !options.some((option) => option.value.toLowerCase() === custom.toLowerCase() || queryValueOptionLabel(option).toLowerCase() === custom.toLowerCase())
  return (
    <div className="relative min-w-0" ref={ref}>
      <div className="flex min-h-10 min-w-0 flex-wrap items-center gap-1 rounded-md bg-steward-ink-950 px-1.5 py-1 ring-1 ring-inset ring-white/10">
        {values.length === 0 && <span className="px-1 text-sm text-steward-mist-muted">Pick values</span>}
        {values.map((item) => (
          <span className="inline-flex max-w-full items-center gap-1 rounded-full border border-white/10 bg-white/[0.04] px-2 py-0.5 text-xs text-steward-mist" key={item}>
            <span className="min-w-0 truncate">{labelFor(item)}</span>
            <button
              aria-label={`Remove ${labelFor(item)}`}
              className="shrink-0 text-steward-slate transition hover:text-steward-mist"
              onClick={() => toggle(item)}
              type="button"
            >×</button>
          </span>
        ))}
        <button
          aria-expanded={open}
          aria-label={`Add to ${label}`}
          className={cx(plainButtonClass, 'min-h-7 shrink-0 px-2 py-0.5 text-xs')}
          onClick={() => setOpen((current) => !current)}
          type="button"
        ><span aria-hidden="true">+</span> Add</button>
      </div>
      {open && (
        <div className={cx(menuSurfaceClass, 'absolute left-0 top-full z-20 mt-1 w-full min-w-52 max-w-80 p-1.5')}>
          <label className="block">
            <span className="sr-only">Search values</span>
            <input
              className={cx(compactInputClass, 'min-h-8 w-full px-2 py-1 text-xs')}
              onChange={(event) => setSearch(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Escape') setOpen(false)
                if (event.key === 'Enter') {
                  event.preventDefault()
                  if (customIsNew) {
                    toggle(custom)
                    setSearch('')
                  } else if (filtered.length === 1) {
                    toggle(filtered[0].value, queryValueOptionLabel(filtered[0]))
                    setSearch('')
                  }
                }
              }}
              placeholder="Search values"
              ref={searchRef}
              type="search"
              value={search}
            />
          </label>
          <div className="mt-1 max-h-56 overflow-y-auto steward-scrollbar" role="listbox">
            {filtered.map((option) => (
              <OptionRow key={option.value} onPick={() => toggle(option.value, queryValueOptionLabel(option))} option={option} picked={has(option.value)} />
            ))}
            {customIsNew && (
              <button
                className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-left text-xs')}
                onClick={() => {
                  toggle(custom)
                  setSearch('')
                }}
                type="button"
              ><span aria-hidden="true" className="mr-1.5">+</span>Add “{custom}”</button>
            )}
            {filtered.length === 0 && !customIsNew && <p className="px-2 py-1.5 text-xs text-steward-slate">No matching values.</p>}
          </div>
        </div>
      )}
    </div>
  )
}
