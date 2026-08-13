import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { correlationEventName, getLastCorrelationId } from './api'
import {
  buildIssueReportUrl,
  collectIssueContext,
  guideTopics,
  type GuideTopicID,
  type GuideView,
  type ResolvedBranding,
  type WalkthroughStatus,
} from './guide'
import { buttonClass as primaryButtonClass, inputClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-WORKSPACE-001, A11Y-001, DOC-001, DOC-002. Features: experience.workspace, experience.help.

export type GuideDestination = { view: GuideView; topic: GuideTopicID }

type GuideExperienceProps = {
  branding: ResolvedBranding
  destination: GuideDestination
  issuesUrl: string
  onClose: () => void
  onFollowSection?: (topic: GuideTopicID, anchor: string) => void
  onNavigate: (destination: GuideDestination) => void
  onWalkthroughStatus: (status: WalkthroughStatus) => void
  open: boolean
  permissions: readonly string[]
  roles: readonly string[]
  version: string
}

type GuideInvitationProps = {
  onNavigate: (destination: GuideDestination) => void
  onWalkthroughStatus: (status: WalkthroughStatus) => void
  roles: readonly string[]
  status: WalkthroughStatus
}

export function GuideInvitation({ onNavigate, onWalkthroughStatus, roles, status }: GuideInvitationProps) {
  if (status !== 'new') return null
  return (
    <section aria-labelledby="guide-invitation-heading" className="rounded-2xl border border-steward-blue/35 bg-steward-blue/10 p-5 shadow-[inset_0_1px_0_rgba(255,255,255,0.035)]" data-feature="experience.help" data-requirement="A11Y-001 DOC-001 DOC-002">
      <p className="text-sm font-semibold text-[#8eb7ff]">Guide — Role-aware walkthrough</p>
      <h2 className="mt-2 text-xl font-semibold" id="guide-invitation-heading">Take a quick tour of your workspace</h2>
      <p className="mt-2 max-w-3xl leading-7 text-steward-mist-muted">The tour adapts to {roles.length > 0 ? `your ${roles.join(', ')} role` : 'your current access'}, can be skipped at any time, and never prevents normal work.</p>
      <div className="mt-4 flex flex-wrap gap-3">
        <button className={primaryButtonClass} onClick={() => onNavigate({ view: 'walkthrough', topic: 'workspace' })} type="button">Start walkthrough</button>
        <button className={secondaryButtonClass} onClick={() => onWalkthroughStatus('skipped')} type="button">Skip for now</button>
      </div>
    </section>
  )
}

export default function GuideExperience({ branding, destination, issuesUrl, onClose, onFollowSection, onNavigate, onWalkthroughStatus, open, permissions, roles, version }: GuideExperienceProps) {
  const [activeStep, setActiveStep] = useState(0)
  const [walkthroughResult, setWalkthroughResult] = useState('')
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const guidePanelRef = useRef<HTMLDivElement>(null)
  const openerRef = useRef<HTMLElement | null>(null)
  const wasOpen = useRef(false)
  const availableTopics = useMemo(() => guideTopics.filter((topic) => !topic.permission || permissions.includes(topic.permission)), [permissions])
  const walkthrough = useMemo(() => availableTopics.length > 0 ? availableTopics : guideTopics.filter((topic) => topic.id === 'workspace' || topic.id === 'guide'), [availableTopics])

  const closeGuide = useCallback(() => {
    onClose()
    queueMicrotask(() => openerRef.current?.focus())
  }, [onClose])

  useEffect(() => {
    if (open && !wasOpen.current) {
      openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
      queueMicrotask(() => closeButtonRef.current?.focus())
    }
    wasOpen.current = open
  }, [open])

  useEffect(() => {
    if (!open) return
    const priorOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    function handleDialogKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        closeGuide()
        return
      }
      if (event.key !== 'Tab') return
      const focusable = Array.from(guidePanelRef.current?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [])
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleDialogKeyDown)
    return () => {
      document.body.style.overflow = priorOverflow
      window.removeEventListener('keydown', handleDialogKeyDown)
    }
  }, [closeGuide, open])

  function followSection(anchor: string) {
    const followedTopic = guideTopics.find((candidate) => candidate.anchor === anchor)
    if (followedTopic) onFollowSection?.(followedTopic.id, anchor)
    onClose()
    queueMicrotask(() => {
      const target = document.getElementById(anchor)
      if (!target) return
      if (!target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1')
      target.scrollIntoView({ block: 'start' })
      target.focus()
    })
  }

  function startWalkthrough() {
    setActiveStep(0)
    setWalkthroughResult('')
    onNavigate({ view: 'walkthrough', topic: walkthrough[0]?.id ?? 'workspace' })
  }

  function moveStep(next: number) {
    if (next >= walkthrough.length) {
      onWalkthroughStatus('completed')
      setWalkthroughResult('Walkthrough completed. You can close Guide or replay it from the start.')
      setActiveStep(Math.max(0, walkthrough.length - 1))
      return
    }
    const bounded = Math.max(0, next)
    setActiveStep(bounded)
    onNavigate({ view: 'walkthrough', topic: walkthrough[bounded]?.id ?? 'workspace' })
  }

  if (!open) return null
  const topic = guideTopics.find((candidate) => candidate.id === destination.topic) ?? guideTopics[0]
  const currentStep = walkthrough[Math.min(activeStep, walkthrough.length - 1)] ?? guideTopics[0]

  return (<>
    <button aria-label="Dismiss Guide backdrop" className="fixed inset-0 z-40 cursor-default bg-black/60 backdrop-blur-sm" onClick={closeGuide} type="button" />
    <div aria-label="Guide help and walkthroughs" aria-modal="true" className="fixed inset-y-0 right-0 z-50 w-full max-w-lg overflow-y-auto border-l border-white/10 bg-steward-ink-900/98 p-5 shadow-2xl shadow-black/60 sm:p-6" data-feature="experience.help" data-requirement="A11Y-001 DOC-001 DOC-002" ref={guidePanelRef} role="dialog">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-semibold text-steward-teal">Guide</p>
          <h2 className="mt-1 text-2xl font-bold tracking-tight">Help, walkthroughs, and feedback</h2>
        </div>
        <button aria-label="Close Guide" className={secondaryButtonClass} onClick={closeGuide} ref={closeButtonRef} type="button">Close</button>
      </div>
      <p className="mt-3 leading-7 text-steward-mist-muted">Guide stays optional and non-gating. Use Escape or Close at any time, then replay a walkthrough whenever you need it.</p>

      <nav aria-label="Guide sections" className="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4">
        {([['help', 'Help'], ['walkthrough', 'Walkthrough'], ['accessibility', 'Accessibility'], ['report', 'Report issue']] as const).map(([view, label]) => (
          <button aria-pressed={destination.view === view} className={`${smallTabClass} ${destination.view === view ? 'border-steward-teal bg-steward-teal/10 text-steward-teal' : ''}`} key={view} onClick={() => view === 'walkthrough' ? startWalkthrough() : onNavigate({ view, topic: destination.topic })} type="button">{label}</button>
        ))}
      </nav>

      {destination.view === 'help' && <ContextHelp onFollowSection={followSection} onNavigate={onNavigate} topic={topic} topics={guideTopics} />}
      {destination.view === 'walkthrough' && <Walkthrough activeStep={activeStep} currentStep={currentStep} onFollowSection={followSection} onMove={moveStep} onSkip={() => { onWalkthroughStatus('skipped'); setWalkthroughResult('Walkthrough skipped. You can replay it whenever you are ready.') }} onStart={startWalkthrough} result={walkthroughResult} roles={roles} total={walkthrough.length} />}
      {destination.view === 'accessibility' && <BrandingAudit branding={branding} />}
      {destination.view === 'report' && <IssueReporter issuesUrl={issuesUrl} topic={topic} version={version} />}
    </div>
  </>)
}

function ContextHelp({ onFollowSection, onNavigate, topic, topics }: { onFollowSection: (anchor: string) => void; onNavigate: (destination: GuideDestination) => void; topic: (typeof guideTopics)[number]; topics: typeof guideTopics }) {
  return (
    <section aria-labelledby="guide-context-heading" className="mt-6">
      <label className="block text-sm font-semibold text-steward-mist-muted" htmlFor="guide-topic">Help topic</label>
      <select className={inputClass} id="guide-topic" onChange={(event) => onNavigate({ view: 'help', topic: event.target.value as GuideTopicID })} value={topic.id}>
        {topics.map((item) => <option key={item.id} value={item.id}>{item.name} — {item.descriptor}</option>)}
      </select>
      <div className={`${subpanelClass} mt-5 p-5`}>
        <p className="text-sm font-semibold text-steward-teal">{topic.name} — {topic.descriptor}</p>
        <h3 className="mt-2 text-xl font-semibold" id="guide-context-heading">What you can do here</h3>
        <p className="mt-3 leading-7 text-steward-mist-muted">{topic.summary}</p>
        <div className="mt-4 rounded-lg border border-steward-blue/30 bg-steward-blue/10 p-4">
          <p className="text-sm font-semibold text-[#8eb7ff]">Example</p>
          <p className="mt-1 text-sm leading-6 text-steward-mist-muted">{topic.example}</p>
        </div>
        <div className="mt-5 flex flex-wrap gap-3">
          {topic.anchor && document.getElementById(topic.anchor) && <a className={primaryButtonClass} href={`#${topic.anchor}`} onClick={(event) => { event.preventDefault(); onFollowSection(topic.anchor as string) }}>Go to {topic.name}</a>}
          <a className={secondaryButtonClass} href={topic.docsUrl}>Read {topic.name} documentation</a>
        </div>
      </div>
    </section>
  )
}

function Walkthrough({ activeStep, currentStep, onFollowSection, onMove, onSkip, onStart, result, roles, total }: { activeStep: number; currentStep: (typeof guideTopics)[number]; onFollowSection: (anchor: string) => void; onMove: (step: number) => void; onSkip: () => void; onStart: () => void; result: string; roles: readonly string[]; total: number }) {
  return (
    <section aria-labelledby="guide-walkthrough-heading" className="mt-6">
      <p className="text-sm font-semibold text-steward-teal">Step {activeStep + 1} of {total}</p>
      <h3 className="mt-2 text-xl font-semibold" id="guide-walkthrough-heading">{currentStep.name} — {currentStep.descriptor}</h3>
      <p className="mt-3 leading-7 text-steward-mist-muted">For {roles.length > 0 ? roles.join(', ') : 'your current access'}: {currentStep.summary}</p>
      <p className="mt-4 rounded-lg border border-steward-blue/30 bg-steward-blue/10 p-4 text-sm leading-6 text-steward-mist-muted"><strong className="text-steward-mist">Try this:</strong> {currentStep.example}</p>
      {result && <p className="mt-4 rounded-lg border border-steward-success/50 bg-steward-success/15 p-4 text-sm font-semibold text-[#aaf0c6]" role="status">{result}</p>}
      {currentStep.anchor && document.getElementById(currentStep.anchor) && <a className={`${primaryButtonClass} mt-4 inline-flex`} href={`#${currentStep.anchor}`} onClick={(event) => { event.preventDefault(); onFollowSection(currentStep.anchor as string) }}>Go to this section</a>}
      <div className="mt-6 flex flex-wrap gap-3">
        <button className={secondaryButtonClass} disabled={activeStep === 0} onClick={() => onMove(activeStep - 1)} type="button">Back</button>
        <button className={primaryButtonClass} onClick={() => onMove(activeStep + 1)} type="button">{activeStep + 1 === total ? 'Finish walkthrough' : 'Next'}</button>
        <button className={secondaryButtonClass} onClick={onSkip} type="button">Skip walkthrough</button>
        <button className="min-h-11 px-3 py-2 text-sm font-semibold text-steward-teal underline underline-offset-4" onClick={onStart} type="button">Replay from start</button>
      </div>
    </section>
  )
}

function BrandingAudit({ branding }: { branding: ResolvedBranding }) {
  return (
    <section aria-labelledby="guide-accessibility-heading" className="mt-6">
      <p className="text-sm font-semibold text-steward-teal">Brand setup check</p>
      <h3 className="mt-2 text-xl font-semibold" id="guide-accessibility-heading">WCAG 2.2 AA color safety</h3>
      <p className="mt-3 leading-7 text-steward-mist-muted">StewardMesh validates both dark and light canvas pairs before applying public branding colors. Critical failures fall back to the verified StewardMesh palette.</p>
      <p className={`mt-4 rounded-lg border p-4 font-semibold ${branding.usedFallback ? 'border-steward-danger/50 bg-steward-danger/15 text-[#ffccd1]' : 'border-steward-success/50 bg-steward-success/15 text-[#aaf0c6]'}`} role="status">
        {branding.usedFallback ? 'Unsafe branding was blocked. The verified default palette is active.' : 'Branding contrast passed. The configured palette is active.'}
      </p>
      {branding.validation.invalidColors.length > 0 && <p className="mt-3 text-sm text-[#ffccd1]">Invalid six-digit colors: {branding.validation.invalidColors.join(', ')}.</p>}
      <ul className="mt-5 space-y-3">
        {branding.validation.checks.map((check) => <li className="flex items-start justify-between gap-4 rounded-lg border border-steward-ink-800 bg-steward-ink-950/40 p-3" key={check.id}><span><strong className="block">{check.label}</strong><span className="text-sm text-steward-mist-muted">Minimum {check.minimum.toFixed(1)}:1</span></span><span className={`font-semibold ${check.passed ? 'text-[#aaf0c6]' : 'text-[#ffccd1]'}`}>{check.passed ? 'Pass' : 'Blocked'} · {check.ratio.toFixed(2)}:1</span></li>)}
      </ul>
      <h4 className="mt-6 font-semibold">Color-vision and non-color guidance</h4>
      <ul className="mt-2 list-disc space-y-2 pl-5 text-sm leading-6 text-steward-mist-muted">{branding.validation.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul>
    </section>
  )
}

function IssueReporter({ issuesUrl, topic, version }: { issuesUrl: string; topic: (typeof guideTopics)[number]; version: string }) {
  const [component, setComponent] = useState(topic.name)
  const [correlationId, setCorrelationId] = useState(getLastCorrelationId)
  useEffect(() => setComponent(topic.name), [topic.name])
  useEffect(() => {
    function updateCorrelation(event: Event) {
      if (event instanceof CustomEvent && typeof event.detail === 'string') setCorrelationId(event.detail)
    }
    window.addEventListener(correlationEventName, updateCorrelation)
    return () => window.removeEventListener(correlationEventName, updateCorrelation)
  }, [])
  const context = collectIssueContext(component, version, correlationId)
  const reportUrl = buildIssueReportUrl(issuesUrl, context)
  return (
    <section aria-labelledby="guide-report-heading" className="mt-6">
      <p className="text-sm font-semibold text-steward-teal">Sanitized issue report</p>
      <h3 className="mt-2 text-xl font-semibold" id="guide-report-heading">Prepare technical context</h3>
      <p className="mt-3 leading-7 text-steward-mist-muted">Guide includes only allow-listed technical context. It excludes names, email, roles, record values, query strings, cookies, CSRF values, tokens, files, and request bodies.</p>
      <label className="mt-5 block text-sm font-semibold text-steward-mist-muted" htmlFor="guide-report-component">Component</label>
      <select className={inputClass} id="guide-report-component" onChange={(event) => setComponent(event.target.value)} value={component}>
        {guideTopics.map((item) => <option key={item.id} value={item.name}>{item.name} — {item.descriptor}</option>)}
      </select>
      <dl className={`${subpanelClass} mt-5 grid gap-3 p-4 text-sm sm:grid-cols-2`}>
        {Object.entries(context).map(([key, value]) => <div className="min-w-0" key={key}><dt className="font-semibold text-steward-mist">{contextLabel(key)}</dt><dd className="mt-1 break-words text-steward-mist-muted">{value}</dd></div>)}
      </dl>
      <a className={`${primaryButtonClass} mt-5 inline-flex`} href={reportUrl} rel="noreferrer" target="_blank">Review issue before submitting</a>
      <p className="mt-3 text-sm leading-6 text-steward-mist-muted">The issue form opens in a new tab. Review the generated context and remove private information from anything you add.</p>
    </section>
  )
}

function contextLabel(key: string) {
  const labels: Record<string, string> = { page: 'Page', component: 'Component', version: 'Version', browser: 'Browser', viewport: 'Viewport', system: 'System', correlationId: 'Correlation ID' }
  return labels[key] ?? key
}

const smallTabClass = 'min-h-11 rounded-xl border border-white/10 bg-white/[0.03] px-2 py-2 text-sm font-semibold text-steward-mist transition hover:border-steward-teal/40 hover:bg-steward-teal/8'
