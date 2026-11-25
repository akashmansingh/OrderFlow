import { useRef, useState, type KeyboardEvent } from 'react'
import { useAuth } from '../context/AuthContext'
import { useCountdown } from '../hooks/useCountdown'
import type { Hold } from '../hooks/useHolds'
import { api, ApiError, errorMessage, type EventItem } from '../lib/api'
import { HOLD_WINDOW_SECONDS, MAX_QUANTITY } from '../lib/config'
import { formatEventDate, newIdempotencyKey, plural } from '../lib/format'
import { Modal } from './Modal'

interface ReservationModalProps {
  /** Live event data (availability keeps updating while the modal is open). */
  event: EventItem | null
  onClose: () => void
  onReserved: (hold: Hold) => void
  /** Called after a 409 so the list can refresh its availability. */
  onConflict: () => void
}

export function ReservationModal({ event, onClose, onReserved, onConflict }: ReservationModalProps) {
  return (
    <Modal open={event !== null} onClose={onClose} label={event ? `Reserve tickets for ${event.name}` : 'Reserve tickets'}>
      {event && <Checkout event={event} onClose={onClose} onReserved={onReserved} onConflict={onConflict} />}
    </Modal>
  )
}

type Problem =
  | { kind: 'sold-out'; detail: string }
  | { kind: 'rate-limit'; retryAt: number }
  | { kind: 'error'; message: string }

function Checkout({
  event,
  onClose,
  onReserved,
  onConflict,
}: {
  event: EventItem
  onClose: () => void
  onReserved: (hold: Hold) => void
  onConflict: () => void
}) {
  const { session } = useAuth()
  const [quantity, setQuantity] = useState(1)
  const [pending, setPending] = useState(false)
  const [problem, setProblem] = useState<Problem | null>(null)

  // One key per checkout session. It is kept across retries of the same
  // request (network error, 429) and replaced when the request changes or
  // the server gave a definitive "no".
  const keyRef = useRef(newIdempotencyKey())

  const maxQuantity = Math.min(MAX_QUANTITY, Math.max(event.available, 0))
  const wait = useCountdown(problem?.kind === 'rate-limit' ? problem.retryAt : null, problem?.kind === 'rate-limit')
  const waiting = wait !== null && wait > 0
  const date = formatEventDate(event.startsAt)

  function choose(n: number) {
    if (n < 1 || n > MAX_QUANTITY || n === quantity) return
    keyRef.current = newIdempotencyKey()
    setQuantity(n)
    setProblem(null)
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
      e.preventDefault()
      choose(Math.min(quantity + 1, Math.max(maxQuantity, 1)))
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
      e.preventDefault()
      choose(Math.max(quantity - 1, 1))
    }
  }

  async function reserve() {
    if (!session) return
    setPending(true)
    setProblem(null)
    try {
      const r = await api.reserve(session.token, event.id, quantity, keyRef.current)
      const now = Date.now()
      onReserved({
        id: r.id,
        eventId: event.id,
        eventName: event.name,
        eventStartsAt: event.startsAt,
        quantity: r.quantity || quantity,
        status: r.status,
        createdAt: r.createdAt ?? now,
        expiresAt: r.expiresAt ?? now + HOLD_WINDOW_SECONDS * 1000,
        idempotencyKey: keyRef.current,
        traceparent: r.traceparent,
        payment: r.payment,
      })
      onClose()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        keyRef.current = newIdempotencyKey()
        setProblem({ kind: 'sold-out', detail: e.message })
        onConflict()
      } else if (e instanceof ApiError && e.status === 429) {
        setProblem({ kind: 'rate-limit', retryAt: Date.now() + (e.retryAfter ?? 15) * 1000 })
      } else {
        setProblem({ kind: 'error', message: errorMessage(e) })
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="on-paper overflow-hidden rounded-lg bg-paper text-ink shadow-[6px_7px_0_var(--color-cobalt-deep)]">
      <div className="border-b-2 border-dashed border-ink/30 p-5 sm:p-6 pb-4 sm:pb-5">
        <h2 className="m-0 font-display text-[clamp(2rem,7vw,3.2rem)] font-black uppercase leading-[0.9] tracking-tight">
          {event.name}
        </h2>
        <p className="mb-0 mt-2 text-[0.95rem] font-medium text-ink/75">
          {date && <>{date}. </>}
          {event.available > 0 ? `${plural(event.available, 'ticket')} left.` : 'No tickets left.'}
        </p>
      </div>

      <div className="flex flex-col gap-5 p-5 sm:p-6">
        <div>
          <p id="qty-label" className="m-0 mb-2.5 text-sm font-semibold">
            How many tickets?
          </p>
          <div
            role="radiogroup"
            aria-labelledby="qty-label"
            onKeyDown={onKeyDown}
            className="grid grid-cols-5 gap-1.5 sm:gap-2 sm:grid-cols-10"
          >
            {Array.from({ length: MAX_QUANTITY }, (_, i) => i + 1).map((n) => {
              const disabled = n > maxQuantity
              // Numbers above the live availability never look selected, even if
              // the pick was made before tickets sold out.
              const filled = n <= quantity && !disabled
              return (
                <button
                  key={n}
                  type="button"
                  role="radio"
                  aria-checked={n === quantity}
                  tabIndex={n === quantity ? 0 : -1}
                  disabled={disabled}
                  title={disabled ? `Only ${event.available} left` : undefined}
                  onClick={() => choose(n)}
                  className={`h-11 sm:h-12 cursor-pointer rounded-[4px] border-2 border-ink font-display text-[1.45rem] sm:text-[1.65rem] font-extrabold leading-none transition-colors disabled:cursor-not-allowed disabled:border-ink/20 disabled:text-ink/25 ${
                    filled ? 'bg-ink text-paper' : 'bg-transparent text-ink hover:bg-ink/10'
                  } ${n === quantity && !disabled ? 'ring-[3px] ring-marigold ring-offset-0' : ''}`}
                >
                  {n}
                </button>
              )
            })}
          </div>
          <p className="mb-0 mt-3 text-sm text-ink/75" aria-live="polite">
            {plural(quantity, 'ticket')} will be held for {HOLD_WINDOW_SECONDS / 60} minutes while payment is approved.
            {quantity > event.available && event.available > 0 && (
              <strong className="block text-ink"> Only {event.available} left right now.</strong>
            )}
          </p>
        </div>

        {problem?.kind === 'sold-out' && (
          <div role="alert" className="rounded-[5px] bg-vermilion px-4 py-3 text-ink">
            <p className="m-0 font-display text-2xl font-extrabold leading-tight">Sold out</p>
            <p className="m-0 mt-0.5 text-sm font-medium">
              There aren’t enough tickets left for that. Try fewer, or pick another event.
            </p>
            {problem.detail && <p className="m-0 mt-1 font-mono text-xs opacity-70">{problem.detail}</p>}
          </div>
        )}
        {problem?.kind === 'rate-limit' && (
          <p role="alert" className="m-0 rounded-[5px] bg-marigold px-4 py-3 text-sm font-semibold text-ink">
            Too many requests. {waiting ? `You can try again in ${wait}s.` : 'You can try again now.'}
          </p>
        )}
        {problem?.kind === 'error' && (
          <p role="alert" className="m-0 rounded-[5px] bg-vermilion px-4 py-3 text-sm font-semibold text-ink">
            {problem.message}
          </p>
        )}

        <div className="flex flex-wrap items-center justify-end gap-2 pt-2">
          <button type="button" className="btn btn-outline-ink max-sm:flex-1" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-ink min-w-40 max-sm:flex-1"
            disabled={pending || waiting || maxQuantity === 0}
            onClick={reserve}
          >
            {pending ? 'Reserving…' : 'Reserve tickets'}
          </button>
        </div>
      </div>
    </div>
  )
}
