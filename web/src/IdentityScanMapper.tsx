import { assignCapturedValue, identityFieldLabels, type CapturedIdentityValue, type IdentityAssignment, type IdentityField } from './scanIdentity'
import { cx, inputClass, labelClass, StatusBadge, subpanelClass } from './ui'

// Requirements: REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001. Features: inventory.identifiers, inventory.models.

export type IdentityCatalogMatch = {
  field: IdentityField
  value: string
  recordId: string
  label: string
  kind: 'serialNumber' | 'assetTag' | 'modelNumber'
}

const assignmentOptions: { value: IdentityAssignment; label: string }[] = [
  { value: 'serialNumber', label: identityFieldLabels.serialNumber },
  { value: 'assetTag', label: identityFieldLabels.assetTag },
  { value: 'modelNumber', label: identityFieldLabels.modelNumber },
  { value: 'ignore', label: 'Ignore this value' },
]

export function matchForValue(matches: readonly IdentityCatalogMatch[], value: string, assigned: IdentityAssignment) {
  if (assigned === 'ignore') return matches.find((item) => item.value.toLowerCase() === value.toLowerCase())
  return matches.find((item) => item.field === assigned && item.value.toLowerCase() === value.toLowerCase())
    ?? matches.find((item) => item.value.toLowerCase() === value.toLowerCase())
}

export default function IdentityScanMapper({
  matches,
  onChange,
  values,
}: {
  matches: readonly IdentityCatalogMatch[]
  onChange: (values: CapturedIdentityValue[]) => void
  values: CapturedIdentityValue[]
}) {
  return (
    <fieldset className={cx(subpanelClass, 'mt-4 border-steward-teal/25 bg-steward-teal/[0.04] p-4')}>
      <legend className="px-1 text-sm font-semibold text-steward-mist">Map scanned values</legend>
      <p className="mt-1 text-sm leading-6 text-steward-mist-muted">Several barcodes were captured. Choose where each value belongs. Existing serials, asset tags, and catalog model numbers are highlighted.</p>
      <ol className="mt-4 grid gap-2">
        {values.map((item, index) => {
          const match = matchForValue(matches, item.value, item.assigned)
          return (
            <li className="rounded-md border border-white/[0.08] bg-steward-ink-950/80 p-3" key={item.id}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-[11px] font-medium uppercase tracking-[0.12em] text-steward-slate">Capture {index + 1}</p>
                  <p className="mt-1 break-all font-mono text-sm font-semibold text-steward-mist">{item.value}</p>
                  <p className="mt-1 text-xs text-steward-mist-muted">Suggested {identityFieldLabels[item.suggested].toLowerCase()}</p>
                </div>
                {match && (
                  <StatusBadge tone="success">
                    Matches existing {identityFieldLabels[match.kind].toLowerCase()} on {match.label}
                  </StatusBadge>
                )}
              </div>
              <label className={`${labelClass} mt-3`}>Map to
                <select
                  className={inputClass}
                  onChange={(event) => onChange(assignCapturedValue(values, item.id, event.target.value as IdentityAssignment))}
                  value={item.assigned}
                >
                  {assignmentOptions.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
            </li>
          )
        })}
      </ol>
    </fieldset>
  )
}
