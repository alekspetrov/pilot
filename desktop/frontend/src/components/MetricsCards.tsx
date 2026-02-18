import { MetricsData } from '../hooks/useDashboard'
import { Sparkline } from './ui/Sparkline'

interface MetricsCardsProps {
  metrics: MetricsData
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

function fmtCost(n: number): string {
  if (n === 0) return '$0.00'
  if (n < 0.01) return `$${n.toFixed(4)}`
  return `$${n.toFixed(2)}`
}

export function MetricsCards({ metrics }: MetricsCardsProps) {
  return (
    <div className="grid grid-cols-3 gap-2 px-4">
      {/* TOKENS card */}
      <div className="border border-slate rounded p-2 flex flex-col gap-1">
        <div className="text-midgray text-xs">TOKENS</div>
        <div className="text-steel text-sm font-bold">{fmtTokens(metrics.totalTokens)}</div>
        <div className="text-gray text-xs">↑{fmtTokens(metrics.inputTokens)}</div>
        <div className="text-gray text-xs">↓{fmtTokens(metrics.outputTokens)}</div>
        <div className="mt-1">
          <Sparkline data={metrics.tokenHistory} color="#7eb8da" width={90} height={16} />
        </div>
      </div>

      {/* COST card */}
      <div className="border border-slate rounded p-2 flex flex-col gap-1">
        <div className="text-midgray text-xs">COST</div>
        <div className="text-sage text-sm font-bold">{fmtCost(metrics.totalCostUSD)}</div>
        <div className="text-gray text-xs">{fmtCost(metrics.costPerTask)}/task</div>
        <div className="mt-1">
          <Sparkline data={metrics.costHistory} color="#7ec699" width={90} height={16} />
        </div>
      </div>

      {/* QUEUE card */}
      <div className="border border-slate rounded p-2 flex flex-col gap-1">
        <div className="text-midgray text-xs">TASKS</div>
        <div className="text-lightgray text-sm font-bold">{metrics.totalTasks}</div>
        <div className="text-sage text-xs">✓ {metrics.succeeded}</div>
        <div className="text-rose text-xs">✗ {metrics.failed}</div>
        <div className="mt-1">
          <Sparkline data={metrics.taskHistory} color="#8b949e" width={90} height={16} />
        </div>
      </div>
    </div>
  )
}
