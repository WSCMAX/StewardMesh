import SectionNav from './SectionNav'

// Requirements: REQ-ATLAS-001, REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001. Feature: experience.workspace.

export type AtlasSection = 'assets' | 'scan' | 'labels' | 'models'

const tabs = [
  { id: 'assets' as const, label: 'Assets', description: 'Search, inspect, and maintain organization-owned records' },
  { id: 'scan' as const, label: 'Scan', description: 'Find an asset or associate a barcode or QR code' },
  { id: 'labels' as const, label: 'Labels', description: 'Preview and print Atlas Codes for the visible asset set', write: true },
  { id: 'models' as const, label: 'Models', description: 'Shared manufacturer and model defaults for repeated assets' },
]

type AtlasSectionNavProps = {
  active: AtlasSection
  canWrite: boolean
  onChange: (section: AtlasSection) => void
}

export default function AtlasSectionNav({ active, canWrite, onChange }: AtlasSectionNavProps) {
  return <SectionNav active={active} ariaLabel="Atlas section navigation" canWrite={canWrite} idPrefix="atlas" onChange={onChange} tabs={tabs} />
}
