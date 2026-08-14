import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import { buttonClass, dangerButtonClass, inputClass, labelClass, panelClass, secondaryButtonClass, StatusBadge, subpanelClass } from './ui'

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

type Condition = 'over_budget' | 'forecast_over_budget' | 'unpaid' | 'overdue' | 'expiration' | 'renewal' | 'unused_commitment' | 'reconciliation'
type Severity = 'info' | 'warning' | 'critical'
type AlertStatus = 'active' | 'acknowledged' | 'resolved'

type SignalRule = {
  id: string
  name: string
  condition: Condition
  severity: Severity
  enabled: boolean
  thresholdDays: number[]
  fiscalPeriod?: string
  scenario?: string
  revision: Revision
}

type SignalAlert = {
  id: string
  ruleId: string
  condition: Condition
  severity: Severity
  status: AlertStatus
  title: string
  summary: string
  targetType: string
  targetId: string
  dueAt?: string
  thresholdDays: number
  assignedKind?: string
  assignedId?: string
  acknowledgedBy?: string
  acknowledgedAt?: string
  firstDetectedAt: string
  lastObservedAt: string
  resolvedAt?: string
  revision: Revision
}

type SignalSubscription = {
  id: string
  ruleId?: string
  targetKind: 'group' | 'webhook'
  targetId: string
  enabled: boolean
  createdAt: string
}

type SubscriptionTarget = {
  targetKind: 'group' | 'webhook'
  targetId: string
  label: string
}

type EvaluationResult = { asOf: string; rules: number; created: number; refreshed: number; resolved: number }

const conditions: readonly Condition[] = ['over_budget', 'forecast_over_budget', 'unpaid', 'overdue', 'expiration', 'renewal', 'unused_commitment', 'reconciliation']
const severities: readonly Severity[] = ['info', 'warning', 'critical']
const statuses: readonly AlertStatus[] = ['active', 'acknowledged', 'resolved']

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isDateTime(value: unknown) {
  return typeof value === 'string' && value.length <= 64 && !Number.isNaN(Date.parse(value))
}

function isSignalRule(value: unknown): value is SignalRule {
  if (!isRecord(value)) return false
  return typeof value.id === 'string' && value.id.length > 0 && value.id.length <= 128
    && typeof value.name === 'string' && value.name.length > 0 && value.name.length <= 160
    && conditions.includes(value.condition as Condition) && severities.includes(value.severity as Severity)
    && typeof value.enabled === 'boolean' && Array.isArray(value.thresholdDays)
    && value.thresholdDays.length <= 8 && value.thresholdDays.every((day) => Number.isInteger(day) && Number(day) >= 0 && Number(day) <= 3660)
    && isRevision(value.revision)
}

function isSignalAlert(value: unknown): value is SignalAlert {
  if (!isRecord(value)) return false
  return typeof value.id === 'string' && value.id.length > 0 && value.id.length <= 128
    && typeof value.ruleId === 'string' && value.ruleId.length > 0 && value.ruleId.length <= 128
    && conditions.includes(value.condition as Condition) && severities.includes(value.severity as Severity) && statuses.includes(value.status as AlertStatus)
    && typeof value.title === 'string' && value.title.length > 0 && value.title.length <= 200
    && typeof value.summary === 'string' && value.summary.length > 0 && value.summary.length <= 500
    && typeof value.targetType === 'string' && typeof value.targetId === 'string'
    && Number.isInteger(value.thresholdDays) && Number(value.thresholdDays) >= 0 && Number(value.thresholdDays) <= 3660
    && isDateTime(value.firstDetectedAt) && isDateTime(value.lastObservedAt)
    && (value.dueAt === undefined || isDateTime(value.dueAt))
    && (value.acknowledgedAt === undefined || isDateTime(value.acknowledgedAt))
    && (value.resolvedAt === undefined || isDateTime(value.resolvedAt))
    && isRevision(value.revision)
}

function isSignalSubscription(value: unknown): value is SignalSubscription {
  if (!isRecord(value)) return false
  return typeof value.id === 'string' && value.id.length > 0 && value.id.length <= 128
    && (value.ruleId === undefined || typeof value.ruleId === 'string')
    && (value.targetKind === 'group' || value.targetKind === 'webhook')
    && typeof value.targetId === 'string' && value.targetId.length > 0 && value.targetId.length <= 128
    && typeof value.enabled === 'boolean' && isDateTime(value.createdAt)
}

function isSubscriptionTarget(value: unknown): value is SubscriptionTarget {
  return isRecord(value) && (value.targetKind === 'group' || value.targetKind === 'webhook')
    && typeof value.targetId === 'string' && value.targetId.length > 0 && value.targetId.length <= 128
    && typeof value.label === 'string' && value.label.length > 0 && value.label.length <= 160
}

function readItems<T>(value: unknown, validator: (item: unknown) => item is T, label: string): T[] {
  if (!isRecord(value) || !Array.isArray(value.items) || !value.items.every(validator)) throw new Error(`invalid ${label} response`)
  return value.items
}

function isEvaluationResult(value: unknown): value is EvaluationResult {
  if (!isRecord(value) || !isDateTime(value.asOf)) return false
  return ['rules', 'created', 'refreshed', 'resolved'].every((key) => Number.isInteger(value[key]) && Number(value[key]) >= 0)
}

function words(value: string) {
  return value.split('_').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ')
}

function dateLabel(value: string | undefined) {
  if (!value || Number.isNaN(Date.parse(value))) return 'Not scheduled'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function toneFor(alert: SignalAlert): 'success' | 'warning' | 'info' | 'neutral' {
  if (alert.status === 'resolved') return 'success'
  if (alert.severity === 'critical') return 'warning'
  if (alert.status === 'acknowledged') return 'info'
  return 'neutral'
}

export default function SignalsManager({ csrfToken, onOpenHelp, permissions }: { csrfToken: string; onOpenHelp?: () => void; permissions: readonly string[] }) {
  const canRead = permissions.includes('signals.read')
  const canWrite = permissions.includes('signals.write')
  const [rules, setRules] = useState<SignalRule[]>([])
  const [alerts, setAlerts] = useState<SignalAlert[]>([])
  const [subscriptions, setSubscriptions] = useState<SignalSubscription[]>([])
  const [subscriptionTargets, setSubscriptionTargets] = useState<SubscriptionTarget[]>([])
  const [statusFilter, setStatusFilter] = useState<AlertStatus | ''>('')
  const [loading, setLoading] = useState(canRead)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => { if (error) errorRef.current?.focus() }, [error])

  const load = useCallback(async (signal?: AbortSignal) => {
    if (!canRead) return
    const query = statusFilter ? `?status=${encodeURIComponent(statusFilter)}&limit=500` : '?limit=500'
    const [ruleValue, alertValue, subscriptionValue, targetValue] = await Promise.all([
      requestJSON('/api/v1/signals/rules', { signal }),
      requestJSON(`/api/v1/signals/alerts${query}`, { signal }),
      requestJSON('/api/v1/signals/subscriptions', { signal }),
      requestJSON('/api/v1/signals/subscription-targets', { signal }),
    ])
    setRules(readItems(ruleValue, isSignalRule, 'Signals rules'))
    setAlerts(readItems(alertValue, isSignalAlert, 'Signals alerts'))
    setSubscriptions(readItems(subscriptionValue, isSignalSubscription, 'Signals subscriptions'))
    setSubscriptionTargets(readItems(targetValue, isSubscriptionTarget, 'Signals subscription targets'))
  }, [canRead, statusFilter])

  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    setLoading(true)
    load(controller.signal)
      .catch((loadError: unknown) => {
        if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) setError(loadError instanceof ApiRequestError ? loadError.message : 'Signals could not be loaded.')
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [canRead, load])

  async function mutate(label: string, operation: () => Promise<void>, success: string) {
    setBusy(label)
    setError('')
    setStatus('')
    try {
      await operation()
      await load()
      if (success) setStatus(success)
    } catch (mutationError) {
      setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The Signals change could not be completed.')
    } finally {
      setBusy('')
    }
  }

  async function createRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const thresholdText = String(values.get('thresholdDays') ?? '').trim()
    const thresholdDays = thresholdText === '' ? [] : thresholdText.split(/[\s,]+/).filter(Boolean).map(Number)
    if (thresholdDays.some((day) => !Number.isInteger(day) || day < 0 || day > 3660)) {
      setError('Threshold days must be whole numbers from 0 through 3660.')
      return
    }
    await mutate('create-rule', async () => {
      const response = await requestJSON('/api/v1/signals/rules', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('name') ?? '').trim(), condition: String(values.get('condition') ?? ''), severity: String(values.get('severity') ?? ''),
          thresholdDays, fiscalPeriod: String(values.get('fiscalPeriod') ?? '').trim(), scenario: String(values.get('scenario') ?? '').trim(), enabled: true,
        }),
      })
      if (!isSignalRule(response)) throw new Error('invalid created Signals rule')
      form.reset()
    }, 'Alert rule created.')
  }

  async function evaluate() {
    await mutate('evaluate', async () => {
      const response = await requestJSON('/api/v1/signals/evaluate', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: '{}' })
      if (!isEvaluationResult(response)) throw new Error('invalid Signals evaluation response')
      setStatus(`Evaluation complete: ${response.created} created, ${response.refreshed} refreshed, and ${response.resolved} resolved.`)
    }, '')
  }

  async function toggleRule(rule: SignalRule) {
    await mutate(`toggle-${rule.id}`, async () => {
      const response = await requestJSON(`/api/v1/signals/rules/${encodeURIComponent(rule.id)}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ name: rule.name, condition: rule.condition, severity: rule.severity, enabled: !rule.enabled, thresholdDays: rule.thresholdDays, fiscalPeriod: rule.fiscalPeriod ?? '', scenario: rule.scenario ?? '', revision: rule.revision }),
      })
      if (!isSignalRule(response)) throw new Error('invalid updated Signals rule')
    }, rule.enabled ? 'Alert rule disabled.' : 'Alert rule enabled.')
  }

  async function acknowledge(alert: SignalAlert) {
    await mutate(`ack-${alert.id}`, async () => {
      const response = await requestJSON(`/api/v1/signals/alerts/${encodeURIComponent(alert.id)}/acknowledge`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ revision: alert.revision }),
      })
      if (!isSignalAlert(response)) throw new Error('invalid acknowledged Signals alert')
    }, 'Alert acknowledged.')
  }

  async function assign(event: FormEvent<HTMLFormElement>, alert: SignalAlert) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    await mutate(`assign-${alert.id}`, async () => {
      const response = await requestJSON(`/api/v1/signals/alerts/${encodeURIComponent(alert.id)}/assignment`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ kind: String(values.get('kind') ?? ''), targetId: String(values.get('targetId') ?? '').trim(), revision: alert.revision }),
      })
      if (!isSignalAlert(response)) throw new Error('invalid assigned Signals alert')
    }, 'Alert assignment updated.')
  }

  async function createSubscription(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const encodedTarget = String(values.get('target') ?? '')
    const separator = encodedTarget.indexOf('|')
    const targetKind = separator < 0 ? '' : encodedTarget.slice(0, separator)
    const targetId = separator < 0 ? '' : encodedTarget.slice(separator + 1)
    if (!subscriptionTargets.some((target) => target.targetKind === targetKind && target.targetId === targetId)) {
      setError('Choose an available Reach delivery target.')
      return
    }
    await mutate('create-subscription', async () => {
      const response = await requestJSON('/api/v1/signals/subscriptions', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ ruleId: String(values.get('ruleId') ?? ''), targetKind, targetId }),
      })
      if (!isSignalSubscription(response)) throw new Error('invalid created Signals subscription')
      form.reset()
    }, 'Subscription created.')
  }

  async function deleteSubscription(subscription: SignalSubscription) {
    await mutate(`delete-${subscription.id}`, async () => {
      await requestJSON(`/api/v1/signals/subscriptions/${encodeURIComponent(subscription.id)}`, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } })
    }, 'Subscription removed.')
  }

  if (!canRead) return <section className={`${panelClass} p-5 sm:p-7`} data-feature="alerts.rules" data-requirement="REQ-SIGNALS-001"><h2 className="text-2xl font-semibold">Signals</h2><p className="mt-3 text-steward-mist-muted">Your session does not include permission <code>signals.read</code>.</p></section>

  const exportQuery = statusFilter ? `?status=${encodeURIComponent(statusFilter)}&limit=500` : '?limit=500'
  return <section aria-labelledby="signals-heading" className="grid gap-6" data-feature="alerts.rules" data-requirement="REQ-SIGNALS-001">
    <div className={`${panelClass} p-5 sm:p-7`}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Signals</p><h2 className="mt-2 text-2xl font-semibold" id="signals-heading">Actionable alerts and rules</h2><p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">Evaluate operational and financial conditions, deduplicate repeated observations, and retain visible acknowledgment, assignment, resolution, and delivery history.</p></div>
        <div className="flex flex-wrap gap-2">{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Signals help</button>}{canWrite && <button className={buttonClass} disabled={busy !== ''} onClick={evaluate} type="button">{busy === 'evaluate' ? 'Evaluating…' : 'Evaluate now'}</button>}</div>
      </div>
      {error && <div aria-live="assertive" className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/10 p-4 text-sm text-[#ffbdc3]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {status && <p aria-live="polite" className="mt-4 rounded-xl border border-steward-success/35 bg-steward-success/10 p-4 text-sm text-[#98eab9]" role="status">{status}</p>}
      {!canWrite && <p className="mt-4 rounded-xl border border-steward-blue/35 bg-steward-blue/10 p-4 text-sm text-[#a9c7ff]">You have read-only Signals access. Creating rules, evaluating, assigning, and acknowledging require <code>signals.write</code>.</p>}
    </div>

    <div className={`${panelClass} p-5 sm:p-7`}>
      <div className="flex flex-wrap items-end justify-between gap-4"><div><h3 className="text-xl font-semibold">Alert queue</h3><p className="mt-1 text-sm text-steward-mist-muted">Status and severity are always shown as text, not color alone.</p></div><div className="flex flex-wrap items-end gap-3"><div><label className={labelClass} htmlFor="signalsStatus">Status</label><select className={inputClass} id="signalsStatus" onChange={(event) => setStatusFilter(event.target.value as AlertStatus | '')} value={statusFilter}><option value="">All statuses</option>{statuses.map((value) => <option key={value} value={value}>{words(value)}</option>)}</select></div><a className={secondaryButtonClass} href={`/api/v1/signals/report.csv${exportQuery}`}>Export CSV</a></div></div>
      {loading ? <p className="mt-5 text-sm text-steward-mist-muted">Loading alerts…</p> : alerts.length === 0 ? <p className="mt-5 rounded-xl border border-dashed border-white/15 p-6 text-center text-sm text-steward-mist-muted">No alerts match this status. Evaluate enabled rules after Ledger, Horizon, or Stack data changes.</p> : <ul className="mt-5 grid gap-4">
        {alerts.map((alert) => <li className={`${subpanelClass} min-w-0 p-4 sm:p-5`} key={alert.id}>
          <div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><div className="flex flex-wrap gap-2"><StatusBadge tone={toneFor(alert)}>{words(alert.status)}</StatusBadge><StatusBadge tone={alert.severity === 'critical' ? 'warning' : alert.severity === 'info' ? 'info' : 'neutral'}>{words(alert.severity)} severity</StatusBadge><StatusBadge>{words(alert.condition)}</StatusBadge></div><h4 className="mt-3 text-lg font-semibold">{alert.title}</h4><p className="mt-1 max-w-4xl break-words text-sm leading-6 text-steward-mist-muted">{alert.summary}</p></div>{canWrite && alert.status === 'active' && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => acknowledge(alert)} type="button">{busy === `ack-${alert.id}` ? 'Acknowledging…' : 'Acknowledge'}</button>}</div>
          <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2 xl:grid-cols-4"><div><dt className="text-xs font-semibold uppercase tracking-wide text-steward-slate">Target</dt><dd className="mt-1 break-all font-mono">{alert.targetType}:{alert.targetId}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-steward-slate">Due</dt><dd className="mt-1">{dateLabel(alert.dueAt)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-steward-slate">Last observed</dt><dd className="mt-1">{dateLabel(alert.lastObservedAt)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-steward-slate">Assignment</dt><dd className="mt-1 break-all">{alert.assignedId ? `${alert.assignedKind}: ${alert.assignedId}` : 'Unassigned'}</dd></div></dl>
          {canWrite && alert.status !== 'resolved' && <form className="mt-4 grid gap-3 border-t border-white/[0.08] pt-4 sm:grid-cols-[10rem_minmax(12rem,1fr)_auto] sm:items-end" onSubmit={(event) => assign(event, alert)}><div><label className={labelClass} htmlFor={`signalAssignKind-${alert.id}`}>Assign to</label><select className={inputClass} id={`signalAssignKind-${alert.id}`} name="kind"><option value="identity">Identity</option><option value="group">Group</option></select></div><div><label className={labelClass} htmlFor={`signalAssignTarget-${alert.id}`}>Configured target ID</label><input className={inputClass} id={`signalAssignTarget-${alert.id}`} maxLength={128} name="targetId" pattern="[A-Za-z0-9][A-Za-z0-9._:\-]{0,127}" required /></div><button className={secondaryButtonClass} disabled={busy !== ''} type="submit">{busy === `assign-${alert.id}` ? 'Assigning…' : 'Assign alert'}</button></form>}
        </li>)}
      </ul>}
    </div>

    <div className="grid gap-6 xl:grid-cols-2">
      <div className={`${panelClass} p-5 sm:p-7`}><h3 className="text-xl font-semibold">Alert rules</h3><p className="mt-1 text-sm leading-6 text-steward-mist-muted">Renewal and expiration rules default to 180, 90, 60, and 30 days. Other threshold-based conditions default to 30 days.</p>
        <ul className="mt-4 grid gap-3">{rules.map((rule) => <li className={`${subpanelClass} p-4`} key={rule.id}><div className="flex flex-wrap items-start justify-between gap-2"><div><h4 className="font-semibold">{rule.name}</h4><p className="mt-1 text-sm text-steward-mist-muted">{words(rule.condition)} · {words(rule.severity)} severity</p></div><div className="flex flex-wrap items-center gap-2"><StatusBadge tone={rule.enabled ? 'success' : 'neutral'}>{rule.enabled ? 'Enabled' : 'Disabled'}</StatusBadge>{canWrite && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => toggleRule(rule)} type="button">{busy === `toggle-${rule.id}` ? 'Updating…' : rule.enabled ? `Disable ${rule.name}` : `Enable ${rule.name}`}</button>}</div></div>{rule.thresholdDays.length > 0 && <p className="mt-2 text-xs text-steward-mist-muted">Thresholds: {rule.thresholdDays.join(', ')} days</p>}</li>)}</ul>
        {rules.length === 0 && !loading && <p className="mt-4 text-sm text-steward-mist-muted">No rules have been configured.</p>}
        {canWrite && <details className={`${subpanelClass} mt-5 p-4`}><summary className="cursor-pointer font-semibold">Create an alert rule</summary><form className="mt-4 grid gap-4 sm:grid-cols-2" onSubmit={createRule}><div className="sm:col-span-2"><label className={labelClass} htmlFor="signalRuleName">Rule name</label><input className={inputClass} id="signalRuleName" maxLength={160} name="name" required /></div><div><label className={labelClass} htmlFor="signalCondition">Condition</label><select className={inputClass} id="signalCondition" name="condition">{conditions.map((condition) => <option key={condition} value={condition}>{words(condition)}</option>)}</select></div><div><label className={labelClass} htmlFor="signalSeverity">Severity</label><select className={inputClass} id="signalSeverity" name="severity">{severities.map((severity) => <option key={severity} value={severity}>{words(severity)}</option>)}</select></div><div><label className={labelClass} htmlFor="signalThresholds">Threshold days <span className="font-normal">(optional)</span></label><input className={inputClass} id="signalThresholds" maxLength={64} name="thresholdDays" placeholder="180, 90, 60, 30" /></div><div><label className={labelClass} htmlFor="signalFiscalPeriod">Fiscal period <span className="font-normal">(optional)</span></label><input className={inputClass} id="signalFiscalPeriod" maxLength={32} name="fiscalPeriod" placeholder="FY2027" /></div><div><label className={labelClass} htmlFor="signalScenario">Scenario <span className="font-normal">(optional)</span></label><input className={inputClass} id="signalScenario" maxLength={64} name="scenario" placeholder="baseline" /></div><div className="sm:self-end"><button className={buttonClass} disabled={busy !== ''} type="submit">{busy === 'create-rule' ? 'Creating…' : 'Create rule'}</button></div></form></details>}
      </div>

      <div className={`${panelClass} p-5 sm:p-7`}><h3 className="text-xl font-semibold">Reach subscriptions</h3><p className="mt-1 text-sm leading-6 text-steward-mist-muted">Choose an enabled, fully configured Reach group or webhook. URLs, provider credentials, and delivery response bodies are never accepted here.</p>
        <ul className="mt-4 grid gap-3">{subscriptions.map((subscription) => { const target = subscriptionTargets.find((candidate) => candidate.targetKind === subscription.targetKind && candidate.targetId === subscription.targetId); return <li className={`${subpanelClass} flex flex-wrap items-center justify-between gap-3 p-4`} key={subscription.id}><div><p className="font-semibold">{target?.label ?? `${words(subscription.targetKind)} target unavailable`}</p><p className="mt-1 break-all font-mono text-xs text-steward-mist-muted">{subscription.targetId}{subscription.ruleId ? ` · rule ${subscription.ruleId}` : ' · all rules'}</p>{!target && <p className="mt-1 text-xs text-[#ffd08a]">New deliveries fail closed while this Reach target is disabled or invalid.</p>}</div>{canWrite && <button className={dangerButtonClass} disabled={busy !== ''} onClick={() => deleteSubscription(subscription)} type="button">{busy === `delete-${subscription.id}` ? 'Removing…' : 'Remove'}</button>}</li> })}</ul>
        {subscriptions.length === 0 && !loading && <p className="mt-4 text-sm text-steward-mist-muted">No delivery subscriptions have been configured.</p>}
        {canWrite && <form className={`${subpanelClass} mt-5 grid gap-4 p-4 sm:grid-cols-2`} onSubmit={createSubscription}><div className="sm:col-span-2"><label className={labelClass} htmlFor="signalSubscriptionTarget">Delivery target</label><select className={inputClass} id="signalSubscriptionTarget" name="target" required><option value="">Select an enabled Reach target</option>{subscriptionTargets.map((target) => <option key={`${target.targetKind}:${target.targetId}`} value={`${target.targetKind}|${target.targetId}`}>{words(target.targetKind)} · {target.label} · {target.targetId}</option>)}</select>{subscriptionTargets.length === 0 && <p className="mt-1 text-xs font-normal text-[#ffd08a]">Configure and enable a valid Reach group or webhook before subscribing.</p>}</div><div className="sm:col-span-2"><label className={labelClass} htmlFor="signalSubscriptionRule">Rule <span className="font-normal">(optional)</span></label><select className={inputClass} id="signalSubscriptionRule" name="ruleId"><option value="">All rules</option>{rules.map((rule) => <option key={rule.id} value={rule.id}>{rule.name}</option>)}</select></div><div className="sm:col-span-2"><button className={buttonClass} disabled={busy !== '' || subscriptionTargets.length === 0} type="submit">{busy === 'create-subscription' ? 'Creating…' : 'Create subscription'}</button></div></form>}
      </div>
    </div>
  </section>
}
