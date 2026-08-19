import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx, menuSurfaceClass } from '../ui'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// The menu a cell offers on right-click, on the context-menu key, and from the
// row's own menu button. It is deliberately flat: sections carry a caption and
// their choices sit inline rather than behind submenus, because a submenu adds
// a second focus surface and a hover timing problem for a menu whose longest
// branch is five colour swatches.

export type MenuEntry =
  | { kind: 'action'; id: string; label: string; hint?: string; disabled?: boolean; run: () => void }
  | { kind: 'choice'; id: string; label: string; checked: boolean; swatch?: string; run: () => void }
  | { kind: 'group'; id: string; label: string; entries: readonly MenuEntry[] }
  | { kind: 'separator'; id: string }

export type MenuAnchor = { x: number; y: number }

const itemSelector = '[role="menuitem"]:not([aria-disabled="true"]),[role="menuitemradio"]:not([aria-disabled="true"])'
const itemClass = 'flex min-h-8 w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs text-steward-mist transition hover:bg-steward-teal/15 focus:bg-steward-teal/15 focus:outline-none aria-disabled:cursor-not-allowed aria-disabled:text-steward-slate aria-disabled:hover:bg-transparent'

export function separator(id: string): MenuEntry {
  return { kind: 'separator', id }
}

function Entry({ entry, onRun }: { entry: MenuEntry; onRun: () => void }) {
  if (entry.kind === 'separator') return <div className="my-1 border-t border-white/10" role="separator" />
  if (entry.kind === 'group') {
    return <div aria-label={entry.label} role="group">
      <p aria-hidden="true" className="px-2 pb-0.5 pt-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-steward-slate">{entry.label}</p>
      {entry.entries.map((child) => <Entry entry={child} key={child.id} onRun={onRun} />)}
    </div>
  }
  if (entry.kind === 'choice') {
    return <button
      aria-checked={entry.checked}
      className={itemClass}
      onClick={() => { entry.run(); onRun() }}
      role="menuitemradio"
      tabIndex={-1}
      type="button"
    >
      {entry.swatch
        ? <span aria-hidden="true" className={cx('size-3 shrink-0 rounded-sm border border-white/25', entry.swatch)} />
        : <span aria-hidden="true" className="w-3 shrink-0 text-center text-steward-teal">{entry.checked ? '•' : ''}</span>}
      <span className="truncate">{entry.label}</span>
      {entry.checked && entry.swatch && <span aria-hidden="true" className="ml-auto text-steward-teal">✓</span>}
    </button>
  }
  return <button
    aria-disabled={entry.disabled || undefined}
    className={itemClass}
    onClick={() => {
      if (entry.disabled) return
      entry.run()
      onRun()
    }}
    role="menuitem"
    tabIndex={-1}
    title={entry.hint}
    type="button"
  >
    <span className="truncate">{entry.label}</span>
    {entry.hint && <span aria-hidden="true" className="ml-auto pl-3 text-[10px] text-steward-slate">{entry.hint}</span>}
  </button>
}

export default function ContextMenu({ anchor, label, entries, onClose }: {
  anchor: MenuAnchor
  /** Names the menu for assistive technology, e.g. "Actions for Lab server". */
  label: string
  entries: readonly MenuEntry[]
  /** Called on Escape, outside click, and after any entry runs. */
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement | null>(null)
  const [position, setPosition] = useState(anchor)

  const items = useCallback(() => Array.from(menuRef.current?.querySelectorAll<HTMLElement>(itemSelector) ?? []), [])

  // Open against the pointer, then pull back inside the viewport so a menu
  // raised from the last row or the rightmost column stays fully reachable.
  useLayoutEffect(() => {
    const element = menuRef.current
    if (!element) return
    const { width, height } = element.getBoundingClientRect()
    const margin = 8
    setPosition({
      x: Math.max(margin, Math.min(anchor.x, window.innerWidth - width - margin)),
      y: Math.max(margin, Math.min(anchor.y, window.innerHeight - height - margin)),
    })
  }, [anchor.x, anchor.y, entries])

  useEffect(() => {
    items()[0]?.focus()
  }, [items])

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      if (!menuRef.current?.contains(event.target as Node)) onClose()
    }
    // A menu pinned to a page coordinate goes stale the moment the grid scrolls
    // underneath it. Scroll inside the menu itself has to stay, or a long
    // action list cannot be reached.
    function handleMovement(event: Event) {
      if (event.target instanceof Node && menuRef.current?.contains(event.target)) return
      onClose()
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    window.addEventListener('scroll', handleMovement, true)
    window.addEventListener('resize', handleMovement)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      window.removeEventListener('scroll', handleMovement, true)
      window.removeEventListener('resize', handleMovement)
    }
  }, [onClose])

  function moveFocus(step: number, absolute?: 'first' | 'last') {
    const focusable = items()
    if (focusable.length === 0) return
    if (absolute === 'first') {
      focusable[0].focus()
      return
    }
    if (absolute === 'last') {
      focusable[focusable.length - 1].focus()
      return
    }
    const current = focusable.indexOf(document.activeElement as HTMLElement)
    const next = (current + step + focusable.length) % focusable.length
    focusable[next].focus()
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape' || event.key === 'Tab') {
      onClose()
      event.preventDefault()
      return
    }
    if (event.key === 'ArrowDown') { moveFocus(1); event.preventDefault(); return }
    if (event.key === 'ArrowUp') { moveFocus(-1); event.preventDefault(); return }
    if (event.key === 'Home') { moveFocus(0, 'first'); event.preventDefault(); return }
    if (event.key === 'End') { moveFocus(0, 'last'); event.preventDefault() }
  }

  return createPortal(
    <div
      aria-label={label}
      className={cx(menuSurfaceClass, 'fixed z-50 max-h-[min(24rem,80vh)] w-64 overflow-y-auto p-1 steward-scrollbar')}
      onContextMenu={(event) => event.preventDefault()}
      onKeyDown={handleKeyDown}
      onWheel={(event) => event.stopPropagation()}
      ref={menuRef}
      role="menu"
      style={{ left: position.x, top: position.y }}
    >
      {entries.map((entry) => <Entry entry={entry} key={entry.id} onRun={onClose} />)}
    </div>,
    document.body,
  )
}
