// Requirements: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

// Atlas Codes associations stay Code 128 and QR. Camera capture for serials,
// asset tags, and manufacturer model numbers also accepts the 1D/2D formats
// printed on vendor and property labels.

export type CapturedSymbology = 'code128' | 'qr' | 'code39' | 'other'

export const identityBarcodeFormats = [
  'code_128', 'code_39', 'code_93', 'codabar', 'itf',
  'ean_13', 'ean_8', 'upc_a', 'upc_e',
  'qr_code', 'data_matrix', 'pdf417',
] as const

export function symbologyFromFormat(format?: string): CapturedSymbology | null {
  if (format === 'qr_code') return 'qr'
  if (format === 'code_128') return 'code128'
  if (format === 'code_39') return 'code39'
  if (typeof format === 'string' && format.length > 0) return 'other'
  return null
}

export function isAtlasCodeSymbology(symbology: string): symbology is 'code128' | 'qr' {
  return symbology === 'code128' || symbology === 'qr'
}
