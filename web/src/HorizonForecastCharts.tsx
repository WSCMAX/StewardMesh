import { useId } from 'react'
import { piesByDepartment, stackByYear, type ForecastViewItem } from './horizonForecastViews'
import { subpanelClass } from './ui'

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

const seriesColors = ['#16BFA7', '#4C8DFF', '#C57900', '#168C4B', '#CC3D4A', '#A78BFA', '#F7FAFC', '#7DD3FC']

type HorizonForecastChartsProps = {
  currency: string
  departmentLabel: (key: string) => string
  items: readonly ForecastViewItem[]
  kindLabel: (key: string) => string
  money: (minor: number, currency: string) => string
}

export default function HorizonForecastCharts({ currency, departmentLabel, items, kindLabel, money }: HorizonForecastChartsProps) {
  const typeStack = stackByYear(items, (item) => kindLabel(item.kind))
  const departmentStack = stackByYear(items, (item) => departmentLabel(item.department))
  const pies = piesByDepartment(items, departmentLabel, kindLabel)
  return (
    <div className="mt-6 grid gap-6">
      <StackedLineChart
        caption="Each line is an asset type stacked inside the yearly replacement total."
        currency={currency}
        money={money}
        series={typeStack.series}
        title="Replacement by year and type"
        years={typeStack.years}
      />
      <StackedLineChart
        caption="Each line is a department stacked inside the yearly replacement total."
        currency={currency}
        money={money}
        series={departmentStack.series}
        title="Replacement by year and department"
        years={departmentStack.years}
      />
      <section aria-labelledby="horizon-department-pies-heading" className={`${subpanelClass} p-4`}>
        <h4 className="font-semibold" id="horizon-department-pies-heading">Type mix by department</h4>
        <p className="mt-1 text-sm leading-6 text-steward-mist-muted">One pie per department. Missing department or type is Other.</p>
        {pies.length === 0
          ? <p className="mt-3 text-sm text-steward-mist-muted">No replacement items match these forecast controls.</p>
          : <ul className="mt-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{pies.map((pie) => (
            <li className="rounded-lg border border-white/10 bg-white/[0.025] p-3" key={pie.key}>
              <p className="font-medium">{pie.label}</p>
              <p className="mt-0.5 text-sm text-steward-mist-muted">{money(pie.totalMinor, currency)}</p>
              <PieChart currency={currency} money={money} slices={pie.slices} title={pie.label} />
            </li>
          ))}</ul>}
      </section>
    </div>
  )
}

function StackedLineChart({ caption, currency, money, series, title, years }: {
  caption: string
  currency: string
  money: (minor: number, currency: string) => string
  series: readonly string[]
  title: string
  years: readonly { year: number; label: string; totalMinor: number; series: Record<string, number> }[]
}) {
  const headingId = useId()
  const width = 720
  const height = 220
  const pad = { top: 16, right: 16, bottom: 28, left: 56 }
  const innerWidth = width - pad.left - pad.right
  const innerHeight = height - pad.top - pad.bottom
  const maximum = Math.max(1, ...years.map((year) => year.totalMinor))
  const xFor = (index: number) => years.length <= 1 ? innerWidth / 2 : (index / (years.length - 1)) * innerWidth
  const yFor = (value: number) => innerHeight - (value / maximum) * innerHeight
  return (
    <section aria-labelledby={headingId} className={`${subpanelClass} p-4`}>
      <h4 className="font-semibold" id={headingId}>{title}</h4>
      <p className="mt-1 text-sm leading-6 text-steward-mist-muted">{caption}</p>
      {years.length === 0
        ? <p className="mt-3 text-sm text-steward-mist-muted">No yearly replacement rows to chart.</p>
        : <>
          <div className="mt-4 overflow-x-auto">
            <svg aria-hidden="true" className="h-56 w-full min-w-[28rem] text-steward-mist" preserveAspectRatio="none" role="img" viewBox={`0 0 ${width} ${height}`}>
              <line stroke="currentColor" strokeOpacity="0.2" x1={pad.left} x2={width - pad.right} y1={pad.top + innerHeight} y2={pad.top + innerHeight} />
              {years.map((year, index) => {
                let baseline = 0
                return series.map((name, seriesIndex) => {
                  const value = year.series[name] ?? 0
                  const next = baseline + value
                  const x = pad.left + xFor(index)
                  const top = pad.top + yFor(next)
                  const bottom = pad.top + yFor(baseline)
                  const nextIndex = Math.min(index + 1, years.length - 1)
                  let nextBaseline = 0
                  for (let cursor = 0; cursor < seriesIndex; cursor += 1) nextBaseline += years[nextIndex]?.series[series[cursor]] ?? 0
                  const nextValue = (years[nextIndex]?.series[name] ?? 0)
                  const nextTop = pad.top + yFor(nextBaseline + nextValue)
                  const nextBottom = pad.top + yFor(nextBaseline)
                  const nextX = pad.left + xFor(nextIndex)
                  baseline = next
                  const color = seriesColors[seriesIndex % seriesColors.length]
                  return <g key={`${year.year}-${name}`}>
                    <polygon fill={color} fillOpacity="0.28" points={`${x},${bottom} ${nextX},${nextBottom} ${nextX},${nextTop} ${x},${top}`} />
                    <line stroke={color} strokeWidth="2" x1={x} x2={nextX} y1={top} y2={nextTop} />
                  </g>
                })
              })}
              {years.map((year, index) => (
                <text fill="currentColor" fontSize="11" key={year.year} textAnchor="middle" x={pad.left + xFor(index)} y={height - 8}>{year.label}</text>
              ))}
            </svg>
          </div>
          <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-2 text-sm">{series.map((name, index) => (
            <li className="flex items-center gap-2" key={name}>
              <span aria-hidden="true" className="size-2.5 rounded-sm" style={{ background: seriesColors[index % seriesColors.length] }} />
              <span>{name}</span>
            </li>
          ))}</ul>
          <table className="sr-only">
            <caption>{title} amounts</caption>
            <thead><tr><th>Year</th>{series.map((name) => <th key={name}>{name}</th>)}<th>Total</th></tr></thead>
            <tbody>{years.map((year) => <tr key={year.year}><td>{year.label}</td>{series.map((name) => <td key={name}>{money(year.series[name] ?? 0, currency)}</td>)}<td>{money(year.totalMinor, currency)}</td></tr>)}</tbody>
          </table>
        </>}
    </section>
  )
}

function PieChart({ currency, money, slices, title }: {
  currency: string
  money: (minor: number, currency: string) => string
  slices: readonly { key: string; label: string; replacementCostMinor: number }[]
  title: string
}) {
  const total = slices.reduce((sum, slice) => sum + slice.replacementCostMinor, 0)
  const radius = 42
  const cx = 48
  const cy = 48
  let start = -Math.PI / 2
  const arcs = slices.map((slice, index) => {
    const portion = total === 0 ? 0 : slice.replacementCostMinor / total
    const sweep = portion * Math.PI * 2
    const end = start + sweep
    const path = slicePath(cx, cy, radius, start, end, portion >= 1)
    start = end
    return { ...slice, path, color: seriesColors[index % seriesColors.length], portion }
  })
  return (
    <div className="mt-3 flex items-start gap-3">
      <svg aria-hidden="true" className="size-24 shrink-0" viewBox="0 0 96 96">
        {arcs.map((arc) => <path d={arc.path} fill={arc.color} key={arc.key} />)}
      </svg>
      <ul className="min-w-0 text-sm">
        {arcs.map((arc) => (
          <li className="flex items-start gap-2" key={arc.key}>
            <span aria-hidden="true" className="mt-1 size-2.5 shrink-0 rounded-sm" style={{ background: arc.color }} />
            <span><span className="font-medium">{arc.label}</span> {money(arc.replacementCostMinor, currency)} ({Math.round(arc.portion * 100)}%)</span>
          </li>
        ))}
      </ul>
      <span className="sr-only">{title} type mix</span>
    </div>
  )
}

function slicePath(cx: number, cy: number, radius: number, start: number, end: number, full: boolean) {
  if (full) return `M ${cx} ${cy - radius} A ${radius} ${radius} 0 1 1 ${cx - 0.01} ${cy - radius} Z`
  const startX = cx + Math.cos(start) * radius
  const startY = cy + Math.sin(start) * radius
  const endX = cx + Math.cos(end) * radius
  const endY = cy + Math.sin(end) * radius
  const large = end - start > Math.PI ? 1 : 0
  return `M ${cx} ${cy} L ${startX} ${startY} A ${radius} ${radius} 0 ${large} 1 ${endX} ${endY} Z`
}
