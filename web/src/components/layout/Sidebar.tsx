import { NavLink, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Network,
  GitBranch,
  Server,
  Shield,
  Globe,
  LayoutGrid,
  Lock,
  Workflow,
  ShieldCheck,
  Cpu,
  Boxes,
  Globe2,
  BarChart3,
  Waypoints,
  ScrollText,
  ShieldAlert,
  Settings,
  Download,
  Upload,
  History,
  Puzzle,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useApp } from '@/contexts/AppContext'

interface NavItem {
  name: string
  path: string
  icon: LucideIcon
}

interface NavGroup {
  label: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    label: 'Overview',
    items: [
      { name: 'Dashboard', path: '/dashboard', icon: LayoutDashboard },
    ],
  },
  {
    label: 'Traffic Management',
    items: [
      { name: 'Gateways', path: '/gateways', icon: Network },
      { name: 'Routes', path: '/routes', icon: GitBranch },
      { name: 'Backends', path: '/backends', icon: Server },
      { name: 'Policies', path: '/policies', icon: Shield },
      { name: 'Load Shedding', path: '/load-shedding', icon: Zap },
    ],
  },
  {
    label: 'Networking',
    items: [
      { name: 'VIPs', path: '/vips', icon: Globe },
      { name: 'IP Pools', path: '/ippools', icon: LayoutGrid },
      { name: 'Certificates', path: '/certificates', icon: Lock },
    ],
  },
  {
    label: 'Service Mesh',
    items: [
      { name: 'Mesh Overview', path: '/mesh', icon: Workflow },
      { name: 'Mesh Policies', path: '/mesh-policies', icon: ShieldCheck },
    ],
  },
  {
    label: 'Cluster',
    items: [
      { name: 'Agents', path: '/agents', icon: Cpu },
      { name: 'Clusters', path: '/clusters', icon: Boxes },
      { name: 'Federation', path: '/federation', icon: Globe2 },
    ],
  },
  {
    label: 'Observability',
    items: [
      { name: 'Metrics', path: '/metrics-dashboard', icon: BarChart3 },
      { name: 'Traces', path: '/traces', icon: Waypoints },
      { name: 'Logs', path: '/logs', icon: ScrollText },
      { name: 'WAF Events', path: '/waf', icon: ShieldAlert },
    ],
  },
  {
    label: 'Extensions',
    items: [
      { name: 'WASM Plugins', path: '/wasm-plugins', icon: Puzzle },
    ],
  },
  {
    label: 'Settings',
    items: [
      { name: 'Configuration', path: '/config', icon: Settings },
    ],
  },
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
      <nav className="flex-1 overflow-y-auto px-3">
        {navGroups.map((group) => (
          <div key={group.label} className="mb-3">
            <div className="mb-1 px-3 text-[11px] font-semibold uppercase tracking-wider text-slate-500">
              {group.label}
            </div>
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const isActive = location.pathname === item.path ||
                  (item.path !== '/dashboard' && location.pathname.startsWith(item.path))

                return (
                  <NavLink
                    key={item.path}
                    to={item.path}
                    className={cn(
                      'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-slate-800 text-white'
                        : 'text-slate-400 hover:bg-slate-800/50 hover:text-white'
                    )}
                  >
                    <item.icon className="h-4 w-4" />
                    {item.name}
                  </NavLink>
                )
              })}
            </div>
          </div>
        ))}
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
