import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { Layout } from './components/Layout'
import { AppPage } from './pages/AppPage'
import { History } from './pages/History'
import { Home, NamespaceIndex } from './pages/Home'
import { HostPage } from './pages/HostPage'
import { Hosts } from './pages/Hosts'
import { Apps, Services } from './pages/Inventory'
import { Login } from './pages/Login'
import { ProjectPage } from './pages/Project'
import { Projects } from './pages/Projects'
import { Settings } from './pages/Settings'
import { Admin } from './pages/admin/Admin'
import { RequireSession } from './session'

// The graph and its layout engine load on the first visit of the graph.
const GraphPage = lazy(() => import('./pages/graph/GraphPage').then((m) => ({ default: m.GraphPage })))

export function App() {
  return (
    <Routes>
      <Route path="login" element={<Login />} />
      <Route element={<RequireSession><Layout /></RequireSession>}>
        <Route index element={<NamespaceIndex />} />
        <Route path="admin" element={<Admin />} />
        <Route path=":ns">
          <Route index element={<Home />} />
          <Route path="graph" element={<Suspense fallback={<div className="p-8 text-ink-muted">Chargement du graphe…</div>}><GraphPage /></Suspense>} />
          <Route path="hosts" element={<Hosts />} />
          <Route path="hosts/:id" element={<HostPage />} />
          <Route path="apps" element={<Apps />} />
          <Route path="apps/:id" element={<AppPage />} />
          <Route path="services" element={<Services />} />
          <Route path="projects" element={<Projects />} />
          <Route path="projects/:slug" element={<ProjectPage />} />
          <Route path="history" element={<History />} />
          <Route path="settings" element={<Settings />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
