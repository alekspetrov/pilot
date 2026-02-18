import { HistoryEntry } from '../hooks/useDashboard'
import { StatusIcon } from './ui/StatusIcon'
import { Card } from './ui/Card'

interface HistoryPanelProps {
  history: HistoryEntry[]
  onOpenURL?: (url: string) => void
}

function timeAgo(isoStr: string): string {
  if (!isoStr) return ''
  const dt = new Date(isoStr)
  const now = Date.now()
  const diffMs = now - dt.getTime()
  const diffSec = Math.floor(diffMs / 1000)

  if (diffSec < 60) return 'just now'
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`

  return dt.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}

export function HistoryPanel({ history }: HistoryPanelProps) {
  return (
    <Card title="HISTORY">
      {history.length === 0 ? (
        <div className="text-gray text-xs py-1">no history</div>
      ) : (
        history.map(entry => (
          <div key={entry.id} className="flex flex-col gap-0.5">
            <div className="flex items-center gap-1.5 text-xs">
              <StatusIcon status={entry.status} />
              <span className="text-midgray shrink-0">{entry.id}</span>
              {entry.isEpic && (
                <span className="text-amber shrink-0 text-xs">
                  [{entry.doneSubs}/{entry.totalSubs}]
                </span>
              )}
              <span className="text-lightgray flex-1 truncate">{truncate(entry.title, 22)}</span>
              <span className="text-gray shrink-0 text-right text-xs">{timeAgo(entry.completedAt)}</span>
            </div>
            {entry.duration && (
              <div className="text-gray text-xs pl-4">{entry.duration}</div>
            )}
          </div>
        ))
      )}
    </Card>
  )
}
