import { useEffect, useId, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { Chevron } from './Icon'

// PickerLimit is the most namespaces the picker lists: past it, a search
// narrows them.
export const PickerLimit = 10

interface Namespace {
  id: string
  slug: string
  name: string
}

// matching keeps the namespaces whose slug or name holds the search.
export function matching<T extends Namespace>(namespaces: T[], q: string): T[] {
  const s = q.trim().toLowerCase()
  return s ? namespaces.filter((n) => n.slug.toLowerCase().includes(s) || n.name.toLowerCase().includes(s)) : namespaces
}

// NamespacePicker opens another namespace: a search over those of the user,
// listing PickerLimit of them at most.
export function NamespacePicker({ namespaces, current }: { namespaces: Namespace[]; current?: string }) {
  const navigate = useNavigate()
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const found = matching(namespaces, q)
  const shown = found.slice(0, PickerLimit)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const pick = (slug: string) => {
    setOpen(false)
    setQ('')
    navigate(`/${slug}`)
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const step = e.key === 'ArrowDown' ? 1 : -1
      setActive((a) => (a + step + shown.length) % Math.max(shown.length, 1))
    } else if (e.key === 'Enter' && shown[active]) {
      e.preventDefault()
      pick(shown[active].slug)
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  return (
    <div ref={root} className="relative">
      <button type="button" className="lc-input inline-flex items-center gap-2 font-mono [&_svg]:size-3.5 [&_svg]:text-ink-muted" aria-haspopup="dialog" aria-expanded={open}
        onClick={() => { setOpen(!open); setActive(0) }}>
        <span className="sr-only">Namespace :</span>{current || 'namespace'}<Chevron dir="down" />
      </button>
      {open ? (
        <div role="dialog" aria-label="Changer de namespace" className="lc-panel absolute left-0 top-10 z-50 w-80">
          <div className="lc-panel__section">
            <label htmlFor={id} className="sr-only">Chercher un namespace</label>
            <input id={id} className="lc-input w-full" type="search" placeholder="Chercher un namespace" autoFocus autoComplete="off"
              role="combobox" aria-expanded aria-controls={`${id}-list`} aria-activedescendant={shown[active] ? `${id}-${shown[active].id}` : undefined}
              value={q} onChange={(e) => { setQ(e.target.value); setActive(0) }} onKeyDown={onKey} />
            <ul id={`${id}-list`} role="listbox" aria-label="Namespaces" className="m-0 flex list-none flex-col p-0">
              {shown.map((n, i) => (
                <li key={n.id} id={`${id}-${n.id}`} role="option" aria-selected={i === active}
                  className={'flex cursor-pointer items-baseline gap-2 rounded-sm px-2 py-1.5 ' + (i === active ? 'bg-surface-hover' : '')}
                  onMouseEnter={() => setActive(i)} onMouseDown={(e) => { e.preventDefault(); pick(n.slug) }}>
                  <span className={'font-mono text-[13px]' + (n.slug === current ? ' font-semibold text-brand' : '')}>{n.slug}</span>
                  {n.name !== n.slug ? <span className="truncate text-[12.5px] text-ink-muted">{n.name}</span> : null}
                </li>
              ))}
            </ul>
            {found.length === 0 ? <span className="lc-muted text-[13px]">Aucun namespace ne correspond.</span> : null}
            {found.length > PickerLimit ? (
              <span className="lc-muted text-[12.5px]" role="status">
                {PickerLimit} namespaces affichés sur {found.length} : affinez la recherche pour trouver les autres.
              </span>
            ) : null}
          </div>
          <div className="lc-panel__section py-2.5">
            <Link to="/" className="text-[13px]" onClick={() => setOpen(false)}>Tous les namespaces</Link>
          </div>
        </div>
      ) : null}
    </div>
  )
}
