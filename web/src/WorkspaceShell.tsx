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
  const raw = hash.replace(/^#/, '')
  const path = raw.split('?')[0] ?? ''
  const candidate = path.replace(/^workspace-/, '')
  return workspaceAreaIDs.includes(candidate as WorkspaceAreaID) ? candidate as WorkspaceAreaID : 'overview'
}

export function workspaceHash(area: WorkspaceAreaID) {
  return `#workspace-${area}`
}

const navExpandedStorageKey = 'stewardmesh.workspace.navExpanded'

function readNavExpanded() {
  try {
    return window.localStorage.getItem(navExpandedStorageKey) === '1'
  } catch {
    return false
  }
}

export default function WorkspaceShell({ activeArea, areas, assetCount, healthLabel, onNavigate, onOpenHelp, onReportIssue, roles, visitedAreas }: WorkspaceShellProps) {
  const active = areas.find((area) => area.id === activeArea) ?? areas[0]
  const roleSummary = roles.length > 0 ? roles.join(', ') : 'No role assigned'
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [navExpanded, setNavExpanded] = useState(readNavExpanded)
  const mobileMenuButtonRef = useRef<HTMLButtonElement>(null)
  const mobileCloseButtonRef = useRef<HTMLButtonElement>(null)
  const mobilePanelRef = useRef<HTMLDivElement>(null)

  function toggleNavExpanded() {
    setNavExpanded((current) => {
      const next = !current
      try {
        window.localStorage.setItem(navExpandedStorageKey, next ? '1' : '0')
      } catch {
        /* ignore quota / private mode */
      }
      return next
    })
  }

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

  const navigation = <WorkspaceNavigation active={active} areas={areas} collapsed={!navExpanded} onNavigate={navigate} onOpenHelp={onOpenHelp} onReportIssue={onReportIssue} />

  const recordScope = active.readAccess ? scopeSummary(active.readAccess) : 'Workspace overview'
  const changeScope = active.writePermission
    ? active.writeAccess?.level === 'organization' ? 'Changes allowed'
      : active.writeAccess?.level === 'scoped' ? scopeSummary(active.writeAccess)
        : `Requires ${active.writePermission}`
    : 'Read only'

  return (
    <section className={cx('min-w-0 max-w-full lg:grid lg:min-h-[calc(100svh-3.25rem)]', navExpanded ? 'lg:grid-cols-[13rem_minmax(0,1fr)]' : 'lg:grid-cols-[3.25rem_minmax(0,1fr)]')} data-feature="experience.workspace" data-requirement="REQ-WORKSPACE-001">
      <aside aria-label="Workspace navigation" className="steward-scrollbar sticky top-[3.25rem] hidden max-h-[calc(100svh-3.25rem)] overflow-y-auto border-r border-white/10 bg-steward-ink-950 p-1.5 lg:block">
        <div className={cx('mb-1 flex', navExpanded ? 'justify-end px-1' : 'justify-center')}>
          <button
            aria-expanded={navExpanded}
            aria-label={navExpanded ? 'Collapse workspace navigation' : 'Expand workspace navigation'}
            className={`${plainButtonClass} min-h-8 px-2`}
            onClick={toggleNavExpanded}
            type="button"
          >
            <MenuIcon open={navExpanded} />
            <span className="sr-only">{navExpanded ? 'Collapse navigation' : 'Expand navigation'}</span>
          </button>
        </div>
        {navigation}
      </aside>

      {mobileNavOpen && <div className="fixed inset-0 z-50 lg:hidden">
        <button aria-hidden="true" className="absolute inset-0 cursor-default bg-black/60" onClick={closeMobileNavigation} tabIndex={-1} type="button" />
        <div aria-label="Workspace navigation" aria-modal="true" className="absolute inset-y-0 left-0 w-[min(18rem,88vw)] overflow-y-auto border-r border-white/10 bg-steward-ink-950 p-3" ref={mobilePanelRef} role="dialog">
          <div className="mb-2 flex justify-end"><button aria-label="Close workspace navigation" className={plainButtonClass} onClick={closeMobileNavigation} ref={mobileCloseButtonRef} type="button"><MenuIcon open /></button></div>
          <WorkspaceNavigation active={active} areas={areas} collapsed={false} onNavigate={navigate} onOpenHelp={onOpenHelp} onReportIssue={onReportIssue} />
        </div>
      </div>}

      <div aria-hidden={mobileNavOpen ? true : undefined} className="min-w-0 px-3 py-3 sm:px-4">
        <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-white/[0.07] pb-2">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <button aria-expanded={mobileNavOpen} aria-label="Open workspace navigation" className={`${secondaryButtonClass} px-2.5 lg:hidden`} onClick={() => setMobileNavOpen(true)} ref={mobileMenuButtonRef} type="button"><MenuIcon /></button>
              <h2 className="truncate text-lg font-semibold text-steward-mist" id="workspace-context-heading" tabIndex={-1}>{active.name}</h2>
            </div>
            <p className="mt-0.5 truncate text-xs text-steward-mist-muted">
              <span>{active.descriptor}</span>
              <span aria-hidden="true"> · </span>
              <span>{changeScope}</span>
              <span className="sr-only"> Signed in as {roleSummary}. {recordScope}. {healthLabel}.</span>
            </p>
          </div>
          <button className={secondaryButtonClass} onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Help for {active.name}</button>
        </header>

        <div className="mt-3 min-w-0">
          {healthLabel === 'Unavailable' && <p className="mb-3 rounded-md border border-steward-warning/40 bg-steward-warning/10 p-3 text-sm leading-6 text-steward-mist-muted" role="status"><strong className="text-steward-mist">Service unavailable.</strong> Previously loaded context may be stale, and protected reads or changes may fail until the Go service reconnects.</p>}
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

function WorkspaceNavigation({ active, areas, collapsed, onNavigate, onOpenHelp, onReportIssue }: { active: WorkspaceArea; areas: readonly WorkspaceArea[]; collapsed: boolean; onNavigate: (area: WorkspaceAreaID) => void; onOpenHelp: (topic: GuideTopicID) => void; onReportIssue: () => void }) {
  return <>
    {!collapsed && <div className="px-2 pb-2 pt-1.5">
      <p className="text-[13px] font-medium text-steward-mist">Workspace</p>
    </div>}
    <nav aria-label="Product areas">
      <ul className="space-y-px">
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
                'group relative flex min-h-9 w-full items-center rounded-md py-1 text-left transition',
                collapsed ? 'justify-center px-1' : 'gap-2 px-2',
                selected ? 'bg-white/[0.06] text-white' : 'text-steward-mist-muted hover:bg-white/[0.035] hover:text-white',
              )}
              href={workspaceHash(area.id)}
              onClick={(event) => { event.preventDefault(); onNavigate(area.id) }}
              title={collapsed ? `${area.name} — ${area.descriptor}` : area.descriptor}
            >
              {!collapsed && selected && <span aria-hidden="true" className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-steward-teal" />}
              <span aria-hidden="true" className={cx('grid size-6 shrink-0 place-items-center text-current', selected ? 'text-steward-teal' : 'text-steward-slate group-hover:text-steward-mist')}><AreaIcon area={area.id as AreaIconName} className="size-3.5" /></span>
              {!collapsed && <span className="min-w-0 flex-1 truncate text-sm font-medium">{area.name}</span>}
              {accessLabel && <span className="sr-only">{accessLabel}</span>}
            </a>
          </li>
        })}
      </ul>
    </nav>
    {!collapsed && <div className="mt-3 grid gap-0.5 border-t border-white/[0.07] px-1 pt-3">
      <button className={`${plainButtonClass} min-h-9 justify-start`} onClick={() => onOpenHelp(active.id === 'overview' ? 'workspace' : active.id)} type="button">Open Guide</button>
      <button className={`${plainButtonClass} min-h-9 justify-start text-steward-mist-muted hover:text-steward-teal`} onClick={onReportIssue} type="button">Report an issue</button>
    </div>}
  </>
}

