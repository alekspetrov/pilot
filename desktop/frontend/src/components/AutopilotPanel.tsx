import { ServerStatus, ConfigSummary } from '../hooks/useDashboard'
import { Card } from './ui/Card'

interface AutopilotPanelProps {
  serverStatus: ServerStatus | null
  config: ConfigSummary | null
}

export function AutopilotPanel({ serverStatus, config }: AutopilotPanelProps) {
  const running = serverStatus?.running ?? false
  const autopilot = config?.autopilot ?? 'unknown'
  const adapters = config?.adapters ?? []

  return (
    <Card title="AUTOPILOT">
      <div className="flex flex-col gap-1 text-xs font-mono">
        <div className="flex justify-between">
          <span className="text-midgray">gateway</span>
          <span className={running ? 'text-sage' : 'text-rose'}>
            {running ? 'running' : 'offline'}
          </span>
        </div>
        <div className="flex justify-between">
          <span className="text-midgray">mode</span>
          <span className="text-lightgray">{autopilot || '—'}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-midgray">adapters</span>
          <span className="text-lightgray">
            {adapters.length > 0 ? adapters.join(', ') : 'none'}
          </span>
        </div>
      </div>
      {!running && (
        <div className="text-gray text-xs mt-1">
          run <span className="text-steel">pilot start</span> to enable live queue
        </div>
      )}
    </Card>
  )
}
