import SectionNav from './SectionNav'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning, experience.workspace.

export type HorizonSection = 'queue' | 'forecast' | 'plans' | 'defaults'

const tabs = [
  { id: 'queue' as const, label: 'Due now', description: 'Replace, retire, or keep running what needs attention' },
  { id: 'forecast' as const, label: 'Forecast', description: 'See replacement spend by year, site, or department' },
  { id: 'plans' as const, label: 'Plans', description: 'Name replacement plans, see how many assets they cover, then edit per-asset assumptions' },
  { id: 'defaults' as const, label: 'Defaults', description: 'Set useful-life policy by asset type and review model lineage' },
]

type HorizonSectionNavProps = {
  active: HorizonSection
  onChange: (section: HorizonSection) => void
}

export default function HorizonSectionNav({ active, onChange }: HorizonSectionNavProps) {
  return <SectionNav active={active} ariaLabel="Horizon section navigation" idPrefix="horizon" onChange={onChange} tabs={tabs} />
}
