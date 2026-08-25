import { expect, test } from 'vitest'
import { aggregateNamedBuckets, bucketValue, forecastViewGroupBy, otherBucket, piesByDepartment, stackByYear, type ForecastViewItem } from './horizonForecastViews'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

const items: ForecastViewItem[] = [
  { planId: 'p1', assetId: 'a1', assetName: 'Lab laptop', scenario: 'baseline', fiscalYear: 2027, replacementCostMinor: 1000, currency: 'USD', department: 'dept-it', kind: 'laptop', manufacturer: 'Dell', building: 'b-1' },
  { planId: 'p2', assetId: 'a2', assetName: 'Core server', scenario: 'baseline', fiscalYear: 2027, replacementCostMinor: 4000, currency: 'USD', department: 'dept-it', kind: 'server', manufacturer: 'Dell', building: '' },
  { planId: 'p3', assetId: 'a3', assetName: 'Office laptop', scenario: 'baseline', fiscalYear: 2028, replacementCostMinor: 2000, currency: 'USD', department: '', kind: 'laptop', manufacturer: '', building: 'b-1' },
]

test('missing department, manufacturer, and building become Other', () => {
  expect(bucketValue('')).toBe(otherBucket)
  expect(bucketValue('  ')).toBe(otherBucket)
  expect(aggregateNamedBuckets(items, (item) => item.building, (key) => key).map((bucket) => bucket.key)).toEqual([otherBucket, 'b-1'])
  expect(aggregateNamedBuckets(items, (item) => item.manufacturer, (key) => key).map((bucket) => bucket.key)).toEqual(['Dell', otherBucket])
})

test('department plus type view keeps year grouping for the API', () => {
  expect(forecastViewGroupBy('department_type')).toBe('fiscal_year')
  expect(forecastViewGroupBy('manufacturer')).toBe('manufacturer')
})

test('stacks replacement cost by year and type', () => {
  const stacked = stackByYear(items, (item) => item.kind)
  expect(stacked.series).toEqual(['laptop', 'server'])
  expect(stacked.years[0]).toMatchObject({ year: 2027, series: { laptop: 1000, server: 4000 }, totalMinor: 5000 })
  expect(stacked.years[1]).toMatchObject({ year: 2028, series: { laptop: 2000 }, totalMinor: 2000 })
})

test('builds a pie per department including Other', () => {
  const pies = piesByDepartment(items, (key) => key === 'dept-it' ? 'Information Technology' : key, (key) => key)
  expect(pies.map((pie) => pie.label)).toEqual(['Information Technology', otherBucket])
  expect(pies[0].slices.map((slice) => slice.key)).toEqual(['server', 'laptop'])
  expect(pies[1].slices).toEqual([expect.objectContaining({ key: 'laptop', replacementCostMinor: 2000 })])
})
