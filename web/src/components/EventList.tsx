import type { CSSProperties } from 'react'
import type { EventItem } from '../lib/api'
import type { EventsStatus } from '../hooks/useEvents'
import { EventCard } from './EventCard'

interface EventListProps {
  signedIn: boolean
  events: EventItem[]
  status: EventsStatus
  error: string | null
  creating: boolean
  createError: string | null
  onCreateDemo: () => void
  onReserve: (event: EventItem) => void
  onRetry: () => void
  onOpenAuth: (tab: 'login' | 'register') => void
}

function SkeletonTicket() {
  return (
    <li className="ticket-wrap list-none" aria-hidden="true">
      <div className="ticket" style={{ '--stub': '10.5rem' } as CSSProperties}>
        <div className="ticket-main">
          <div className="skeleton h-10 w-3/4" />
          <div className="skeleton h-4 w-1/3" />
          <div className="skeleton mt-4 h-9 w-40" />
        </div>
        <div className="ticket-stub stub-out" />
      </div>
    </li>
  )
}

function LockedTicket({ onOpenAuth }: { onOpenAuth: EventListProps['onOpenAuth'] }) {
  return (
    <div className="ticket-wrap">
      <div className="ticket on-paper" style={{ '--stub': '11rem' } as CSSProperties}>
        <div className="ticket-main justify-center">
          <h3 className="m-0 font-display text-[clamp(2rem,4.6vw,3.1rem)] font-black uppercase leading-[0.92] tracking-tight">
            Sign in to see what’s on sale
          </h3>
          <p className="m-0 max-w-[44ch] text-[0.98rem] text-ink/75">
            Make an account in a few seconds. Payments are simulated, so there’s no card to enter.
          </p>
        </div>
        <div className="ticket-stub stub-ok gap-2">
          <button type="button" className="btn btn-ink btn-sm w-full max-[36rem]:px-2 max-[36rem]:text-xs" onClick={() => onOpenAuth('login')}>
            Sign in
          </button>
          <button type="button" className="btn btn-outline-ink btn-sm w-full max-[36rem]:px-2 max-[36rem]:text-xs" onClick={() => onOpenAuth('register')}>
            Create account
          </button>
        </div>
      </div>
    </div>
  )
}

function Notice({ tone, children }: { tone: 'warn' | 'error'; children: React.ReactNode }) {
  return (
    <p
      role={tone === 'error' ? 'alert' : 'status'}
      className={`m-0 rounded-[5px] px-3.5 py-2.5 text-sm font-semibold text-ink ${
        tone === 'error' ? 'bg-vermilion' : 'bg-marigold'
      }`}
    >
      {children}
    </p>
  )
}

export function EventList(props: EventListProps) {
  const { signedIn, events, status, error, creating, createError, onCreateDemo, onReserve, onRetry, onOpenAuth } = props

  return (
    <section aria-labelledby="on-sale-heading" className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="on-sale-heading" className="m-0 font-display text-[2rem] font-extrabold leading-none tracking-wide">
          On sale
        </h2>
        {signedIn && (
          <button type="button" className="btn btn-outline-light btn-sm" onClick={onCreateDemo} disabled={creating}>
            {creating ? 'Creating…' : 'Create demo event'}
          </button>
        )}
      </div>

      {createError && <Notice tone="error">{createError}</Notice>}
      {signedIn && error && status === 'ready' && (
        <Notice tone="warn">Can’t refresh right now, so availability may be out of date. {error}</Notice>
      )}

      {!signedIn && <LockedTicket onOpenAuth={onOpenAuth} />}

      {signedIn && status === 'loading' && (
        <ul className="m-0 flex flex-col gap-7 p-0">
          <SkeletonTicket />
          <SkeletonTicket />
        </ul>
      )}

      {signedIn && status === 'error' && (
        <div className="flex flex-col items-start gap-3 rounded-lg border-2 border-dashed border-mist/60 p-6">
          <p className="m-0 font-display text-3xl font-extrabold">Events didn’t load</p>
          <p className="m-0 max-w-[52ch] text-mist">
            {error} If you’re running locally, start the API with <code className="font-mono text-paper">docker compose up -d --wait</code>.
          </p>
          <button type="button" className="btn btn-paper btn-sm" onClick={onRetry}>
            Try again
          </button>
        </div>
      )}

      {signedIn && status === 'ready' && events.length === 0 && (
        <div className="flex flex-col items-start gap-3 rounded-lg border-2 border-dashed border-mist/60 p-6">
          <p className="m-0 font-display text-3xl font-extrabold">Nothing on sale yet</p>
          <p className="m-0 max-w-[52ch] text-mist">
            Create a demo event to get something to reserve. It opens with 50 tickets.
          </p>
          <button type="button" className="btn btn-paper btn-sm" onClick={onCreateDemo} disabled={creating}>
            {creating ? 'Creating…' : 'Create demo event'}
          </button>
        </div>
      )}

      {signedIn && status === 'ready' && events.length > 0 && (
        <ul className="m-0 flex flex-col gap-7 p-0">
          {events.map((event, i) => (
            <EventCard key={event.id} event={event} index={i} onReserve={onReserve} />
          ))}
        </ul>
      )}
    </section>
  )
}
