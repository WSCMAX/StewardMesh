import { describe, expect, test } from 'vitest'
import { encodeLookupText } from './grid/columns'
import {
  labelCellDisplay,
  labelCellText,
  labelColumnKey,
  labelDefinitionIdFromColumnKey,
  type LabelAssignment,
  type LabelDefinition,
} from './labelsGrid'

const deploymentGroup: LabelDefinition = {
  id: 'deployment-group',
  name: 'Deployment group',
  valueKind: 'multiselect',
  applicableRecordTypes: ['atlas.asset'],
  options: ['Lab A', 'Office refresh'],
  status: 'active',
  revision: 1,
}

const assignment: LabelAssignment = {
  definitionId: 'deployment-group',
  recordType: 'atlas.asset',
  recordId: 'asset-1',
  values: ['Lab A', 'Office refresh'],
  revision: 2,
}

describe('label grid helpers', () => {
  test('maps label column keys back to definition ids', () => {
    expect(labelColumnKey('deployment-group')).toBe('label:deployment-group')
    expect(labelDefinitionIdFromColumnKey('label:deployment-group')).toBe('deployment-group')
  })

  test('renders multiselect assignment text for the grid', () => {
    expect(labelCellText(deploymentGroup, assignment)).toBe(encodeLookupText([
      { id: 'Lab A', primary: false },
      { id: 'Office refresh', primary: false },
    ]))
    expect(labelCellDisplay(deploymentGroup, assignment)).toBe('Lab A, Office refresh')
  })

  test('renders flag assignments as yes/no cell text', () => {
    const flagDefinition: LabelDefinition = { ...deploymentGroup, valueKind: 'flag', options: undefined }
    expect(labelCellText(flagDefinition, assignment)).toBe('yes')
    expect(labelCellText(flagDefinition, undefined)).toBe('')
  })
})
