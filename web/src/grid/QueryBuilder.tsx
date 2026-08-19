import { compactInputClass, cx, labelClass, plainButtonClass, secondaryButtonClass } from '../ui'
import {
  conditionNeedsValue, emptyCondition, emptyGroup, encodeQuery, maximumEncodedQueryLength, operatorLabels, operatorsForKind,
  parseQuery, type QueryCondition, type QueryField, type QueryGroup, type QueryJoin, type QueryModel, type QueryOperator,
} from './queryLanguage'

// Requirements: REQ-ATLAS-001, REQ-WORKSPACE-001. Feature: experience.grid.

type QueryBuilderProps = {
  encoded: string
  error?: string
  fields: readonly QueryField[]
  model: QueryModel
  onEncodedChange: (value: string) => void
  onModelChange: (model: QueryModel) => void
}

export default function QueryBuilder({ encoded, error, fields, model, onEncodedChange, onModelChange }: QueryBuilderProps) {
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
                    field?.options && field.options.length > 0 && (operator === 'eq' || operator === 'neq') ? (
                      <label className="min-w-0">
                        <span className="sr-only">Value</span>
                        <select className={compactInputClass} onChange={(event) => updateCondition(group.id, condition.id, { value: event.target.value })} value={condition.value}>
                          <option value="">Choose value</option>
                          {field.options.map((option) => <option key={option} value={option}>{option}</option>)}
                        </select>
                      </label>
                    ) : (
                      <label className="min-w-0">
                        <span className="sr-only">Value</span>
                        <input className={compactInputClass} onChange={(event) => updateCondition(group.id, condition.id, { value: event.target.value })} placeholder={operator === 'in' || operator === 'not_in' ? 'one, two' : 'Value'} value={condition.value} />
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
