interface SparklineProps {
  data: number[]
  color?: string
  width?: number
  height?: number
}

/** SVG bar sparkline normalized to 1-8 scale. */
export function Sparkline({
  data,
  color = '#7eb8da',
  width = 120,
  height = 20,
}: SparklineProps) {
  const max = Math.max(...data, 1)
  const barCount = data.length
  const barWidth = Math.floor(width / barCount) - 1
  const gap = 1

  const bars = data.map((val, i) => {
    const normalizedHeight = Math.max(2, Math.round((val / max) * (height - 2)))
    const x = i * (barWidth + gap)
    const y = height - normalizedHeight
    return { x, y, w: barWidth, h: normalizedHeight }
  })

  return (
    <svg width={width} height={height} style={{ display: 'block' }}>
      {bars.map((b, i) => (
        <rect key={i} x={b.x} y={b.y} width={b.w} height={b.h} fill={color} opacity={0.7} rx={1} />
      ))}
      {/* Pulsing dot on last bar */}
      {bars.length > 0 && (
        <circle
          cx={bars[bars.length - 1].x + barWidth / 2}
          cy={bars[bars.length - 1].y}
          r={2}
          fill={color}
          className="pulse"
        />
      )}
    </svg>
  )
}
