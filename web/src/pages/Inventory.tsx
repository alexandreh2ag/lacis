import { useQuery } from '@apollo/client'
import { Link, useParams } from 'react-router'
import { Copyable, CopyButton } from '@/components/Copy'
import { TechLogo } from '@/components/Icon'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar, pickerField } from '@/components/FilterBar'
import { constraintOf, versionField, versionParam, versionParamChanges } from '@/components/VersionFilter'
import { Consumers } from '@/components/UsedBy'
import { envOptions, Pager, SearchInput, SortHeader, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AppSortField, ServiceSortField } from '@/graphql/graphql'

const AppsDoc = graphql(`
  query Apps($ns: String!, $filter: AppFilter, $sort: AppSort, $page: PageInput) {
    namespace(slug: $ns) {
      id
      environments { id slug sortOrder }
      projects { id slug }
      facets { frameworks { name versions } runtimes { name versions } }
      apps(filter: $filter, sort: $sort, page: $page) {
        total
        items { id name path framework frameworkVersion vhosts project { slug } environment { slug } host { id hostname } code { version commit } runtime { type version } }
      }
    }
  }
`)

const ServicesDoc = graphql(`
  query Services($ns: String!, $filter: ServiceFilter, $sort: ServiceSort, $page: PageInput) {
    namespace(slug: $ns) {
      id
      environments { id slug sortOrder }
      facets { serviceTypes }
      services(filter: $filter, sort: $sort, page: $page) {
        total
        items { id type instance version host { id hostname project { slug } environment { slug } } dependents { ...UsedByLink } }
      }
    }
  }
`)

function HostLink({ ns, host }: { ns: string; host: { id: string; hostname: string } }) {
  return <span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${host.id}`}>{host.hostname}</Link><CopyButton value={host.hostname} /></span>
}

export function Apps() {
  const { ns = '' } = useParams()
  const l = useListParams({ sort: 'NAME' })
  const envs = l.list('env')
  const projects = l.list('project')
  const framework = versionParam(l.get, 'fw')
  const runtime = versionParam(l.get, 'rt')
  const { data, error } = useQuery(AppsDoc, {
    variables: { ns, filter: { search: l.q, envs, projects, framework: constraintOf(framework), runtime: constraintOf(runtime) }, sort: { by: l.sort as AppSortField, desc: l.desc }, page: { offset: (l.page - 1) * l.size, limit: l.size } },
  })
  const list = data?.namespace?.apps
  const onSort = (by: string) => l.toggleSort(by, l.sort, l.desc)
  const head = (label: string, by: AppSortField) => <SortHeader label={label} by={by} sort={l.sort} desc={l.desc} onSort={onSort} />
  return (
    <Page title="Apps">
      <FilterBar
        search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder="Chercher une app, un chemin, une version, un framework, un host" />}
        fields={[
          chipsField({ label: 'Environnement', short: 'Env', options: envOptions(data?.namespace?.environments), selected: envs, onChange: (v) => l.set({ env: v }) }),
          pickerField({ label: 'Projet', choose: 'Choisir un projet', options: (data?.namespace?.projects ?? []).map((x) => x.slug), selected: projects, onChange: (v) => l.set({ project: v }) }),
          versionField({ label: 'Framework', facets: data?.namespace?.facets.frameworks ?? [], value: framework, onChange: (v) => l.set(versionParamChanges('fw', v)) }),
          versionField({ label: 'Runtime', facets: data?.namespace?.facets.runtimes ?? [], value: runtime, onChange: (v) => l.set(versionParamChanges('rt', v)) }),
        ]}
        onClearAll={() => l.set({ env: null, project: null, ...versionParamChanges('fw', null), ...versionParamChanges('rt', null) })}
        count={list ? `${list.total} apps` : ''}
      />

      {error ? <p role="alert">{error.message}</p> : null}
      <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
        <thead><tr>{head('App', 'NAME')}{head('Projet', 'PROJECT')}{head('Env', 'ENV')}{head('Host', 'HOSTNAME')}{head('Version', 'VERSION')}{head('Framework', 'FRAMEWORK')}<th>Domaine</th></tr></thead>
        <tbody>{(list?.items ?? []).map((a) => (
          <tr key={a.id}>
            <td><span className="inline-flex items-center gap-2 font-semibold"><span className="lc-node__techs"><TechLogo tech={a.framework ?? 'app'} /></span><Link to={`/${ns}/apps/${a.id}`}>{a.name}</Link></span></td>
            <td>{a.project ? <Link to={`/${ns}/projects/${a.project.slug}`}>{a.project.slug}</Link> : '—'}</td>
            <td>{a.environment?.slug}</td>
            <td><HostLink ns={ns} host={a.host} /></td>
            <td>{a.code?.version ? <Copyable value={a.code.version} /> : null}</td>
            <td className="text-ink-muted">{[a.framework, a.frameworkVersion].filter(Boolean).join(' ')}</td>
            <td>{a.vhosts[0] ? <Copyable value={a.vhosts[0]} /> : null}</td>
          </tr>
        ))}</tbody>
      </table></div>
      {list ? <Pager page={l.page} size={l.size} total={list.total} noun="apps" onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} /> : null}
    </Page>
  )
}

export function Services() {
  const { ns = '' } = useParams()
  const l = useListParams({ sort: 'TYPE' })
  const envs = l.list('env')
  const types = l.list('type')
  const { data, error } = useQuery(ServicesDoc, {
    variables: { ns, filter: { search: l.q, envs, types }, sort: { by: l.sort as ServiceSortField, desc: l.desc }, page: { offset: (l.page - 1) * l.size, limit: l.size } },
  })
  const list = data?.namespace?.services
  const onSort = (by: string) => l.toggleSort(by, l.sort, l.desc)
  const head = (label: string, by: ServiceSortField) => <SortHeader label={label} by={by} sort={l.sort} desc={l.desc} onSort={onSort} />
  return (
    <Page title="Services">
      <FilterBar
        search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder="Chercher un type, une version, un host" />}
        fields={[
          pickerField({ label: 'Type', choose: 'Choisir un type', options: data?.namespace?.facets.serviceTypes ?? [], selected: types, onChange: (v) => l.set({ type: v }) }),
          chipsField({ label: 'Environnement', short: 'Env', options: envOptions(data?.namespace?.environments), selected: envs, onChange: (v) => l.set({ env: v }) }),
        ]}
        onClearAll={() => l.set({ env: null, type: null })}
        count={list ? `${list.total} services` : ''}
      />
      {error ? <p role="alert">{error.message}</p> : null}
      <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
        <thead><tr>{head('Service', 'TYPE')}<th>Instance</th>{head('Version', 'VERSION')}{head('Host', 'HOSTNAME')}<th>Projet</th><th>Env</th><th>Utilisé par</th></tr></thead>
        <tbody>{(list?.items ?? []).map((s) => (
          <tr key={s.id}>
            <td><span className="inline-flex items-center gap-2 font-semibold"><span className="lc-node__techs"><TechLogo tech={s.type} /></span>{s.type}</span></td>
            <td className="lc-mono text-ink-muted">{s.instance !== 'default' ? s.instance : ''}</td>
            <td className="lc-mono">{s.version}</td>
            <td><HostLink ns={ns} host={s.host} /></td>
            <td>{s.host.project ? <Link to={`/${ns}/projects/${s.host.project.slug}`}>{s.host.project.slug}</Link> : '—'}</td>
            <td>{s.host.environment?.slug}</td>
            <td style={{ whiteSpace: 'normal', minWidth: 200 }}><Consumers ns={ns} links={s.dependents} /></td>
          </tr>
        ))}</tbody>
      </table></div>
      {list ? <Pager page={l.page} size={l.size} total={list.total} noun="services" onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} /> : null}
    </Page>
  )
}
