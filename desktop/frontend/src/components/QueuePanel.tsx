import { TaskDisplay } from '../hooks/useDashboard'
import { StatusIcon } from './ui/StatusIcon'
import { ProgressBar } from './ui/ProgressBar'
import { Card } from './ui/Card'

interface QueuePanelProps {
  tasks: TaskDisplay[]
  onOpenURL: (url: string) => void
}

const stateOrder: Record<string, number> = {
  completed: 0,
  done:      0,
  running:   1,
  queued:    2,
  pending:   3,
  failed:    4,
}

function stateMeta(task: TaskDisplay): string {
  if (task.status === 'running') return `${task.progress}%`
  if (task.status === 'completed' || task.status === 'done') return 'done'
  if (task.status === 'queued') return 'queue'
  if (task.status === 'pending') return 'wait'
  if (task.status === 'failed') return 'fail'
  return ''
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}

export function QueuePanel({ tasks, onOpenURL }: QueuePanelProps) {
  const sorted = [...tasks].sort((a, b) => {
    const ao = stateOrder[a.status] ?? 3
    const bo = stateOrder[b.status] ?? 3
    return ao - bo
  })

  return (
    <Card title="QUEUE">
      {sorted.length === 0 ? (
        <div className="text-gray text-xs py-1">no tasks</div>
      ) : (
        sorted.map((task, i) => (
          <div
            key={task.id}
            className="flex flex-col gap-0.5 cursor-pointer hover:opacity-80"
            onClick={() => task.issueURL && onOpenURL(task.issueURL)}
          >
            <div className="flex items-center gap-1.5 text-xs">
              <StatusIcon status={task.status} pulse />
              <span className="text-gray w-8 shrink-0">{task.status.slice(0, 7)}</span>
              <span className="text-midgray shrink-0">{task.id}</span>
              <span className="text-lightgray flex-1 truncate">{truncate(task.title, 20)}</span>
              <span className="text-gray shrink-0 text-right w-10">{stateMeta(task)}</span>
            </div>
            <ProgressBar status={task.status} progress={task.progress} shimmerOffset={i} />
          </div>
        ))
      )}
    </Card>
  )
}
