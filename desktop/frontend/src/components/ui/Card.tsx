import { ReactNode } from 'react'

interface CardProps {
  title: string
  children: ReactNode
  className?: string
}

export function Card({ title, children, className = '' }: CardProps) {
  return (
    <div
      className={`border border-slate rounded p-3 flex flex-col gap-2 ${className}`}
      style={{ backgroundColor: '#1e222a' }}
    >
      <div className="text-midgray text-xs uppercase tracking-widest">{title}</div>
      {children}
    </div>
  )
}
