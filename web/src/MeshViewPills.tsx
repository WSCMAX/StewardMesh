import { occupancyRoleMeta, occupancyRoles, type GraphColorMode } from './graphModel'
import { compactInputClass, cx } from './ui'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type MeshQuickViewID = 'campus' | 'people' | 'inventory' | 'software' | 'custom'

export type MeshQuickView = {
  id: MeshQuickViewID
  label: string
  description: string
  colorMode: GraphColorMode
  hideInactivePeople: boolean
  hideInactiveAssets: boolean
  occupancyHubs: boolean
  identityHubs: boolean
}

export const meshQuickViews: readonly MeshQuickView[] = [
  {
    id: 'campus',
    label: 'Campus',
    description: 'Active people and assets, occupancy colors, user-type hubs',
    colorMode: 'campus',
    hideInactivePeople: true,
    hideInactiveAssets: true,
    occupancyHubs: true,
    identityHubs: false,
  },
  {
    id: 'people',
    label: 'People',
    description: 'Occupancy colors with instructor, student, and identity hubs',
    colorMode: 'occupancy',
    hideInactivePeople: true,
    hideInactiveAssets: false,
    occupancyHubs: true,
    identityHubs: true,
  },
  {
    id: 'inventory',
    label: 'Inventory',
    description: 'Active assets colored by model',
    colorMode: 'model',
    hideInactivePeople: false,
    hideInactiveAssets: true,
    occupancyHubs: false,
    identityHubs: false,
  },
  {
    id: 'software',
    label: 'Software',
    description: 'Color by product, keep license and install links',
    colorMode: 'source',
    hideInactivePeople: false,
    hideInactiveAssets: false,
    occupancyHubs: false,
    identityHubs: false,
  },
]

export const defaultMeshQuickView = meshQuickViews[0]

const colorModeOptions: ReadonlyArray<[GraphColorMode, string]> = [
  ['campus', 'Campus: occupancy + model'],
  ['occupancy', 'Occupancy role'],
  ['model', 'Asset model'],
  ['type', 'Record type'],
  ['source', 'Product'],
  ['status', 'Status'],
]

const pillSelectClass = `${compactInputClass} rounded-full px-3`
const pillButtonClass = 'rounded-full border px-2.5 py-1 text-xs font-medium transition min-h-8'

export function matchingQuickView(state: Omit<MeshQuickView, 'id' | 'label' | 'description'>): MeshQuickViewID {
  const match = meshQuickViews.find((view) => (
    view.colorMode === state.colorMode
    && view.hideInactivePeople === state.hideInactivePeople
    && view.hideInactiveAssets === state.hideInactiveAssets
    && view.occupancyHubs === state.occupancyHubs
    && view.identityHubs === state.identityHubs
  ))
  return match?.id ?? 'custom'
}

export function MeshViewPills({
  colorMode,
  hideInactiveAssets,
  hideInactivePeople,
  identityHubs,
  occupancyHubs,
  onColorModeChange,
  onHideInactiveAssetsChange,
  onHideInactivePeopleChange,
  onIdentityHubsChange,
  onOccupancyHubsChange,
  onQuickViewChange,
  quickView,
}: {
  colorMode: GraphColorMode
  hideInactiveAssets: boolean
  hideInactivePeople: boolean
  identityHubs: boolean
  occupancyHubs: boolean
  onColorModeChange: (mode: GraphColorMode) => void
  onHideInactiveAssetsChange: (value: boolean) => void
  onHideInactivePeopleChange: (value: boolean) => void
  onIdentityHubsChange: (value: boolean) => void
  onOccupancyHubsChange: (value: boolean) => void
  onQuickViewChange: (view: MeshQuickView) => void
  quickView: MeshQuickViewID
}) {
  const activeView = meshQuickViews.find((view) => view.id === quickView)
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex min-w-0 items-center gap-2">
          <span className="text-xs font-medium text-steward-mist-muted">View</span>
          <select
            aria-label="Campus view"
            className={pillSelectClass}
            onChange={(event) => {
              const next = meshQuickViews.find((view) => view.id === event.target.value)
              if (next) onQuickViewChange(next)
            }}
            value={quickView === 'custom' ? 'custom' : quickView}
          >
            {meshQuickViews.map((view) => <option key={view.id} value={view.id}>{view.label}</option>)}
            <option value="custom">Custom</option>
          </select>
        </label>
        <label className="flex min-w-0 items-center gap-2">
          <span className="text-xs font-medium text-steward-mist-muted">Color</span>
          <select
            aria-label="Color records by"
            className={pillSelectClass}
            onChange={(event) => onColorModeChange(event.target.value as GraphColorMode)}
            value={colorMode}
          >
            {colorModeOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <button
          aria-pressed={hideInactivePeople}
          className={cx(pillButtonClass, hideInactivePeople ? 'border-steward-teal/60 bg-steward-teal/15 text-steward-mist' : 'border-white/12 text-steward-mist-muted hover:border-white/25 hover:text-steward-mist')}
          onClick={() => onHideInactivePeopleChange(!hideInactivePeople)}
          type="button"
        >
          Active people
        </button>
        <button
          aria-pressed={hideInactiveAssets}
          className={cx(pillButtonClass, hideInactiveAssets ? 'border-steward-teal/60 bg-steward-teal/15 text-steward-mist' : 'border-white/12 text-steward-mist-muted hover:border-white/25 hover:text-steward-mist')}
          onClick={() => onHideInactiveAssetsChange(!hideInactiveAssets)}
          type="button"
        >
          Active assets
        </button>
        <button
          aria-pressed={occupancyHubs}
          className={cx(pillButtonClass, occupancyHubs ? 'border-steward-teal/60 bg-steward-teal/15 text-steward-mist' : 'border-white/12 text-steward-mist-muted hover:border-white/25 hover:text-steward-mist')}
          onClick={() => onOccupancyHubsChange(!occupancyHubs)}
          type="button"
        >
          User-type nodes
        </button>
        <button
          aria-pressed={identityHubs}
          className={cx(pillButtonClass, identityHubs ? 'border-steward-teal/60 bg-steward-teal/15 text-steward-mist' : 'border-white/12 text-steward-mist-muted hover:border-white/25 hover:text-steward-mist')}
          onClick={() => onIdentityHubsChange(!identityHubs)}
          type="button"
        >
          Identity-type nodes
        </button>
      </div>
      <p className="text-xs leading-5 text-steward-mist-muted">
        {activeView?.description ?? 'Mixed filters. People with more than one occupancy role use a gradient. User-type nodes group instructors, students, residents, office, and lab users.'}
        {' '}
        Occupancy: {occupancyRoles.map((role) => occupancyRoleMeta[role].label).join(', ')}.
      </p>
    </div>
  )
}
