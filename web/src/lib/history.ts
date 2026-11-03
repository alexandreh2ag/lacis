import { t } from '@/i18n'

// describe writes a change event of the history in the language spoken: a
// title and the fields that changed, before → after.

export interface ChangeEvent {
  id: number
  occurredAt: string
  hostname?: string | null
  entityType: string
  kind: string
  before?: unknown
  after?: unknown
}

export interface Diff {
  field: string
  before: string
  after: string
}

export interface Described {
  title: string
  // subject is the name of the entity, written in bold.
  subject: string
  diffs: Diff[]
  detail?: string
}

type State = Record<string, unknown>

const identity = new Set(['hostname', 'name', 'path', 'type', 'instance', 'from', 'to', 'to_type', 'key', 'source'])
// noise are the fields a change always moves along: not worth telling.
const noise = /(^|\.)(deployed_at|pipeline_id|pipeline_url|deployed_by)$/

function flatten(v: unknown, prefix = '', out: Record<string, string> = {}): Record<string, string> {
  if (v && typeof v === 'object' && !Array.isArray(v)) {
    for (const [k, x] of Object.entries(v as State)) flatten(x, prefix ? `${prefix}.${k}` : k, out)
  } else if (v !== undefined && v !== null && prefix) {
    out[prefix] = Array.isArray(v) ? v.join(', ') : String(v)
  }
  return out
}

function parse(v: unknown): State {
  if (typeof v === 'string') {
    try { return JSON.parse(v) as State } catch { return {} }
  }
  return (v ?? {}) as State
}

export function diffs(before: unknown, after: unknown): Diff[] {
  const b = flatten(parse(before))
  const a = flatten(parse(after))
  const keys = [...new Set([...Object.keys(b), ...Object.keys(a)])].filter((k) => !identity.has(k) && !noise.test(k) && b[k] !== a[k])
  // A deployment tells its version and commit first.
  const rank = (k: string) => ['code.version', 'version', 'code.commit', 'code.branch'].indexOf(k) + 1 || 99
  keys.sort((x, y) => rank(x) - rank(y) || x.localeCompare(y))
  return keys.slice(0, 4).map((k) => ({ field: t().components.history.fields[k] ?? k.replace(/_/g, ' '), before: b[k] ?? '', after: a[k] ?? '' }))
}

export function describe(e: ChangeEvent): Described {
  const s = e.kind === 'removed' ? parse(e.before) : parse(e.after)
  const name = (k: string) => (typeof s[k] === 'string' ? (s[k] as string) : '')
  let subject = ''
  switch (e.entityType) {
    case 'host': subject = name('hostname') || e.hostname || 'host'; break
    case 'app': subject = name('name') || name('path'); break
    case 'service': subject = [name('type'), name('instance') !== 'default' ? name('instance') : ''].filter(Boolean).join(' '); break
    case 'webserver': case 'runtime': subject = name('type'); break
    case 'link': subject = [name('from'), name('to')].filter(Boolean).join(' → '); break
  }
  const h = t().components.history
  const verb = e.entityType === 'app' && e.kind === 'changed' ? h.deployed
    : e.kind === 'added' ? h.added
      : e.kind === 'removed' ? h.removed
        : h.updated
  const detail = e.entityType === 'link' ? [name('key'), name('source')].filter(Boolean).join(' · ') : undefined
  return { title: h.title(h.entities[e.entityType] ?? e.entityType, verb), subject, diffs: e.kind === 'changed' ? diffs(e.before, e.after) : [], detail }
}
