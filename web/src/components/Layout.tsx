import { Link, NavLink, Outlet, useParams } from 'react-router'
import { canAdminister } from '@/pages/admin/Admin'
import { useSession, useLogout } from '@/session'
import { useState } from 'react'
import { langs, useLang, useT } from '@/i18n'
import { useTheme, type Theme } from '@/lib/theme'
import { NamespacePicker } from './NamespacePicker'

// Outer nodes of the Lacis mark (a small mesh around a central node), on a 32×32 grid.
const logoNodes: [number, number][] = [[7, 10], [18, 6], [26, 14], [22.5, 25.5], [9, 24]]

// The strokes of the icons of the account menu, on a 24×24 grid.
const menuIcons = {
  profile: 'M12 4a4 4 0 1 0 0 8a4 4 0 1 0 0-8M4 21a8 8 0 0 1 16 0',
  admin: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z',
  logout: 'M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 17l-5-5 5-5M5 12h11',
}

function MenuIcon({ d }: { d: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d} />
    </svg>
  )
}

export function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true" className="flex-none">
      <g fill="none" stroke="var(--ink)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <path d="M7 10L18 6L26 14L22.5 25.5L9 24Z" />
        {/* spokes stop short of the central node, leaving a gap around it */}
        <path d="M7 10L12 13.83M18 6L16.52 12.22M26 14L19.78 15.48M22.5 25.5L18.2 19.97M9 24L12.62 19.83" />
      </g>
      <g fill="var(--ink)">
        {logoNodes.map(([cx, cy]) => <circle key={`${cx}-${cy}`} cx={cx} cy={cy} r="2.4" />)}
      </g>
      <circle cx="15.5" cy="16.5" r="3.4" fill="var(--brand)" />
    </svg>
  )
}

const themes: Theme[] = ['system', 'light', 'dark']

const sections = ['graph', 'projects', 'hosts', 'apps', 'services', 'history'] as const

export function TopBar() {
  const { ns } = useParams()
  const session = useSession()
  const logout = useLogout()
  const [menu, setMenu] = useState(false)
  const [theme, setTheme] = useTheme()
  const t = useT()
  const language = useLang()
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
        <nav aria-label={t.common.nav.sections} className="flex gap-0.5 text-[13.5px]">
          {[...sections, ...(canEdit ? ['settings' as const] : [])].map((s) => ({ to: s, label: t.common.nav[s] })).map((s) => (
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
          aria-haspopup="menu" aria-expanded={menu} aria-label={t.common.account.of(name)} onClick={() => setMenu(!menu)}>
          {initials}
        </button>
        {menu ? (
          <div role="menu" className="lc-panel absolute right-0 top-10 z-50 w-64">
            <div className="lc-panel__section">
              <span className="font-semibold">{name}</span>
              <span className="lc-muted font-mono text-[12.5px]">{username}{session.me.isAdmin ? ' · admin' : ''}</span>
            </div>
            <div className="lc-panel__section">
              <span className="lc-label">{t.common.account.theme}</span>
              <div className="lc-zoom self-start" role="group" aria-label={t.common.account.theme}>
                {themes.map((th) => (
                  <button key={th} type="button" aria-pressed={theme === th} onClick={() => setTheme(th)}>{t.common.account.themes[th]}</button>
                ))}
              </div>
            </div>
            <div className="lc-panel__section">
              <span className="lc-label">{t.common.account.language}</span>
              <div className="lc-zoom self-start" role="group" aria-label={t.common.account.language}>
                {([null, ...langs] as const).map((l) => (
                  <button key={l ?? 'auto'} type="button" aria-pressed={language.picked === l} onClick={() => language.pick(l)}>
                    {t.common.account.languages[l ?? 'auto']}
                  </button>
                ))}
              </div>
            </div>
            <div className="lc-menu">
              <Link role="menuitem" to="/account" className="lc-menu__item" onClick={() => setMenu(false)}>
                <MenuIcon d={menuIcons.profile} />{t.common.account.profile}
              </Link>
              {canAdminister(session.access) ? (
                <Link role="menuitem" to="/admin" className="lc-menu__item" onClick={() => setMenu(false)}>
                  <MenuIcon d={menuIcons.admin} />{t.common.account.administration}
                </Link>
              ) : null}
            </div>
            <div className="lc-menu">
              <button role="menuitem" type="button" className="lc-menu__item" onClick={() => void logout()}>
                <MenuIcon d={menuIcons.logout} />{t.common.account.logout}
              </button>
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
