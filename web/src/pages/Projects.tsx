import { useQuery } from '@apollo/client'
import { useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { envOptions, Pager, SearchInput, SortHeader, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'
import { t as text, useT } from '@/i18n'

const ProjectsDoc = graphql(`
  query Projects($ns: String!) {
    namespace(slug: $ns) {
      id
      environments { id slug sortOrder }
      projects { ...ProjectRowFields }
    }
  }
  fragment ProjectRowFields on Project {
    id slug
    usage { hosts }
    appCount
    envs { slug }
    dependsOn { slug }
    usesExternals
  }
`)

export interface ProjectRow {
  slug: string
  envs: string[]
  hosts: number
  apps: number
  deps: string[]
}

interface CatalogProject {
  slug: string
  usage: { hosts: number }
  appCount: number
  envs: { slug: string }[]
  dependsOn: { slug: string }[]
  usesExternals: boolean
}

// projectRows are the projects of the catalog with their envs, hosts and
// apps, and what they depend on: a project is listed even when its apps run
// on the hosts of another one, or when nothing names it yet.
export function projectRows(projects: CatalogProject[]): ProjectRow[] {
  return projects.map((p) => ({
    slug: p.slug,
    envs: p.envs.map((e) => e.slug),
    hosts: p.usage.hosts,
    apps: p.appCount,
    deps: [...p.dependsOn.map((d) => d.slug), ...(p.usesExternals ? [text().pages.projects.externals] : [])],
  }))
}

// ProjectsTable lists the projects of a namespace, its search, filters,
// order and page in the URL.
export function ProjectsTable({ ns, rows, environments }: { ns: string; rows: ProjectRow[]; environments: { slug: string; sortOrder: number }[] }) {
  const t = useT()
  const tp = t.pages.projects
  const ts = t.pages.shared
  const l = useListParams({ sort: 'slug' })
  const envs = l.list('env')
  const q = l.q.toLowerCase()
  const list = rows.filter((r) => (!envs.length || r.envs.some((e) => envs.includes(e))) && (!q || [r.slug, ...r.deps].some((x) => x.toLowerCase().includes(q))))
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
        search={<SearchInput value={l.q} onChange={(v) => l.set({ q: v })} placeholder={tp.search} width={300} />}
        fields={[chipsField({ label: ts.environment, short: ts.env, options: envOptions(environments), selected: envs, onChange: (v) => l.set({ env: v }) })]}
        count={tp.count(list.length)}
      />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            <SortHeader label={ts.project} by="slug" sort={l.sort} desc={l.desc} onSort={onSort} />
            <th>{tp.envs}</th>
            <SortHeader label={tp.hosts} by="hosts" sort={l.sort} desc={l.desc} onSort={onSort} num />
            <SortHeader label={tp.apps} by="apps" sort={l.sort} desc={l.desc} onSort={onSort} num />
            <th>{tp.dependsOn}</th>
          </tr></thead>
          <tbody>
            {list.slice((l.page - 1) * l.size, l.page * l.size).map((r) => (
              <tr key={r.slug}>
                <td><span className="lc-copyable"><Link className="lc-mono font-semibold" to={`/${ns}/projects/${r.slug}`}>{r.slug || ts.noProject}</Link>{r.slug ? <CopyButton value={r.slug} /> : null}</span></td>
                <td>{r.envs.join(', ')}</td>
                <td className="lc-num">{r.hosts}</td>
                <td className="lc-num">{r.apps}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 220 }}>{r.deps.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={l.page} size={l.size} total={list.length} noun={ts.nouns.projects} onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} />
    </>
  )
}

export function Projects() {
  const { ns = '' } = useParams()
  const { data, error } = useQuery(ProjectsDoc, { variables: { ns } })
  const n = data?.namespace
  const rows = useMemo(() => (n ? projectRows(n.projects) : []), [n])
  const t = useT()
  return (
    <Page title={t.pages.projects.title}>
      {error ? <p role="alert">{t.pages.projects.failed(error.message)}</p> : null}
      {n ? <ProjectsTable ns={ns} rows={rows} environments={n.environments} /> : !error ? <span className="lc-muted">{t.pages.shared.loading}</span> : null}
    </Page>
  )
}
