interface StatusIconProps {
  status: string
  pulse?: boolean
}

const iconMap: Record<string, { char: string; color: string }> = {
  completed: { char: '✓', color: '#7ec699' },
  done:      { char: '✓', color: '#7ec699' },
  running:   { char: '●', color: '#7eb8da' },
  queued:    { char: '◌', color: '#8b949e' },
  pending:   { char: '·', color: '#3d4450' },
  failed:    { char: '✗', color: '#d48a8a' },
}

export function StatusIcon({ status, pulse }: StatusIconProps) {
  const icon = iconMap[status] ?? iconMap['pending']

  return (
    <span
      className={pulse && status === 'running' ? 'pulse' : undefined}
      style={{ color: icon.color, fontFamily: 'monospace' }}
    >
      {icon.char}
    </span>
  )
}
