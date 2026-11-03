import { useMemo } from 'react'
import { useSearchParams } from 'react-router'
import type { GraphFilter, HostStatus, LinkConfidence } from '@/graphql/graphql'

// A lens is a way to read the namespace: its projects, to understand
// and document it, or its hosts, for incidents and changes. Both open
// on the same content: apps, services, webservers and runtimes.
export const lenses = [
  { key: 'projects', level: 'PROJECT' },
  { key: 'infra', level: 'HOST' },
] as const

export type Lens = (typeof lenses)[number]

export type GraphParams = ReturnType<typeof useGraphParams>

// useGraphParams keeps the lens, the opened zone and the filters of the
// graph in the URL.
export function useGraphParams() {
  const [params, setParams] = useSearchParams()
  return useMemo(() => {
    // level=host is the address of the graph before the lenses.
    const lens: Lens = lenses.find((l) => l.key === params.get('lens')) ?? (params.get('level') === 'host' ? lenses[1] : lenses[0])
    const statuses = params.getAll('status') as HostStatus[]
    const p = {
      lens,
      // open is the zone opened: one at a time.
      open: params.get('open'),
      envs: params.getAll('env'),
      projects: params.getAll('project'),
      hosts: params.getAll('host'),
      statuses: statuses.length ? statuses : (['ACTIVE', 'STALE'] as HostStatus[]),
      confidences: params.getAll('conf') as LinkConfidence[],
      externals: params.get('ext') !== '0',
      // isolate only means something with envs picked.
      isolate: params.get('iso') === '1' && params.getAll('env').length > 0,
      set(changes: Record<string, string | string[] | null>) {
        setParams((prev) => {
          const next = new URLSearchParams(prev)
          next.delete('level')
          for (const [k, v] of Object.entries(changes)) {
            next.delete(k)
            if (Array.isArray(v)) v.forEach((x) => next.append(k, x))
            else if (v !== null) next.set(k, v)
          }
          return next
        })
      },
    }
    // filter is the filter of the graph and impact queries.
    const filter: GraphFilter = { envs: p.envs, projects: p.projects, hosts: p.hosts, status: p.statuses, confidences: p.confidences,
      externals: p.externals, isolate: p.isolate, expand: p.open ? [p.open] : [] }
    // viewKey changes with what the graph shows, not with the zone opened:
    // a new view fits the viewport again.
    const viewKey = JSON.stringify([lens.key, p.envs, p.projects, p.hosts, p.statuses, p.confidences, p.externals, p.isolate])
    return { ...p, filter, viewKey }
  }, [params, setParams])
}
