import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import type { GuideTopicID } from './guide'
import { AreaIcon, MenuIcon, cx, panelClass, plainButtonClass, secondaryButtonClass, type AreaIconName } from './ui'
import { scopeSummary, type PermissionAccess } from './workspaceAccess'

// Requirements: REQ-WORKSPACE-001, REQ-SIGNALS-001, REQ-REACH-001, REQ-EXCHANGE-001. Features: experience.workspace, alerts.rules, messaging.delivery, migration.packages.

export type WorkspaceAreaID = 'overview' | Exclude<GuideTopicID, 'workspace' | 'guide'>

export type WorkspaceArea = {
  id: WorkspaceAreaID
  name: string
  descriptor: string
  summary: string
  permission?: string
  writePermission?: string
  readAccess?: PermissionAccess
  writeAccess?: PermissionAccess
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

const workspaceAreaIDs: readonly WorkspaceAreaID[] = ['overview', 'atlas', 'horizon', 'ledger', 'stack', 'signals', 'reach', 'threads', 'vault', 'exchange', 'people', 'mesh', 'bridge', 'guard']

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
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const mobileMenuButtonRef = useRef<HTMLButtonElement>(null)
  const mobileCloseButtonRef = useRef<HTMLButtonElement>(null)
  const mobilePanelRef = useRef<HTMLDivElement>(null)

  const closeMobileNavigation = useCallback(() => {
    setMobileNavOpen(false)
    queueMicrotask(() => mobileMenuButtonRef.current?.focus())
  }, [])

  useEffect(() => {
    if (!mobileNavOpen) return
    function handleDialogKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        closeMobileNavigation()
        return
      }
      if (event.key !== 'Tab') return
      const focusable = Array.from(mobilePanelRef.current?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [])
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    const priorOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    queueMicrotask(() => mobileCloseButtonRef.current?.focus())
    window.addEventListener('keydown', handleDialogKeyDown)
    return () => {
      document.body.style.overflow = priorOverflow
      window.removeEventListener('keydown', handleDialogKeyDown)
    }
  }, [closeMobileNavigation, mobileNavOpen])

  function navigate(area: WorkspaceAreaID) {
    setMobileNavOpen(false)
    onNavigate(area)
  }

  const navigation = <WorkspaceNavigation active={active} areas={areas} onNavigate={navigate} onOpenHelp={onOpenHelp} onReportIssue={onReportIssue} />

  return (
    <section className="min-w-0 max-w-full lg:grid lg:grid-cols-[15.5rem_minmax(0,1fr)] lg:items-start lg:gap-5" data-feature="experience.workspace" data-requirement="REQ-WORKSPACE-001">
      <aside aria-label="Workspace navigation" className={`${panelClass} steward-scrollbar sticky top-[4.5rem] hidden max-h-[calc(100vh-5.5rem)] overflow-y-auto p-2 lg:block`}>
        {navigation}
      </aside>

      {mobileNavOpen && <div className="fixed inset-0 z-50 lg:hidden">
        <button aria-hidden="true" className="absolute inset-0 cursor-default bg-black/60" onClick={closeMobileNavigation} tabIndex={-1} type="button" />
        <div aria-label="Workspace navigation" aria-modal="true" className="absolute inset-y-0 left-0 w-[min(20rem,88vw)] overflow-y-auto border-r border-white/10 bg-steward-ink-900 p-3" ref={mobilePanelRef} role="dialog">
          <div className="mb-2 flex justify-end"><button aria-label="Close workspace navigation" className={plainButtonClass} onClick={closeMobileNavigation} ref={mobileCloseButtonRef} type="button"><MenuIcon open /></button></div>
          {navigation}
        </div>
      </div>}

      <div aria-hidden={mobileNavOpen ? true : undefined} className="min-w-0">
        <header className={`${panelClass} overflow-hidden`}>
          <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-3 sm:px-5">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <button aria-expanded={mobileNavOpen} aria-label="Open workspace navigation" className={`${secondaryButtonClass} px-2.5 lg:hidden`} onClick={() => setMobileNavOpen(true)} ref={mobileMenuButtonRef} type="button"><MenuIcon /></button>
                <p className="text-[13px] text-steward-slate">{active.name}</p>
              </div>
              <h2 className="mt-1 text-xl font-semibold text-steward-mist" id="workspace-context-heading" tabIndex={-1}>{active.name} — {active.descriptor}</h2>
              <p className="mt-1 max-w-3xl text-sm leading-6 text-steward-mist-muted">{active.summary}</p>
            </div>
            <button className={secondaryButtonClass} onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Help for {active.name}</button>
          </div>
          <dl className="grid grid-cols-2 border-t border-white/[0.07] text-sm lg:grid-cols-5">
            <ContextItem label="Current area" value={active.name} />
            <ContextItem label="Your access" value={roleSummary} />
            <ContextItem label="Visible records" value={active.readAccess ? scopeSummary(active.readAccess) : 'Workspace overview'} />
            <ContextItem label="Changes" value={active.writePermission ? active.writeAccess?.level === 'organization' ? 'Allowed organization-wide' : active.writeAccess?.level === 'scoped' ? scopeSummary(active.writeAccess) : `Requires ${active.writePermission}` : 'No feature changes'} />
            <ContextItem label="Service" value={healthLabel} />
          </dl>
        </header>

        <div className="mt-4 min-w-0">
          {healthLabel === 'Unavailable' && <p className="mb-4 rounded-md border border-steward-warning/40 bg-steward-warning/10 p-3 text-sm leading-6 text-steward-mist-muted" role="status"><strong className="text-steward-mist">Service unavailable.</strong> Previously loaded context may be stale, and protected reads or changes may fail until the Go service reconnects.</p>}
          {areas.map((area) => <section
            aria-labelledby="workspace-context-heading"
            className="min-w-0"
            hidden={area.id !== active.id}
            id={area.id === 'overview' ? 'workspace-overview' : `guide-${area.id}`}
            key={area.id}
            role="region"
          >
            {visitedAreas.has(area.id) ? area.content : <p className={`${panelClass} p-4 text-steward-mist-muted`} role="status">Opening {area.name}…</p>}
          </section>)}
        </div>

        {active.id === 'overview' && <dl className="sr-only"><dt>Tracked assets</dt><dd>{assetCount}</dd></dl>}
      </div>
    </section>
  )
}

function WorkspaceNavigation({ active, areas, onNavigate, onOpenHelp, onReportIssue }: { active: WorkspaceArea; areas: readonly WorkspaceArea[]; onNavigate: (area: WorkspaceAreaID) => void; onOpenHelp: (topic: GuideTopicID) => void; onReportIssue: () => void }) {
  return <>
    <div className="px-2.5 pb-3 pt-2">
      <p className="text-[13px] font-medium text-steward-mist">Workspace</p>
      <p className="mt-0.5 text-xs leading-5 text-steward-slate">One product area at a time.</p>
    </div>
    <nav aria-label="Product areas">
      <ul className="space-y-0.5">
        {areas.map((area) => {
          const selected = area.id === active.id
          const accessLabel = area.readAccess?.level === 'none' ? 'limited access'
            : area.readAccess?.level === 'scoped' ? 'scoped access'
              : area.writePermission && area.writeAccess?.level !== 'organization' ? 'read only' : ''
          return <li key={area.id}>
            <a
              aria-current={selected ? 'page' : undefined}
              aria-label={`${area.name} — ${area.descriptor}${accessLabel ? ` (${accessLabel})` : ''}`}
              className={cx(
                'group relative flex min-h-10 w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left transition',
                selected ? 'bg-white/[0.06] text-white' : 'text-steward-mist-muted hover:bg-white/[0.035] hover:text-white',
              )}
              href={workspaceHash(area.id)}
              onClick={(event) => { event.preventDefault(); onNavigate(area.id) }}
            >
              {selected && <span aria-hidden="true" className="absolute inset-y-2 left-0 w-0.5 rounded-full bg-steward-teal" />}
              <span aria-hidden="true" className={cx('grid size-7 shrink-0 place-items-center text-current', selected ? 'text-steward-teal' : 'text-steward-slate group-hover:text-steward-mist')}><AreaIcon area={area.id as AreaIconName} className="size-4" /></span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{area.name}</span>
                <span className="block truncate text-[11px] text-steward-mist-muted">{area.descriptor}</span>
              </span>
              {accessLabel && <span className="sr-only">{accessLabel}</span>}
            </a>
          </li>
        })}
      </ul>
    </nav>
    <div className="mt-3 grid gap-0.5 border-t border-white/[0.07] px-1 pt-3">
      <button className={`${plainButtonClass} min-h-9 justify-start`} onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Open Guide</button>
      <button className={`${plainButtonClass} min-h-9 justify-start text-steward-mist-muted hover:text-steward-teal`} onClick={onReportIssue} type="button">Report an issue</button>
    </div>
  </>
}

function ContextItem({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0 border-b border-white/[0.07] px-4 py-2.5 last:col-span-2 last:border-b-0 lg:col-span-1 lg:border-b-0 lg:last:col-span-1"><dt className="text-[11px] text-steward-slate">{label}</dt><dd className="mt-0.5 break-words text-sm font-medium leading-5 text-steward-mist">{value}</dd></div>
}
