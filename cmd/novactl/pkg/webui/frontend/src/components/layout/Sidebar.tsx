import { NavLink, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Server,
  Route,
  Database,
  Star,
  Shield,
  Cpu,
  Download,
  Upload,
  History,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useApp } from '@/contexts/AppContext'

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/gateways', icon: Server, label: 'Gateways' },
  { to: '/routes', icon: Route, label: 'Routes' },
  { to: '/backends', icon: Database, label: 'Backends' },
  { to: '/vips', icon: Star, label: 'VIPs' },
  { to: '/policies', icon: Shield, label: 'Policies' },
  { to: '/agents', icon: Cpu, label: 'Agents' },
]

export function Sidebar() {
  const location = useLocation()
  const { mode, readOnly, exportConfig, showImportDialog, showHistory } = useApp()

  return (
    <aside className="flex h-screen w-64 flex-col bg-slate-900 text-white">
      {/* Logo */}
      <div className="flex items-center gap-3 px-6 py-5">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500 to-purple-600 font-bold text-lg">
          N
        </div>
        <div>
          <h1 className="font-semibold text-lg">NovaEdge</h1>
          <span className="text-xs text-slate-400">v1.0.0</span>
        </div>
      </div>

      {/* Mode Indicator */}
      <div className="mx-4 mb-4">
        <div
          className={cn(
            'rounded-md px-3 py-1.5 text-xs font-medium',
            mode === 'kubernetes'
              ? 'bg-blue-500/20 text-blue-300'
              : 'bg-green-500/20 text-green-300',
            readOnly && 'bg-yellow-500/20 text-yellow-300'
          )}
        >
          {mode === 'kubernetes' ? 'Kubernetes Mode' : 'Standalone Mode'}
          {readOnly && ' (Read-Only)'}
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 px-3">
        {navItems.map((item) => {
          const isActive =
            item.to === '/'
              ? location.pathname === '/'
              : location.pathname.startsWith(item.to)

          return (
            <NavLink
              key={item.to}
              to={item.to}
              className={cn(
                'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                isActive
                  ? 'bg-slate-800 text-white'
                  : 'text-slate-400 hover:bg-slate-800/50 hover:text-white'
              )}
            >
              <item.icon className="h-5 w-5" />
              {item.label}
            </NavLink>
          )
        })}
      </nav>

      {/* Footer Actions */}
      <div className="mt-auto border-t border-slate-800 p-4">
        <div className="space-y-2">
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start text-slate-400 hover:text-white hover:bg-slate-800"
            onClick={exportConfig}
          >
            <Download className="mr-2 h-4 w-4" />
            Export Config
          </Button>
          {!readOnly && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start text-slate-400 hover:text-white hover:bg-slate-800"
              onClick={showImportDialog}
            >
              <Upload className="mr-2 h-4 w-4" />
              Import Config
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start text-slate-400 hover:text-white hover:bg-slate-800"
            onClick={showHistory}
          >
            <History className="mr-2 h-4 w-4" />
            History
          </Button>
        </div>
      </div>
    </aside>
  )
}
