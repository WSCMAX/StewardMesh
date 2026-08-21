import SectionNav from './SectionNav'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace, identity.directory.

export type PeopleSection = 'directory' | 'locations' | 'references' | 'workflows' | 'imports'

const tabs = [
  { id: 'directory' as const, label: 'Directory', description: 'Edit identities in a spreadsheet' },
  { id: 'locations' as const, label: 'Locations', description: 'Sites, buildings, rooms, and departments as sheets' },
  { id: 'references' as const, label: 'Location references', description: 'Typed occupancy links and the catalog of reference types' },
  { id: 'workflows' as const, label: 'Workflows & assignments', description: 'Guided creation and asset assignments' },
  { id: 'imports' as const, label: 'Directory imports', description: 'Preview and apply external directory sources' },
]

type PeopleSectionNavProps = {
  active: PeopleSection
  onChange: (section: PeopleSection) => void
}

export default function PeopleSectionNav({ active, onChange }: PeopleSectionNavProps) {
  return <SectionNav active={active} ariaLabel="People section navigation" idPrefix="people" onChange={onChange} tabs={tabs} />
}
