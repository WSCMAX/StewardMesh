import type { ReactNode } from 'react'

// Shared surface tokens for every product area. Radii stay modest and shadows
// stay quiet so Atlas, People, Stack, and the rest read as one application
// rather than a set of decorative cards.
export function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ')
}

export const panelClass = 'rounded-md border border-white/[0.09] bg-steward-ink-900'
export const subpanelClass = 'rounded-md border border-white/[0.08] bg-steward-ink-950/55'
/** Floating menus and pickers. Opaque on purpose so grid cells do not show through. */
export const menuSurfaceClass = 'rounded-md border border-white/12 bg-steward-ink-900 text-steward-mist shadow-lg shadow-black/40 overscroll-contain'
export const inputClass = 'mt-1.5 min-h-11 w-full rounded-md border-0 bg-steward-ink-950 px-3 py-2 text-base text-steward-mist ring-1 ring-inset ring-white/10 transition placeholder:text-steward-slate hover:ring-white/18 focus:ring-2 focus:ring-inset focus:ring-steward-teal disabled:cursor-not-allowed disabled:text-steward-mist-muted disabled:opacity-100 sm:text-sm'
export const compactInputClass = 'min-h-10 min-w-0 rounded-md border-0 bg-steward-ink-950 px-2.5 py-1.5 text-sm text-steward-mist ring-1 ring-inset ring-white/10 transition hover:ring-white/18 focus:ring-2 focus:ring-inset focus:ring-steward-teal disabled:cursor-not-allowed disabled:text-steward-mist-muted disabled:opacity-100'
export const labelClass = 'block text-sm font-medium text-steward-mist'
export const buttonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-md border border-transparent bg-steward-teal px-3.5 py-2 text-sm font-semibold text-steward-ink-950 transition hover:bg-[#35d2bd] disabled:cursor-wait disabled:bg-steward-teal disabled:text-steward-ink-950 disabled:opacity-100'
export const secondaryButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-md border border-white/12 bg-transparent px-3.5 py-2 text-sm font-medium text-steward-mist transition hover:border-white/20 hover:bg-white/[0.04] disabled:cursor-not-allowed disabled:text-steward-mist-muted disabled:opacity-100'
export const dangerButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-md border border-steward-danger/45 bg-steward-danger/10 px-3.5 py-2 text-sm font-medium text-[#ffbdc3] transition hover:border-steward-danger/70 hover:bg-steward-danger/20 disabled:cursor-not-allowed disabled:text-[#ffbdc3] disabled:opacity-100'
export const plainButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-md px-2.5 py-2 text-sm font-medium text-steward-teal transition hover:bg-steward-teal/10 hover:text-[#5be0cc] disabled:cursor-not-allowed disabled:text-steward-mist-muted disabled:opacity-100 disabled:hover:bg-transparent disabled:hover:text-steward-mist-muted'
export const sectionKickerClass = 'text-[13px] font-medium text-steward-slate'
export const sectionTitleClass = 'mt-1 text-xl font-semibold text-steward-mist'
export const sectionDescriptionClass = 'mt-1.5 max-w-3xl text-sm leading-6 text-steward-mist-muted'
export const tableWrapClass = 'min-w-0 max-w-full overflow-x-auto rounded-md border border-white/[0.08] bg-steward-ink-950/40'
export const emptyStateClass = 'rounded-md border border-dashed border-white/12 bg-transparent px-4 py-7 text-center text-sm leading-6 text-steward-mist-muted'

// Dense spreadsheet surfaces. Rows stay a fixed height so a full screen of
// records scans as a grid, which the roomier table tokens above cannot do.
export const gridWrapClass = 'steward-grid-scroll min-w-0 max-w-full overflow-auto overscroll-contain rounded-md border border-white/[0.08] bg-steward-ink-950 isolate focus-visible:-outline-offset-2'
export const gridTableClass = 'w-full table-fixed border-separate border-spacing-0 text-left text-[13px] leading-5'
export const gridHeaderCellClass = 'sticky top-0 z-10 border-b border-white/12 bg-steward-ink-900 px-2.5 py-1.5 text-left align-middle font-medium text-steward-mist-muted relative focus-visible:outline-none'
export const gridCellClass = 'max-w-0 truncate border-b border-white/[0.06] px-2.5 align-middle focus-visible:outline-none'
export const gridActionCellClass = 'border-b border-white/[0.06] bg-steward-ink-950 px-2.5 align-middle focus-visible:outline-none'
export const gridActionButtonClass = 'inline-flex min-h-6 shrink-0 items-center justify-center rounded-md px-1.5 text-xs font-medium text-steward-teal transition hover:bg-steward-teal/10 hover:text-[#5be0cc] disabled:cursor-not-allowed disabled:text-steward-mist-muted disabled:opacity-100'
export const gridEditorClass = 'h-full min-h-8 w-full min-w-0 rounded-none border-0 bg-steward-ink-950 px-2 py-0 text-[13px] text-steward-mist ring-2 ring-inset ring-steward-teal focus:outline-none'
export const gridToolbarClass = 'flex flex-wrap items-center gap-2 border-b border-white/[0.08] bg-steward-ink-900 px-3 py-2'
export const gridFilterClass = 'h-7 w-full min-w-0 rounded-sm border-0 bg-steward-ink-950 px-2 py-0 text-xs font-normal text-steward-mist ring-1 ring-inset ring-white/10 placeholder:text-steward-slate focus:ring-2 focus:ring-inset focus:ring-steward-teal'

export type AreaIconName = 'overview' | 'atlas' | 'horizon' | 'ledger' | 'stack' | 'signals' | 'reach' | 'threads' | 'vault' | 'exchange' | 'people' | 'mesh' | 'bridge' | 'guard'

const iconPaths: Record<AreaIconName, ReactNode> = {
  overview: <><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></>,
  atlas: <><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" /><path d="m4.5 7.8 7.5 4.3 7.5-4.3M12 12.1V21" /></>,
  horizon: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.5 2M4.8 5.2 3 3.5M19.2 5.2 21 3.5" /></>,
  ledger: <><path d="M6 3h12a2 2 0 0 1 2 2v16l-3-2-2 2-3-2-3 2-3-2-2 2V5a2 2 0 0 1 2-2Z" /><path d="M8 8h8M8 12h8M8 16h5" /></>,
  stack: <><path d="m12 3 8 4-8 4-8-4 8-4Z" /><path d="m4 12 8 4 8-4M4 17l8 4 8-4" /></>,
  signals: <><path d="M4 17h16M6 13l3-3 3 2 5-6 2 2" /><path d="M5 21h14" /><circle cx="6" cy="13" r="1" /><circle cx="17" cy="6" r="1" /></>,
  reach: <><path d="M4 5h16v12H7l-3 3V5Z" /><path d="m7 9 5 3 5-3" /></>,
  threads: <><path d="M9.5 14.5 14.5 9.5" /><path d="M7.2 17.8 5.4 19.6a3.5 3.5 0 0 1-5-5l4-4a3.5 3.5 0 0 1 5 0" transform="translate(2 -1)" /><path d="m14.8 6.2 1.8-1.8a3.5 3.5 0 0 1 5 5l-4 4a3.5 3.5 0 0 1-5 0" transform="translate(-2 1)" /></>,
  vault: <><rect x="3" y="5" width="18" height="15" rx="2" /><circle cx="12" cy="12.5" r="3" /><path d="M12 9.5V5M12 15.5V20M9 12.5H3M15 12.5h6" /></>,
  exchange: <><path d="M4 7h13M14 4l3 3-3 3M20 17H7M10 14l-3 3 3 3" /><rect x="3" y="3" width="18" height="18" rx="3" /></>,
  people: <><path d="M16 21v-2a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v2" /><circle cx="9.5" cy="7" r="4" /><path d="M17 11a3.5 3.5 0 0 0 0-7M21 21v-2a4 4 0 0 0-3-3.7" /></>,
  mesh: <><circle cx="6" cy="7" r="2.5" /><circle cx="18" cy="7" r="2.5" /><circle cx="7" cy="18" r="2.5" /><circle cx="17" cy="17" r="2.5" /><path d="M8.2 8.2 15.8 8.2M7.8 9.2 8.8 15.8M16.2 9.2 15.4 14.8M9.4 18h5.2" /></>,
  bridge: <><path d="M8 7h8M8 17h8" /><path d="M6 4v6M18 14v6" /><circle cx="6" cy="12" r="2" /><circle cx="18" cy="12" r="2" /></>,
  guard: <><path d="M12 3 20 6v5c0 5.2-3.3 8.5-8 10-4.7-1.5-8-4.8-8-10V6l8-3Z" /><path d="m8.5 12 2.2 2.2 4.8-5" /></>,
}

export function AreaIcon({ area, className = 'size-5' }: { area: AreaIconName; className?: string }) {
  return <svg aria-hidden="true" className={className} fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" viewBox="0 0 24 24">{iconPaths[area]}</svg>
}

export function ChevronRightIcon({ className = 'size-4' }: { className?: string }) {
  return <svg aria-hidden="true" className={className} fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" viewBox="0 0 20 20"><path d="m7.5 4 6 6-6 6" /></svg>
}

export function MenuIcon({ open = false, className = 'size-5' }: { open?: boolean; className?: string }) {
  return <svg aria-hidden="true" className={className} fill="none" stroke="currentColor" strokeLinecap="round" strokeWidth="1.8" viewBox="0 0 24 24">{open ? <><path d="M6 6l12 12M18 6 6 18" /></> : <><path d="M4 7h16M4 12h16M4 17h16" /></>}</svg>
}

export function StatusBadge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'success' | 'warning' | 'info' | 'neutral' }) {
  const tones = {
    success: 'border-steward-success/35 bg-steward-success/12 text-[#98eab9]',
    warning: 'border-steward-warning/40 bg-steward-warning/12 text-[#ffd08a]',
    info: 'border-steward-blue/35 bg-steward-blue/12 text-[#a9c7ff]',
    neutral: 'border-white/10 bg-white/[0.04] text-steward-mist-muted',
  }
  return <span className={cx('inline-flex items-center gap-1.5 rounded-sm border px-1.5 py-0.5 text-xs font-medium', tones[tone])}>{children}</span>
}

export function ProductHeader({
  actions, description, headingId, headingLevel = 2, kicker, title,
}: {
  actions?: ReactNode
  description?: ReactNode
  headingId: string
  headingLevel?: 2 | 3
  kicker?: string
  title: string
}) {
  const Heading = headingLevel === 3 ? 'h3' : 'h2'
  return (
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0 max-w-3xl">
        {kicker && <p className={sectionKickerClass}>{kicker}</p>}
        <Heading className={cx(sectionTitleClass, !kicker && 'mt-0')} id={headingId}>{title}</Heading>
        {description && <p className={sectionDescriptionClass}>{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  )
}
