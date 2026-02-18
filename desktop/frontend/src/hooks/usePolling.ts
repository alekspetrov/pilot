import { useEffect, useRef, useState } from 'react'

/** Returns a monotonically increasing tick counter (1s interval) and a blinking toggle. */
export function usePolling() {
  const [tick, setTick] = useState(0)
  const [isBlinking, setIsBlinking] = useState(true)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    intervalRef.current = setInterval(() => {
      setTick(t => t + 1)
      setIsBlinking(b => !b)
    }, 1000)

    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current)
      }
    }
  }, [])

  return { tick, isBlinking }
}
