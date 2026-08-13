import type { ReactNode } from 'react'

// Shared Tailwind Plus/Catalyst-inspired primitives, adapted to the StewardMesh
// dark palette. Keep product surfaces on these tokens so interaction states stay
// consistent across modules without hiding semantic HTML behind a component API.
export function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ')
}

export const panelClass = 'rounded-2xl border border-white/[0.08] bg-steward-ink-900/80 shadow-[0_24px_80px_-52px_rgba(0,0,0,0.95)] backdrop-blur-sm'
export const subpanelClass = 'rounded-xl border border-white/[0.08] bg-steward-ink-950/45 shadow-[inset_0_1px_0_rgba(255,255,255,0.025)]'
export const inputClass = 'mt-2 min-h-11 w-full rounded-xl border-0 bg-steward-ink-950/75 px-3.5 py-2.5 text-base text-steward-mist shadow-sm ring-1 ring-inset ring-white/10 transition placeholder:text-steward-slate hover:ring-white/20 focus:ring-2 focus:ring-inset focus:ring-steward-teal disabled:cursor-not-allowed disabled:opacity-55 sm:text-sm'
export const compactInputClass = 'min-h-11 min-w-0 rounded-xl border-0 bg-steward-ink-950/75 px-3 py-2 text-sm text-steward-mist ring-1 ring-inset ring-white/10 transition hover:ring-white/20 focus:ring-2 focus:ring-inset focus:ring-steward-teal disabled:cursor-not-allowed disabled:opacity-55'
export const labelClass = 'block text-sm font-semibold text-steward-mist'
export const buttonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-transparent bg-steward-teal px-4 py-2.5 text-sm font-semibold text-steward-ink-950 shadow-sm shadow-black/20 transition hover:bg-[#35d2bd] active:translate-y-px disabled:cursor-wait disabled:opacity-55 disabled:active:translate-y-0'
export const secondaryButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.045] px-4 py-2.5 text-sm font-semibold text-steward-mist shadow-sm transition hover:border-steward-teal/50 hover:bg-steward-teal/10 hover:text-white active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50 disabled:active:translate-y-0'
export const dangerButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-steward-danger/45 bg-steward-danger/10 px-4 py-2.5 text-sm font-semibold text-[#ffbdc3] transition hover:border-steward-danger/70 hover:bg-steward-danger/20 disabled:cursor-not-allowed disabled:opacity-50'
export const plainButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl px-3 py-2 text-sm font-semibold text-steward-teal transition hover:bg-steward-teal/10 hover:text-[#5be0cc] disabled:opacity-50'
export const sectionKickerClass = 'text-xs font-semibold uppercase tracking-[0.18em] text-steward-teal'
export const sectionTitleClass = 'mt-2 text-2xl font-semibold tracking-tight text-steward-mist sm:text-[1.75rem]'
export const sectionDescriptionClass = 'mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted sm:text-base sm:leading-7'
export const tableWrapClass = 'min-w-0 max-w-full overflow-x-auto rounded-xl border border-white/[0.08] bg-steward-ink-950/25'
export const emptyStateClass = 'rounded-xl border border-dashed border-white/15 bg-white/[0.02] px-5 py-8 text-center text-sm leading-6 text-steward-mist-muted'

export type AreaIconName = 'overview' | 'atlas' | 'horizon' | 'ledger' | 'stack' | 'signals' | 'threads' | 'vault' | 'exchange' | 'people' | 'guard'

const iconPaths: Record<AreaIconName, ReactNode> = {
  overview: <><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></>,
  atlas: <><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" /><path d="m4.5 7.8 7.5 4.3 7.5-4.3M12 12.1V21" /></>,
  horizon: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.5 2M4.8 5.2 3 3.5M19.2 5.2 21 3.5" /></>,
  ledger: <><path d="M6 3h12a2 2 0 0 1 2 2v16l-3-2-2 2-3-2-3 2-3-2-2 2V5a2 2 0 0 1 2-2Z" /><path d="M8 8h8M8 12h8M8 16h5" /></>,
  stack: <><path d="m12 3 8 4-8 4-8-4 8-4Z" /><path d="m4 12 8 4 8-4M4 17l8 4 8-4" /></>,
  signals: <><path d="M4 17h16M6 13l3-3 3 2 5-6 2 2" /><path d="M5 21h14" /><circle cx="6" cy="13" r="1" /><circle cx="17" cy="6" r="1" /></>,
  threads: <><path d="M9.5 14.5 14.5 9.5" /><path d="M7.2 17.8 5.4 19.6a3.5 3.5 0 0 1-5-5l4-4a3.5 3.5 0 0 1 5 0" transform="translate(2 -1)" /><path d="m14.8 6.2 1.8-1.8a3.5 3.5 0 0 1 5 5l-4 4a3.5 3.5 0 0 1-5 0" transform="translate(-2 1)" /></>,
  vault: <><rect x="3" y="5" width="18" height="15" rx="2" /><circle cx="12" cy="12.5" r="3" /><path d="M12 9.5V5M12 15.5V20M9 12.5H3M15 12.5h6" /></>,
  exchange: <><path d="M4 7h13M14 4l3 3-3 3M20 17H7M10 14l-3 3 3 3" /><rect x="3" y="3" width="18" height="18" rx="3" /></>,
  people: <><path d="M16 21v-2a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v2" /><circle cx="9.5" cy="7" r="4" /><path d="M17 11a3.5 3.5 0 0 0 0-7M21 21v-2a4 4 0 0 0-3-3.7" /></>,
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
    neutral: 'border-white/10 bg-white/[0.045] text-steward-mist-muted',
  }
  return <span className={cx('inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-semibold', tones[tone])}>{children}</span>
}
