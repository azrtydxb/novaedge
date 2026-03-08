import { lazy, Suspense, useState, useEffect, useCallback } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from './components/layout/AppLayout'
import { ImportDialog } from './components/config/ImportDialog'
import { HistoryDialog } from './components/config/HistoryDialog'
import { useApp } from './contexts/AppContext'
import { api } from './api/client'

// Lazy load pages for code splitting
const Dashboard = lazy(() => import('./pages/Dashboard'))
const Gateways = lazy(() => import('./pages/Gateways'))
const RoutesPage = lazy(() => import('./pages/Routes'))
const Backends = lazy(() => import('./pages/Backends'))
const Policies = lazy(() => import('./pages/Policies'))
const Agents = lazy(() => import('./pages/Agents'))
const Login = lazy(() => import('./pages/Login'))

// New pages (lazy loaded - will be created in later tasks)
const Certificates = lazy(() => import('./pages/Certificates'))
const IPPools = lazy(() => import('./pages/IPPools'))
const Clusters = lazy(() => import('./pages/Clusters'))
const Federation = lazy(() => import('./pages/Federation'))
const MeshOverview = lazy(() => import('./pages/MeshOverview'))
const MeshPolicies = lazy(() => import('./pages/MeshPolicies'))
const MetricsDashboard = lazy(() => import('./pages/MetricsDashboard'))
const Traces = lazy(() => import('./pages/Traces'))
const Logs = lazy(() => import('./pages/Logs'))
const WAFEvents = lazy(() => import('./pages/WAFEvents'))
const ConfigPage = lazy(() => import('./pages/Config'))
const LoadShedding = lazy(() => import('./pages/LoadShedding'))
const WASMPlugins = lazy(() => import('./pages/WASMPlugins'))
const SDWANOverview = lazy(() => import('./pages/SDWANOverview'))

function LoadingFallback() {
  return (
    <div className="flex items-center justify-center h-64">
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
    </div>
  )
}

export default function App() {
  const { isImportDialogOpen, setIsImportDialogOpen, isHistoryOpen, setIsHistoryOpen } = useApp()
  const [authChecked, setAuthChecked] = useState(false)
  const [authenticated, setAuthenticated] = useState(false)
  const [authEnabled, setAuthEnabled] = useState(false)
  const [oidcEnabled, setOidcEnabled] = useState(false)

  useEffect(() => {
    api.auth.getSession()
      .then((session) => {
        setAuthenticated(session.authenticated)
        setAuthEnabled(session.authEnabled)
        setOidcEnabled(session.oidcEnabled)
        setAuthChecked(true)
      })
      .catch(() => {
        // If session endpoint fails, assume auth is not enabled
        setAuthEnabled(false)
        setAuthenticated(true)
        setAuthChecked(true)
      })
  }, [])

  const handleLoginSuccess = useCallback(() => {
    setAuthenticated(true)
  }, [])

  if (!authChecked) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-900">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  // If auth is enabled and user is not authenticated, show login
  if (authEnabled && !authenticated) {
    return (
      <Suspense fallback={<LoadingFallback />}>
        <Routes>
          <Route
            path="/login"
            element={<Login oidcEnabled={oidcEnabled} onLoginSuccess={handleLoginSuccess} />}
          />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </Suspense>
    )
  }

  return (
    <>
      <AppLayout>
        <Suspense fallback={<LoadingFallback />}>
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/login" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/gateways" element={<Gateways />} />
            <Route path="/routes" element={<RoutesPage />} />
            <Route path="/backends" element={<Backends />} />

            <Route path="/policies" element={<Policies />} />
            <Route path="/agents" element={<Agents />} />
            <Route path="/certificates" element={<Certificates />} />
            <Route path="/ippools" element={<IPPools />} />
            <Route path="/clusters" element={<Clusters />} />
            <Route path="/federation" element={<Federation />} />
            <Route path="/mesh" element={<MeshOverview />} />
            <Route path="/mesh-policies" element={<MeshPolicies />} />
            <Route path="/metrics-dashboard" element={<MetricsDashboard />} />
            <Route path="/traces" element={<Traces />} />
            <Route path="/logs" element={<Logs />} />
            <Route path="/waf" element={<WAFEvents />} />
            <Route path="/config" element={<ConfigPage />} />
            <Route path="/load-shedding" element={<LoadShedding />} />
            <Route path="/wasm-plugins" element={<WASMPlugins />} />
            <Route path="/sdwan" element={<SDWANOverview />} />
          </Routes>
        </Suspense>
      </AppLayout>

      <ImportDialog open={isImportDialogOpen} onOpenChange={setIsImportDialogOpen} />
      <HistoryDialog open={isHistoryOpen} onOpenChange={setIsHistoryOpen} />
    </>
  )
}
