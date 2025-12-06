import React, { createContext, useContext, useState, useEffect, useCallback } from 'react'
import type { ModeInfo } from '@/api/types'
import { api } from '@/api/client'

interface AppContextValue {
  mode: 'kubernetes' | 'standalone'
  readOnly: boolean
  namespace: string
  setNamespace: (ns: string) => void
  namespaces: string[]
  refreshNamespaces: () => Promise<void>
  exportConfig: () => Promise<void>
  showImportDialog: () => void
  showHistory: () => void
  isImportDialogOpen: boolean
  setIsImportDialogOpen: (open: boolean) => void
  isHistoryOpen: boolean
  setIsHistoryOpen: (open: boolean) => void
}

const AppContext = createContext<AppContextValue | null>(null)

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [mode, setMode] = useState<'kubernetes' | 'standalone'>('kubernetes')
  const [readOnly, setReadOnly] = useState(false)
  const [namespace, setNamespace] = useState('all')
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [isImportDialogOpen, setIsImportDialogOpen] = useState(false)
  const [isHistoryOpen, setIsHistoryOpen] = useState(false)

  // Fetch mode on mount
  useEffect(() => {
    async function fetchMode() {
      try {
        const modeInfo: ModeInfo = await api.getMode()
        setMode(modeInfo.mode)
        setReadOnly(modeInfo.readOnly)
      } catch (error) {
        console.error('Failed to fetch mode:', error)
      }
    }
    fetchMode()
  }, [])

  // Fetch namespaces
  const refreshNamespaces = useCallback(async () => {
    if (mode === 'kubernetes') {
      try {
        const ns = await api.namespaces.list()
        setNamespaces(ns)
      } catch (error) {
        console.error('Failed to fetch namespaces:', error)
      }
    }
  }, [mode])

  useEffect(() => {
    refreshNamespaces()
  }, [refreshNamespaces])

  // Export configuration
  const exportConfig = useCallback(async () => {
    try {
      const blob = await api.config.export(namespace)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'novaedge-config.yaml'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (error) {
      console.error('Failed to export config:', error)
    }
  }, [namespace])

  const showImportDialog = useCallback(() => {
    setIsImportDialogOpen(true)
  }, [])

  const showHistory = useCallback(() => {
    setIsHistoryOpen(true)
  }, [])

  const value: AppContextValue = {
    mode,
    readOnly,
    namespace,
    setNamespace,
    namespaces,
    refreshNamespaces,
    exportConfig,
    showImportDialog,
    showHistory,
    isImportDialogOpen,
    setIsImportDialogOpen,
    isHistoryOpen,
    setIsHistoryOpen,
  }

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}

export function useApp() {
  const context = useContext(AppContext)
  if (!context) {
    throw new Error('useApp must be used within an AppProvider')
  }
  return context
}
