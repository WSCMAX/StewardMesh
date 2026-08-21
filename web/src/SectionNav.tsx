import { cx, secondaryButtonClass } from './ui'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001. Feature: experience.workspace.

export type SectionTab<ID extends string> = {
  id: ID
  label: string
  description: string
  write?: boolean
}

type SectionNavProps<ID extends string> = {
  active: ID
  ariaLabel: string
  canWrite?: boolean
  idPrefix: string
  onChange: (section: ID) => void
  tabs: readonly SectionTab<ID>[]
}

export default function SectionNav<ID extends string>({ active, ariaLabel, canWrite = true, idPrefix, onChange, tabs }: SectionNavProps<ID>) {
  const visible = tabs.filter((tab) => !tab.write || canWrite)
  return (
    <nav aria-label={ariaLabel} className="border-b border-white/10">
      <div className="flex gap-1 overflow-x-auto steward-scrollbar" role="tablist">
        {visible.map((tab) => {
          const selected = active === tab.id
          return (
            <button
              aria-controls={`${idPrefix}-panel-${tab.id}`}
              aria-selected={selected}
              className={cx(
                'relative shrink-0 px-3 py-2.5 text-sm font-medium transition',
                selected ? 'text-steward-mist' : `${secondaryButtonClass} min-h-0 rounded-none border-transparent bg-transparent px-3 py-2.5 text-steward-mist-muted`,
              )}
              id={`${idPrefix}-tab-${tab.id}`}
              key={tab.id}
              onClick={() => onChange(tab.id)}
              role="tab"
              title={tab.description}
              type="button"
            >
              {tab.label}
              {selected && <span aria-hidden="true" className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-steward-teal" />}
            </button>
          )
        })}
      </div>
      <p className="mt-2.5 text-sm text-steward-mist-muted">{visible.find((tab) => tab.id === active)?.description}</p>
    </nav>
  )
}
