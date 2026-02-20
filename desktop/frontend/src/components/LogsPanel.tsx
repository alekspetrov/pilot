import React, { useRef, useEffect } from 'react'
import { Card } from './ui/Card'
import type { LogEntry } from '../types'

export type { LogEntry }

interface LogsPanelProps {
  entries: LogEntry[]
}

export function LogsPanel({ entries }: LogsPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries])

  return (
    <Card title="LOGS" className="flex-1 min-h-0">
      <div ref={scrollRef} className="overflow-y-auto h-full log-scroll">
        {entries.length === 0 ? (
          <div className="text-gray text-[10px]">no log entries</div>
        ) : (
          entries.map((e, i) => (
            <div key={i} className="flex gap-0 text-[10px] leading-tight py-px">
              {e.component && (
                <span className="text-steel shrink-0">[{e.component}]&nbsp;</span>
              )}
              <span className="text-lightgray truncate">{e.message}</span>
            </div>
          ))
        )}
      </div>
    </Card>
  )
}
