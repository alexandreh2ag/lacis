import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { Link } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminAgentsQuery, AdminTokensQuery } from '@/graphql/graphql'
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
    agents { id createdAt lastSeenAt revokedAt tokenLabel conflictAt conflictHostnames host { id hostname namespace } }
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

// Agents lists the enrolled agents and the host of their snapshots.
export function Agents({ write }: { write: boolean }) {
  const { data, error } = useQuery(AgentsDoc)
  const [states, setStates] = useState<string[]>([])
  const [revoking, setRevoking] = useState<Agent | null>(null)
  const t = useT()
  const g = t.admin.agents
  const rows = (data?.agents ?? []).filter((x) => !states.length || states.includes(x.revokedAt ? 'revoked' : 'active')
    || (states.includes('conflict') && !!x.conflictAt))
  const tbl = useTable(rows, (x) => [x.host?.hostname ?? '', x.host?.namespace ?? '', x.tokenLabel ?? ''],
    (x, by) => by === 'host' ? x.host?.hostname ?? '' : by === 'namespace' ? x.host?.namespace ?? '' : by === 'seen' ? x.lastSeenAt ?? '' : x.createdAt, 'created', true)
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />

  if (error) return <p role="alert">{g.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={g.search} width={300} />}
        fields={[chipsField({ label: t.admin.tokens.state, options: (['active', 'revoked', 'conflict'] as const).map((s) => ({ value: s, label: g.states[s] })),
          selected: states, onChange: setStates })]}
        count={g.count(tbl.list.length)} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(g.host, 'host')}{head(g.namespace, 'namespace')}<th>{g.token}</th>{head(g.enrolled, 'created')}{head(g.lastSeen, 'seen')}<th>{t.admin.tokens.state}</th>
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((x) => (
              <tr key={x.id}>
                <td>{x.host ? (
                  <span className="lc-copyable"><Link className="lc-mono font-semibold" to={`/${x.host.namespace}/hosts/${x.host.id}`}>{x.host.hostname}</Link><CopyButton value={x.host.hostname} /></span>
                ) : <span className="lc-muted">{g.waiting}</span>}</td>
                <td>{x.host ? <Link to={`/${x.host.namespace}`}>{x.host.namespace}</Link> : <span className="lc-muted">—</span>}</td>
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
