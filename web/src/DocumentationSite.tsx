import { useEffect, useMemo, useRef, useState } from 'react'
import {
  documentationByID,
  documentationHref,
  documentationPages,
  searchDocumentation,
  type DocumentationPage,
  type DocumentationSection,
  type DocumentationTopicID,
} from './documentation'
import { AreaIcon, ChevronRightIcon, StatusBadge, buttonClass, cx, inputClass, panelClass, plainButtonClass, secondaryButtonClass, type AreaIconName } from './ui'

// Requirements: A11Y-001, DOC-001. Feature: experience.help.

type DocumentationSiteProps = {
  topicID: DocumentationTopicID
}

const documentationGroups = ['Start here', 'Product areas', 'Administration'] as const

const iconForPage: Record<DocumentationTopicID, AreaIconName> = {
  overview: 'overview',
  workspace: 'overview',
  atlas: 'atlas',
  horizon: 'horizon',
  ledger: 'ledger',
  stack: 'stack',
  signals: 'signals',
  threads: 'threads',
  vault: 'vault',
  people: 'people',
  guard: 'guard',
  guide: 'overview',
}

export default function DocumentationSite({ topicID }: DocumentationSiteProps) {
  const page = documentationByID[topicID]
  const pageIndex = documentationPages.findIndex((candidate) => candidate.id === topicID)
  const previousPage = pageIndex > 0 ? documentationPages[pageIndex - 1] : null
  const nextPage = pageIndex < documentationPages.length - 1 ? documentationPages[pageIndex + 1] : null
  const [query, setQuery] = useState('')
  const [navigationOpen, setNavigationOpen] = useState(false)
  const headingRef = useRef<HTMLHeadingElement>(null)
  const priorTopic = useRef(topicID)
  const results = useMemo(() => searchDocumentation(query), [query])

  useEffect(() => {
    document.title = `${page.title} documentation · StewardMesh`
    if (priorTopic.current !== topicID) {
      window.scrollTo({ top: 0 })
      queueMicrotask(() => headingRef.current?.focus())
      priorTopic.current = topicID
    }
    setNavigationOpen(false)
  }, [page.title, topicID])

  return (
    <div className="min-h-screen text-steward-mist" data-feature="experience.help" data-requirement="A11Y-001 DOC-001">
      <a className="sr-only rounded-xl bg-steward-teal px-3 py-2 font-semibold text-steward-ink-950 focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50" href="#documentation-main">Skip to documentation</a>
      <header className="sticky top-0 z-30 border-b border-white/[0.07] bg-steward-ink-950/90 backdrop-blur-xl">
        <div className="mx-auto flex max-w-[100rem] items-center justify-between gap-4 px-4 py-3 sm:px-6">
          <a aria-label="StewardMesh documentation" className="group flex min-w-0 items-center gap-3 rounded-xl" href={documentationHref('overview')}>
            <span className="grid size-11 shrink-0 place-items-center rounded-xl border border-white/10 bg-steward-ink-900 shadow-sm"><img alt="" aria-hidden="true" className="h-9 w-auto" height="370" src="/brand/stewardmesh-s-mark.svg" width="294" /></span>
            <span className="hidden min-w-0 sm:block">
              <span className="block truncate text-lg font-bold tracking-tight text-white sm:text-xl">StewardMesh</span>
              <span className="block text-xs font-semibold uppercase tracking-[0.14em] text-steward-teal">Documentation</span>
            </span>
          </a>
          <a aria-label="Back to application" className={secondaryButtonClass} href={page.appHref}><span className="sm:hidden">Back to app</span><span className="hidden sm:inline">Back to application</span></a>
        </div>
      </header>

      <div className="mx-auto grid max-w-[100rem] gap-5 px-4 py-5 sm:px-6 lg:grid-cols-[17.5rem_minmax(0,1fr)] lg:gap-7 lg:py-7 xl:grid-cols-[17.5rem_minmax(0,1fr)_14rem]">
        <aside aria-label="Documentation topics" className={`${panelClass} h-fit overflow-hidden lg:sticky lg:top-[5.75rem]`}>
          <div className="border-b border-white/[0.07] p-4">
            <button aria-expanded={navigationOpen} className={`${secondaryButtonClass} w-full justify-between lg:hidden`} onClick={() => setNavigationOpen((current) => !current)} type="button">
              Browse documentation
              <ChevronRightIcon className={cx('size-4 transition', navigationOpen && 'rotate-90')} />
            </button>
            <div className={cx(navigationOpen ? 'block' : 'hidden', 'lg:block')}>
              <label className="block text-sm font-semibold text-steward-mist" htmlFor="documentation-search">Search documentation</label>
              <div className="relative">
                <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 z-10 size-4 -translate-y-1/2 text-steward-slate" />
                <input className={`${inputClass} pl-10`} id="documentation-search" onChange={(event) => setQuery(event.target.value)} placeholder="Search topics" type="search" value={query} />
              </div>
            </div>
          </div>
          <div className={cx(navigationOpen ? 'block' : 'hidden', 'steward-scrollbar max-h-[calc(100vh-14rem)] overflow-y-auto p-3 lg:block')}>
            {results.length === 0 ? <p className="px-3 py-6 text-center text-sm text-steward-mist-muted" role="status">No documentation matches “{query}”.</p> : <DocumentationNavigation current={topicID} pages={results} />}
          </div>
        </aside>

        <main className="min-w-0" id="documentation-main" tabIndex={-1}>
          <article className={`${panelClass} relative overflow-hidden px-5 py-7 sm:px-8 sm:py-10 lg:px-10`}>
            <div aria-hidden="true" className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-steward-green via-steward-teal to-steward-blue" />
            <div className="flex flex-wrap items-center gap-2 text-xs font-semibold uppercase tracking-[0.16em] text-steward-slate">
              <a className="rounded-md text-steward-teal hover:text-[#5be0cc]" href={documentationHref('overview')}>Docs</a>
              <ChevronRightIcon className="size-3" />
              <span>{page.group}</span>
            </div>
            <header className="mt-6 border-b border-white/[0.08] pb-8">
              <p className="text-sm font-semibold text-steward-teal">{page.kicker}</p>
              <h1 className="mt-2 text-3xl font-bold tracking-tight text-white sm:text-4xl" ref={headingRef} tabIndex={-1}>{page.title}</h1>
              <p className="mt-4 max-w-3xl text-base leading-7 text-steward-mist-muted sm:text-lg sm:leading-8">{page.summary}</p>
              <div className="mt-6 flex flex-wrap items-center gap-3">
                <a className={buttonClass} href={page.appHref}>{page.appLabel}<ChevronRightIcon /></a>
                <StatusBadge tone="success">Current host</StatusBadge>
                <StatusBadge>Public guidance</StatusBadge>
              </div>
            </header>

            <div className="docs-prose mt-9 space-y-12">
              {page.sections.map((section, index) => <DocumentationSectionView index={index} key={section.id} section={section} />)}
            </div>

            <RelatedGuides page={page} />

            <nav aria-label="Previous and next documentation" className="mt-12 grid gap-3 border-t border-white/[0.08] pt-6 sm:grid-cols-2">
              {previousPage ? <PageDirectionLink direction="Previous" page={previousPage} /> : <span />}
              {nextPage && <PageDirectionLink align="right" direction="Next" page={nextPage} />}
            </nav>
          </article>
          <p className="px-2 py-5 text-center text-xs leading-5 text-steward-slate">Local product guidance served by StewardMesh. Engineering and requirement references remain independent repository documentation.</p>
        </main>

        <aside aria-label="On this page" className="hidden xl:block">
          <div className="sticky top-[6.5rem] border-l border-white/[0.08] pl-5">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-steward-slate">On this page</p>
            <ul className="mt-4 space-y-1">
              {page.sections.map((section) => <li key={section.id}><button className={`${plainButtonClass} min-h-9 w-full justify-start px-2 py-1 text-left text-steward-mist-muted`} onClick={() => scrollToSection(section.id)} type="button">{section.title}</button></li>)}
            </ul>
          </div>
        </aside>
      </div>
    </div>
  )
}

function DocumentationNavigation({ current, pages }: { current: DocumentationTopicID; pages: readonly DocumentationPage[] }) {
  return <nav aria-label="Documentation">
    {documentationGroups.map((group) => {
      const groupPages = pages.filter((page) => page.group === group)
      if (groupPages.length === 0) return null
      return <div className="mb-5 last:mb-0" key={group}>
        <p className="px-3 text-[0.6875rem] font-semibold uppercase tracking-[0.16em] text-steward-slate">{group}</p>
        <ul className="mt-2 space-y-1">
          {groupPages.map((page) => <li key={page.id}>
            <a aria-current={page.id === current ? 'page' : undefined} className={cx('group flex min-h-11 items-center gap-3 rounded-xl px-3 py-2 text-sm font-semibold transition', page.id === current ? 'bg-steward-teal/12 text-white ring-1 ring-inset ring-steward-teal/25' : 'text-steward-mist-muted hover:bg-white/[0.045] hover:text-white')} href={documentationHref(page.id)}>
              <span aria-hidden="true" className={cx('grid size-8 shrink-0 place-items-center rounded-lg border', page.id === current ? 'border-steward-teal/30 bg-steward-teal text-steward-ink-950' : 'border-white/[0.07] bg-white/[0.035] text-steward-slate group-hover:text-steward-mist')}><AreaIcon area={iconForPage[page.id]} className="size-4" /></span>
              <span>{page.title}</span>
            </a>
          </li>)}
        </ul>
      </div>
    })}
  </nav>
}

function DocumentationSectionView({ index, section }: { index: number; section: DocumentationSection }) {
  return <section aria-labelledby={`documentation-section-${section.id}-heading`} className="scroll-mt-24" id={`documentation-section-${section.id}`}>
    <div className="flex items-center gap-3">
      <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-lg border border-steward-teal/25 bg-steward-teal/10 text-xs font-bold text-steward-teal">{String(index + 1).padStart(2, '0')}</span>
      <h2 className="text-xl font-bold tracking-tight text-white sm:text-2xl" id={`documentation-section-${section.id}-heading`}>{section.title}</h2>
    </div>
    {section.paragraphs?.map((paragraph) => <p className="mt-4 text-base leading-7 text-steward-mist-muted" key={paragraph}>{paragraph}</p>)}
    {section.bullets && <ul className="mt-5 space-y-3">{section.bullets.map((bullet) => <li className="flex gap-3 text-base leading-7 text-steward-mist-muted" key={bullet}><CheckIcon /><span>{bullet}</span></li>)}</ul>}
    {section.steps && <ol className="mt-6 space-y-4">{section.steps.map((step, stepIndex) => <li className="grid grid-cols-[2rem_minmax(0,1fr)] gap-3" key={step.title}><span aria-hidden="true" className="grid size-8 place-items-center rounded-full border border-steward-blue/30 bg-steward-blue/10 text-sm font-bold text-[#a9c7ff]">{stepIndex + 1}</span><div><h3 className="font-semibold text-steward-mist">{step.title}</h3><p className="mt-1 text-sm leading-6 text-steward-mist-muted sm:text-base sm:leading-7">{step.body}</p></div></li>)}</ol>}
    {section.callout && <DocumentationCallout callout={section.callout} />}
  </section>
}

function DocumentationCallout({ callout }: { callout: NonNullable<DocumentationSection['callout']> }) {
  const tones = {
    info: 'border-steward-blue/35 bg-steward-blue/10 text-[#b8d1ff]',
    success: 'border-steward-success/40 bg-steward-success/10 text-[#aaf0c6]',
    warning: 'border-steward-warning/40 bg-steward-warning/10 text-[#ffd08a]',
  }
  return <aside className={cx('mt-6 rounded-xl border p-4', tones[callout.tone ?? 'info'])}><p className="text-sm font-semibold">{callout.title}</p><p className="mt-1 text-sm leading-6 text-steward-mist-muted">{callout.body}</p></aside>
}

function RelatedGuides({ page }: { page: DocumentationPage }) {
  return <section aria-labelledby="related-guides-heading" className="mt-12 border-t border-white/[0.08] pt-9">
    <h2 className="text-xl font-bold text-white" id="related-guides-heading">Related guides</h2>
    <div className="mt-4 grid gap-3 sm:grid-cols-3">
      {page.related.map((relatedID) => {
        const related = documentationByID[relatedID]
        return <a className="group rounded-xl border border-white/[0.08] bg-steward-ink-950/35 p-4 transition hover:border-steward-teal/35 hover:bg-steward-teal/[0.06]" href={documentationHref(related.id)} key={related.id}><span aria-hidden="true" className="grid size-9 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.035] text-steward-slate group-hover:text-steward-teal"><AreaIcon area={iconForPage[related.id]} /></span><h3 className="mt-3 font-semibold text-steward-mist">{related.title}</h3><p className="mt-1 text-sm leading-5 text-steward-mist-muted">{related.kicker}</p><span className="mt-3 inline-flex items-center gap-1 text-sm font-semibold text-steward-teal">Read guide <ChevronRightIcon /></span></a>
      })}
    </div>
  </section>
}

function PageDirectionLink({ align = 'left', direction, page }: { align?: 'left' | 'right'; direction: string; page: DocumentationPage }) {
  return <a className={cx('rounded-xl border border-white/[0.08] bg-steward-ink-950/35 p-4 transition hover:border-steward-teal/35 hover:bg-steward-teal/[0.06]', align === 'right' && 'text-right')} href={documentationHref(page.id)}><span className="text-xs font-semibold uppercase tracking-[0.14em] text-steward-slate">{direction}</span><span className={cx('mt-1 flex items-center gap-1 font-semibold text-steward-mist', align === 'right' && 'justify-end')}>{align === 'left' && <ChevronRightIcon className="size-4 rotate-180" />}{page.title}{align === 'right' && <ChevronRightIcon className="size-4" />}</span></a>
}

function scrollToSection(id: string) {
  const section = document.getElementById(`documentation-section-${id}`)
  section?.scrollIntoView({ block: 'start', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
}

function SearchIcon({ className }: { className: string }) {
  return <svg aria-hidden="true" className={className} fill="none" stroke="currentColor" strokeLinecap="round" strokeWidth="1.8" viewBox="0 0 24 24"><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></svg>
}

function CheckIcon() {
  return <svg aria-hidden="true" className="mt-1.5 size-4 shrink-0 text-steward-teal" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" viewBox="0 0 20 20"><path d="m4 10 4 4 8-8" /></svg>
}
