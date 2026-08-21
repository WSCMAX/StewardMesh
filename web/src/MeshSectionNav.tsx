import SectionNav from './SectionNav'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships, experience.workspace.

export type MeshSection = 'graph' | 'data'

const tabs = [
  { id: 'graph' as const, label: 'Graph', description: 'Explore connected records across products' },
  { id: 'data' as const, label: 'Data', description: 'Sort, filter, and query the same records in a table' },
]

type MeshSectionNavProps = {
  active: MeshSection
  onChange: (section: MeshSection) => void
}

export default function MeshSectionNav({ active, onChange }: MeshSectionNavProps) {
  return <SectionNav active={active} ariaLabel="Mesh section navigation" idPrefix="mesh" onChange={onChange} tabs={tabs} />
}
