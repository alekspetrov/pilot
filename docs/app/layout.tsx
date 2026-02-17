import type { Metadata } from 'next'

export const metadata: Metadata = {
  title: {
    template: '%s – Pilot Docs',
    default: 'Pilot — AI That Ships Your Tickets'
  },
  description: 'Pilot documentation — autonomous AI development pipeline that turns tickets into pull requests',
  openGraph: {
    type: 'website',
    title: 'Pilot — AI That Ships Your Tickets',
    description: 'Autonomous AI development pipeline. Label a ticket, get a PR. Self-hosted, source-available.',
    url: 'https://pilot.quantflow.studio',
    images: 'https://pilot.quantflow.studio/pilot-preview.png',
    siteName: 'Pilot Docs'
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Pilot — AI That Ships Your Tickets',
    description: 'Autonomous AI development pipeline. Label a ticket, get a PR. Self-hosted, source-available.',
    images: 'https://pilot.quantflow.studio/pilot-preview.png'
  }
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <link rel="icon" href="/favicon.ico" />
      </head>
      <body>{children}</body>
    </html>
  )
}
