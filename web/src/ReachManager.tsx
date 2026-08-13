import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, StatusBadge, subpanelClass } from './ui'

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

type ProviderKind = 'smtp' | 'ses' | 'gmail_oauth' | 'outlook_oauth' | 'teams' | 'webhook'
type Endpoint = { id: string; label: string; kind: ProviderKind }
type Provider = { id: string; name: string; kind: ProviderKind; endpointId: string; sender?: string; secretConfigured: boolean; enabled: boolean; revision: number }
type Template = { id: string; name: string; subject: string; body: string; revision: number }
type Recipient = { kind: 'email' | 'channel'; address: string }
type Group = { id: string; name: string; providerId: string; templateId: string; recipients: Recipient[]; revision: number }
type Message = { id: string; groupId?: string; providerId: string; sourceKind: string; sourceId?: string; subject: string; body: string; recipients: Recipient[]; status: 'delivered' | 'retrying' | 'failed' | 'queued'; attempts: number; nextAttemptAt?: string; lastErrorCode?: string; createdAt: string; updatedAt: string }
type Attempt = { id: string; messageId: string; attempt: number; outcome: 'succeeded' | 'retrying' | 'failed'; errorCode?: string; retryable: boolean; nextAttemptAt?: string; occurredAt: string }
type ProviderTest = { id: string; providerId: string; outcome: 'succeeded' | 'failed'; errorCode?: string; testedBy: string; testedAt: string }
type ProcessResult = { examined: number; delivered: number; retrying: number; failed: number }

const providerKinds: readonly ProviderKind[] = ['smtp', 'ses', 'gmail_oauth', 'outlook_oauth', 'teams', 'webhook']
const messageStatuses = ['delivered', 'retrying', 'failed', 'queued'] as const
const idPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const secretReferencePattern = /^(env|external):[A-Za-z0-9][A-Za-z0-9._-]{0,95}$/

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function isID(value: unknown): value is string { return typeof value === 'string' && idPattern.test(value) }
function isDateTime(value: unknown) { return typeof value === 'string' && value.length <= 64 && !Number.isNaN(Date.parse(value)) }
function isEndpoint(value: unknown): value is Endpoint {
  return isRecord(value) && isID(value.id) && typeof value.label === 'string' && value.label.length > 0 && value.label.length <= 160 && providerKinds.includes(value.kind as ProviderKind)
    && !('url' in value) && !('address' in value) && !('serverName' in value)
}
function isProvider(value: unknown): value is Provider {
  return isRecord(value) && isID(value.id) && typeof value.name === 'string' && value.name.length > 0 && value.name.length <= 160
    && providerKinds.includes(value.kind as ProviderKind) && isID(value.endpointId) && typeof value.secretConfigured === 'boolean'
    && typeof value.enabled === 'boolean' && Number.isInteger(value.revision) && Number(value.revision) > 0 && !('secretRef' in value)
}
function isTemplate(value: unknown): value is Template {
  return isRecord(value) && isID(value.id) && typeof value.name === 'string' && value.name.length > 0 && value.name.length <= 160
    && typeof value.subject === 'string' && value.subject.length > 0 && value.subject.length <= 200 && typeof value.body === 'string' && value.body.length > 0 && value.body.length <= 4000
    && Number.isInteger(value.revision) && Number(value.revision) > 0
}
function isRecipient(value: unknown): value is Recipient {
  return isRecord(value) && (value.kind === 'email' || value.kind === 'channel') && typeof value.address === 'string' && value.address.length > 0 && value.address.length <= 320
}
function isGroup(value: unknown): value is Group {
  return isRecord(value) && isID(value.id) && typeof value.name === 'string' && value.name.length > 0 && value.name.length <= 160
    && isID(value.providerId) && isID(value.templateId) && Array.isArray(value.recipients) && value.recipients.length > 0 && value.recipients.length <= 100
    && value.recipients.every(isRecipient) && Number.isInteger(value.revision) && Number(value.revision) > 0
}
function isMessage(value: unknown): value is Message {
  return isRecord(value) && isID(value.id) && isID(value.providerId) && typeof value.sourceKind === 'string'
    && typeof value.subject === 'string' && value.subject.length > 0 && value.subject.length <= 200 && typeof value.body === 'string' && value.body.length > 0 && value.body.length <= 4000
    && Array.isArray(value.recipients) && value.recipients.length <= 100 && value.recipients.every(isRecipient)
    && messageStatuses.includes(value.status as Message['status']) && Number.isInteger(value.attempts) && Number(value.attempts) >= 0 && Number(value.attempts) <= 8
    && isDateTime(value.createdAt) && isDateTime(value.updatedAt) && (value.nextAttemptAt === undefined || isDateTime(value.nextAttemptAt))
}
function isAttempt(value: unknown): value is Attempt {
  return isRecord(value) && isID(value.id) && isID(value.messageId) && Number.isInteger(value.attempt) && Number(value.attempt) > 0 && Number(value.attempt) <= 8
    && (value.outcome === 'succeeded' || value.outcome === 'retrying' || value.outcome === 'failed') && typeof value.retryable === 'boolean'
    && isDateTime(value.occurredAt) && (value.nextAttemptAt === undefined || isDateTime(value.nextAttemptAt))
}
function isProviderTest(value: unknown): value is ProviderTest {
  return isRecord(value) && isID(value.id) && isID(value.providerId) && (value.outcome === 'succeeded' || value.outcome === 'failed')
    && typeof value.testedBy === 'string' && isDateTime(value.testedAt)
}
function readItems<T>(value: unknown, validator: (item: unknown) => item is T, label: string): T[] {
  if (!isRecord(value) || !Array.isArray(value.items) || value.items.length > 500 || !value.items.every(validator)) throw new Error(`invalid ${label} response`)
  return value.items
}
function isProcessResult(value: unknown): value is ProcessResult {
  return isRecord(value) && ['examined', 'delivered', 'retrying', 'failed'].every((key) => Number.isInteger(value[key]) && Number(value[key]) >= 0)
}
function words(value: string) { return value.split('_').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ') }
function dateLabel(value?: string) { return value && !Number.isNaN(Date.parse(value)) ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : 'Not scheduled' }
function statusTone(status: string): 'success' | 'warning' | 'info' | 'neutral' { return status === 'delivered' || status === 'succeeded' ? 'success' : status === 'retrying' ? 'warning' : status === 'failed' ? 'warning' : 'neutral' }

export default function ReachManager({ csrfToken, onOpenHelp, permissions }: { csrfToken: string; onOpenHelp?: () => void; permissions: readonly string[] }) {
  const canRead = permissions.includes('messaging.read')
  const canWrite = permissions.includes('messaging.write')
  const [endpoints, setEndpoints] = useState<Endpoint[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [templates, setTemplates] = useState<Template[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [messages, setMessages] = useState<Message[]>([])
  const [providerTests, setProviderTests] = useState<ProviderTest[]>([])
  const [attempts, setAttempts] = useState<Attempt[]>([])
  const [selectedMessage, setSelectedMessage] = useState('')
  const [providerKind, setProviderKind] = useState<ProviderKind>('webhook')
  const [loading, setLoading] = useState(canRead)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => { if (error) errorRef.current?.focus() }, [error])
  const load = useCallback(async (signal?: AbortSignal) => {
    if (!canRead) return
    const [endpointValue, providerValue, templateValue, groupValue, messageValue] = await Promise.all([
      requestJSON('/api/v1/reach/endpoints', { signal }), requestJSON('/api/v1/reach/providers', { signal }),
      requestJSON('/api/v1/reach/templates', { signal }), requestJSON('/api/v1/reach/groups', { signal }), requestJSON('/api/v1/reach/messages?limit=100', { signal }),
    ])
    const nextProviders = readItems(providerValue, isProvider, 'Reach providers')
    const testValues = await Promise.all(nextProviders.map((provider) => requestJSON(`/api/v1/reach/providers/${encodeURIComponent(provider.id)}/tests`, { signal })))
    setEndpoints(readItems(endpointValue, isEndpoint, 'Reach endpoints'))
    setProviders(nextProviders)
    setTemplates(readItems(templateValue, isTemplate, 'Reach templates'))
    setGroups(readItems(groupValue, isGroup, 'Reach groups'))
    setMessages(readItems(messageValue, isMessage, 'Reach messages'))
    setProviderTests(testValues.flatMap((value) => readItems(value, isProviderTest, 'Reach provider tests')))
  }, [canRead])

  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    setLoading(true)
    load(controller.signal).catch((loadError: unknown) => {
      if (!(loadError instanceof DOMException && loadError.name === 'AbortError')) setError(loadError instanceof ApiRequestError ? loadError.message : 'Reach could not be loaded.')
    }).finally(() => setLoading(false))
    return () => controller.abort()
  }, [canRead, load])

  async function mutate(label: string, operation: () => Promise<void>, success: string) {
    setBusy(label); setError(''); setStatus('')
    try { await operation(); await load(); if (success) setStatus(success) }
    catch (mutationError) { setError(mutationError instanceof ApiRequestError ? mutationError.message : 'The Reach change could not be completed.') }
    finally { setBusy('') }
  }
  const writeHeaders = { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }

  async function createProvider(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const values = new FormData(form)
    const secretRef = String(values.get('secretRef') ?? '').trim()
    if (!secretReferencePattern.test(secretRef)) { setError('Use an external secret reference beginning with env: or external:.'); return }
    await mutate('provider', async () => {
      const value = await requestJSON('/api/v1/reach/providers', { method: 'POST', headers: writeHeaders, body: JSON.stringify({
        name: String(values.get('name') ?? '').trim(), kind: providerKind, endpointId: String(values.get('endpointId') ?? ''), sender: String(values.get('sender') ?? '').trim(), secretRef,
      }) })
      if (!isProvider(value)) throw new Error('invalid created Reach provider')
      form.reset(); setProviderKind('webhook')
    }, 'Provider configured without storing credentials.')
  }
  async function createTemplate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const values = new FormData(form)
    await mutate('template', async () => {
      const value = await requestJSON('/api/v1/reach/templates', { method: 'POST', headers: writeHeaders, body: JSON.stringify({ name: String(values.get('name') ?? '').trim(), subject: String(values.get('subject') ?? '').trim(), body: String(values.get('body') ?? '').trim() }) })
      if (!isTemplate(value)) throw new Error('invalid created Reach template'); form.reset()
    }, 'Plain-text template created.')
  }
  async function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const values = new FormData(form)
    await mutate('group', async () => {
      const value = await requestJSON('/api/v1/reach/groups', { method: 'POST', headers: writeHeaders, body: JSON.stringify({
        name: String(values.get('name') ?? '').trim(), providerId: String(values.get('providerId') ?? ''), templateId: String(values.get('templateId') ?? ''),
        recipients: [{ kind: String(values.get('recipientKind') ?? ''), address: String(values.get('address') ?? '').trim() }],
      }) })
      if (!isGroup(value)) throw new Error('invalid created Reach group'); form.reset()
    }, 'Subscriber group created.')
  }
  async function rotateSecret(event: FormEvent<HTMLFormElement>, provider: Provider) {
    event.preventDefault(); const form = event.currentTarget; const reference = String(new FormData(form).get('secretRef') ?? '').trim()
    if (!secretReferencePattern.test(reference)) { setError('Use an external secret reference beginning with env: or external:.'); return }
    await mutate(`rotate-${provider.id}`, async () => {
      const value = await requestJSON(`/api/v1/reach/providers/${encodeURIComponent(provider.id)}/rotate-secret`, { method: 'POST', headers: writeHeaders, body: JSON.stringify({ secretRef: reference, revision: provider.revision, confirm: true }) })
      if (!isProvider(value)) throw new Error('invalid rotated Reach provider'); form.reset()
    }, 'External secret reference rotated.')
  }
  async function testProvider(provider: Provider) {
    await mutate(`test-${provider.id}`, async () => {
      const value = await requestJSON(`/api/v1/reach/providers/${encodeURIComponent(provider.id)}/test`, { method: 'POST', headers: writeHeaders, body: JSON.stringify({ confirm: true }) })
      if (!isProviderTest(value)) throw new Error('invalid Reach provider test')
    }, 'Provider test recorded. No message content was sent.')
  }
  async function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const values = new FormData(form)
    if (values.get('confirm') !== 'yes') { setError('Confirm the external delivery before sending.'); return }
    await mutate('send', async () => {
      const value = await requestJSON('/api/v1/reach/messages/send', { method: 'POST', headers: { ...writeHeaders, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ groupId: String(values.get('groupId') ?? ''), confirm: true, variables: {
        title: String(values.get('title') ?? '').trim(), summary: String(values.get('summary') ?? '').trim(), severity: String(values.get('severity') ?? '').trim(), record_id: String(values.get('recordId') ?? '').trim(),
      } }) })
      if (!isMessage(value)) throw new Error('invalid Reach send result'); form.reset()
    }, 'Confirmed message attempted. Review delivery history for the result.')
  }
  async function processSignals() {
    await mutate('signals', async () => {
      const value = await requestJSON('/api/v1/reach/signals/process', { method: 'POST', headers: writeHeaders, body: JSON.stringify({ confirm: true, limit: 100 }) })
      if (!isProcessResult(value)) throw new Error('invalid Reach Signals result')
      setStatus(`Signals processed: ${value.delivered} delivered, ${value.retrying} retrying, and ${value.failed} failed.`)
    }, '')
  }
  async function retryMessage(message: Message) {
    await mutate(`retry-${message.id}`, async () => {
      const value = await requestJSON(`/api/v1/reach/messages/${encodeURIComponent(message.id)}/retry`, { method: 'POST', headers: writeHeaders, body: JSON.stringify({ confirm: true }) })
      if (!isMessage(value)) throw new Error('invalid Reach retry result')
    }, 'Confirmed retry attempted.')
  }
  async function viewAttempts(message: Message) {
    setBusy(`attempts-${message.id}`); setError('')
    try {
      const value = await requestJSON(`/api/v1/reach/messages/${encodeURIComponent(message.id)}/attempts`)
      setAttempts(readItems(value, isAttempt, 'Reach attempts')); setSelectedMessage(message.id)
    } catch { setError('Delivery attempts could not be loaded.') }
    finally { setBusy('') }
  }

  if (!canRead) return <section className={`${panelClass} p-6`} data-feature="messaging.delivery" data-requirement="REQ-REACH-001"><h3 className="text-xl font-semibold text-white">Reach is permission limited</h3><p className="mt-2 text-sm text-steward-mist-muted">Ask an administrator for messaging.read access.</p></section>
  return <section aria-labelledby="reach-heading" className="grid min-w-0 max-w-full grid-cols-[minmax(0,1fr)] gap-6" data-feature="messaging.delivery" data-requirement="REQ-REACH-001">
    <header className={`${panelClass} p-5 sm:p-6`}><div className="flex flex-wrap items-start justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Reach</p><h3 className="mt-2 text-2xl font-semibold text-white" id="reach-heading">Confirmed, traceable delivery</h3><p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Configure deployment-approved email, Teams, and webhook adapters; route subscriber groups; and inspect sanitized retry history. Credentials and endpoint URLs never appear here.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Open Reach help</button>}</div>
      {!canWrite && <p className="mt-4 rounded-xl border border-white/10 bg-white/[0.03] p-3 text-sm text-steward-mist-muted">You have read-only Reach access. messaging.write is required to configure or deliver messages.</p>}
      {loading && <p className="mt-4 text-sm text-steward-mist-muted" role="status">Loading Reach…</p>}
      {error && <div className="mt-4 rounded-xl border border-steward-danger/45 bg-steward-danger/10 p-3 text-sm text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {status && <p className="mt-4 rounded-xl border border-steward-success/35 bg-steward-success/10 p-3 text-sm text-[#98eab9]" role="status">{status}</p>}
    </header>

    {canWrite && <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-3">
      <form className={`${panelClass} p-5`} onSubmit={createProvider}><h4 className="text-lg font-semibold text-white">Configure provider</h4><p className="mt-1 text-sm text-steward-mist-muted">Choose a fixed deployment route and reference an external secret.</p>
        <label className={`${labelClass} mt-4`}>Provider name<input className={inputClass} maxLength={160} name="name" required /></label>
        <label className={`${labelClass} mt-4`}>Provider type<select className={inputClass} name="kind" onChange={(event) => setProviderKind(event.target.value as ProviderKind)} value={providerKind}>{providerKinds.map((kind) => <option key={kind} value={kind}>{words(kind)}</option>)}</select></label>
        <label className={`${labelClass} mt-4`}>Approved endpoint<select className={inputClass} name="endpointId" required><option value="">Select an endpoint</option>{endpoints.filter((endpoint) => endpoint.kind === providerKind).map((endpoint) => <option key={endpoint.id} value={endpoint.id}>{endpoint.label}</option>)}</select></label>
        {!['teams', 'webhook'].includes(providerKind) && <label className={`${labelClass} mt-4`}>Sender email<input className={inputClass} maxLength={320} name="sender" required type="email" /></label>}
        <label className={`${labelClass} mt-4`}>External secret reference<input autoComplete="off" className={inputClass} maxLength={105} name="secretRef" pattern="(env|external):[A-Za-z0-9][A-Za-z0-9._\-]{0,95}" placeholder="env:operations-hook" required /></label>
        <button className={`${buttonClass} mt-4 w-full`} disabled={busy !== '' || endpoints.every((endpoint) => endpoint.kind !== providerKind)} type="submit">Configure provider</button>
      </form>
      <form className={`${panelClass} p-5`} onSubmit={createTemplate}><h4 className="text-lg font-semibold text-white">Create template</h4><p className="mt-1 text-sm text-steward-mist-muted">Plain text only. Tokens: title, summary, severity, record_id, organization.</p>
        <label className={`${labelClass} mt-4`}>Template name<input className={inputClass} maxLength={160} name="name" required /></label>
        <label className={`${labelClass} mt-4`}>Subject<input className={inputClass} maxLength={200} name="subject" placeholder="{{severity}}: {{title}}" required /></label>
        <label className={`${labelClass} mt-4`}>Body<textarea className={inputClass} maxLength={4000} name="body" placeholder={'{{summary}}\nRecord {{record_id}}'} required rows={4} /></label>
        <button className={`${buttonClass} mt-4 w-full`} disabled={busy !== ''} type="submit">Create template</button>
      </form>
      <form className={`${panelClass} p-5`} onSubmit={createGroup}><h4 className="text-lg font-semibold text-white">Create subscriber group</h4><p className="mt-1 text-sm text-steward-mist-muted">Add one validated recipient now; the API supports up to 100.</p>
        <label className={`${labelClass} mt-4`}>Group name<input className={inputClass} maxLength={160} name="name" required /></label>
        <label className={`${labelClass} mt-4`}>Provider<select className={inputClass} name="providerId" required><option value="">Select a provider</option>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></label>
        <label className={`${labelClass} mt-4`}>Template<select className={inputClass} name="templateId" required><option value="">Select a template</option>{templates.map((template) => <option key={template.id} value={template.id}>{template.name}</option>)}</select></label>
        <label className={`${labelClass} mt-4`}>Recipient type<select className={inputClass} name="recipientKind"><option value="email">Email</option><option value="channel">Channel</option></select></label>
        <label className={`${labelClass} mt-4`}>Recipient address<input className={inputClass} maxLength={320} name="address" required /></label>
        <button className={`${buttonClass} mt-4 w-full`} disabled={busy !== '' || providers.length === 0 || templates.length === 0} type="submit">Create group</button>
      </form>
    </div>}

    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-2">
      <section className={`${panelClass} p-5`} aria-labelledby="reach-providers-heading"><h4 className="text-lg font-semibold text-white" id="reach-providers-heading">Providers and test history</h4><div className="mt-4 space-y-3">{providers.length === 0 ? <p className="text-sm text-steward-mist-muted">No providers are configured.</p> : providers.map((provider) => <article className={`${subpanelClass} p-4`} key={provider.id}><div className="flex flex-wrap items-start justify-between gap-3"><div><h5 className="font-semibold text-white">{provider.name}</h5><p className="mt-1 text-sm text-steward-mist-muted">{words(provider.kind)} · {provider.endpointId} · secret {provider.secretConfigured ? 'configured' : 'missing'}</p></div><StatusBadge tone={provider.enabled ? 'success' : 'neutral'}>{provider.enabled ? 'Enabled' : 'Disabled'}</StatusBadge></div>
          {canWrite && <><button className={`${secondaryButtonClass} mt-3`} disabled={busy !== ''} onClick={() => testProvider(provider)} type="button">Confirm and test {provider.name}</button><form className="mt-3 flex flex-col gap-2 sm:flex-row" onSubmit={(event) => rotateSecret(event, provider)}><label className="min-w-0 flex-1 text-sm font-semibold text-steward-mist">New external secret reference<input autoComplete="off" className={inputClass} name="secretRef" pattern="(env|external):[A-Za-z0-9][A-Za-z0-9._\-]{0,95}" required /></label><button className={`${secondaryButtonClass} self-end`} disabled={busy !== ''} type="submit">Confirm rotation</button></form></>}
          <ul className="mt-3 space-y-1 text-sm text-steward-mist-muted">{providerTests.filter((test) => test.providerId === provider.id).map((test) => <li key={test.id}><StatusBadge tone={statusTone(test.outcome)}>{words(test.outcome)}</StatusBadge> <span>{dateLabel(test.testedAt)}{test.errorCode ? ` · ${words(test.errorCode)}` : ''}</span></li>)}</ul>
        </article>)}</div></section>
      <section className={`${panelClass} p-5`} aria-labelledby="reach-groups-heading"><h4 className="text-lg font-semibold text-white" id="reach-groups-heading">Subscriber groups</h4><div className="mt-4 space-y-3">{groups.length === 0 ? <p className="text-sm text-steward-mist-muted">No groups are configured.</p> : groups.map((group) => <article className={`${subpanelClass} p-4`} key={group.id}><h5 className="font-semibold text-white">{group.name}</h5><p className="mt-1 break-words text-sm text-steward-mist-muted">Provider {group.providerId} · template {group.templateId}</p><ul className="mt-2 text-sm text-steward-mist-muted">{group.recipients.map((recipient) => <li className="break-all" key={`${recipient.kind}:${recipient.address}`}>{words(recipient.kind)}: {recipient.address}</li>)}</ul></article>)}</div></section>
    </div>

    {canWrite && <form className={`${panelClass} p-5 sm:p-6`} onSubmit={sendMessage}><div className="flex flex-wrap items-start justify-between gap-3"><div><h4 className="text-lg font-semibold text-white">Send a message</h4><p className="mt-1 text-sm text-steward-mist-muted">A confirmed action immediately invokes the selected group’s provider. Templates remain plain text.</p></div><button className={secondaryButtonClass} disabled={busy !== ''} onClick={processSignals} type="button">Confirm and process pending Signals</button></div>
      <div className="mt-4 grid gap-4 md:grid-cols-2"><label className={labelClass}>Subscriber group<select className={inputClass} name="groupId" required><option value="">Select a group</option>{groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}</select></label><label className={labelClass}>Title<input className={inputClass} maxLength={500} name="title" required /></label><label className={labelClass}>Summary<input className={inputClass} maxLength={500} name="summary" required /></label><label className={labelClass}>Severity<input className={inputClass} maxLength={500} name="severity" required /></label><label className={labelClass}>Record ID<input className={inputClass} maxLength={500} name="recordId" required /></label></div>
      <label className="mt-4 flex items-start gap-3 text-sm leading-6 text-steward-mist"><input className="mt-1 size-4 accent-steward-teal" name="confirm" required type="checkbox" value="yes" /><span>I confirm this message may be delivered to recipients outside StewardMesh.</span></label><button className={`${buttonClass} mt-4`} disabled={busy !== '' || groups.length === 0} type="submit">Confirm and send</button>
    </form>}

    <section className={`${panelClass} min-w-0 p-5 sm:p-6`} aria-labelledby="reach-history-heading"><h4 className="text-lg font-semibold text-white" id="reach-history-heading">Delivery history</h4><div className="mt-4 space-y-3">{messages.length === 0 ? <p className="text-sm text-steward-mist-muted">No delivery attempts have been recorded.</p> : messages.map((message) => <article className={`${subpanelClass} min-w-0 p-4`} key={message.id}><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h5 className="break-words font-semibold text-white">{message.subject}</h5><p className="mt-1 break-words text-sm text-steward-mist-muted">{words(message.sourceKind)} · {message.attempts} attempt{message.attempts === 1 ? '' : 's'} · {dateLabel(message.updatedAt)}</p>{message.lastErrorCode && <p className="mt-1 text-sm text-[#ffd08a]">Sanitized error: {words(message.lastErrorCode)}</p>}{message.nextAttemptAt && <p className="mt-1 text-sm text-steward-mist-muted">Next retry: {dateLabel(message.nextAttemptAt)}</p>}</div><StatusBadge tone={statusTone(message.status)}>{words(message.status)}</StatusBadge></div><div className="mt-3 flex flex-wrap gap-2"><button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => viewAttempts(message)} type="button">View attempts</button>{canWrite && message.status !== 'delivered' && message.attempts < 8 && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => retryMessage(message)} type="button">Confirm retry</button>}</div>
          {selectedMessage === message.id && <ol className="mt-3 space-y-2 border-t border-white/10 pt-3">{attempts.map((attempt) => <li className="text-sm text-steward-mist-muted" key={attempt.id}>Attempt {attempt.attempt}: <StatusBadge tone={statusTone(attempt.outcome)}>{words(attempt.outcome)}</StatusBadge> {dateLabel(attempt.occurredAt)}{attempt.errorCode ? ` · ${words(attempt.errorCode)}` : ''}</li>)}</ol>}
        </article>)}</div></section>
  </section>
}
