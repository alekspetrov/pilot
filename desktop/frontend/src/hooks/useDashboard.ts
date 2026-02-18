import { useEffect, useRef, useState } from 'react'
import { usePolling } from './usePolling'

// Wails runtime bindings — generated at build time, available globally in desktop app
declare const window: Window & {
  go?: {
    main?: {
      App?: {
        GetMetrics: () => Promise<MetricsData>
        GetQueueTasks: () => Promise<TaskDisplay[]>
        GetHistory: (limit: number) => Promise<HistoryEntry[]>
        GetServerStatus: () => Promise<ServerStatus>
        GetVersion: () => Promise<string>
        GetConfig: () => Promise<ConfigSummary>
        OpenInBrowser: (url: string) => Promise<void>
      }
    }
  }
}

export interface MetricsData {
  totalTokens: number
  inputTokens: number
  outputTokens: number
  totalCostUSD: number
  costPerTask: number
  totalTasks: number
  succeeded: number
  failed: number
  tokenHistory: number[]
  costHistory: number[]
  taskHistory: number[]
}

export interface TaskDisplay {
  id: string
  title: string
  status: 'pending' | 'queued' | 'running' | 'completed' | 'failed'
  phase: string
  progress: number
  duration: string
  issueURL: string
  prURL: string
}

export interface HistoryEntry {
  id: string
  title: string
  status: string
  duration: string
  completedAt: string
  parentID: string
  isEpic: boolean
  totalSubs: number
  doneSubs: number
}

export interface ServerStatus {
  running: boolean
  version: string
}

export interface ConfigSummary {
  gatewayPort: number
  autopilot: string
  adapters: string[]
}

interface DashboardState {
  metrics: MetricsData | null
  tasks: TaskDisplay[]
  history: HistoryEntry[]
  serverStatus: ServerStatus | null
  version: string
  config: ConfigSummary | null
  loading: boolean
}

const defaultMetrics: MetricsData = {
  totalTokens: 0,
  inputTokens: 0,
  outputTokens: 0,
  totalCostUSD: 0,
  costPerTask: 0,
  totalTasks: 0,
  succeeded: 0,
  failed: 0,
  tokenHistory: [0, 0, 0, 0, 0, 0, 0],
  costHistory: [0, 0, 0, 0, 0, 0, 0],
  taskHistory: [0, 0, 0, 0, 0, 0, 0],
}

function getApp() {
  return window.go?.main?.App
}

export function useDashboard() {
  const { tick } = usePolling()
  const [state, setState] = useState<DashboardState>({
    metrics: null,
    tasks: [],
    history: [],
    serverStatus: null,
    version: 'dev',
    config: null,
    loading: true,
  })

  const statusTickRef = useRef(0)

  useEffect(() => {
    const app = getApp()
    if (!app) {
      // Dev mode without Wails runtime — use defaults
      setState(s => ({ ...s, metrics: defaultMetrics, loading: false }))
      return
    }

    void Promise.all([
      app.GetMetrics().catch(() => defaultMetrics),
      app.GetQueueTasks().catch(() => [] as TaskDisplay[]),
      app.GetHistory(5).catch(() => [] as HistoryEntry[]),
      app.GetVersion().catch(() => 'dev'),
      app.GetConfig().catch(() => null),
    ]).then(([metrics, tasks, history, version, config]) => {
      setState(s => ({ ...s, metrics, tasks, history, version, config, loading: false }))
    })
  }, [tick])

  // Server status polling at 5s interval
  useEffect(() => {
    statusTickRef.current += 1
    if (statusTickRef.current % 5 !== 0) return

    const app = getApp()
    if (!app) return

    void app.GetServerStatus().then(status => {
      setState(s => ({ ...s, serverStatus: status }))
    }).catch(() => {
      setState(s => ({ ...s, serverStatus: { running: false, version: '' } }))
    })
  }, [tick])

  const openInBrowser = (url: string) => {
    const app = getApp()
    if (app) void app.OpenInBrowser(url)
    else window.open(url, '_blank')
  }

  return { ...state, openInBrowser }
}
