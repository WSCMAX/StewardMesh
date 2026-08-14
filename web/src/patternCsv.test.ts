import { expect, test } from 'vitest'
import { maximumPatternCsvBytes, parsePatternCsv, serializePatternCsv } from './patternCsv'

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

const template = {
  id: 'typed-row',
  version: 3,
  fields: [
    { key: 'title', csvHeader: 'Title', type: 'text' as const },
    { key: 'quantity', csvHeader: 'Quantity', type: 'number' as const },
    { key: 'budgetMinor', csvHeader: 'Budget minor', type: 'money' as const },
    { key: 'dueOn', csvHeader: 'Due on', type: 'date' as const },
    { key: 'state', csvHeader: 'State', type: 'enum' as const },
    { key: 'evidence', csvHeader: 'Evidence', type: 'attachment' as const },
    { key: 'owner', csvHeader: 'Owner', type: 'reference' as const },
  ],
}

test('round trips one typed CSV row by exact versioned headers', () => {
  const values = {
    title: 'Portable, "safe" row', quantity: 2.5, budgetMinor: 1250,
    dueOn: '2026-08-13', state: 'ready', evidence: 'blob-1', owner: 'person-1',
  }
  const encoded = serializePatternCsv(template, values)
  expect(encoded).toContain('"Portable, ""safe"" row"')
  expect(parsePatternCsv(template, encoded)).toEqual(values)
})

test('rejects header drift, additional rows, formulas, unsafe money, and oversized CSV', () => {
  expect(() => parsePatternCsv(template, 'Wrong,Quantity,Budget minor,Due on,State,Evidence,Owner\nA,1,2,2026-08-13,ready,blob-1,person-1\n')).toThrow(/exactly match/)
  expect(() => parsePatternCsv(template, 'Title,Quantity,Budget minor,Due on,State,Evidence,Owner\nA,1,2,2026-08-13,ready,blob-1,person-1\nB,1,2,2026-08-13,ready,blob-2,person-2\n')).toThrow(/exactly one/)
  expect(() => serializePatternCsv(template, { title: '=WEBSERVICE("example")' })).toThrow(/formula/)
  expect(() => parsePatternCsv(template, 'Title,Quantity,Budget minor,Due on,State,Evidence,Owner\n@SUM(A1),1,2,2026-08-13,ready,blob-1,person-1\n')).toThrow(/formula/)
  expect(() => serializePatternCsv(template, { budgetMinor: 1.5 })).toThrow(/integer/)
  for (const unsafe of ['9007199254740990.5', '1.0000000000000001', '0.99999999999999999']) {
    expect(() => parsePatternCsv(template, `Title,Quantity,Budget minor,Due on,State,Evidence,Owner\nA,1,${unsafe},2026-08-13,ready,blob-1,person-1\n`)).toThrow(/integer/)
  }
  expect(() => parsePatternCsv(template, 'x'.repeat(maximumPatternCsvBytes + 1))).toThrow(/128 KiB/)
})

test('rejects malformed quoting', () => {
  const header = 'Title,Quantity,Budget minor,Due on,State,Evidence,Owner\n'
  expect(() => parsePatternCsv(template, `${header}"unterminated,1,2,2026-08-13,ready,blob-1,person-1`)).toThrow(/unterminated/)
  expect(() => parsePatternCsv(template, `${header}"closed"junk,1,2,2026-08-13,ready,blob-1,person-1\n`)).toThrow(/after a closing quote/)
  expect(() => parsePatternCsv(template, `${header}ab"cd,1,2,2026-08-13,ready,blob-1,person-1\n`)).toThrow(/quote inside an unquoted value/)
})
