import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { AppLayout } from './components/layout/AppLayout'
import { ImportDialog } from './components/config/ImportDialog'
import { HistoryDialog } from './components/config/HistoryDialog'
import { useApp } from './contexts/AppContext'

// Lazy load pages for code splitting
const Dashboard = lazy(() => import('./pages/Dashboard'))
const Gateways = lazy(() => import('./pages/Gateways'))
const RoutesPage = lazy(() => import('./pages/Routes'))
const Backends = lazy(() => import('./pages/Backends'))
const VIPs = lazy(() => import('./pages/VIPs'))
const Policies = lazy(() => import('./pages/Policies'))
const Agents = lazy(() => import('./pages/Agents'))

function LoadingFallback() {
  return (
    <div className="flex items-center justify-center h-64">
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
    </div>
  )
}

export default function App() {
  const { isImportDialogOpen, setIsImportDialogOpen, isHistoryOpen, setIsHistoryOpen } = useApp()

  return (
    <>
      <AppLayout>
        <Suspense fallback={<LoadingFallback />}>
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/gateways" element={<Gateways />} />
            <Route path="/routes" element={<RoutesPage />} />
            <Route path="/backends" element={<Backends />} />
            <Route path="/vips" element={<VIPs />} />
            <Route path="/policies" element={<Policies />} />
            <Route path="/agents" element={<Agents />} />
          </Routes>
        </Suspense>
      </AppLayout>

      <ImportDialog open={isImportDialogOpen} onOpenChange={setIsImportDialogOpen} />
      <HistoryDialog open={isHistoryOpen} onOpenChange={setIsHistoryOpen} />
    </>
  )
}
