import { customIconOf, hasLogo, initials, logoOf } from '@/lib/logos'

export type Kind = 'app' | 'service' | 'proxy' | 'host' | 'project' | 'external'

const kinds: Record<Kind, React.ReactNode> = {
  app: <><rect x="2" y="2.5" width="12" height="11" rx="1.5" /><path d="M2 5.5h12M6 8.5l-1.5 1.5L6 11.5M10 8.5l1.5 1.5-1.5 1.5" /></>,
  service: <><ellipse cx="8" cy="3.8" rx="5" ry="1.8" /><path d="M3 3.8v8.4c0 1 2.2 1.8 5 1.8s5-.8 5-1.8V3.8M3 8c0 1 2.2 1.8 5 1.8s5-.8 5-1.8" /></>,
  proxy: <path d="M1.5 8h4M5.5 8l3.5-4h5M5.5 8l3.5 4h5M12 2l2 2-2 2M12 10l2 2-2 2" />,
  host: <><rect x="2" y="2" width="12" height="5" rx="1" /><rect x="2" y="9" width="12" height="5" rx="1" /><path d="M4.5 4.5h.01M4.5 11.5h.01" /></>,
  project: <><path d="M8 1.8 14 5 8 8.2 2 5z" /><path d="M2 8l6 3.2L14 8M2 11l6 3.2 6-3.2" /></>,
  external: <><circle cx="8" cy="8" r="6" /><path d="M2 8h12M8 2c1.8 1.8 2.6 3.8 2.6 6S9.8 12.2 8 14M8 2C6.2 3.8 5.4 5.8 5.4 8s.8 4.2 2.6 6" /></>,
}

export function KindIcon({ kind }: { kind: Kind }) {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {kinds[kind]}
    </svg>
  )
}

// TechLogo draws the logo of a technology: the one uploaded in the
// administration, else the embedded one, else its initials. builtin
// leaves the uploaded one out.
export function TechLogo({ tech, title, builtin }: { tech: string; title?: string; builtin?: boolean }) {
  const url = builtin ? undefined : customIconOf(tech)
  if (url) {
    return <img className="lc-techicon" src={url} alt={title ?? ''} title={title} aria-hidden={!title} />
  }
  const logo = logoOf(tech)
  if (!logo) {
    return <span className="lc-node__logo" title={title ?? tech} aria-hidden={!title}>{initials(tech)}</span>
  }
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" role={title ? 'img' : undefined} aria-hidden={!title} aria-label={title}>
      {title ? <title>{title}</title> : null}
      <path d={logo.path} />
    </svg>
  )
}

// Glyph is the logo of the main technology, else the icon of the kind.
export function Glyph({ kind, tech }: { kind: Kind; tech?: string | null }) {
  return tech && hasLogo(tech) ? <TechLogo tech={tech} /> : <KindIcon kind={kind} />
}

export function Chevron({ dir }: { dir: 'left' | 'right' | 'down' | 'up' }) {
  const d = { left: 'M10 3.5 5.5 8l4.5 4.5', right: 'M6 3.5 10.5 8 6 12.5', down: 'M3.5 6 8 10.5 12.5 6', up: 'M3.5 10 8 5.5l4.5 4.5' }[dir]
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d} />
    </svg>
  )
}
