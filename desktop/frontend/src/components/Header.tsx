interface HeaderProps {
  version: string
}

export function Header({ version }: HeaderProps) {
  return (
    <div className="flex items-baseline justify-between px-4 pt-4 pb-2">
      <span className="text-steel font-bold text-sm tracking-wider">PILOT</span>
      <span className="text-midgray text-xs">{version}</span>
    </div>
  )
}
