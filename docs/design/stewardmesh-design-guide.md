# StewardMesh Design Guide

StewardMesh is a calm, capable workspace for understanding relationships and planning what comes next. The visual language is a dark, structured canvas with precise graph-like accents. It should feel trustworthy and modern, never neon, playful, or generic SaaS.

## Project assets

- Primary vector mark: [`web/public/brand/stewardmesh-s-mark.svg`](../../web/public/brand/stewardmesh-s-mark.svg)
- PNG export: [`web/public/brand/stewardmesh-logo.png`](../../web/public/brand/stewardmesh-logo.png)

Use the SVG in the interface and as the favicon so the mark remains crisp at any size. Keep the PNG as the portable raster export for integrations that do not accept SVG.

## Brand palette

The three brand colors come directly from the S-mesh mark. They represent a connected flow from **connection** to **coordination** to **structure**.

| Token | Hex | Role | Use it for |
| --- | --- | --- | --- |
| `mesh-green` | `#46CF51` | Connection | Fresh starts, healthy status, top-level mesh accents |
| `mesh-teal` | `#16BFA7` | Stewardship | Primary actions, active states, links, selected controls |
| `mesh-blue` | `#1768EF` | Planning | Structure, information, planning views, secondary emphasis |
| `ink-950` | `#061827` | Foundation | Primary dark page background and high-emphasis dark controls |
| `ink-900` | `#0B2238` | Surface | Cards, navigation, elevated panels on dark pages |
| `ink-800` | `#12314C` | Border/surface | Subtle separators, table headers, disabled dark controls |
| `mist-50` | `#F7FAFC` | Light canvas | Light pages and high-emphasis text on dark surfaces |
| `mist-200` | `#DCE6EF` | Supporting text | Subtitles, quiet labels, secondary icons on dark surfaces |
| `slate-500` | `#6E8294` | Muted text | Metadata, timestamps, empty-state details |

### Semantic colors

Use semantic colors for meaning, not brand colors alone. This prevents a green mesh accent from being mistaken for a successful save.

| State | Background / icon | Text on light surfaces | Notes |
| --- | --- | --- | --- |
| Success | `#168C4B` | `#106B3A` | Completion, connected, synced |
| Warning | `#C57900` | `#8A5300` | Needs attention, not blocked |
| Danger | `#CC3D4A` | `#A52C36` | Destructive actions and errors |
| Info | `#1768EF` | `#1253C4` | Planning, guidance, neutral system updates |

## Color rules

- Use `mesh-teal` for the one most important action in a view. Do not use both a green and teal primary button together.
- Use green and blue as supporting signals, chart series, status accents, or low-frequency highlights.
- Keep large canvas areas neutral (`mist-50` in light mode, `ink-950` in dark mode). The logo colors work best as intentional points of connection.
- On dark surfaces, use `mist-50` for headings and `mist-200` for readable body text. Do not use saturated mesh colors for paragraph copy.
- On light surfaces, reserve the bright brand colors for fills, icons, borders, and badges. Use the darker semantic shades for text.

## Typography

Use **Inter** as the interface typeface. It is already the application default and suits dense organization and planning views.

| Element | Weight | Size / line height | Tailwind |
| --- | --- | --- | --- |
| Product or page title | 700 | 30–36px / 1.15 | `text-3xl font-bold tracking-tight` |
| Section title | 650–700 | 20–24px / 1.25 | `text-xl font-bold tracking-tight` |
| Card title | 600 | 16–18px / 1.3 | `text-base font-semibold` |
| Body | 400 | 14–16px / 1.55 | `text-sm leading-6` or `text-base leading-7` |
| Label / control | 600 | 12–14px / 1.2 | `text-xs font-semibold` |
| Metadata | 400–500 | 12–13px / 1.4 | `text-xs text-steward-slate` |

Use sentence case. Avoid all-caps labels except short, established status tags. Keep headings direct: “Directory health”, “Connected systems”, “Plan changes”.

## Layout and components

- **Spacing:** use a 4px base unit. Common rhythm: `gap-2`, `gap-4`, `gap-6`, `p-4`, `p-6`, `p-8`.
- **Corners:** `rounded-xl` for cards and modals, `rounded-lg` for controls, `rounded-full` only for tags, avatars, and compact status chips.
- **Borders:** thin and quiet: `border-steward-ink-800/70` on dark, `border-slate-200` on light.
- **Shadows:** restrained. Prefer borders and surface contrast over dramatic floating cards.
- **Graphs/relationships:** use 2px rounded connectors; use the green → teal → blue sequence to show ordered flow. Always pair color with labels, icons, or shape when the relationship has meaning.

## Tailwind v4 tokens

Add this immediately after `@import "tailwindcss";` in the global stylesheet.

```css
@theme {
  --font-sans: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;

  --color-steward-green: #46cf51;
  --color-steward-teal: #16bfa7;
  --color-steward-blue: #1768ef;

  --color-steward-ink-950: #061827;
  --color-steward-ink-900: #0b2238;
  --color-steward-ink-800: #12314c;
  --color-steward-mist: #f7fafc;
  --color-steward-mist-muted: #dce6ef;
  --color-steward-slate: #6e8294;

  --color-steward-success: #168c4b;
  --color-steward-warning: #c57900;
  --color-steward-danger: #cc3d4a;
}
```

### Copy-ready component styling

```tsx
// Page shell
<main className="min-h-screen bg-steward-ink-950 text-steward-mist">
  <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">...</div>
</main>

// Primary action
<button className="rounded-lg bg-steward-teal px-4 py-2 text-sm font-semibold text-steward-ink-950 shadow-sm transition hover:bg-[#29cfb9] focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-steward-mist">
  Create directory
</button>

// Secondary action
<button className="rounded-lg border border-steward-ink-800 bg-steward-ink-900 px-4 py-2 text-sm font-semibold text-steward-mist transition hover:border-steward-blue hover:bg-steward-ink-800">
  View plan
</button>

// Dark-surface card
<section className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6 shadow-sm">
  <h2 className="text-lg font-semibold text-steward-mist">Connected systems</h2>
  <p className="mt-2 text-sm leading-6 text-steward-mist-muted">...</p>
</section>

// Status chip: use text and an icon/name, not color alone
<span className="inline-flex items-center gap-1.5 rounded-full bg-steward-success/15 px-2.5 py-1 text-xs font-semibold text-[#67dd99]">
  <span className="size-1.5 rounded-full bg-steward-green" /> Connected
</span>
```

## Accessibility baseline

- Use `mist-50` on `ink-950` / `ink-900` for ordinary text and controls.
- Retain visible keyboard focus. The existing cyan focus ring is appropriate; align it to `mesh-teal` only if it remains clear against both light and dark surfaces.
- Do not convey status only with green, teal, or blue. Include words, an icon, or a distinct shape.
- Keep interactive targets at least 44×44px where practical.
- Respect reduced-motion preferences. Relationship graphs may animate gently, but never need motion to be understood.

## Context packet for collaborators or coding agents

> StewardMesh is a dark-first relationship and planning product. Use Inter. The brand palette is mesh green `#46CF51`, stewardship teal `#16BFA7`, planning blue `#1768EF`, on deep navy ink `#061827`. Treat teal as the single primary-action color; green and blue are supporting accents. UI should feel precise, calm, connected, and trustworthy. Use neutral dark surfaces, generous spacing, subtle borders, rounded-xl cards, rounded-lg controls, and restrained shadows. Avoid rainbow gradients, neon glow, oversized pills, and generic SaaS illustrations. Use semantic success/warning/danger colors independently from the brand accents. Build with Tailwind v4 CSS theme tokens named `steward-*`.
