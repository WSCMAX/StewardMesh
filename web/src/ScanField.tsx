import { useId, useState } from 'react'
import BarcodeCameraCapture from './BarcodeCameraCapture'
import { capturedValuesFromScan, mergeCapturedValues, preferredModelIdentifier, type CapturedIdentityValue } from './scanIdentity'
import { cx, inputClass, labelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001. Features: inventory.identifiers, inventory.models.

type ScanFieldProps = {
  label: string
  value: string
  onChange: (value: string) => void
  name?: string
  help?: string
  placeholder?: string
  maxLength?: number
  required?: boolean
  parseValue?: (raw: string) => string
}

export default function ScanField({
  help, label, maxLength, name, onChange, parseValue, placeholder, required, value,
}: ScanFieldProps) {
  const [cameraOpen, setCameraOpen] = useState(false)
  const [choices, setChoices] = useState<CapturedIdentityValue[]>([])
  const generatedId = useId()
  const inputId = name ? `${name}-field` : generatedId
  const helpID = help ? `${inputId}-help` : undefined

  function adopt(raw: string) {
    onChange((parseValue ? parseValue(raw) : raw).trim())
    setCameraOpen(false)
    setChoices([])
  }

  return (
    <div>
      <label className={labelClass} htmlFor={inputId}>{label}</label>
      {help && <p className="mt-1 text-xs font-normal leading-5 text-steward-mist-muted" id={helpID}>{help}</p>}
      <div className="mt-1.5 flex items-stretch gap-2">
        <input
          aria-describedby={helpID}
          autoCapitalize="none"
          autoComplete="off"
          autoCorrect="off"
          className={`${inputClass} mt-0 font-mono`}
          id={inputId}
          maxLength={maxLength}
          name={name}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          required={required}
          spellCheck={false}
          value={value}
        />
        <button aria-label={`Scan ${label}`} className={cx(secondaryButtonClass, 'shrink-0 px-3')} onClick={() => { setCameraOpen((open) => !open); setChoices([]) }} type="button">
          {cameraOpen ? 'Hide camera' : 'Scan'}
        </button>
      </div>
      {cameraOpen && (
        <div className={cx(subpanelClass, 'mt-2 p-3')}>
          <p className="text-xs text-steward-mist-muted">Point at the printed {label.toLowerCase()}. Frames stay in this browser.</p>
          <div className="mt-2">
            <BarcodeCameraCapture
              autoStart
              onCapture={(code) => adopt(code.value)}
              onCaptures={(codes) => {
                const captured = mergeCapturedValues([], codes.flatMap((code) => capturedValuesFromScan(code.value)))
                if (captured.length > 1) {
                  setChoices(captured)
                  return
                }
                adopt(preferredModelIdentifier(codes.map((code) => code.value)) || codes[0]?.value || '')
              }}
            />
          </div>
        </div>
      )}
      {choices.length > 1 && (
        <fieldset className={cx(subpanelClass, 'mt-3 border-steward-teal/25 bg-steward-teal/[0.04] p-3')}>
          <legend className="px-1 text-sm font-semibold">Choose the {label.toLowerCase()}</legend>
          <p className="mt-1 text-sm text-steward-mist-muted">Several barcodes were captured. Select the value that belongs in this field.</p>
          <div className="mt-3 flex flex-col gap-2">
            {choices.map((item) => (
              <button
                className={cx(secondaryButtonClass, 'justify-start font-mono')}
                key={item.id}
                onClick={() => adopt(item.value)}
                type="button"
              >
                Use {item.value}
              </button>
            ))}
          </div>
        </fieldset>
      )}
    </div>
  )
}
