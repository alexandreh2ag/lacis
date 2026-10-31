import { useApolloClient, useQuery } from '@apollo/client'
import { createContext, useContext, useEffect } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { onUnauthorized } from './apollo'
import { setCustomIcons } from './lib/logos'
import { config } from './config'
import { graphql } from './graphql'
import type { SessionQuery } from './graphql/graphql'

const SessionDoc = graphql(`
  query Session {
    me { id username firstName lastName isAdmin }
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
  const { data, error, loading } = useQuery(SessionDoc, { fetchPolicy: 'network-only' })
  useEffect(() => onUnauthorized(() => navigate(loginPath(window.location.pathname.startsWith(config.basePath)
    ? { pathname: '/' + window.location.pathname.slice(config.basePath.length), search: window.location.search }
    : location))), [navigate, location])
  if (loading && !data) return <div className="p-8 text-ink-muted">Chargement…</div>
  if (error && 'statusCode' in (error.networkError ?? {}) && (error.networkError as { statusCode: number }).statusCode === 401) {
    return <Navigate to={loginPath(location)} replace />
  }
  if (error || !data) return <div className="p-8" role="alert">Le serveur ne répond pas : {error?.message}</div>
  // The logos uploaded for technologies show wherever a technology does.
  setCustomIcons(data.techIcons)
  return <SessionContext.Provider value={data}>{children}</SessionContext.Provider>
}

export function useLogout() {
  const client = useApolloClient()
  const navigate = useNavigate()
  return async () => {
    await fetch('auth/logout', { method: 'POST', credentials: 'same-origin' })
    await client.clearStore()
    navigate('/login', { replace: true })
  }
}
