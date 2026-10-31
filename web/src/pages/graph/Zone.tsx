import { Handle, Position, type NodeProps } from '@xyflow/react'
import { memo } from 'react'
import { Link } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Glyph } from '@/components/Icon'
import { StatusBadge } from '@/components/Listing'
import { ago } from '@/lib/format'
import { kindOf, title } from './kinds'
import type { GNode } from './query'
import { ZONE_PAD, type RowKey, type Zone } from './zones'

export interface ZoneData extends Record<string, unknown> {
  zone: Zone
  ns: string
  // other is the link to the zone in the other lens.
  other: { label: string; to: string } | null
  onClose: () => void
}

// pageOf is the page of a zone, on the tab listing a row.
export function pageOf(ns: string, n: GNode, row?: RowKey): string {
  if (n.kind === 'PROJECT') return `/${ns}/projects/${encodeURIComponent(n.project ?? '')}${row === 'apps' ? '?tab=apps' : row ? '?tab=hosts' : ''}`
  if (n.hosts.length === 1) return `/${ns}/hosts/${n.hosts[0].id}${row === 'apps' ? '?tab=apps' : row ? '?tab=components' : ''}`
  return n.project ? `/${ns}/projects/${encodeURIComponent(n.project)}?tab=hosts` : `/${ns}/hosts/${n.hosts[0]?.id ?? ''}`
}

function plural(n: number, word: string) {
  return `${n} ${word}${n > 1 ? 's' : ''}`
}

// Header tells what the zone is: the project or the host, its state, and
// where to read more.
function Header({ zone, ns, other, onClose }: ZoneData) {
  const n = zone.node
  const host = n.hosts.length === 1 ? n.hosts[0] : null
  const apps = zone.rows.find((r) => r.key === 'apps')
  const diverging = apps?.shown.filter((a) => a.divergences.length) ?? []
  const first = diverging[0]?.divergences[0]
  const ip = host?.ips.find((i) => i.defaultRoute) ?? host?.ips[0]
  const lastSeen = n.hosts.map((h) => h.lastSeenAt).sort()[0]
  return (
    <div className="lc-zone__head">
      <div className="flex items-center gap-2.5">
        <span className={`lc-node__icon lc-node--${kindOf(n)}`} style={{ width: 28, height: 28 }}><Glyph kind={kindOf(n)} /></span>
        <span className={n.kind === 'HOST' ? 'font-mono text-[14px] font-semibold' : 'heading'}>{title(n)}</span>
        {n.kind === 'HOST' && host ? <span className="nodrag -ml-2"><CopyButton value={host.hostname} /></span> : null}
        <span className="lc-muted text-[12.5px]">{[n.kind === 'PROJECT' ? 'projet' : n.hosts.length > 1 ? `${n.hosts.length} hosts` : 'host', n.kind === 'HOST' ? n.project : null, n.env].filter(Boolean).join(' · ')}</span>
        {n.status ? <StatusBadge status={n.status} /> : null}
        <span className="flex-1" />
        <button type="button" className="lc-btn lc-btn--ghost nodrag h-[26px]" onClick={onClose}>Replier</button>
      </div>
      <span className="lc-zone__line">
        {host ? [host.os?.pretty, ip?.address].filter(Boolean).join(' · ') : `${plural(n.hosts.length, 'host')} : ${n.members.join(', ')}`}
        {apps ? ` · ${plural(apps.total, 'app')}` : ''}
      </span>
      {first ? (
        <span className="lc-badge lc-badge--stale self-start">
          {first.app} : {first.hosts.map((h) => `${h.version || h.commit?.slice(0, 7) || '?'} sur ${h.hostname}`).join(', ')}
          {diverging.length > 1 ? ` · ${diverging.length - 1} autre${diverging.length > 2 ? 's' : ''}` : ''}
        </span>
      ) : (
        <span className="lc-zone__line">
          {host?.agentVersion ? `Agent ${host.agentVersion} · ` : ''}{lastSeen ? `dernier snapshot ${ago(lastSeen)}` : ''}
        </span>
      )}
      <span className="nodrag flex gap-3.5 text-[12.5px]">
        <Link to={pageOf(ns, n)}>{n.kind === 'PROJECT' ? 'Fiche du projet' : 'Fiche du host'}</Link>
        {other ? <Link to={other.to}>{other.label}</Link> : null}
        <Link to={`/${ns}/history`}>Historique</Link>
      </span>
    </div>
  )
}

// ZoneNode is a project, a host or a stack opened on the graph: its
// header, then its content in rows of three cards at most; the cards are
// nodes of their own, children of the zone.
export const ZoneNode = memo(function ZoneNode({ data }: NodeProps) {
  const d = data as ZoneData
  return (
    <div className="lc-zone" style={{ width: d.zone.width, height: d.zone.height }}>
      <Handle type="target" position={Position.Left} className="lc-handle" isConnectable={false} />
      <Header {...d} />
      {d.zone.rows.length === 0 ? <span className="lc-muted absolute text-[13px]" style={{ left: ZONE_PAD, top: d.zone.height - 60 }}>Aucun contenu collecté.</span> : null}
      {d.zone.rows.map((r) => (
        <span key={r.key} className="lc-label absolute" style={{ left: ZONE_PAD, top: r.top }}>
          {r.label} <span className="font-normal tracking-normal">· {r.total}</span>
        </span>
      ))}
      <Handle type="source" position={Position.Right} className="lc-handle" isConnectable={false} />
    </div>
  )
})

export interface MoreData extends Record<string, unknown> {
  label: string
  to: string
}

// MoreNode stands for the content a row does not show; the links of that
// content leave from it.
export const MoreNode = memo(function MoreNode({ data }: NodeProps) {
  const { label, to } = data as MoreData
  return (
    <div className="lc-more-card">
      <Handle type="target" position={Position.Left} className="lc-handle" isConnectable={false} />
      <b className="text-[13px] leading-4">{label}</b>
      <Link className="nodrag text-[12.5px]" to={to}>Voir plus</Link>
      <Handle type="source" position={Position.Right} className="lc-handle" isConnectable={false} />
    </div>
  )
})
