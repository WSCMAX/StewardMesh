import { expect, test } from 'vitest'
import {
  applyIdentityScan,
  assignCapturedValue,
  assignIdentityField,
  capturedValuesFromScan,
  classifyIdentityValue,
  emptyDeviceIdentity,
  identityFromCaptured,
  mergeCapturedValues,
  modelIdentifierFromScan,
  modelNumberDecisionNeeded,
  parseIdentityPayload,
  preferredModelIdentifier,
} from './scanIdentity'

// Requirements: REQ-ATLAS-001, REQ-ATLAS-CODES-001. Features: inventory.assets, inventory.identifiers.

test('classifies a manufacturer laptop label into serial, model, and internal asset tag', () => {
  expect(classifyIdentityValue('43188')).toBe('assetTag')
  expect(classifyIdentityValue('PF41ER5H')).toBe('serialNumber')
  expect(classifyIdentityValue('20W5S51T00')).toBe('modelNumber')

  const captured = applyIdentityScan(
    applyIdentityScan(applyIdentityScan(emptyDeviceIdentity(), '20W5S51T00'), 'PF41ER5H'),
    '43188',
  )
  expect(captured).toEqual({ serialNumber: 'PF41ER5H', assetTag: '43188', modelNumber: '20W5S51T00' })
})

test('parses factory 1S payloads and labeled manufacturer stickers', () => {
  expect(parseIdentityPayload('1S20W5S51T00PF41ER5H')).toEqual({
    modelNumber: '20W5S51T00',
    serialNumber: 'PF41ER5H',
  })
  expect(modelIdentifierFromScan('1S20W5S51T00PF41ER5H')).toBe('20W5S51T00')
  expect(modelIdentifierFromScan('20W5S51T00')).toBe('20W5S51T00')
  expect(parseIdentityPayload('MTM: 20W5S51T00 S/N: PF41ER5H')).toEqual({
    modelNumber: '20W5S51T00',
    serialNumber: 'PF41ER5H',
  })
})

test('extracts serial and model from a support URL and lets the operator reassign a field', () => {
  expect(parseIdentityPayload('https://support.example.test/products?mtm=20W5S51T00&sn=PF41ER5H')).toEqual({
    modelNumber: '20W5S51T00',
    serialNumber: 'PF41ER5H',
  })
  expect(assignIdentityField({ serialNumber: 'PF41ER5H', assetTag: '', modelNumber: '20W5S51T00' }, 'assetTag', 'PF41ER5H')).toEqual({
    serialNumber: '',
    assetTag: 'PF41ER5H',
    modelNumber: '20W5S51T00',
  })
})

test('lets the operator map several captured barcodes onto serial, tag, and model', () => {
  const captured = mergeCapturedValues(
    capturedValuesFromScan('20W5S51T00'),
    mergeCapturedValues(capturedValuesFromScan('PF41ER5H'), capturedValuesFromScan('43188')),
  )
  expect(identityFromCaptured(captured)).toEqual({
    serialNumber: 'PF41ER5H',
    assetTag: '43188',
    modelNumber: '20W5S51T00',
  })
  const remapped = assignCapturedValue(captured, 'serialNumber:pf41er5h', 'assetTag')
  expect(identityFromCaptured(remapped)).toEqual({
    serialNumber: '',
    assetTag: 'PF41ER5H',
    modelNumber: '20W5S51T00',
  })
  expect(preferredModelIdentifier(['1S20W5S51T00PF41ER5H', '43188'])).toBe('20W5S51T00')
  expect(modelNumberDecisionNeeded('20W5S51T00', 'T14-G3')).toBe(true)
  expect(modelNumberDecisionNeeded('20W5S51T00', '20W5S51T00')).toBe(false)
  expect(modelNumberDecisionNeeded('20W5S51T00', '')).toBe(true)
})
