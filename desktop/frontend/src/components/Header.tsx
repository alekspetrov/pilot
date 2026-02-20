import React from 'react'

const PILOT_LOGO = `   ██████╗ ██╗██╗      ██████╗ ████████╗
   ██╔══██╗██║██║     ██╔═══██╗╚══██╔══╝
   ██████╔╝██║██║     ██║   ██║   ██║
   ██╔═══╝ ██║██║     ██║   ██║   ██║
   ██║     ██║███████╗╚██████╔╝   ██║
   ╚═╝     ╚═╝╚══════╝ ╚═════╝    ╚═╝`

interface HeaderProps {
  serverRunning: boolean
  version?: string
}

export function Header({ serverRunning, version }: HeaderProps) {
  return (
    <div className="px-3 py-2 border-b border-border">
      <div className="flex items-start justify-between">
        <div>
          <pre className="text-steel font-mono font-bold text-[8px] leading-[1.1] select-none">
            {PILOT_LOGO}
          </pre>
          {version && (
            <div className="text-steel text-[10px] mt-0.5 ml-[12px]">
              Pilot {version}
            </div>
          )}
        </div>
        <span className={`flex items-center gap-1 text-[10px] mt-1 ${serverRunning ? 'text-sage' : 'text-gray'}`}>
          <span className={`inline-block w-1.5 h-1.5 rounded-full ${serverRunning ? 'bg-sage pulse' : 'bg-gray'}`} />
          {serverRunning ? 'daemon running' : 'daemon offline'}
        </span>
      </div>
    </div>
  )
}
