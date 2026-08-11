import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import type { Asset } from './AtlasInventory'

// Requirement: REQ-THREADS-001. Feature: goals.tags.

type Tag = {
  id: string
  organizationId: string
  name: string
  parentId?: string
  inheritByDefault: boolean
  revision: number
}

type Goal = {
  id: string
  organizationId: string
  name: string
  description?: string
  parentId?: string
  revision: number
}

type TagRule = {
  tagId: string
  mode: 'include' | 'suppress'
  revision: number
}

type EffectiveTag = {
  tag: Tag
  state: 'explicit' | 'inherited' | 'suppressed'
  sourceTagId?: string
  rule?: TagRule
}

type GoalLink = {
  goalId: string
  targetType: 'asset' | 'purchase'
  targetId: string
}

type ThreadsManagerProps = {
  assets: readonly Asset[]
  csrfToken: string
  permissions: readonly string[]
  onOpenHelp?: () => void
}

const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2 text-steward-mist shadow-inner shadow-black/20'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2 font-semibold text-steward-ink-950 transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const secondaryButtonClass = 'min-h-11 rounded-lg border border-steward-teal px-3 py-2 text-sm font-semibold text-steward-teal transition hover:bg-steward-teal/10 disabled:cursor-wait disabled:opacity-60'

function items(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const candidate = (value as Record<string, unknown>).items
  return Array.isArray(candidate) ? candidate : []
}

function isTag(value: unknown): value is Tag {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string' && typeof item.name === 'string'
    && typeof item.inheritByDefault === 'boolean' && typeof item.revision === 'number'
    && (item.parentId === undefined || typeof item.parentId === 'string')
}

function isGoal(value: unknown): value is Goal {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string' && typeof item.name === 'string'
    && typeof item.revision === 'number' && (item.parentId === undefined || typeof item.parentId === 'string')
}

function isEffectiveTag(value: unknown): value is EffectiveTag {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  if (!isTag(item.tag) || !['explicit', 'inherited', 'suppressed'].includes(String(item.state))) return false
  if (item.rule === undefined) return item.sourceTagId === undefined || typeof item.sourceTagId === 'string'
  if (typeof item.rule !== 'object' || item.rule === null) return false
  const rule = item.rule as Record<string, unknown>
  return typeof rule.tagId === 'string' && ['include', 'suppress'].includes(String(rule.mode)) && typeof rule.revision === 'number'
}

function isGoalLink(value: unknown): value is GoalLink {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.goalId === 'string' && ['asset', 'purchase'].includes(String(item.targetType)) && typeof item.targetId === 'string'
}

export default function ThreadsManager({ assets, csrfToken, permissions, onOpenHelp }: ThreadsManagerProps) {
  const [tags, setTags] = useState<Tag[]>([])
  const [goals, setGoals] = useState<Goal[]>([])
  const [assetID, setAssetID] = useState('')
  const [effectiveTags, setEffectiveTags] = useState<EffectiveTag[]>([])
  const [goalLinks, setGoalLinks] = useState<GoalLink[]>([])
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const canRead = permissions.includes('goals.read')
  const canWrite = permissions.includes('goals.write')

  const tagByID = useMemo(() => new Map(tags.map((tag) => [tag.id, tag])), [tags])
  const goalByID = useMemo(() => new Map(goals.map((goal) => [goal.id, goal])), [goals])
  const effectiveByID = useMemo(() => new Map(effectiveTags.map((item) => [item.tag.id, item])), [effectiveTags])

  useEffect(() => {
    if (!canRead) return
    let active = true
    Promise.all([requestJSON('/api/v1/tags'), requestJSON('/api/v1/goals')])
      .then(([tagResponse, goalResponse]) => {
        const nextTags = items(tagResponse)
        const nextGoals = items(goalResponse)
        if (!nextTags.every(isTag) || !nextGoals.every(isGoal)) throw new Error('invalid Threads response')
        if (active) {
          setTags(nextTags)
          setGoals(nextGoals)
        }
      })
      .catch(() => {
        if (active) showError('Tags and goals could not be loaded.')
      })
    return () => { active = false }
  }, [canRead])

  useEffect(() => {
    if (!assetID && assets.length > 0) setAssetID(assets[0].id)
    if (assetID && !assets.some((asset) => asset.id === assetID)) setAssetID(assets[0]?.id ?? '')
  }, [assetID, assets])

  useEffect(() => {
    if (!canRead || !assetID) {
      setEffectiveTags([])
      setGoalLinks([])
      return
    }
    let active = true
    Promise.all([
      requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/tags`),
      requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/goals`),
    ])
      .then(([tagResponse, goalResponse]) => {
        const nextTags = items(tagResponse)
        const nextLinks = items(goalResponse)
        if (!nextTags.every(isEffectiveTag) || !nextLinks.every(isGoalLink)) throw new Error('invalid relationship response')
        if (active) {
          setEffectiveTags(nextTags)
          setGoalLinks(nextLinks)
        }
      })
      .catch(() => {
        if (active) showError('Asset tag and goal relationships could not be loaded.')
      })
    return () => { active = false }
  }, [assetID, canRead])

  function showError(value: string) {
    setError(value)
    queueMicrotask(() => errorRef.current?.focus())
  }

  async function reloadTags() {
    const response = await requestJSON('/api/v1/tags')
    const next = items(response)
    if (!next.every(isTag)) throw new Error('invalid tags response')
    setTags(next)
  }

  async function reloadGoals() {
    const response = await requestJSON('/api/v1/goals')
    const next = items(response)
    if (!next.every(isGoal)) throw new Error('invalid goals response')
    setGoals(next)
  }

  async function reloadRelationships() {
    if (!assetID) return
    const [tagResponse, goalResponse] = await Promise.all([
      requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/tags`),
      requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/goals`),
    ])
    const nextTags = items(tagResponse)
    const nextLinks = items(goalResponse)
    if (!nextTags.every(isEffectiveTag) || !nextLinks.every(isGoalLink)) throw new Error('invalid relationship response')
    setEffectiveTags(nextTags)
    setGoalLinks(nextLinks)
  }

  async function createTag(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('tag-create')
    setError('')
    try {
      await requestJSON('/api/v1/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('tagName') ?? ''),
          parentId: String(values.get('tagParent') ?? ''),
          inheritByDefault: values.get('inheritByDefault') === 'on',
        }),
      })
      await reloadTags()
      form.reset()
      setMessage('Tag created in the organization hierarchy.')
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The tag could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function createGoal(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('goal-create')
    setError('')
    try {
      await requestJSON('/api/v1/goals', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          name: String(values.get('goalName') ?? ''), description: String(values.get('goalDescription') ?? ''),
          parentId: String(values.get('goalParent') ?? ''),
        }),
      })
      await reloadGoals()
      form.reset()
      setMessage('Goal created in the strategy hierarchy.')
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be created.')
    } finally {
      setBusy('')
    }
  }

  async function setTag(tag: Tag, mode: 'include' | 'suppress') {
    if (!assetID) return
    const current = effectiveByID.get(tag.id)?.rule
    setBusy(`tag-${tag.id}`)
    setError('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/tags/${encodeURIComponent(tag.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ mode, revision: current?.revision ?? 0 }),
      })
      await reloadRelationships()
      setMessage(mode === 'include' ? `${tag.name} explicitly applied.` : `${tag.name} explicitly suppressed.`)
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The tag rule could not be saved.')
    } finally {
      setBusy('')
    }
  }

  async function removeTagRule(tag: Tag, revision: number) {
    if (!assetID) return
    setBusy(`tag-${tag.id}`)
    setError('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/tags/${encodeURIComponent(tag.id)}?revision=${revision}`, {
        method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken },
      })
      await reloadRelationships()
      setMessage(`${tag.name} now follows inherited behavior.`)
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The tag rule could not be removed.')
    } finally {
      setBusy('')
    }
  }

  async function linkGoal(goalID: string) {
    if (!assetID || !goalID) return
    setBusy('goal-link')
    setError('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/goals/${encodeURIComponent(goalID)}`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken },
      })
      await reloadRelationships()
      setMessage('Goal linked to the selected asset.')
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be linked.')
    } finally {
      setBusy('')
    }
  }

  async function unlinkGoal(goalID: string) {
    if (!assetID) return
    setBusy(`goal-${goalID}`)
    setError('')
    try {
      await requestJSON(`/api/v1/threads/asset/${encodeURIComponent(assetID)}/goals/${encodeURIComponent(goalID)}`, {
        method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken },
      })
      await reloadRelationships()
      setMessage('Goal unlinked from the selected asset.')
    } catch (requestError) {
      showError(requestError instanceof ApiRequestError ? requestError.message : 'The goal could not be unlinked.')
    } finally {
      setBusy('')
    }
  }

  if (!canRead) return null

  return (
    <section aria-labelledby="threads-heading" className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6" data-feature="goals.tags" data-requirement="REQ-THREADS-001">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="text-sm font-semibold text-steward-teal">Shared context and strategy</p>
          <h2 className="mt-1 text-2xl font-semibold" id="threads-heading">Threads — Tags and goals</h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Connect inventory to a consistent tag hierarchy and strategic goals. Every applied, inherited, and suppressed value stays visible so provenance is never silent.</p>
        </div>
        {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Threads help</button>}
      </div>

      {error && <div className="mt-4 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-4 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}

      {canWrite && <div className="mt-6 grid gap-5 lg:grid-cols-2">
        <form aria-label="Create tag" className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/55 p-5" onSubmit={createTag}>
          <h3 className="text-lg font-semibold">Create a tag</h3>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Tag name<input className={inputClass} maxLength={100} name="tagName" required /></label>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Parent tag (optional)<select className={inputClass} name="tagParent"><option value="">No parent</option>{tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}</select></label>
          <label className="mt-4 flex min-h-11 items-center gap-3 text-sm"><input className="size-5" name="inheritByDefault" type="checkbox" />Inherit this tag when a child tag is applied</label>
          <button className={`${buttonClass} mt-4`} disabled={busy === 'tag-create'} type="submit">{busy === 'tag-create' ? 'Creating tag…' : 'Create tag'}</button>
        </form>

        <form aria-label="Create goal" className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/55 p-5" onSubmit={createGoal}>
          <h3 className="text-lg font-semibold">Create a goal</h3>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Goal name<input className={inputClass} maxLength={160} name="goalName" required /></label>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Description (optional)<textarea className={inputClass} maxLength={2000} name="goalDescription" rows={2} /></label>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Parent goal (optional)<select className={inputClass} name="goalParent"><option value="">No parent</option>{goals.map((goal) => <option key={goal.id} value={goal.id}>{goal.name}</option>)}</select></label>
          <button className={`${buttonClass} mt-4`} disabled={busy === 'goal-create'} type="submit">{busy === 'goal-create' ? 'Creating goal…' : 'Create goal'}</button>
        </form>
      </div>}

      <div className="mt-6 grid gap-6 lg:grid-cols-2">
        <div>
          <h3 className="text-lg font-semibold">Tag hierarchy</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">Parent-child structure is shown as a nested list. Select an asset to manage explicit and inherited values.</p>
          <label className="mt-4 block text-sm font-semibold text-steward-mist-muted">Asset<select className={inputClass} onChange={(event) => setAssetID(event.target.value)} value={assetID}><option value="">Select an asset</option>{assets.map((asset) => <option key={asset.id} value={asset.id}>{asset.name}</option>)}</select></label>
          <TagTree busy={busy} canWrite={canWrite} effectiveByID={effectiveByID} onRemove={removeTagRule} onSet={setTag} tagByID={tagByID} tags={tags} />
        </div>

        <div>
          <h3 className="text-lg font-semibold">Goals for this asset</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">Goals become durable dimensions for later planning and analytics.</p>
          {canWrite && <GoalLinker busy={busy} goalLinks={goalLinks} goals={goals} onLink={linkGoal} />}
          {goalLinks.length === 0 ? <p className="mt-4 rounded-lg border border-dashed border-steward-ink-800 p-4 text-sm text-steward-mist-muted">No goals are linked to this asset.</p> : <ul aria-label="Linked goals" className="mt-4 space-y-3">{goalLinks.map((link) => {
            const goal = goalByID.get(link.goalId)
            return <li className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-steward-ink-800 bg-steward-ink-950/55 p-4" key={link.goalId}><span><strong>{goal?.name ?? link.goalId}</strong>{goal?.description && <span className="mt-1 block text-sm text-steward-mist-muted">{goal.description}</span>}</span>{canWrite && <button className={secondaryButtonClass} disabled={busy === `goal-${link.goalId}`} onClick={() => void unlinkGoal(link.goalId)} type="button">Unlink</button>}</li>
          })}</ul>}
        </div>
      </div>
    </section>
  )
}

function TagTree({ tags, tagByID, effectiveByID, canWrite, busy, onSet, onRemove }: {
  tags: Tag[]
  tagByID: Map<string, Tag>
  effectiveByID: Map<string, EffectiveTag>
  canWrite: boolean
  busy: string
  onSet: (tag: Tag, mode: 'include' | 'suppress') => Promise<void>
  onRemove: (tag: Tag, revision: number) => Promise<void>
}) {
  const children = new Map<string, Tag[]>()
  for (const tag of tags) {
    const parent = tag.parentId && tagByID.has(tag.parentId) ? tag.parentId : ''
    children.set(parent, [...(children.get(parent) ?? []), tag])
  }
  for (const entries of children.values()) entries.sort((left, right) => left.name.localeCompare(right.name))

  function branch(parentID: string, level: number): ReactNode {
    const entries = children.get(parentID) ?? []
    if (entries.length === 0) return null
    return <ul aria-label={level === 1 ? 'Organization tag hierarchy' : undefined} className={`${level === 1 ? 'mt-4' : 'ml-5 mt-3 border-l border-steward-ink-800 pl-4'} space-y-3`}>{entries.map((tag) => {
      const effective = effectiveByID.get(tag.id)
      const source = effective?.sourceTagId ? tagByID.get(effective.sourceTagId)?.name ?? effective.sourceTagId : ''
      return <li key={tag.id}>
        <div className="rounded-lg border border-steward-ink-800 bg-steward-ink-950/55 p-4">
          <div className="flex flex-wrap items-start justify-between gap-3"><span><strong>{tag.name}</strong><span className="ml-2 rounded-full bg-steward-ink-800 px-2 py-1 text-xs">{effective?.state ?? 'unassigned'}</span>{tag.inheritByDefault && <span className="mt-1 block text-xs text-steward-mist-muted">Inherited by child assignments</span>}{effective?.state === 'inherited' && <span className="mt-1 block text-xs text-steward-mist-muted">Inherited from {source}</span>}{effective?.rule && <span className="mt-1 block text-xs text-steward-mist-muted">Explicit {effective.rule.mode} rule, revision {effective.rule.revision}</span>}</span>
            {canWrite && <span className="flex flex-wrap gap-2"><button className={secondaryButtonClass} disabled={busy === `tag-${tag.id}`} onClick={() => void onSet(tag, 'include')} type="button">Apply</button><button className={secondaryButtonClass} disabled={busy === `tag-${tag.id}`} onClick={() => void onSet(tag, 'suppress')} type="button">Suppress</button>{effective?.rule && <button className={secondaryButtonClass} disabled={busy === `tag-${tag.id}`} onClick={() => void onRemove(tag, effective.rule!.revision)} type="button">Use inheritance</button>}</span>}
          </div>
        </div>
        {branch(tag.id, level + 1)}
      </li>
    })}</ul>
  }
  return tags.length === 0 ? <p className="mt-4 rounded-lg border border-dashed border-steward-ink-800 p-4 text-sm text-steward-mist-muted">No tags have been created.</p> : branch('', 1)
}

function GoalLinker({ goals, goalLinks, busy, onLink }: { goals: Goal[]; goalLinks: GoalLink[]; busy: string; onLink: (goalID: string) => Promise<void> }) {
  const [goalID, setGoalID] = useState('')
  const linked = new Set(goalLinks.map((link) => link.goalId))
  const available = goals.filter((goal) => !linked.has(goal.id))
  return <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-end"><label className="flex-1 text-sm font-semibold text-steward-mist-muted">Goal<select className={inputClass} onChange={(event) => setGoalID(event.target.value)} value={goalID}><option value="">Select a goal</option>{available.map((goal) => <option key={goal.id} value={goal.id}>{goal.name}</option>)}</select></label><button className={buttonClass} disabled={!goalID || busy === 'goal-link'} onClick={() => { void onLink(goalID); setGoalID('') }} type="button">Link goal</button></div>
}
