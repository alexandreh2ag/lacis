import { useQuery } from '@apollo/client'
import { useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { envOptions, Pager, SearchInput, SortHeader, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'

const ProjectsDoc = graphql(`
  query Projects($ns: String!) {
    namespace(slug: $ns) {
      id
      environments { id slug sortOrder }
      graph(level: PROJECT, filter: { status: [ACTIVE, STALE, INACTIVE] }) {
        nodes { id kind label project env count apps }
        edges { from to }
      }
    }
  }
`)

export interface ProjectRow {
  slug: string
  envs: string[]
  hosts: number
  apps: number
  deps: string[]
}

interface ProjectGraph {
  nodes: { id: string; kind: string; label: string; project?: string | null; env?: string | null; count: number; apps: number }[]
  edges: { from: string; to: string }[]
}

// projectRows sums the project nodes of the graph by project: their envs,
// hosts and apps, and the projects they depend on.
export function projectRows(graph: ProjectGraph): ProjectRow[] {
  const by = new Map<string, ProjectRow>()
  for (const node of graph.nodes) {
    if (node.kind !== 'PROJECT') continue
    const slug = node.project || node.label
    const r = by.get(slug) ?? { slug, envs: [], hosts: 0, apps: 0, deps: [] }
    if (node.env) r.envs.push(node.env)
    r.hosts += node.count
    r.apps += node.apps
    by.set(slug, r)
  }
  const nodes = new Map(graph.nodes.map((x) => [x.id, x]))
  for (const e of graph.edges) {
    const from = nodes.get(e.from)
    const to = nodes.get(e.to)
    if (!from || !to || from.kind !== 'PROJECT') continue
    const r = by.get(from.project || from.label)
    const name = to.kind === 'PROJECT' ? to.label : 'externes'
    if (r && name !== r.slug && !r.deps.includes(name)) r.deps.push(name)
  }
  return [...by.values()]
}

// ProjectsTable lists the projects of a namespace, its search, filters,
// order and page in the URL.
export function ProjectsTable({ ns, rows, environments }: { ns: string; rows: ProjectRow[]; environments: { slug: string; sortOrder: number }[] }) {
  const l = useListParams({ sort: 'slug' })
  const envs = l.list('env')
  const q = l.q.toLowerCase()
  const list = rows.filter((r) => (!envs.length || r.envs.some((e) => envs.includes(e))) && (!q || [r.slug, ...r.deps].some((x) => x.includes(q))))
  const key = l.sort as keyof ProjectRow
  list.sort((a, b) => {
    const x = a[key]
    const y = b[key]
    const c = typeof x === 'number' ? x - (y as number) : String(x).localeCompare(String(y), 'fr', { numeric: true })
    return (l.desc ? -c : c) || a.slug.localeCompare(b.slug)
  })
  const onSort = (by: string) => l.toggleSort(by, l.sort, l.desc)
  return (
    <>
      <FilterBar
        search={<SearchInput value={l.q} onChange={(v) => l.set({ q: v })} placeholder="Chercher un projet, une dépendance" width={300} />}
        fields={[chipsField({ label: 'Environnement', short: 'Env', options: envOptions(environments), selected: envs, onChange: (v) => l.set({ env: v }) })]}
        count={`${list.length} projets`}
      />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            <SortHeader label="Projet" by="slug" sort={l.sort} desc={l.desc} onSort={onSort} />
            <th>Envs</th>
            <SortHeader label="Hosts" by="hosts" sort={l.sort} desc={l.desc} onSort={onSort} num />
            <SortHeader label="Apps" by="apps" sort={l.sort} desc={l.desc} onSort={onSort} num />
            <th>Dépend de</th>
          </tr></thead>
          <tbody>
            {list.slice((l.page - 1) * l.size, l.page * l.size).map((r) => (
              <tr key={r.slug}>
                <td><span className="lc-copyable"><Link className="lc-mono font-semibold" to={`/${ns}/projects/${r.slug}`}>{r.slug || 'sans projet'}</Link>{r.slug ? <CopyButton value={r.slug} /> : null}</span></td>
                <td>{r.envs.join(', ')}</td>
                <td className="lc-num">{r.hosts}</td>
                <td className="lc-num">{r.apps}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 220 }}>{r.deps.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={l.page} size={l.size} total={list.length} noun="projets" onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} />
    </>
  )
}

export function Projects() {
  const { ns = '' } = useParams()
  const { data, error } = useQuery(ProjectsDoc, { variables: { ns } })
  const n = data?.namespace
  const rows = useMemo(() => (n ? projectRows(n.graph) : []), [n])
  return (
    <Page title="Projets">
      {error ? <p role="alert">Les projets n'ont pas pu être chargés : {error.message}</p> : null}
      {n ? <ProjectsTable ns={ns} rows={rows} environments={n.environments} /> : !error ? <span className="lc-muted">Chargement…</span> : null}
    </Page>
  )
}
