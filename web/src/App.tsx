import { useEffect, useState } from 'react'

type Module = readonly [name: string, description: string]

type Asset = {
  id: string
  name: string
  kind: string
  status: string
}

type AssetResponse = {
  items?: Asset[]
}

const modules: Module[] = [
  ['Atlas', 'Asset inventory'],
  ['Horizon', 'Lifecycle planning'],
  ['Ledger', 'Procurement and budgets'],
  ['Threads', 'Tags and strategic goals'],
  ['People', 'Users and departments'],
  ['Guide', 'Help and walkthroughs'],
]

export default function App() {
  const [health, setHealth] = useState('Checking service…')
  const [assets, setAssets] = useState<Asset[]>([])

  useEffect(() => {
    fetch('/healthz')
      .then((response) => response.json())
      .then(() => setHealth('Service connected'))
      .catch(() => setHealth('Start the Go service to connect'))

    fetch('/api/v1/assets')
      .then((response) => response.json() as Promise<AssetResponse>)
      .then((body) => setAssets(body.items ?? []))
      .catch(() => setAssets([]))
  }, [])

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100">
      <header className="border-b border-slate-800 bg-slate-900/80">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-5">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.25em] text-cyan-300">Binary Cornfield presents</p>
            <h1 className="mt-1 text-2xl font-semibold tracking-tight">StewardMesh</h1>
          </div>
          <p className="rounded-full border border-emerald-500/30 px-3 py-1 text-sm text-emerald-300" aria-live="polite">{health}</p>
        </div>
      </header>

      <main className="mx-auto max-w-7xl space-y-10 px-6 py-10">
        <section aria-labelledby="welcome-heading" className="max-w-3xl">
          <p className="text-sm font-medium text-cyan-300">Connect what you steward. Plan what comes next.</p>
          <h2 id="welcome-heading" className="mt-3 text-4xl font-semibold tracking-tight sm:text-5xl">A clear view of what your organization owns, funds, and operates.</h2>
          <p className="mt-5 text-lg leading-8 text-slate-300">StewardMesh brings inventory, lifecycle planning, procurement, goals, and ownership into one accessible workspace.</p>
        </section>

        <section aria-labelledby="modules-heading">
          <div className="flex items-end justify-between gap-4">
            <div><h2 id="modules-heading" className="text-xl font-semibold">StewardMesh modules</h2><p className="mt-1 text-sm text-slate-400">Every module has a plain-language descriptor and accessible help.</p></div>
            <a className="text-sm text-cyan-300 underline underline-offset-4 hover:text-cyan-200" href="https://github.com/maxlemke/OpenInventory/issues">Report an issue</a>
          </div>
          <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {modules.map(([name, description]) => <article key={name} className="rounded-2xl border border-slate-800 bg-slate-900 p-5 shadow-xl shadow-black/10"><p className="text-lg font-semibold">{name}</p><p className="mt-2 text-sm text-slate-400">{description}</p></article>)}
          </div>
        </section>

        <section aria-labelledby="assets-heading" className="rounded-2xl border border-slate-800 bg-slate-900 p-6">
          <h2 id="assets-heading" className="text-xl font-semibold">Atlas — Asset inventory</h2>
          <p className="mt-1 text-sm text-slate-400">The first vertical slice is ready for asset records and repository integration.</p>
          {assets.length === 0 ? <p className="mt-6 rounded-xl border border-dashed border-slate-700 p-5 text-sm text-slate-400">No assets yet. Add your first server or device through the API to begin.</p> : <ul className="mt-6 divide-y divide-slate-800">{assets.map((asset) => <li className="flex justify-between py-4" key={asset.id}><span>{asset.name}</span><span className="text-sm text-slate-400">{asset.kind} · {asset.status}</span></li>)}</ul>}
        </section>
      </main>
    </div>
  )
}
