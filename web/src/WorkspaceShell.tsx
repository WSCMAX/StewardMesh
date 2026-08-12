import type { ReactNode } from 'react'
import type { GuideTopicID } from './guide'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

export type WorkspaceAreaID = 'overview' | Exclude<GuideTopicID, 'workspace' | 'guide'>

export type WorkspaceArea = {
  id: WorkspaceAreaID
  name: string
  descriptor: string
  summary: string
  permission?: string
  limited?: boolean
  content: ReactNode
}

type WorkspaceShellProps = {
  activeArea: WorkspaceAreaID
  areas: readonly WorkspaceArea[]
  assetCount: number
  healthLabel: string
  onNavigate: (area: WorkspaceAreaID) => void
  onOpenHelp: (topic: GuideTopicID) => void
  onReportIssue: () => void
  roles: readonly string[]
  visitedAreas: ReadonlySet<WorkspaceAreaID>
}

const workspaceAreaIDs: readonly WorkspaceAreaID[] = ['overview', 'atlas', 'horizon', 'ledger', 'threads', 'vault', 'people', 'guard']

export function workspaceAreaFromHash(hash: string): WorkspaceAreaID {
  const candidate = hash.replace(/^#workspace-/, '')
  return workspaceAreaIDs.includes(candidate as WorkspaceAreaID) ? candidate as WorkspaceAreaID : 'overview'
}

export function workspaceHash(area: WorkspaceAreaID) {
  return `#workspace-${area}`
}

export default function WorkspaceShell({ activeArea, areas, assetCount, healthLabel, onNavigate, onOpenHelp, onReportIssue, roles, visitedAreas }: WorkspaceShellProps) {
  const active = areas.find((area) => area.id === activeArea) ?? areas[0]
  const roleSummary = roles.length > 0 ? roles.join(', ') : 'No role assigned'

  return (
    <section className="lg:grid lg:grid-cols-[16rem_minmax(0,1fr)] lg:items-start lg:gap-6" data-feature="experience.workspace" data-requirement="REQ-WORKSPACE-001">
      <aside className="mb-5 rounded-2xl border border-steward-ink-800/80 bg-steward-ink-900/95 p-3 shadow-xl shadow-black/10 lg:sticky lg:top-5 lg:mb-0" aria-label="Workspace navigation">
        <div className="px-2 pb-3 pt-2">
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Workspace</p>
          <p className="mt-1 text-sm leading-5 text-steward-mist-muted">Move between focused work areas without losing your place.</p>
        </div>
        <nav aria-label="Product areas">
          <ul className="flex snap-x gap-2 overflow-x-auto pb-2 lg:block lg:space-y-1 lg:overflow-visible lg:pb-0">
            {areas.map((area) => {
              const selected = area.id === active.id
              const limited = Boolean(area.limited)
              return <li className="min-w-[10rem] snap-start lg:min-w-0" key={area.id}>
                <a
                  aria-label={`${area.name} — ${area.descriptor}${limited ? ' (limited access)' : ''}`}
                  aria-current={selected ? 'page' : undefined}
                  className={`group flex min-h-12 w-full items-center gap-3 rounded-xl border px-3 py-2 text-left transition ${selected ? 'border-steward-teal/60 bg-steward-teal/12 text-steward-mist shadow-inner shadow-steward-teal/5' : 'border-transparent text-steward-mist-muted hover:border-steward-ink-800 hover:bg-steward-ink-950/45 hover:text-steward-mist'}`}
                  href={workspaceHash(area.id)}
                  onClick={(event) => { event.preventDefault(); onNavigate(area.id) }}
                >
                  <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-lg text-xs font-bold ${selected ? 'bg-steward-teal text-steward-ink-950' : 'bg-steward-ink-800 text-steward-mist'}`}>{area.name.slice(0, 2).toUpperCase()}</span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-semibold">{area.name}</span>
                    <span className="block truncate text-xs">{area.descriptor}</span>
                  </span>
                  {limited && <span className="sr-only">Limited access</span>}
                </a>
              </li>
            })}
          </ul>
        </nav>
        <div className="mt-3 grid grid-cols-2 gap-2 border-t border-steward-ink-800/80 pt-3 lg:grid-cols-1">
          <button className="min-h-11 rounded-lg border border-steward-ink-800 px-3 py-2 text-sm font-semibold text-steward-mist-muted transition hover:border-steward-teal hover:text-steward-teal" onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Open Guide</button>
          <button className="min-h-11 rounded-lg px-3 py-2 text-sm font-semibold text-steward-teal underline underline-offset-4" onClick={onReportIssue} type="button">Report an issue</button>
        </div>
      </aside>

      <div className="min-w-0">
        <header className="rounded-2xl border border-steward-ink-800/80 bg-[linear-gradient(135deg,rgba(18,49,76,0.95),rgba(11,34,56,0.92))] p-5 shadow-xl shadow-black/10 sm:p-6">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              <p className="text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal">Workspace <span aria-hidden="true">/</span> {active.name}</p>
              <h2 className="mt-2 text-2xl font-bold tracking-tight sm:text-3xl" id="workspace-context-heading" tabIndex={-1}>{active.name} — {active.descriptor}</h2>
              <p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">{active.summary}</p>
            </div>
            <button className="min-h-11 shrink-0 rounded-lg border border-steward-teal px-4 py-2 text-sm font-semibold text-steward-teal transition hover:bg-steward-teal/10" onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Help for {active.name}</button>
          </div>
          <dl className="mt-5 grid gap-2 text-sm sm:grid-cols-3">
            <ContextItem label="Current area" value={active.name} />
            <ContextItem label="Your access" value={roleSummary} />
            <ContextItem label="Service" value={healthLabel} />
          </dl>
        </header>

        <div className="mt-5 min-w-0">
          {healthLabel === 'Unavailable' && <p className="mb-4 rounded-xl border border-steward-warning/50 bg-steward-warning/12 p-4 text-sm leading-6 text-steward-mist-muted" role="status"><strong className="text-steward-mist">Service unavailable.</strong> Previously loaded context may be stale, and protected reads or changes may fail until the Go service reconnects.</p>}
          {areas.map((area) => {
            return <div
              aria-labelledby="workspace-context-heading"
              className="min-w-0"
              hidden={area.id !== active.id}
              id={area.id === 'overview' ? 'workspace-overview' : `guide-${area.id}`}
              key={area.id}
            >
              {visitedAreas.has(area.id) ? area.content : <p className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-5 text-steward-mist-muted" role="status">Opening {area.name}…</p>}
            </div>
          })}
        </div>

        {active.id === 'overview' && <dl className="sr-only"><dt>Tracked assets</dt><dd>{assetCount}</dd></dl>}
      </div>
    </section>
  )
}

function ContextItem({ label, value }: { label: string; value: string }) {
  return <div className="rounded-lg border border-steward-ink-800/75 bg-steward-ink-950/35 px-3 py-2"><dt className="text-xs font-semibold uppercase tracking-wide text-steward-mist-muted">{label}</dt><dd className="mt-1 truncate font-semibold text-steward-mist">{value}</dd></div>
}
