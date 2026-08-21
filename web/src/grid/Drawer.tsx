import { useCallback, useEffect, useId, useRef, type ReactNode } from 'react'
import { lockScroll } from '../scrollLock'
import { cx, panelClass, secondaryButtonClass, sectionKickerClass } from '../ui'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// A side panel for record detail and create forms. Moving these out of the page
// flow keeps a screen open on its data instead of a stack of collapsed forms.
// Focus handling matches the Guide overlay: Escape closes, Tab wraps inside the
// panel, and focus returns to whatever opened it.

const focusableSelector = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export type DrawerProps = {
  open: boolean
  onClose: () => void
  title: string
  kicker?: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  /** Header actions shown beside Close, e.g. primary navigation buttons. */
  actions?: ReactNode
  /** Widens the panel for forms with side-by-side fields. */
  wide?: boolean
}

export default function Drawer({ open, onClose, title, kicker, description, children, footer, actions, wide = false }: DrawerProps) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  const closeRef = useRef<HTMLButtonElement | null>(null)
  const openerRef = useRef<HTMLElement | null>(null)
  const wasOpen = useRef(false)
  const headingID = `steward-drawer-heading-${useId()}`

  const closeDrawer = useCallback(() => {
    onClose()
    queueMicrotask(() => openerRef.current?.focus())
  }, [onClose])

  useEffect(() => {
    if (open && !wasOpen.current) {
      openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
      queueMicrotask(() => closeRef.current?.focus())
    }
    wasOpen.current = open
  }, [open])

  useEffect(() => {
    if (!open) return undefined
    const releaseScroll = lockScroll()
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        closeDrawer()
        return
      }
      if (event.key !== 'Tab') return
      const focusable = Array.from(panelRef.current?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])
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
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      releaseScroll()
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [closeDrawer, open])

  if (!open) return null

  return <div className="fixed inset-0 z-40 flex justify-end">
    <div aria-hidden="true" className="absolute inset-0 bg-steward-ink-950/70 backdrop-blur-sm" onClick={closeDrawer} />
    <div
      aria-labelledby={headingID}
      aria-modal="true"
      className={cx(panelClass, 'relative flex h-[100dvh] max-h-[100dvh] w-full flex-col overflow-hidden rounded-none border-y-0 border-r-0', wide ? 'max-w-3xl' : 'max-w-xl')}
      ref={panelRef}
      role="dialog"
    >
      <div className="flex shrink-0 items-start justify-between gap-4 border-b border-white/10 bg-steward-ink-900/95 px-5 py-4">
        <div className="min-w-0">
          {kicker && <p className={sectionKickerClass}>{kicker}</p>}
          <h2 className="mt-1 text-xl font-semibold tracking-tight text-steward-mist" id={headingID}>{title}</h2>
          {description && <p className="mt-1 text-sm leading-6 text-steward-mist-muted">{description}</p>}
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
          {actions}
          <button className={secondaryButtonClass} onClick={closeDrawer} ref={closeRef} type="button">Close</button>
        </div>
      </div>
      <div className={cx('min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-4 steward-scrollbar', footer ? 'pb-28' : 'pb-8')}>{children}</div>
      {footer && <div className="shrink-0 border-t border-white/10 bg-steward-ink-900/95 px-5 py-3">{footer}</div>}
    </div>
  </div>
}
