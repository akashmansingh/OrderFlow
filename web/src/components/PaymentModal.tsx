import { useRef, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { useCountdown } from '../hooks/useCountdown'
import type { Hold } from '../hooks/useHolds'
import { api, ApiError, errorMessage, type PaymentScenario, type Reservation } from '../lib/api'
import { formatClock, formatEventDate, newIdempotencyKey, plural } from '../lib/format'
import { Modal } from './Modal'

interface PaymentModalProps {
  hold: Hold | null
  onClose: () => void
  /** Called with the 202 response: the attempt is accepted, the worker settles it later. */
  onAccepted: (hold: Hold, reservation: Reservation) => void
}

export function PaymentModal({ hold, onClose, onAccepted }: PaymentModalProps) {
  return (
    <Modal open={hold !== null} onClose={onClose} label={hold ? `Pay for ${hold.eventName}` : 'Pay now'}>
      {hold && <Checkout hold={hold} onClose={onClose} onAccepted={onAccepted} />}
    </Modal>
  )
}

type Problem =
  | { kind: 'rate-limit'; retryAt: number }
  | { kind: 'error'; message: string }

const OUTCOMES: { value: PaymentScenario; title: string; note: string }[] = [
  { value: 'approve', title: 'Approve', note: 'The payment goes through and the tickets are confirmed.' },
  { value: 'decline', title: 'Decline', note: 'The payment fails. Your hold stays open so you can try again.' },
]

function Checkout({ hold, onClose, onAccepted }: { hold: Hold; onClose: () => void; onAccepted: PaymentModalProps['onAccepted'] }) {
  const { session } = useAuth()
  const [scenario, setScenario] = useState<PaymentScenario>('approve')
  const [pending, setPending] = useState(false)
  const [problem, setProblem] = useState<Problem | null>(null)
  const secondsLeft = useCountdown(hold.expiresAt, true)

  // One key per attempt. It survives retries of the same request (network error,
  // 429) and changes when the request changes or the server gave a definitive "no".
  // This modal remounts per opening, so a retry after a decline gets a fresh key.
  const keyRef = useRef(newIdempotencyKey())

  const wait = useCountdown(problem?.kind === 'rate-limit' ? problem.retryAt : null, problem?.kind === 'rate-limit')
  const waiting = wait !== null && wait > 0
  const date = formatEventDate(hold.eventStartsAt)
  const retrying = (hold.payment?.attempts ?? 0) > 0
  const expired = secondsLeft === 0

  function choose(next: PaymentScenario) {
    if (next === scenario) return
    keyRef.current = newIdempotencyKey()
    setScenario(next)
    setProblem(null)
  }

  async function pay() {
    if (!session) return
    setPending(true)
    setProblem(null)
    try {
      const r = await api.pay(session.token, hold.id, scenario, keyRef.current)
      onAccepted(hold, r)
      onClose()
    } catch (e) {
      if (e instanceof ApiError && e.status === 429) {
        setProblem({ kind: 'rate-limit', retryAt: Date.now() + (e.retryAfter ?? 15) * 1000 })
      } else {
        // 409 (already paid, in progress, hold over) and 404 are definitive answers.
        if (e instanceof ApiError && e.status >= 400 && e.status < 500) keyRef.current = newIdempotencyKey()
        setProblem({ kind: 'error', message: errorMessage(e) })
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="on-paper overflow-hidden rounded-lg bg-paper text-ink shadow-[6px_7px_0_var(--color-cobalt-deep)]">
      <div className="border-b-2 border-dashed border-ink/30 p-5 sm:p-6 pb-4 sm:pb-5">
        <p className="m-0 mb-1 font-mono text-xs font-semibold uppercase tracking-[0.14em] text-ink/60">
          {retrying ? 'Try again' : 'Checkout'}
        </p>
        <h2 className="m-0 font-display text-[clamp(2rem,7vw,3.2rem)] font-black uppercase leading-[0.9] tracking-tight">
          {hold.eventName}
        </h2>
        <p className="mb-0 mt-2 text-[0.95rem] font-medium text-ink/75">
          {plural(hold.quantity, 'ticket')}
          {date && <>. {date}</>}
        </p>
      </div>

      <div className="flex flex-col gap-5 p-5 sm:p-6">
        <dl className="m-0 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1.5 text-sm">
          <dt className="text-ink/70">Hold time left</dt>
          <dd className="m-0 text-right font-mono font-semibold tabular-nums">{formatClock(secondsLeft ?? 0)}</dd>
          {retrying && (
            <>
              <dt className="text-ink/70">Previous attempts</dt>
              <dd className="m-0 text-right font-mono font-semibold tabular-nums">{hold.payment?.attempts}</dd>
            </>
          )}
        </dl>

        <fieldset className="m-0 border-0 p-0">
          <legend className="mb-2.5 p-0 text-sm font-semibold">Simulated result</legend>
          <div role="radiogroup" className="grid gap-2 sm:grid-cols-2">
            {OUTCOMES.map((o) => {
              const on = o.value === scenario
              return (
                <button
                  key={o.value}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => choose(o.value)}
                  className={`cursor-pointer rounded-[5px] border-2 border-ink p-3 text-left transition-colors ${
                    on ? 'bg-ink text-paper' : 'bg-transparent text-ink hover:bg-ink/10'
                  }`}
                >
                  <span className="block font-display text-xl font-extrabold leading-none">{o.title}</span>
                  <span className={`mt-1.5 block text-[0.8rem] leading-snug ${on ? 'text-paper/80' : 'text-ink/70'}`}>
                    {o.note}
                  </span>
                </button>
              )
            })}
          </div>
          <p className="mb-0 mt-3 text-sm text-ink/70">
            This is a demo. No card is needed and no money moves; you are choosing what the pretend payment provider
            answers.
          </p>
        </fieldset>

        {expired && (
          <p role="alert" className="m-0 rounded-[5px] bg-vermilion px-4 py-3 text-sm font-semibold text-ink">
            The hold window is over, so these tickets can no longer be paid for.
          </p>
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
            disabled={pending || waiting || expired}
            onClick={pay}
          >
            {pending ? 'Sending…' : 'Pay now'}
          </button>
        </div>
      </div>
    </div>
  )
}
