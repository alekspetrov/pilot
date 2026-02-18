interface ProgressBarProps {
  status: string
  progress: number
  shimmerOffset?: number
}

const statusColors: Record<string, string> = {
  completed: '#7ec699',
  done:      '#7ec699',
  running:   '#7eb8da',
  failed:    '#d48a8a',
  queued:    '',
  pending:   '',
}

export function ProgressBar({ status, progress, shimmerOffset = 0 }: ProgressBarProps) {
  const width = Math.max(0, Math.min(100, progress))

  if (status === 'queued') {
    return (
      <div className="w-full h-1.5 rounded overflow-hidden">
        <div
          className="shimmer-bar h-full w-full"
          style={{ animationDelay: `${shimmerOffset * 0.3}s` }}
        />
      </div>
    )
  }

  if (status === 'pending') {
    return <div className="w-full h-1.5 rounded bg-slate" />
  }

  const fillColor = statusColors[status] ?? '#7eb8da'

  return (
    <div className="w-full h-1.5 rounded bg-slate overflow-hidden">
      <div
        className={`h-full rounded transition-all duration-300 ${status === 'running' ? 'pulse' : ''}`}
        style={{ width: `${width}%`, backgroundColor: fillColor }}
      />
    </div>
  )
}
