import SectionNav from './SectionNav'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace, identity.directory.

export type PeopleSection = 'directory' | 'locations' | 'references' | 'workflows' | 'checkouts' | 'imports'

const tabs = [
  { id: 'directory' as const, label: 'Directory', description: 'Edit identities in a spreadsheet' },
  { id: 'locations' as const, label: 'Locations', description: 'Sites, buildings, rooms, and departments as sheets' },
  { id: 'references' as const, label: 'Location references', description: 'Typed occupancy links and the catalog of reference types' },
  { id: 'workflows' as const, label: 'Workflows & assignments', description: 'Guided creation and asset assignments' },
  { id: 'checkouts' as const, label: 'Checkouts', description: 'Reservations, group checkouts, and tagged bulk loans' },
  { id: 'imports' as const, label: 'Directory imports', description: 'Preview and apply external directory sources' },
]

const peopleSections = new Set<PeopleSection>(tabs.map((tab) => tab.id))

export function peopleSectionFromHash(hash: string): PeopleSection {
  const query = hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : ''
  const tab = new URLSearchParams(query).get('tab')
  if (tab && peopleSections.has(tab as PeopleSection)) return tab as PeopleSection
  return 'directory'
}

export function peopleHash(section: PeopleSection = 'directory') {
  return section === 'directory' ? '#workspace-people' : `#workspace-people?tab=${section}`
}

type PeopleSectionNavProps = {
  active: PeopleSection
  onChange: (section: PeopleSection) => void
}

export default function PeopleSectionNav({ active, onChange }: PeopleSectionNavProps) {
  return <SectionNav active={active} ariaLabel="People section navigation" idPrefix="people" onChange={onChange} tabs={tabs} />
}
