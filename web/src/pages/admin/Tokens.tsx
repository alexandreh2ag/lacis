import { useMutation, useQuery } from '@apollo/client'
import { useEffect, useId, useState } from 'react'
import { Link } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { ops } from '@/components/VersionFilter'
import { config } from '@/config'
import { graphql } from '@/graphql'
import type { AdminAgentsQuery, AdminTokensQuery, VersionOp } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { ago, formatDate, formatDateTime } from '@/lib/format'

const TokensDoc = graphql(`
  query AdminTokens {
    enrollmentTokens { id label createdAt expiresAt revokedAt agents }
  }
`)

const CreateTokenDoc = graphql(`
  mutation CreateEnrollmentToken($label: String, $ttl: Int!) {
    createEnrollmentToken(label: $label, ttl: $ttl) { secret token { id } }
  }
`)

const RevokeTokenDoc = graphql(`
  mutation RevokeEnrollmentToken($id: ID!) {
    revokeEnrollmentToken(id: $id) { id revokedAt }
  }
`)

const AgentsDoc = graphql(`
  query AdminAgents {
    agents { id createdAt lastSeenAt revokedAt tokenLabel conflictAt conflictHostnames host { id hostname namespace agentVersion } }
  }
`)

const RevokeAgentDoc = graphql(`
  mutation RevokeAgent($id: ID!) {
    revokeAgent(id: $id) { id revokedAt }
  }
`)

type Token = AdminTokensQuery['enrollmentTokens'][number]
type Agent = AdminAgentsQuery['agents'][number]

type TokenState = 'active' | 'expired' | 'revoked'

// tokenState tells whether a token still enrolls agents at now.
export function tokenState(t: { expiresAt?: string | null; revokedAt?: string | null }, now = Date.now()): TokenState {
  if (t.revokedAt) return 'revoked'
  if (t.expiresAt && Date.parse(t.expiresAt) <= now) return 'expired'
  return 'active'
}

const states: TokenState[] = ['active', 'expired', 'revoked']

function StateBadge({ state }: { state: TokenState }) {
  const b = useT().admin.tokens.badges
  if (state === 'revoked') return <span className="lc-badge lc-badge--error">{b.revoked}</span>
  if (state === 'expired') return <span className="lc-badge lc-badge--inactive">{b.expired}</span>
  return <span className="lc-badge">{b.active}</span>
}

// lifetimes are the validities offered, in days; 0 never expires.
const lifetimes = [1, 7, 30, 90, 0]

// Tokens lists the enrollment tokens; they enroll agents into any
// namespace, the one of their label.
export function Tokens({ write }: { write: boolean }) {
  const { data, error } = useQuery(TokensDoc)
  const [picked, setPicked] = useState<string[]>([])
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'revoke'; token: Token } | null>(null)
  const t = useT()
  const k = t.admin.tokens
  const rows = (data?.enrollmentTokens ?? []).filter((x) => !picked.length || picked.includes(tokenState(x)))
  const tbl = useTable(rows, (x) => [x.label ?? ''],
    (x, by) => by === 'label' ? x.label ?? '' : by === 'agents' ? x.agents : by === 'expires' ? x.expiresAt ?? '9999' : x.createdAt, 'created', true)
  const head = (label: string, by: string, num?: boolean) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} num={num} />

  if (error) return <p role="alert">{k.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">{k.intro}</p>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={k.search} width={260} />}
            fields={[chipsField({ label: k.state, options: states.map((s) => ({ value: s, label: k.states[s] })), selected: picked, onChange: setPicked })]}
            count={k.count(tbl.list.length)} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>{k.create}</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(k.label, 'label')}{head(t.admin.createdOn, 'created')}{head(k.expires, 'expires')}<th>{k.state}</th>{head(k.agents, 'agents', true)}
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((x) => {
              const state = tokenState(x)
              return (
                <tr key={x.id}>
                  <td>{x.label ? <span className="font-semibold">{x.label}</span> : <span className="lc-muted">{t.admin.noLabel}</span>}</td>
                  <td>{formatDate(x.createdAt)}</td>
                  <td>{x.expiresAt ? <span title={formatDateTime(x.expiresAt)}>{formatDate(x.expiresAt)}</span> : <span className="lc-muted">{t.admin.never}</span>}</td>
                  <td><StateBadge state={state} /></td>
                  <td className="lc-num">{x.agents}</td>
                  {write ? (
                    <td className="lc-num">
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={state === 'revoked'}
                        onClick={() => setDialog({ kind: 'revoke', token: x })}>{t.admin.revoke}</button>
                    </td>
                  ) : null}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={k.noun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {dialog?.kind === 'create' ? <CreateToken onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'revoke' ? <RevokeToken token={dialog.token} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

function CreateToken({ onClose }: { onClose: () => void }) {
  const [create] = useMutation(CreateTokenDoc, { refetchQueries: ['AdminTokens'] })
  const a = useAction()
  const [label, setLabel] = useState('')
  const [ttl, setTtl] = useState(7 * 86400)
  const [secret, setSecret] = useState<string | null>(null)
  const t = useT()
  const k = t.admin.tokens
  if (secret) {
    const h = k.howTo
    return (
      <Dialog title={k.created} onClose={onClose}>
        <div className="lc-alert lc-alert--warn">{k.copyNow}</div>
        <div className="lc-secret"><code>{secret}</code><CopyButton value={secret} /></div>
        <p className="lc-muted m-0 text-[13px]">{h[0]}<code className="lc-mono">auth.enrollment_token</code>{h[1]}<code className="lc-mono">agent.yaml</code>
          {h[2]}<code className="lc-mono">LACIS_AUTH_ENROLLMENT_TOKEN</code>{h[3]}<code className="lc-mono">lacis-agent start</code>{h[4]}</p>
      </Dialog>
    )
  }
  return (
    <Dialog title={k.createTitle} submit={t.admin.create} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(async () => {
        const res = await create({ variables: { label: label.trim() || null, ttl } })
        setSecret(res.data?.createEnrollmentToken.secret ?? null)
      })}>
      <Field label={k.label} hint={k.labelHint}>
        {(id) => <input id={id} className="lc-input" value={label} onChange={(e) => setLabel(e.target.value)} />}
      </Field>
      <Field label={k.lifetime}>
        {(id) => (
          <select id={id} className="lc-select" value={ttl} onChange={(e) => setTtl(Number(e.target.value))}>
            {lifetimes.map((d) => <option key={d} value={d * 86400}>{d === 0 ? k.lifetimes.never : d === 1 ? k.lifetimes.day : k.lifetimes.days(d)}</option>)}
          </select>
        )}
      </Field>
    </Dialog>
  )
}

function RevokeToken({ token, onClose }: { token: Token; onClose: () => void }) {
  const [revoke] = useMutation(RevokeTokenDoc, { refetchQueries: ['AdminTokens'] })
  const t = useT()
  const a = useAction()
  return (
    <Dialog title={t.admin.tokens.revokeTitle(token.label)} submit={t.admin.revoke} danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => revoke({ variables: { id: token.id } }), onClose)}>
      <p className="m-0">{t.admin.tokens.revokeText(token.agents)}</p>
    </Dialog>
  )
}

// versionParts are the numbers of a version, as techversion reads them:
// 0.0.6-rc1 gives 0, 0, 6, 1.
export function versionParts(version: string): number[] {
  return (version.match(/\d+/g) ?? []).map(Number)
}

// versionOrder compares two versions on all their parts, to sort them; a
// version without number comes first.
export function versionOrder(a: string, b: string): number {
  const pa = versionParts(a)
  const pb = versionParts(b)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? -1) - (pb[i] ?? -1)
    if (d) return Math.sign(d)
  }
  return 0
}

// versionSatisfies tells whether version meets op constraint, the version
// cut to the parts of the constraint so that 0.0.6 equals 0.0; a constraint
// without number takes every version, a version without number none.
export function versionSatisfies(version: string, op: VersionOp, constraint: string): boolean {
  const c = versionParts(constraint)
  if (!c.length) return true
  const v = versionParts(version)
  if (!v.length) return false
  let d = 0
  for (let i = 0; i < c.length && !d; i++) d = Math.sign((v[i] ?? 0) - c[i])
  return op === 'LT' ? d < 0 : op === 'LTE' ? d <= 0 : op === 'EQ' ? d === 0 : op === 'GT' ? d > 0 : d >= 0
}

// Drift is how the version of an agent stands to the one of the server.
export type Drift = 'same' | 'older' | 'newer' | 'other' | 'unknown'

const bare = (v: string) => v.trim().replace(/^v/i, '')

// agentDrift tells how version stands to server: unknown without version,
// other when they differ without being ordered (a dev build).
export function agentDrift(version: string | null | undefined, server: string): Drift {
  if (!version) return 'unknown'
  if (bare(version) === bare(server)) return 'same'
  if (!versionParts(version).length || !versionParts(server).length) return 'other'
  const d = versionOrder(bare(version), bare(server))
  return d < 0 ? 'older' : d > 0 ? 'newer' : 'other'
}

// versionKey sorts versions as text: 0.0.10 after 0.0.9.
const versionKey = (v: string | null | undefined) => v ? versionParts(v).map((n) => String(n).padStart(8, '0')).join('.') + bare(v) : ''

interface VersionPick {
  op: VersionOp
  version: string
}

// AgentVersionControl picks a comparison and the version it compares to:
// < 0.0.6. The versions seen fill the version in one click.
function AgentVersionControl({ seen, value, onChange }: { seen: [string, number][]; value: VersionPick | null; onChange: (v: VersionPick | null) => void }) {
  const t = useT().components.version
  const id = useId()
  const [op, setOp] = useState<VersionOp>(value?.op ?? 'LT')
  const [version, setVersion] = useState(value?.version ?? '')
  useEffect(() => setVersion(value?.version ?? ''), [value?.version])
  // The version follows the input after a short pause.
  useEffect(() => {
    if (version === (value?.version ?? '')) return
    const timer = setTimeout(() => onChange(version.trim() ? { op, version: version.trim() } : null), 400)
    return () => clearTimeout(timer)
  }, [version, op, value, onChange])
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <label htmlFor={`${id}-op`} className="sr-only">{t.comparison}</label>
        <select id={`${id}-op`} className="lc-input w-[60px] font-mono" value={op}
          onChange={(e) => {
            const next = e.target.value as VersionOp
            setOp(next)
            if (value) onChange({ ...value, op: next })
          }}>
          {ops.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <label htmlFor={`${id}-version`} className="sr-only">{t.version}</label>
        <input id={`${id}-version`} className="lc-input w-[110px] font-mono" placeholder="0.0.6" value={version}
          onChange={(e) => setVersion(e.target.value)} />
      </div>
      {seen.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[12px] text-ink-muted">{t.seen}</span>
          {seen.map(([v, n]) => (
            <button key={v} type="button" className="lc-chip font-mono" aria-pressed={value?.version === v}
              onClick={() => onChange({ op, version: v })}>{v} ({n})</button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function DriftBadge({ drift, server }: { drift: Drift; server: string }) {
  const g = useT().admin.agents
  if (drift === 'older') return <span className="lc-badge lc-badge--stale" title={g.olderHint(server)}>{g.older}</span>
  if (drift === 'newer') return <span className="lc-badge lc-badge--inactive" title={g.newerHint(server)}>{g.newer}</span>
  return null
}

// Agents lists the enrolled agents, the host of their snapshots and the
// version they run, to find the ones the server is ahead of.
export function Agents({ write, server = config.version }: { write: boolean; server?: string }) {
  const { data, error } = useQuery(AgentsDoc)
  const [states, setStates] = useState<string[]>([])
  const [version, setVersion] = useState<VersionPick | null>(null)
  const [differs, setDiffers] = useState(false)
  const [revoking, setRevoking] = useState<Agent | null>(null)
  const t = useT()
  const g = t.admin.agents
  const all = data?.agents ?? []
  const counts = new Map<string, number>()
  for (const x of all) if (x.host?.agentVersion) counts.set(x.host.agentVersion, (counts.get(x.host.agentVersion) ?? 0) + 1)
  const seen = [...counts].sort((a, b) => versionOrder(b[0], a[0]) || b[0].localeCompare(a[0]))
  const rows = all.filter((x) => !states.length || states.includes(x.revokedAt ? 'revoked' : 'active')
    || (states.includes('conflict') && !!x.conflictAt))
    .filter((x) => !version || (!!x.host?.agentVersion && versionSatisfies(x.host.agentVersion, version.op, version.version)))
    .filter((x) => !differs || (!!x.host && agentDrift(x.host.agentVersion, server) !== 'same'))
  const tbl = useTable(rows, (x) => [x.host?.hostname ?? '', x.host?.namespace ?? '', x.tokenLabel ?? '', x.host?.agentVersion ?? ''],
    (x, by) => by === 'host' ? x.host?.hostname ?? '' : by === 'namespace' ? x.host?.namespace ?? '' : by === 'version' ? versionKey(x.host?.agentVersion)
      : by === 'seen' ? x.lastSeenAt ?? '' : x.createdAt, 'created', true)
  const opLabel = ops.find((o) => o.value === version?.op)?.label ?? '<'
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />

  if (error) return <p role="alert">{g.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={g.search} width={300} />}
        fields={[chipsField({ label: t.admin.tokens.state, options: (['active', 'revoked', 'conflict'] as const).map((s) => ({ value: s, label: g.states[s] })),
          selected: states, onChange: setStates }),
        { label: g.versionFilter, short: g.version, value: version ? `${opLabel} ${version.version}` : null, clear: () => setVersion(null),
          control: <AgentVersionControl seen={seen} value={version} onChange={setVersion} /> }]}
        count={g.count(tbl.list.length)}
        actions={<button type="button" className={'lc-btn' + (differs ? ' lc-btn--primary' : '')} aria-pressed={differs} title={g.differsHint}
          onClick={() => setDiffers(!differs)}>{g.differs(server)}</button>} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(g.host, 'host')}{head(g.namespace, 'namespace')}{head(g.version, 'version')}<th>{g.token}</th>{head(g.enrolled, 'created')}{head(g.lastSeen, 'seen')}<th>{t.admin.tokens.state}</th>
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((x) => (
              <tr key={x.id}>
                <td>{x.host ? (
                  <span className="lc-copyable"><Link className="lc-mono font-semibold" to={`/${x.host.namespace}/hosts/${x.host.id}`}>{x.host.hostname}</Link><CopyButton value={x.host.hostname} /></span>
                ) : <span className="lc-muted">{g.waiting}</span>}</td>
                <td>{x.host ? <Link to={`/${x.host.namespace}`}>{x.host.namespace}</Link> : <span className="lc-muted">—</span>}</td>
                <td>{x.host?.agentVersion ? (
                  <span className="inline-flex flex-wrap items-center gap-1.5"><span className="lc-mono">{x.host.agentVersion}</span>
                    <DriftBadge drift={agentDrift(x.host.agentVersion, server)} server={server} /></span>
                ) : <span className="lc-muted">{x.host ? g.unknown : '—'}</span>}</td>
                <td>{x.tokenLabel ?? <span className="lc-muted">{t.admin.noLabel}</span>}</td>
                <td>{formatDate(x.createdAt)}</td>
                <td>{x.lastSeenAt ? <span title={formatDateTime(x.lastSeenAt)}>{ago(x.lastSeenAt)}</span> : <span className="lc-muted">{t.admin.never}</span>}</td>
                <td><span className="inline-flex flex-wrap gap-1.5">
                  {x.revokedAt ? <span className="lc-badge lc-badge--error" title={formatDateTime(x.revokedAt)}>{t.admin.tokens.badges.revoked}</span>
                    : <span className="lc-badge">{t.admin.tokens.badges.active}</span>}
                  {x.conflictAt ? (
                    <span className="lc-badge lc-badge--stale" title={g.conflictHint(formatDateTime(x.conflictAt), x.conflictHostnames ?? [])}>{g.conflict}</span>
                  ) : null}
                </span></td>
                {write ? (
                  <td className="lc-num">
                    <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={!!x.revokedAt} onClick={() => setRevoking(x)}>{t.admin.revoke}</button>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={g.noun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {revoking ? <RevokeAgent agent={revoking} onClose={() => setRevoking(null)} /> : null}
    </>
  )
}

function RevokeAgent({ agent, onClose }: { agent: Agent; onClose: () => void }) {
  const [revoke] = useMutation(RevokeAgentDoc, { refetchQueries: ['AdminAgents'] })
  const a = useAction()
  const t = useT()
  const g = t.admin.agents
  const name = agent.host?.hostname ?? g.thisAgent
  return (
    <Dialog title={g.revokeTitle(name)} submit={t.admin.revoke} danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => revoke({ variables: { id: agent.id } }), onClose)}>
      <p className="m-0">{g.revokeText}{agent.host ? g.revokeHost : ''}</p>
    </Dialog>
  )
}
