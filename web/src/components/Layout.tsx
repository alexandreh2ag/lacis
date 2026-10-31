import { Link, NavLink, Outlet, useParams } from 'react-router'
import { canAdminister } from '@/pages/admin/Admin'
import { useSession, useLogout } from '@/session'
import { useState } from 'react'
import { useTheme, type Theme } from '@/lib/theme'
import { NamespacePicker } from './NamespacePicker'

export function Logo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 22 22" aria-hidden="true" className="flex-none">
      <rect x="1" y="1" width="20" height="20" rx="5" fill="var(--brand)" />
      <path d="M6 6v10h10" fill="none" stroke="var(--on-brand)" strokeWidth="2" strokeLinecap="round" />
      <circle cx="15" cy="7" r="2" fill="var(--on-brand)" />
      <path d="M6 11h4.5l3.5-3" fill="none" stroke="var(--on-brand)" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

const themes: { value: Theme; label: string }[] = [
  { value: 'system', label: 'Système' },
  { value: 'light', label: 'Clair' },
  { value: 'dark', label: 'Sombre' },
]

const sections = [
  { to: 'graph', label: 'Graphe' },
  { to: 'projects', label: 'Projets' },
  { to: 'hosts', label: 'Hosts' },
  { to: 'apps', label: 'Apps' },
  { to: 'services', label: 'Services' },
  { to: 'history', label: 'Historique' },
]

export function TopBar() {
  const { ns } = useParams()
  const session = useSession()
  const logout = useLogout()
  const [menu, setMenu] = useState(false)
  const [theme, setTheme] = useTheme()
  const { username, firstName, lastName } = session.me
  const canEdit = session.namespaces.some((n) => n.slug === ns && n.canEdit)
  const fullName = [firstName, lastName].filter(Boolean).join(' ')
  const name = fullName || username
  const initials = name.split(/[\s._@-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('')
  return (
    <header className="flex h-14 flex-none items-center gap-4 border-b border-border bg-surface-raised px-4">
      <NavLink to={ns ? `/${ns}` : '/'} className="flex items-center gap-2 text-base font-bold text-ink no-underline">
        <Logo />Lacis
      </NavLink>
      {session.namespaces.length > 0 ? (
        <>
          <span className="text-border-strong">/</span>
          <NamespacePicker namespaces={session.namespaces} current={ns} />
        </>
      ) : null}
      {ns ? (
        <nav aria-label="Sections" className="flex gap-0.5 text-[13.5px]">
          {[...sections, ...(canEdit ? [{ to: 'settings', label: 'Paramètres' }] : [])].map((s) => (
            <NavLink key={s.to} to={`/${ns}/${s.to}`}
              className={({ isActive }) => 'rounded-sm px-2.5 py-1.5 font-medium no-underline ' +
                (isActive ? 'bg-surface-hover text-ink' : 'text-ink-muted hover:text-ink')}>
              {s.label}
            </NavLink>
          ))}
        </nav>
      ) : null}
      <span className="flex-1" />
      <div className="relative">
        <button type="button" className="grid size-8 place-items-center rounded-full bg-kind-host-tint text-xs font-semibold text-ink"
          aria-haspopup="menu" aria-expanded={menu} aria-label={`Compte de ${name}`} onClick={() => setMenu(!menu)}>
          {initials}
        </button>
        {menu ? (
          <div role="menu" className="lc-panel absolute right-0 top-10 z-50 w-64">
            <div className="lc-panel__section">
              <span className="font-semibold">{name}</span>
              <span className="lc-muted font-mono text-[12.5px]">{username}{session.me.isAdmin ? ' · admin' : ''}</span>
            </div>
            <div className="lc-panel__section">
              <span className="lc-label">Thème</span>
              <div className="lc-zoom self-start" role="group" aria-label="Thème">
                {themes.map((t) => (
                  <button key={t.value} type="button" aria-pressed={theme === t.value} onClick={() => setTheme(t.value)}>{t.label}</button>
                ))}
              </div>
            </div>
            {canAdminister(session.access) ? (
              <div className="lc-panel__section">
                <Link role="menuitem" to="/admin" className="lc-btn no-underline" onClick={() => setMenu(false)}>Administration</Link>
              </div>
            ) : null}
            <div className="lc-panel__section">
              <button role="menuitem" type="button" className="lc-btn" onClick={() => void logout()}>Se déconnecter</button>
            </div>
          </div>
        ) : null}
      </div>
    </header>
  )
}

export function Layout() {
  return (
    <div className="flex min-h-full flex-col bg-surface text-ink">
      <TopBar />
      <Outlet />
    </div>
  )
}

// Page is a fluid page of listings and entity pages.
export function Page({ title, children, meta }: { title: React.ReactNode; children: React.ReactNode; meta?: React.ReactNode }) {
  return (
    <main className="mx-auto flex w-full max-w-[1376px] flex-1 flex-col gap-6 px-4 pb-12 pt-7 sm:px-8">
      <div className="flex flex-col gap-1">
        <h1 className="display m-0">{title}</h1>
        {meta ? <div className="flex flex-wrap items-center gap-2.5 text-ink-muted">{meta}</div> : null}
      </div>
      {children}
    </main>
  )
}
