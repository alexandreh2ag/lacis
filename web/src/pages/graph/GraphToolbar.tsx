import { Link } from 'react-router'
import { useT } from '@/i18n'
import type { ImpactDirection } from './Impact'
import { lenses, type GraphParams } from './useGraphParams'

// GraphToolbar sits above the graph: where the graph is, the zone
// opened, the impact mode, the lens, and the view.
export function GraphToolbar({ ns, p, opened, impact, onClose, onFit, onRelayout }: {
  ns: string
  p: GraphParams
  opened: { id: string; label: string } | null
  impact: { on: boolean; direction: ImpactDirection; setDirection: (d: ImpactDirection) => void; toggle: () => void }
  onClose: () => void
  onFit: () => void
  onRelayout: () => void
}) {
  const t = useT()
  const crumbs = [t.graph.lenses[p.lens.key], ...p.projects, ...p.envs, ...p.hosts]
  return (
    <div className="absolute inset-x-4 top-3 z-10 flex flex-wrap items-center gap-3">
      <nav className="lc-crumbs rounded-md border border-border bg-surface-raised px-2 py-1" aria-label={t.graph.toolbar.crumbs}>
        <Link to={`/${ns}`}>{ns}</Link>
        {crumbs.map((c, i) => (
          <span key={i} className="contents"><span className="lc-crumbs__sep">/</span><span aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{c}</span></span>
        ))}
      </nav>
      {opened ? (
        <div className="flex items-center gap-1.5 rounded-md border border-border bg-surface-raised px-2 py-1" role="group" aria-label={t.graph.toolbar.openedZone}>
          <span className="text-[12.5px] text-ink-muted">{t.graph.toolbar.opened}</span>
          <button className="lc-chip" type="button" aria-pressed="true" aria-label={t.graph.toolbar.close(opened.label)} onClick={onClose}>
            {opened.label} <span aria-hidden="true">×</span>
          </button>
        </div>
      ) : null}
      <span className="flex-1" />
      {impact.on ? (
        <div className="lc-zoom" role="group" aria-label={t.graph.toolbar.impactDirection}>
          <button type="button" aria-pressed={impact.direction === 'DEPENDENTS'} onClick={() => impact.setDirection('DEPENDENTS')}>{t.graph.toolbar.dependents}</button>
          <button type="button" aria-pressed={impact.direction === 'NEEDS'} onClick={() => impact.setDirection('NEEDS')}>{t.graph.toolbar.needs}</button>
        </div>
      ) : null}
      <button className={'lc-btn' + (impact.on ? ' lc-btn--primary' : '')} type="button" aria-pressed={impact.on}
        onClick={() => {
          // The impact reads the whole graph: the opened zone closes.
          if (!impact.on && p.open) onClose()
          impact.toggle()
        }}>{t.graph.toolbar.impact}</button>
      <div className="lc-zoom" role="group" aria-label={t.graph.toolbar.lens}>
        {lenses.map((l) => (
          <button key={l.key} type="button" aria-pressed={l.key === p.lens.key} onClick={() => p.set({ lens: l.key, open: null })}>{t.graph.lenses[l.key]}</button>
        ))}
      </div>
      <button className="lc-btn" type="button" onClick={onFit}>{t.graph.toolbar.fit}</button>
      <button className="lc-btn" type="button" title={t.graph.toolbar.relayoutTitle} onClick={onRelayout}>{t.graph.toolbar.relayout}</button>
    </div>
  )
}

// Legend tells the colors of the cards and the lines of the links.
export function Legend() {
  const t = useT()
  const l = t.graph.legend
  const line = (label: string, dash?: string) => (
    <span className="lc-edge text-xs">
      <svg viewBox="0 0 56 14" aria-hidden="true" style={{ width: 36 }}>
        <line x1="0" y1="7" x2="48" y2="7" style={{ strokeDasharray: dash }} /><polygon points="48,3 56,7 48,11" />
      </svg>{label}
    </span>
  )
  const dot = (v: string, label: string, dashed = false) => (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span className="size-2.5 rounded-[3px]" style={{ border: `1.5px ${dashed ? 'dashed' : 'solid'} var(${v})`, background: dashed ? 'transparent' : `var(${v}-tint)` }} />{label}
    </span>
  )
  return (
    <div className="absolute bottom-4 left-4 z-10 flex max-w-[calc(100%-120px)] flex-wrap items-center gap-x-5 gap-y-2 rounded-md border border-border bg-surface-raised px-3.5 py-2.5 shadow-panel">
      {dot('--kind-app', l.app)}{dot('--kind-service', l.service)}{dot('--kind-proxy', l.proxy)}{dot('--kind-host', l.host)}{dot('--kind-external', l.external, true)}
      <span className="h-[18px] w-px bg-border" />
      <span className="inline-flex items-center gap-1.5 text-xs">
        <span className="inline-flex gap-0.5">{Array.from({ length: 8 }, (_, i) => <span key={i} className="h-1 w-3.5 rounded-sm" style={{ background: `var(--link-${i + 1})` }} />)}</span>
        {l.color}
      </span>
      {line(l.exact)}{line(l.inferred, '6 4')}{line(l.ambiguous, '1 4')}
    </div>
  )
}
