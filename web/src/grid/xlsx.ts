import { formulaSafeText, type ExportSheet } from './export'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001. Feature: experience.grid.

// Loads a Go WASM module that turns a resolved sheet into an .xlsx document.
// The module is fetched once and then reused; tests inject a stub so they do
// not need a compiled wasm binary.

type GoRuntime = { importObject: WebAssembly.Imports; run: (instance: WebAssembly.Instance) => Promise<void> }

type GoConstructor = new () => GoRuntime

declare global {
  interface Window {
    Go?: GoConstructor
    stewardmeshXlsxExport?: (input: string) => Uint8Array | { error: string }
  }
}

let loading: Promise<void> | null = null

async function ensureRuntime() {
  if (typeof window.stewardmeshXlsxExport === 'function') return
  if (!loading) {
    loading = (async () => {
      if (typeof window.Go !== 'function') {
        await loadScript('/wasm_exec.js')
      }
      const Go = window.Go
      if (typeof Go !== 'function') throw new Error('Go WASM runtime is not available.')
      const go = new Go()
      const source = await WebAssembly.instantiateStreaming(fetch('/xlsx.wasm'), go.importObject)
      void go.run(source.instance)
      await waitFor(() => typeof window.stewardmeshXlsxExport === 'function')
    })()
  }
  await loading
}

function loadScript(src: string) {
  return new Promise<void>((resolve, reject) => {
    const existing = document.querySelector(`script[src="${src}"]`)
    if (existing) {
      resolve()
      return
    }
    const script = document.createElement('script')
    script.src = src
    script.onload = () => resolve()
    script.onerror = () => reject(new Error(`Could not load ${src}.`))
    document.head.append(script)
  })
}

function waitFor(ready: () => boolean) {
  return new Promise<void>((resolve, reject) => {
    const started = Date.now()
    const tick = () => {
      if (ready()) {
        resolve()
        return
      }
      if (Date.now() - started > 8000) {
        reject(new Error('The spreadsheet WASM module did not start.'))
        return
      }
      window.setTimeout(tick, 20)
    }
    tick()
  })
}

export async function xlsxFromSheet(sheet: ExportSheet): Promise<Uint8Array> {
  await ensureRuntime()
  const safeSheet = {
    ...sheet,
    columns: sheet.columns.map((column) => ({ ...column, header: formulaSafeText(column.header) })),
    rows: sheet.rows.map((row) => row.map(formulaSafeText)),
  }
  const exported = window.stewardmeshXlsxExport?.(JSON.stringify(safeSheet))
  if (!exported) throw new Error('The spreadsheet WASM module is not available.')
  if (!(exported instanceof Uint8Array)) throw new Error(exported.error || 'The spreadsheet could not be built.')
  return exported
}
