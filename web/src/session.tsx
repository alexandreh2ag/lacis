import { useApolloClient, useQuery } from '@apollo/client'
import { createContext, useContext, useEffect } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { onUnauthorized } from './apollo'
import { setCustomIcons } from './lib/logos'
import { config } from './config'
import { useT } from './i18n'
import { graphql } from './graphql'
import type { SessionQuery } from './graphql/graphql'

const SessionDoc = graphql(`
  query Session {
    me { id username firstName lastName isAdmin local oidc }
    namespaces { id slug name canEdit }
    access { users { read write } roles { read write } tokens { read write } icons { read write } tasks { read write } }
    techIcons { tech url }
  }
`)

export type Session = SessionQuery

const SessionContext = createContext<Session | null>(null)

export function useSession(): Session {
  const s = useContext(SessionContext)
  if (!s) throw new Error('useSession outside RequireSession')
  return s
}

// loginPath is the login page coming back to the current page.
export function loginPath(location: { pathname: string; search: string }) {
  const back = location.pathname + location.search
  return back === '/' ? '/login' : `/login?redirect=${encodeURIComponent(back)}`
}

// RequireSession loads the user; without session, it goes to the login.
export function RequireSession({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const t = useT()
  const { data, error, loading } = useQuery(SessionDoc, { fetchPolicy: 'network-only' })
  // The logos uploaded for technologies show wherever a technology does.
  const icons = data?.techIcons
  useEffect(() => { if (icons) setCustomIcons(icons) }, [icons])
  useEffect(() => onUnauthorized(() => navigate(loginPath(window.location.pathname.startsWith(config.basePath)
    ? { pathname: '/' + window.location.pathname.slice(config.basePath.length), search: window.location.search }
    : location))), [navigate, location])
  if (loading && !data) return <div className="p-8 text-ink-muted">{t.components.session.loading}</div>
  if (error && 'statusCode' in (error.networkError ?? {}) && (error.networkError as { statusCode: number }).statusCode === 401) {
    return <Navigate to={loginPath(location)} replace />
  }
  if (error || !data) return <div className="p-8" role="alert">{t.components.session.noServer(error?.message ?? '')}</div>
  return <SessionContext.Provider value={data}>{children}</SessionContext.Provider>
}

export function useLogout() {
  const client = useApolloClient()
  const navigate = useNavigate()
  const t = useT()
  // The session is left only once the server closed it: a refused or lost
  // logout keeps the page, and says so. A session already gone (401) is
  // left all the same.
  return async () => {
    let ok = false
    try {
      const res = await fetch('auth/logout', { method: 'POST', credentials: 'same-origin' })
      ok = res.ok || res.status === 401
    } catch {
      ok = false
    }
    if (!ok) {
      window.alert(t.common.account.logoutFailed)
      return
    }
    await client.clearStore()
    navigate('/login', { replace: true })
  }
}
