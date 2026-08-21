import { expect, test } from 'vitest'
import { newWorkspaceRecordFocus, openRecordLabel, recordIDFromNode, workspaceRecordHref, workspaceRecordTarget } from './graphRecord'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

test('maps graph nodes to the product area that edits them', () => {
  expect(recordIDFromNode({ id: 'asset:lab-server', kind: 'asset' })).toBe('lab-server')
  expect(workspaceRecordTarget('asset')).toEqual({ area: 'atlas', name: 'Atlas' })
  expect(workspaceRecordHref('person')).toBe('#workspace-people')
  expect(openRecordLabel('asset', true)).toBe('Edit this asset in Atlas')
  expect(openRecordLabel('purchase_order', false)).toBe('Open in Ledger')
  expect(newWorkspaceRecordFocus({ id: 'asset:lab-server', kind: 'asset' }, 7)).toEqual({
    area: 'atlas', kind: 'asset', recordId: 'lab-server', nonce: 7,
  })
  expect(workspaceRecordTarget('organization')).toBeNull()
})
