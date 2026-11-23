import { GRAFANA_URL } from '../lib/config'

interface HeaderProps {
  email: string | null
  /** Short trace id of the latest reservation, if any. */
  traceHint?: string
  onSignIn: () => void
  onSignOut: () => void
  onOpenTrace: () => void
}

function TicketGlyph() {
  return (
    <svg width="34" height="34" viewBox="0 0 32 32" aria-hidden="true" className="shrink-0">
      <path
        fill="#ffc21a"
        fillRule="evenodd"
        d="M3 7h26v5.5a3.5 3.5 0 0 0 0 7V25H3v-5.5a3.5 3.5 0 0 0 0-7z"
      />
      <path stroke="#0e1030" strokeWidth="1.6" strokeDasharray="2 2.4" d="M21 8.5v15" />
    </svg>
  )
}

export function Header({ email, traceHint, onSignIn, onSignOut, onOpenTrace }: HeaderProps) {
  return (
    <header className="mx-auto flex max-w-[1280px] items-center justify-between gap-3 px-4 py-4 sm:gap-4 sm:px-5 sm:py-5 lg:px-10">
      <a href="/" className="flex items-center gap-2 sm:gap-2.5 text-paper no-underline" aria-label="OrderFlow home">
        <TicketGlyph />
        <span className="font-display text-[1.85rem] sm:text-[2.1rem] font-black leading-none tracking-tight">OrderFlow</span>
      </a>

      <nav className="flex items-center gap-2 sm:gap-3" aria-label="Account and tools">
        <button type="button" className="btn btn-outline-light btn-sm whitespace-nowrap" onClick={onOpenTrace}>
          <span className="sm:hidden">Trace</span>
          <span className="hidden sm:inline">Trace inspector</span>
          {traceHint && <span className="hidden font-mono text-[0.72rem] font-medium text-mist sm:inline">{traceHint}</span>}
        </button>
        <a
          className="btn btn-outline-light btn-sm hidden md:inline-flex"
          href={GRAFANA_URL}
          target="_blank"
          rel="noreferrer"
        >
          Grafana
        </a>
        {email ? (
          <div className="flex items-center gap-2 sm:gap-3">
            <span className="hidden max-w-[14rem] truncate text-sm text-mist lg:inline" title={email}>
              {email}
            </span>
            <button type="button" className="btn btn-paper btn-sm whitespace-nowrap" onClick={onSignOut}>
              Sign out
            </button>
          </div>
        ) : (
          <button type="button" className="btn btn-paper btn-sm whitespace-nowrap" onClick={onSignIn}>
            Sign in
          </button>
        )}
      </nav>
    </header>
  )
}
