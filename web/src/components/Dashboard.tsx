import { useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { useEvents } from '../hooks/useEvents'
import { useHolds, type Hold } from '../hooks/useHolds'
import { api, errorMessage, isTerminal, type EventItem } from '../lib/api'
import { GRAFANA_URL, JAEGER_URL } from '../lib/config'
import { parseTraceparent } from '../lib/format'
import { AuthModal, type AuthTab } from './AuthModal'
import { EventList } from './EventList'
import { Header } from './Header'
import { ReservationModal } from './ReservationModal'
import { ReservationStatus } from './ReservationStatus'
import { TraceInspector } from './TraceInspector'

const DEMO_EVENTS = [
  { name: 'Coldplay Live', capacity: 50 },
  { name: 'Midnight Jazz Cellar', capacity: 20 },
  { name: 'Lakeside Film Night', capacity: 120 },
  { name: 'Synthwave Sunday', capacity: 30 },
]

/**
 * Mounted with key={email}, so per-user state (holds, polling) resets cleanly
 * when someone signs in or out.
 */
export function Dashboard() {
  const { session, notice, dismissNotice, signOut } = useAuth()
  const token = session?.token ?? null
  const { events, status, error, refresh } = useEvents(token)
  const { holds, add, update, clearFinished } = useHolds(session?.email ?? null)

  const [auth, setAuth] = useState<{ open: boolean; tab: AuthTab }>({ open: false, tab: 'login' })
  const [reservingId, setReservingId] = useState<string | null>(null)
  const [inspectOpen, setInspectOpen] = useState(false)
  const [inspectId, setInspectId] = useState<string | null>(null)
  const [newHoldId, setNewHoldId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const reservingEvent = events.find((e) => e.id === reservingId) ?? null
  const latestTrace = parseTraceparent(holds[0]?.traceparent)

  async function createDemoEvent() {
    if (!token) return
    setCreating(true)
    setCreateError(null)
    const demo = DEMO_EVENTS[events.length % DEMO_EVENTS.length]
    const start = new Date()
    start.setDate(start.getDate() + 14 + events.length * 5)
    start.setHours(20, 0, 0, 0)
    try {
      await api.createEvent(token, { name: demo.name, startsAt: start.toISOString(), capacity: demo.capacity })
      refresh()
    } catch (e) {
      setCreateError(errorMessage(e))
    } finally {
      setCreating(false)
    }
  }

  function handleReserved(hold: Hold) {
    add(hold)
    setNewHoldId(hold.id)
    setInspectId(hold.id)
    refresh()
    // On narrow screens the holds sit below the tickets; bring the new one into view.
    if (window.matchMedia('(max-width: 1023px)').matches) {
      window.setTimeout(() => {
        document.getElementById('holds-heading')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }, 150)
    }
  }

  const openAuth = (tab: AuthTab) => setAuth({ open: true, tab })
  const finishedCount = holds.filter((h) => isTerminal(h.status)).length

  return (
    <>
      <Header
        email={session?.email ?? null}
        traceHint={latestTrace?.traceId.slice(0, 8)}
        onSignIn={() => openAuth('login')}
        onSignOut={signOut}
        onOpenTrace={() => setInspectOpen(true)}
      />

      <main className="mx-auto grid max-w-[1280px] gap-8 sm:gap-10 px-4 sm:px-5 pb-20 pt-2 lg:grid-cols-[minmax(0,1fr)_23.5rem] lg:gap-14 lg:px-10">
        <div className="flex min-w-0 flex-col gap-8 sm:gap-12">
          <div className="flex flex-col gap-3 sm:gap-4">
            <h1 className="m-0 max-w-[15ch] font-display text-[clamp(2.4rem,7vw,5.6rem)] font-black uppercase leading-[0.88] tracking-tight">
              Hold the seats. We’ll settle the rest.
            </h1>
            <p className="m-0 max-w-[46ch] text-lg text-mist">
              Pick an event, choose your tickets and watch the reservation settle live. Holds last 10 minutes.
            </p>
          </div>

          {notice && (
            <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-[5px] bg-marigold px-4 py-3 text-ink">
              <span className="text-sm font-semibold">{notice}</span>
              <button type="button" className="btn btn-ink btn-sm" onClick={dismissNotice}>
                Dismiss
              </button>
            </div>
          )}

          <EventList
            signedIn={session !== null}
            events={events}
            status={status}
            error={error}
            creating={creating}
            createError={createError}
            onCreateDemo={createDemoEvent}
            onReserve={(e: EventItem) => setReservingId(e.id)}
            onRetry={refresh}
            onOpenAuth={openAuth}
          />
        </div>

        <aside aria-labelledby="holds-heading" className="min-w-0 lg:sticky lg:top-6 lg:max-h-[calc(100dvh-3rem)] lg:self-start lg:overflow-y-auto lg:pb-3 lg:pr-3">
          <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
            <h2 id="holds-heading" className="m-0 font-display text-[2rem] font-extrabold leading-none tracking-wide">
              Your holds
            </h2>
            {finishedCount > 0 && (
              <button type="button" className="btn btn-outline-light btn-sm" onClick={clearFinished}>
                Clear completed
              </button>
            )}
          </div>

          {holds.length === 0 ? (
            <div className="rounded-lg border-2 border-dashed border-mist/60 p-5">
              <p className="m-0 font-display text-[1.7rem] font-extrabold leading-tight">No holds yet</p>
              <p className="mb-0 mt-1.5 text-[0.95rem] text-mist">
                {session
                  ? 'Choose tickets for an event and the reservation shows up here while payment settles.'
                  : 'Sign in, then reserve tickets. You’ll follow each reservation here as it settles.'}
              </p>
            </div>
          ) : (
            <ul className="m-0 flex flex-col gap-7 p-0">
              {holds.map((hold) => (
                <ReservationStatus
                  key={hold.id}
                  hold={hold}
                  token={token ?? ''}
                  isNew={hold.id === newHoldId}
                  onUpdate={update}
                  onInspect={(id) => {
                    setInspectId(id)
                    setInspectOpen(true)
                  }}
                />
              ))}
            </ul>
          )}
        </aside>
      </main>

      <footer className="mx-auto max-w-[1280px] px-5 pb-10 text-sm text-mist lg:px-10">
        <p className="m-0 max-w-[70ch]">
          OrderFlow is a portfolio project. Payments are simulated and no card is ever charged. Watch the system behind
          it in{' '}
          <a className="text-paper underline underline-offset-2" href={JAEGER_URL} target="_blank" rel="noreferrer">
            Jaeger
          </a>{' '}
          and{' '}
          <a className="text-paper underline underline-offset-2" href={GRAFANA_URL} target="_blank" rel="noreferrer">
            Grafana
          </a>
          .
        </p>
      </footer>

      <AuthModal open={auth.open} initialTab={auth.tab} onClose={() => setAuth((a) => ({ ...a, open: false }))} />
      <ReservationModal
        event={reservingEvent}
        onClose={() => setReservingId(null)}
        onReserved={handleReserved}
        onConflict={refresh}
      />
      <TraceInspector
        open={inspectOpen}
        onClose={() => setInspectOpen(false)}
        holds={holds}
        selectedId={inspectId}
        onSelect={setInspectId}
      />
    </>
  )
}
