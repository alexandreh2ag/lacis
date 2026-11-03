import { useQuery } from '@apollo/client'
import { useMemo, useState } from 'react'
import type { GraphFilter } from '@/graphql/graphql'
import { useT } from '@/i18n'
import type { Lit } from './highlight'
import { ImpactDoc, type GNode } from './query'
import type { Lens } from './useGraphParams'

export type ImpactDirection = 'DEPENDENTS' | 'NEEDS'

export interface ImpactView extends Lit {
  target: string
  // levels give each node reached its distance to the target.
  levels: Map<string, number>
}

// useImpact is the impact mode: a click picks the node whose impact
// shows, instead of opening it. The impact is what depends on the target,
// or what it needs, as the server walks it; the content of the opened
// zone counts as the zone.
export function useImpact(ns: string, lens: Lens, filter: GraphFilter, zoneOf: (id: string) => string) {
  const [on, setOn] = useState(false)
  const [target, setTarget] = useState<string | null>(null)
  const [direction, setDirection] = useState<ImpactDirection>('DEPENDENTS')
  const query = useQuery(ImpactDoc, {
    skip: !on || !target,
    variables: { ns, level: lens.level, filter, node: target ?? '', direction },
  })
  const data = query.data ?? query.previousData
  const impact = useMemo((): ImpactView | null => {
    const got = data?.namespace?.impact
    if (!on || !target || !got) return null
    const levels = new Map<string, number>()
    for (const n of got.nodes) {
      const id = zoneOf(n.id)
      const known = levels.get(id)
      if (id !== target && (known === undefined || n.level < known)) levels.set(id, n.level)
    }
    const edges = new Set(got.edges.map((e) => {
      const [from, to] = e.split('→')
      return `${zoneOf(from)}→${zoneOf(to)}`
    }))
    return { target, levels, edges, nodes: new Set([target, ...levels.keys()]) }
  }, [on, target, data, zoneOf])
  return {
    on, target, direction, impact,
    error: on && target ? query.error : undefined,
    setTarget, setDirection,
    toggle: () => {
      setOn(!on)
      setTarget(null)
    },
    quit: () => {
      setOn(false)
      setTarget(null)
    },
  }
}

// ImpactPanel lists what the impact reaches, level by level, and the
// projects it touches.
export function ImpactPanel({ target, impact, nodes, label, direction, error, onClose }: {
  target: string | null
  impact: { levels: Map<string, number> } | null
  nodes: Map<string, GNode>
  label: (id: string) => string
  direction: ImpactDirection
  error?: string
  onClose: () => void
}) {
  const t = useT()
  const i = t.graph.impact
  const byLevel = new Map<number, string[]>()
  for (const [id, level] of impact?.levels ?? []) byLevel.set(level, [...(byLevel.get(level) ?? []), id])
  const projects = [...new Set([...(impact?.levels.keys() ?? [])].map((id) => nodes.get(id)?.project).filter((x): x is string => !!x))].sort()
  return (
    <aside className="lc-panel absolute right-4 top-16 z-10 max-h-[calc(100%-180px)] w-[320px] overflow-auto" aria-label={i.title}>
      <div className="lc-panel__head">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="lc-panel__title text-[17px]">{i.title}</span>
          <span className="lc-muted text-[12.5px]">
            {target ? <>{direction === 'DEPENDENTS' ? i.dependentsOf : i.needsOf}<b className="font-mono">{label(target)}</b></> : i.pick}
          </span>
        </div>
        <button className="lc-btn lc-btn--icon lc-btn--ghost" type="button" aria-label={i.quit} onClick={onClose}>×</button>
      </div>
      {error ? <div className="lc-panel__section" role="alert">{t.graph.impactError(error)}</div> : null}
      {target && impact && impact.levels.size === 0 ? (
        <div className="lc-panel__section"><span className="lc-muted">{direction === 'DEPENDENTS' ? i.noDependents : i.noNeeds}</span></div>
      ) : null}
      {[...byLevel].map(([level, ids]) => (
        <div key={level} className="lc-panel__section">
          <span className="lc-label">{i.level(level)} · {ids.length}</span>
          {ids.map((id) => <span key={id} className="truncate font-mono text-[12.5px]">{label(id)}</span>)}
        </div>
      ))}
      {projects.length ? (
        <div className="lc-panel__section">
          <span className="lc-label">{i.projects} · {projects.length}</span>
          <span className="text-[13px]">{projects.join(', ')}</span>
        </div>
      ) : null}
    </aside>
  )
}
