// Requirements: A11Y-001, DOC-001, DOC-002. Feature: experience.help.

export type GuideTopicID = 'workspace' | 'atlas' | 'horizon' | 'ledger' | 'threads' | 'vault' | 'people' | 'guard' | 'guide'
export type GuideView = 'help' | 'walkthrough' | 'accessibility' | 'report'
export type WalkthroughStatus = 'new' | 'completed' | 'skipped'

export type GuideTopic = {
  id: GuideTopicID
  name: string
  descriptor: string
  summary: string
  example: string
  docsUrl: string
  anchor?: string
  permission?: string
}

export const guideTopics: GuideTopic[] = [
  { id: 'workspace', name: 'Workspace', descriptor: 'Connected work view', summary: 'Use the module cards and page sections to move from inventory context into people, evidence, goals, and financial planning.', example: 'Start in Atlas with an asset, then use People for stewardship, Vault for evidence, Threads for goals, and Ledger for financial records.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/README.md', anchor: 'main-content' },
  { id: 'atlas', name: 'Atlas', descriptor: 'Asset inventory', summary: 'Register, search, filter, update, and inspect organization-owned assets and their lifecycle history.', example: 'Add a server, assign its location and department, then advance its lifecycle status when it is retired.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/atlas.md', anchor: 'guide-atlas', permission: 'assets.read' },
  { id: 'horizon', name: 'Horizon', descriptor: 'Lifecycle planning', summary: 'Horizon is the planned lifecycle forecasting workspace. Atlas and Ledger now provide its inventory and financial foundations.', example: 'A future forecast will group replacement needs by fiscal year, site, department, tag, goal, and asset class.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/issues/3' },
  { id: 'ledger', name: 'Ledger', descriptor: 'Procurement and budgets', summary: 'Track vendors, purchase orders, contracts, commitments, budgets, reconciled costs, and variance in exact minor units.', example: 'Create a fiscal-year budget, reconcile a billed invoice, and review the resulting over-budget or remaining amount.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/ledger.md', anchor: 'guide-ledger', permission: 'finance.read' },
  { id: 'threads', name: 'Threads', descriptor: 'Tags and strategic goals', summary: 'Connect assets to hierarchical tags and goals while keeping inherited, explicit, and suppressed values visible.', example: 'Apply a child tag to an asset and inspect the parent tag provenance before linking the asset to a strategic goal.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/threads.md', anchor: 'guide-threads', permission: 'goals.read' },
  { id: 'vault', name: 'Vault', descriptor: 'Private files and evidence', summary: 'Store checksummed evidence with ownership and provenance, then authorize downloads only when needed.', example: 'Upload a purchase receipt with its source identity and relate it to the asset or financial record it supports.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/vault.md', anchor: 'guide-vault', permission: 'storage.read' },
  { id: 'people', name: 'People', descriptor: 'Users and departments', summary: 'Organize sites, buildings, rooms, departments, identities, and effective-dated asset assignments.', example: 'Create a site and room, add a person, then record that person as the asset’s primary steward.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/people.md', anchor: 'guide-people', permission: 'directory.read' },
  { id: 'guard', name: 'Guard', descriptor: 'Authentication and authorization', summary: 'Manage secure sign-in, roles, policy bundles, scoped assignments, ownership, and audit boundaries.', example: 'Create a custom read-only role and assign it at the organization, site, department, or resource scope.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/guard.md', anchor: 'guide-guard', permission: 'guard.manage' },
  { id: 'guide', name: 'Guide', descriptor: 'Help and walkthroughs', summary: 'Open contextual help, replay a role-aware walkthrough, inspect branding accessibility, or prepare a sanitized issue report.', example: 'Select a module here, follow its direct documentation link, and open a prefilled report without exposing session or identity data.', docsUrl: 'https://github.com/WSCMAX/StewardMesh/blob/main/docs/features/guide.md' },
]

export type BrandTheme = {
  darkCanvas: string
  darkSurface: string
  lightCanvas: string
  textOnDark: string
  textOnLight: string
  primary: string
  success: string
  warning: string
  danger: string
}

export type ContrastCheck = {
  id: string
  label: string
  ratio: number
  minimum: number
  passed: boolean
}

export type BrandingValidation = {
  blocked: boolean
  checks: ContrastCheck[]
  invalidColors: string[]
  warnings: string[]
}

export type ResolvedBranding = {
  requestedTheme: BrandTheme
  appliedTheme: BrandTheme
  validation: BrandingValidation
  usedFallback: boolean
}

export const defaultBrandTheme: BrandTheme = {
  darkCanvas: '#061827',
  darkSurface: '#0B2238',
  lightCanvas: '#F7FAFC',
  textOnDark: '#F7FAFC',
  textOnLight: '#061827',
  primary: '#16BFA7',
  success: '#168C4B',
  warning: '#C57900',
  danger: '#CC3D4A',
}

const brandKeys = Object.keys(defaultBrandTheme) as (keyof BrandTheme)[]
const hexPattern = /^#[0-9a-f]{6}$/i

function rgb(hex: string) {
  if (!hexPattern.test(hex)) return null
  return [Number.parseInt(hex.slice(1, 3), 16), Number.parseInt(hex.slice(3, 5), 16), Number.parseInt(hex.slice(5, 7), 16)] as const
}

function relativeLuminance(hex: string) {
  const color = rgb(hex)
  if (!color) return null
  const channels = color.map((channel) => {
    const normalized = channel / 255
    return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4
  })
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722
}

export function contrastRatio(foreground: string, background: string) {
  const foregroundLuminance = relativeLuminance(foreground)
  const backgroundLuminance = relativeLuminance(background)
  if (foregroundLuminance === null || backgroundLuminance === null) return 0
  const lighter = Math.max(foregroundLuminance, backgroundLuminance)
  const darker = Math.min(foregroundLuminance, backgroundLuminance)
  return (lighter + 0.05) / (darker + 0.05)
}

function simulatedColor(hex: string, kind: 'protanopia' | 'deuteranopia' | 'tritanopia') {
  const color = rgb(hex)
  if (!color) return [0, 0, 0]
  const [red, green, blue] = color.map((channel) => channel / 255)
  const matrices = {
    protanopia: [[0.567, 0.433, 0], [0.558, 0.442, 0], [0, 0.242, 0.758]],
    deuteranopia: [[0.625, 0.375, 0], [0.7, 0.3, 0], [0, 0.3, 0.7]],
    tritanopia: [[0.95, 0.05, 0], [0, 0.433, 0.567], [0, 0.475, 0.525]],
  } as const
  return matrices[kind].map((row) => row[0] * red + row[1] * green + row[2] * blue)
}

function distance(left: number[], right: number[]) {
  return Math.sqrt(left.reduce((total, value, index) => total + (value - right[index]) ** 2, 0))
}

function composite(foreground: string, background: string, opacity: number) {
  const front = rgb(foreground)
  const back = rgb(background)
  if (!front || !back) return '#000000'
  return `#${front.map((channel, index) => Math.round(channel * opacity + back[index] * (1 - opacity)).toString(16).padStart(2, '0')).join('')}`
}

export function validateBrandTheme(theme: BrandTheme): BrandingValidation {
  const invalidColors = brandKeys.filter((key) => !hexPattern.test(theme[key]))
  const definitions = [
    ['dark-copy', 'Text on the dark canvas', theme.textOnDark, theme.darkCanvas, 4.5],
    ['dark-surface-copy', 'Text on dark surfaces', theme.textOnDark, theme.darkSurface, 4.5],
    ['light-copy', 'Text on the light canvas', theme.textOnLight, theme.lightCanvas, 4.5],
    ['primary-copy', 'Text on the primary action', theme.darkCanvas, theme.primary, 4.5],
    ['primary-hover-copy', 'Text on the primary-action hover state', theme.darkCanvas, '#29CFB9', 4.5],
    ['primary-link-copy', 'Primary links against dark surfaces', theme.primary, theme.darkSurface, 4.5],
    ['link-hover-copy', 'Link hover text against dark surfaces', '#58D9C7', theme.darkSurface, 4.5],
    ['focus-indicator', 'Primary focus indicator against dark surfaces', theme.primary, theme.darkSurface, 3],
    ['success-copy', 'Success text against its tinted surface', '#67DD99', composite(theme.success, theme.darkSurface, 0.15), 4.5],
    ['warning-copy', 'Warning text against its tinted surface', '#FFC46B', composite(theme.warning, theme.darkSurface, 0.15), 4.5],
    ['danger-copy', 'Danger text against its tinted surface', '#FFCCD1', composite(theme.danger, theme.darkSurface, 0.15), 4.5],
  ] as const
  const checks = definitions.map(([id, label, foreground, background, minimum]) => {
    const ratio = contrastRatio(foreground, background)
    return { id, label, ratio, minimum, passed: ratio >= minimum }
  })
  const warnings = ['Never use color alone for status, charts, validation, or relationships; retain a text label, icon, pattern, or shape.']
  const semanticEntries = [['success', theme.success], ['warning', theme.warning], ['danger', theme.danger]] as const
  for (const kind of ['protanopia', 'deuteranopia', 'tritanopia'] as const) {
    for (let left = 0; left < semanticEntries.length; left++) {
      for (let right = left + 1; right < semanticEntries.length; right++) {
        if (distance(simulatedColor(semanticEntries[left][1], kind), simulatedColor(semanticEntries[right][1], kind)) < 0.18) {
          warnings.push(`${semanticEntries[left][0]} and ${semanticEntries[right][0]} may be difficult to distinguish under ${kind}; labels or shapes are required.`)
        }
      }
    }
  }
  return { blocked: invalidColors.length > 0 || checks.some((check) => !check.passed), checks, invalidColors, warnings: [...new Set(warnings)] }
}

export function resolveBranding(values: Partial<BrandTheme>): ResolvedBranding {
  const requestedTheme = { ...defaultBrandTheme }
  for (const key of brandKeys) {
    const value = values[key]?.trim()
    if (value) requestedTheme[key] = value.toUpperCase()
  }
  const validation = validateBrandTheme(requestedTheme)
  return { requestedTheme, appliedTheme: validation.blocked ? defaultBrandTheme : requestedTheme, validation, usedFallback: validation.blocked }
}

export function brandingStyle(theme: BrandTheme) {
  return {
    '--color-steward-ink-950': theme.darkCanvas,
    '--color-steward-ink-900': theme.darkSurface,
    '--color-steward-mist': theme.textOnDark,
    '--color-steward-mist-muted': theme.textOnDark,
    '--color-steward-teal': theme.primary,
    '--color-steward-success': theme.success,
    '--color-steward-warning': theme.warning,
    '--color-steward-danger': theme.danger,
  }
}

export type IssueContext = {
  page: string
  component: string
  version: string
  browser: string
  viewport: string
  system: string
  correlationId: string
}

function safeLine(value: string, fallback: string, maximum = 128) {
  const safe = value.trim().replace(/[^A-Za-z0-9 ./_():+-]/g, '').replace(/\s+/g, ' ').slice(0, maximum)
  return safe || fallback
}

export function detectBrowser(userAgent: string) {
  const candidates = [
    ['Edge', /Edg\/(\d+)/],
    ['Firefox', /Firefox\/(\d+)/],
    ['Chrome', /(?:Chrome|CriOS)\/(\d+)/],
    ['Safari', /Version\/(\d+).+Safari/],
  ] as const
  for (const [name, pattern] of candidates) {
    const match = userAgent.match(pattern)
    if (match) return `${name} ${match[1]}`
  }
  return 'Other browser'
}

export function detectSystem(userAgent: string) {
  if (/Windows NT/i.test(userAgent)) return 'Windows'
  if (/Android/i.test(userAgent)) return 'Android'
  if (/(?:iPhone|iPad|iPod)/i.test(userAgent)) return 'iOS'
  if (/Mac OS X/i.test(userAgent)) return 'macOS'
  if (/Linux/i.test(userAgent)) return 'Linux'
  return 'Other system'
}

export function collectIssueContext(component: string, version: string, correlationId: string): IssueContext {
  const page = typeof window === 'undefined' ? '/' : window.location.pathname
  const userAgent = typeof navigator === 'undefined' ? '' : navigator.userAgent
  const width = typeof window === 'undefined' ? 0 : Math.max(0, Math.min(10000, Math.round(window.innerWidth)))
  const height = typeof window === 'undefined' ? 0 : Math.max(0, Math.min(10000, Math.round(window.innerHeight)))
  const safeCorrelation = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(correlationId) ? correlationId : 'Unavailable'
  return {
    page: safeLine(page, '/'),
    component: safeLine(component, 'Workspace'),
    version: safeLine(version, 'development'),
    browser: detectBrowser(userAgent),
    viewport: `${width}x${height}`,
    system: detectSystem(userAgent),
    correlationId: safeCorrelation,
  }
}

export function buildIssueReportUrl(baseUrl: string, context: IssueContext) {
  const relative = baseUrl.startsWith('/') && !baseUrl.startsWith('//')
  const url = new URL(baseUrl, 'https://stewardmesh.invalid')
  if (url.hostname === 'github.com' && /\/issues\/?$/.test(url.pathname)) url.pathname = `${url.pathname.replace(/\/$/, '')}/new`
  const body = [
    '## StewardMesh context',
    '',
    `- Page: ${safeLine(context.page, '/')}`,
    `- Component: ${safeLine(context.component, 'Workspace')}`,
    `- Version: ${safeLine(context.version, 'development')}`,
    `- Browser: ${safeLine(context.browser, 'Other browser')}`,
    `- Viewport: ${safeLine(context.viewport, '0x0')}`,
    `- System: ${safeLine(context.system, 'Other system')}`,
    `- Correlation ID: ${safeLine(context.correlationId, 'Unavailable')}`,
    '',
    '## Before submitting',
    '',
    '- Describe what you expected and what happened.',
    '- Remove names, emails, asset details, files, credentials, cookies, tokens, and private URLs.',
  ].join('\n')
  url.searchParams.set('title', `[Guide] ${safeLine(context.component, 'Workspace')} issue`)
  url.searchParams.set('body', body)
  return relative ? `${url.pathname}${url.search}` : url.toString()
}

const walkthroughKey = 'stewardmesh.guide.walkthrough.v1'

export function readWalkthroughStatus(): WalkthroughStatus {
  try {
    const value = localStorage.getItem(walkthroughKey)
    return value === 'completed' || value === 'skipped' ? value : 'new'
  } catch {
    return 'new'
  }
}

export function writeWalkthroughStatus(status: WalkthroughStatus) {
  try { localStorage.setItem(walkthroughKey, status) } catch { /* Preference persistence is optional. */ }
}
