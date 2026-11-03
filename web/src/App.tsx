import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { RouteBoundary } from './components/ErrorBoundary'
import { Layout } from './components/Layout'
import { AppPage } from './pages/AppPage'
import { Authorize } from './pages/Authorize'
import { History } from './pages/History'
import { Home, NamespaceIndex } from './pages/Home'
import { HostPage } from './pages/HostPage'
import { Hosts } from './pages/Hosts'
import { Apps, Services } from './pages/Inventory'
import { Login } from './pages/Login'
import { ProjectPage } from './pages/Project'
import { Profile } from './pages/Profile'
import { Projects } from './pages/Projects'
import { Settings } from './pages/Settings'
import { Admin } from './pages/admin/Admin'
import { useT } from './i18n'
import { RequireSession } from './session'

// The graph and its layout engine load on the first visit of the graph.
const GraphPage = lazy(() => import('./pages/graph/GraphPage').then((m) => ({ default: m.GraphPage })))

// page is the element of a route, behind its own error boundary.
function page(element: React.ReactNode) {
  return <RouteBoundary>{element}</RouteBoundary>
}

export function App() {
  const t = useT()
  return (
    <Routes>
      <Route path="login" element={page(<Login />)} />
      <Route path="authorize" element={page(<Authorize />)} />
      <Route element={<RequireSession><Layout /></RequireSession>}>
        <Route index element={page(<NamespaceIndex />)} />
        <Route path="admin" element={page(<Admin />)} />
        <Route path="account" element={page(<Profile />)} />
        <Route path="account/assistants" element={<Navigate to="/account?tab=mcp" replace />} />
        <Route path=":ns">
          <Route index element={page(<Home />)} />
          <Route path="graph" element={page(<Suspense fallback={<div className="p-8 text-ink-muted">{t.common.loadingGraph}</div>}><GraphPage /></Suspense>)} />
          <Route path="hosts" element={page(<Hosts />)} />
          <Route path="hosts/:id" element={page(<HostPage />)} />
          <Route path="apps" element={page(<Apps />)} />
          <Route path="apps/:id" element={page(<AppPage />)} />
          <Route path="services" element={page(<Services />)} />
          <Route path="projects" element={page(<Projects />)} />
          <Route path="projects/:slug" element={page(<ProjectPage />)} />
          <Route path="history" element={page(<History />)} />
          <Route path="settings" element={page(<Settings />)} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
