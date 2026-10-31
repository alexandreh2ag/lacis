import type { Kind } from '@/components/Icon'
import { t } from '@/i18n'
import { plural } from '@/lib/format'
import { techOfDomain } from '@/lib/logos'
import type { GNode } from './query'

// kindOf is how a node is drawn: its color and icon.
export function kindOf(n: GNode): Kind {
  switch (n.kind) {
    case 'APP': return 'app'
    case 'SERVICE': return 'service'
    case 'WEBSERVER': return 'proxy'
    case 'EXTERNAL': return 'external'
    case 'PROJECT': return n.role === 'proxy' ? 'proxy' : n.role === 'service' ? 'service' : 'project'
    case 'RUNTIME': return 'host'
    default: return n.role === 'proxy' ? 'proxy' : n.role === 'service' ? 'service' : 'host'
  }
}

// isItem is the content of an opened zone.
export function isItem(n: GNode): boolean {
  return n.id.startsWith('item:')
}

// canOpen is a node a click opens with its content: a project, a host or
// a stack.
export function canOpen(n: GNode): boolean {
  return !isItem(n) && (n.kind === 'PROJECT' || n.kind === 'HOST')
}

// countOf is the badge of a card: the hosts of a stack or of an item,
// the targets of merged externals.
export function countOf(n: GNode): string | null {
  if (isStack(n) || (isItem(n) && n.count > 1)) return `×${n.count}`
  return n.id.startsWith('externals:') ? String(n.count) : null
}

// mainTech is the technology of the icon: an app host or a project shows
// its kind, a service host its service.
export function mainTech(n: GNode): string | undefined {
  if (n.kind === 'EXTERNAL') return n.members.length ? undefined : techOfDomain(n.label)
  if ((n.kind === 'HOST' || n.kind === 'PROJECT') && n.role === 'app') return undefined
  return n.techs[0]
}

// foldedTechs are the small logos: what an app folds, what a host runs.
export function foldedTechs(n: GNode): string[] {
  if (n.kind === 'EXTERNAL') {
    return [...new Set(n.members.map(techOfDomain).filter((t): t is string => !!t))].slice(0, 3)
  }
  const main = mainTech(n)
  return n.techs.filter((t) => t !== main).slice(0, 3)
}

export function isStack(n: GNode): boolean {
  return n.kind === 'HOST' && n.members.length > 1
}

// stacked draws a card as a pile: a stack, or an item on several hosts.
export function stacked(n: GNode): boolean {
  return isStack(n) || (isItem(n) && n.count > 1)
}

// title is the name of a node, as the card and the panel write it.
export function title(n: GNode): string {
  if (n.kind === 'EXTERNAL' && n.id.startsWith('externals:')) return t().graph.card.externals
  if (n.kind === 'PROJECT' && !n.label) return t().graph.card.noProject
  return n.label
}

// info is the second line of a card.
export function info(n: GNode): string {
  switch (n.kind) {
    case 'PROJECT':
      return n.role === 'app' ? `${plural(n.count, 'host')} · ${plural(n.apps, 'app')}` : (n.detail ?? '')
    case 'HOST':
      if (n.status === 'STALE' || n.status === 'INACTIVE') return n.detail ?? ''
      return n.role === 'app' ? [n.project, n.detail].filter(Boolean).join(' · ') : (n.detail ?? '')
    case 'APP':
      return [n.version, n.detail].filter(Boolean).join(' · ')
    case 'SERVICE':
      return [n.detail, n.hostname].filter(Boolean).join(' · ')
    case 'WEBSERVER':
      return [n.detail, n.hostname].filter(Boolean).join(' · ')
    case 'RUNTIME':
      return n.detail ?? ''
    case 'EXTERNAL':
      return n.id.startsWith('externals:') ? t().graph.card.targets(n.count) : (n.detail ?? '')
  }
  return ''
}
