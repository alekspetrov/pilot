import { useDashboard } from './hooks/useDashboard'
import { Header } from './components/Header'
import { MetricsCards } from './components/MetricsCards'
import { QueuePanel } from './components/QueuePanel'
import { AutopilotPanel } from './components/AutopilotPanel'
import { HistoryPanel } from './components/HistoryPanel'

const defaultMetrics = {
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

export default function App() {
  const { metrics, tasks, history, serverStatus, version, config, openInBrowser } = useDashboard()

  return (
    <div
      className="flex flex-col gap-3 pb-4 min-h-screen"
      style={{ backgroundColor: '#1e222a', color: '#c9d1d9' }}
    >
      <Header version={version} />

      <MetricsCards metrics={metrics ?? defaultMetrics} />

      <div className="px-4">
        <QueuePanel tasks={tasks} onOpenURL={openInBrowser} />
      </div>

      <div className="px-4">
        <AutopilotPanel serverStatus={serverStatus} config={config} />
      </div>

      <div className="px-4">
        <HistoryPanel history={history} onOpenURL={openInBrowser} />
      </div>
    </div>
  )
}
