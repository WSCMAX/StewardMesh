import { describe, expect, test } from 'vitest'
import { fiscalMonthInYear, fiscalYearForDate, fiscalYearStartISO, groupByDeployment, inferDeploymentName } from './horizonPlanning'
import type { Asset } from './AtlasInventory'

describe('horizonPlanning', () => {
  test('computes fiscal year and month for calendar and shifted fiscal starts', () => {
    expect(fiscalYearForDate('2027-06-30T00:00:00Z', 1)).toBe(2027)
    expect(fiscalMonthInYear('2027-06-30T00:00:00Z', 1)).toBe(6)
    expect(fiscalYearForDate('2026-10-01T00:00:00Z', 10)).toBe(2027)
    expect(fiscalMonthInYear('2026-10-01T00:00:00Z', 10)).toBe(1)
    expect(fiscalMonthInYear('2027-03-15T00:00:00Z', 10)).toBe(6)
    expect(fiscalYearStartISO(2027, 10)).toBe('2026-10-01T00:00:00.000Z')
  })

  test('groups assets by shared room into deployment labels', () => {
    const assets = new Map<string, Asset>([
      ['a1', { id: 'a1', organizationId: 'org', name: 'Architecture Design Lab Station 001', kind: 'desktop', status: 'active', roomId: 'room-1', revision: 1, createdAt: '', updatedAt: '' }],
      ['a2', { id: 'a2', organizationId: 'org', name: 'Architecture Design Lab Station 002', kind: 'desktop', status: 'active', roomId: 'room-1', revision: 1, createdAt: '', updatedAt: '' }],
    ])
    const groups = groupByDeployment([
      { assetId: 'a1', assetName: 'Architecture Design Lab Station 001', replacementCostMinor: 100 },
      { assetId: 'a2', assetName: 'Architecture Design Lab Station 002', replacementCostMinor: 200 },
    ], (assetId) => assets.get(assetId))
    expect(groups).toHaveLength(1)
    expect(groups[0].label).toBe('Architecture Design Lab')
    expect(inferDeploymentName('Office Workstation 004')).toBe('Office')
  })
})
