import { useId, useState } from 'react'
import { useT } from '@/i18n'
import { PickerShown } from './MultiPicker'

export interface PickItem {
  id: string
  label: string
  detail?: string | null
}

// AddPicker adds items picked among many: a search lists those not added
// yet, a pick joins the list under it, whose Retirer takes it out. It
// holds in a form: Enter picks, it never submits.
export function AddPicker({ label, placeholder, items, selected, onChange, empty }: {
  label: string
  placeholder: string
  items: PickItem[]
  selected: string[]
  onChange: (ids: string[]) => void
  empty: string
}) {
  const t = useT()
  const id = useId()
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const byID = new Map(items.map((i) => [i.id, i]))
  const free = items.filter((i) => !selected.includes(i.id))
  const s = q.trim().toLowerCase()
  const found = s ? free.filter((i) => i.label.toLowerCase().includes(s) || (i.detail ?? '').toLowerCase().includes(s)) : free
  const shown = found.slice(0, PickerShown)
  const added = selected.map((x) => byID.get(x)).filter((i): i is PickItem => !!i)

  const pick = (item: PickItem) => {
    onChange([...selected, item.id])
    setQ('')
    setActive(0)
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      setOpen(true)
      const step = e.key === 'ArrowDown' ? 1 : -1
      setActive((a) => (a + step + shown.length) % Math.max(shown.length, 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (open && shown[active]) pick(shown[active])
    } else if (e.key === 'Escape' && open) {
      // Escape closes the list, not the dialog around it.
      e.preventDefault()
      e.stopPropagation()
      setOpen(false)
    }
  }

  return (
    <div className="lc-field">
      <label htmlFor={id} className="lc-label">{label}</label>
      <div className="lc-addpick">
        <span className="lc-search w-full">
          <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3 3" />
          </svg>
          <input id={id} className="lc-input" type="search" placeholder={placeholder} autoComplete="off"
            role="combobox" aria-expanded={open} aria-controls={`${id}-list`} aria-activedescendant={open && shown[active] ? `${id}-${active}` : undefined}
            value={q} onChange={(e) => { setQ(e.target.value); setActive(0); setOpen(true) }}
            onFocus={() => setOpen(true)} onBlur={() => setOpen(false)} onKeyDown={onKey} />
        </span>
        {open ? (
          <div className="lc-addpick__results">
            <ul id={`${id}-list`} role="listbox" aria-label={label} className="m-0 flex list-none flex-col p-0">
              {shown.map((item, i) => (
                <li key={item.id} id={`${id}-${i}`} role="option" aria-selected={i === active} className="lc-addpick__option"
                  onMouseEnter={() => setActive(i)} onMouseDown={(e) => { e.preventDefault(); pick(item) }}>
                  <span className="lc-mono">{item.label}</span>
                  {item.detail ? <span className="lc-muted truncate text-[12.5px]">{item.detail}</span> : null}
                  <span className="lc-addpick__add" aria-hidden="true">{t.components.addPicker.add}</span>
                </li>
              ))}
            </ul>
            {found.length === 0 ? <span className="lc-muted px-2 py-1.5 text-[13px]">{free.length ? t.components.noneMatch : t.components.addPicker.allAdded}</span> : null}
            {found.length > PickerShown ? (
              <span className="lc-muted px-2 py-1 text-[12.5px]" role="status">{t.components.shown(PickerShown, found.length)}</span>
            ) : null}
          </div>
        ) : null}
      </div>
      {added.length ? (
        <ul className="lc-order" aria-label={t.components.addPicker.added(label)}>
          {added.map((item) => (
            <li key={item.id}>
              <span className="flex min-w-0 flex-1 flex-col leading-[18px]">
                <span className="lc-mono font-semibold">{item.label}</span>
                {item.detail ? <span className="lc-muted truncate text-[12.5px]">{item.detail}</span> : null}
              </span>
              <button type="button" className="lc-btn lc-btn--small lc-btn--danger" aria-label={t.components.remove(item.label)}
                onClick={() => onChange(selected.filter((x) => x !== item.id))}>{t.components.addPicker.removeButton}</button>
            </li>
          ))}
        </ul>
      ) : <span className="lc-muted text-[13px]">{empty}</span>}
    </div>
  )
}
