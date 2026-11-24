import { useEffect, useRef, useState } from 'react'
import { useCountdown } from '../hooks/useCountdown'
import type { Hold } from '../hooks/useHolds'
import { api, ApiError, isAbort, isTerminal } from '../lib/api'
import { HOLD_WINDOW_SECONDS, RESERVATION_POLL_MS } from '../lib/config'
import { formatClock, formatEventDate, plural } from '../lib/format'

interface ReservationStatusProps {
  hold: Hold
  token: string
  isNew?: boolean
  onUpdate: (id: string, patch: { status?: string; expiresAt?: number }) => void
  onInspect: (id: string) => void
}

type StepState = 'done' | 'active' | 'todo' | 'fail'
type Tone = 'hold' | 'ok' | 'bad'
interface Step {
  label: string
  state: StepState
  tone: Tone
}

/** Maps the backend status onto the three stations of the saga. */
function stepsFor(status: string): Step[] {
  switch (status) {
    case 'confirmed':
      return [
        { label: 'Held', state: 'done', tone: 'ok' },
        { label: 'Paid', state: 'done', tone: 'ok' },
        { label: 'Confirmed', state: 'done', tone: 'ok' },
      ]
    case 'cancelled':
      return [
        { label: 'Held', state: 'done', tone: 'hold' },
        { label: 'Declined', state: 'fail', tone: 'bad' },
        { label: 'Cancelled', state: 'fail', tone: 'bad' },
      ]
    case 'expired':
      return [
        { label: 'Held', state: 'done', tone: 'hold' },
        { label: 'Timed out', state: 'fail', tone: 'bad' },
        { label: 'Expired', state: 'fail', tone: 'bad' },
      ]
    default:
      return [
        { label: 'Held', state: 'done', tone: 'hold' },
        { label: 'Payment', state: 'active', tone: 'hold' },
        { label: 'Result', state: 'todo', tone: 'hold' },
      ]
  }
}

const toneBg: Record<Tone, string> = { hold: 'bg-marigold', ok: 'bg-signal', bad: 'bg-vermilion' }

function Stepper({ status }: { status: string }) {
  const steps = stepsFor(status)
  return (
    <ol className="m-0 flex list-none items-start p-0" aria-label="Reservation progress">
      {steps.map((step, i) => {
        const next = steps[i + 1]
        const lit = step.state === 'done' || step.state === 'fail'
        const connectorLit = lit && next && next.state !== 'todo'
        return (
          <li key={step.label} className={`flex items-start ${i < steps.length - 1 ? 'flex-1' : ''}`}>
            <div className="flex w-14 sm:w-16 flex-col items-center gap-1.5 text-center">
              <span
                className={`block size-[18px] rounded-full border-2 border-ink ${
                  step.state === 'todo' ? 'bg-transparent opacity-40' : toneBg[step.tone]
                } ${step.state === 'active' ? 'pulse-ring' : ''}`}
              />
              <span className={`text-[0.78rem] font-semibold leading-tight ${step.state === 'todo' ? 'text-ink/45' : ''}`}>
                {step.label}
              </span>
            </div>
            {next && (
              <span
                aria-hidden="true"
                className={`-ml-1 mt-[7px] h-[4px] flex-1 rounded-full ${
                  connectorLit ? toneBg[next.tone] : 'bg-ink/15'
                } ${connectorLit ? 'opacity-100' : ''}`}
              />
            )}
          </li>
        )
      })}
    </ol>
  )
}

/** Fixed-width digits so the clock doesn't jitter as it counts. */
function ClockFace({ text }: { text: string }) {
  return (
    <span className="inline-flex font-display text-[4rem] font-black leading-[0.85]" aria-hidden="true">
      {text.split('').map((ch, i) => (
        <span key={i} className={`inline-block text-center ${ch === ':' ? 'w-[0.3em]' : 'w-[0.5em]'}`}>
          {ch}
        </span>
      ))}
    </span>
  )
}

function MinuteCells({ secondsLeft }: { secondsLeft: number }) {
  const minutes = Math.ceil(HOLD_WINDOW_SECONDS / 60)
  const left = secondsLeft / 60
  return (
    <div className="flex gap-1" aria-hidden="true">
      {Array.from({ length: minutes }, (_, i) => (
        <span key={i} className="minute-cell">
          <b style={{ ['--fill' as string]: `${Math.min(1, Math.max(0, left - i)) * 100}%` }} />
        </span>
      ))}
    </div>
  )
}

const MESSAGES: Record<string, string> = {
  held: 'Tickets reserved! Click Pay Now below to complete payment.',
  confirmed: 'Payment approved. Reservation secured.',
  cancelled: 'Payment declined. Your tickets are back on sale.',
  expired: 'Hold expired. Your tickets are back on sale.',
}

export function ReservationStatus({ hold, token, isNew, onUpdate, onInspect }: ReservationStatusProps) {
  const terminal = isTerminal(hold.status)
  const secondsLeft = useCountdown(hold.expiresAt, !terminal)
  const [linkIssue, setLinkIssue] = useState<string | null>(null)
  const [paying, setPaying] = useState(false)
  const [payError, setPayError] = useState<string | null>(null)

  // Only slam the stamp if the status settles while we're watching.
  const watchedLive = useRef(!terminal)

  // Keep the latest callback without restarting the poll loop.
  const onUpdateRef = useRef(onUpdate)
  useEffect(() => {
    onUpdateRef.current = onUpdate
  }, [onUpdate])

  useEffect(() => {
    if (terminal) return
    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()

    const poll = async () => {
      let delay = RESERVATION_POLL_MS
      try {
        const r = await api.getReservation(token, hold.id, controller.signal)
        if (cancelled) return
        setLinkIssue(null)
        onUpdateRef.current(hold.id, { status: r.status, expiresAt: r.expiresAt })
        if (isTerminal(r.status)) return
      } catch (e) {
        if (cancelled || isAbort(e)) return
        if (e instanceof ApiError && e.status === 404) {
          setLinkIssue('The API no longer has this reservation.')
          return
        }
        setLinkIssue('Reconnecting…')
        delay = 3000
      }
      if (!cancelled) timer = window.setTimeout(poll, delay)
    }

    void poll()
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [hold.id, token, terminal])

  const confirmed = hold.status === 'confirmed'
  const message = paying ? 'Processing payment asynchronously…' : (MESSAGES[hold.status] ?? MESSAGES.held)
  const date = formatEventDate(hold.eventStartsAt)
  const windowOver = !terminal && secondsLeft === 0

  const handlePay = async () => {
    setPaying(true)
    setPayError(null)
    try {
      await api.payReservation(token, hold.id)
    } catch (err) {
      setPayError(err instanceof Error ? err.message : 'Payment failed to initiate')
      setPaying(false)
    }
  }

  return (
    <li className={`ticket-wrap list-none ${isNew ? 'hold-new rounded-lg' : ''}`}>
      <article
        className="on-paper relative overflow-hidden rounded-lg bg-paper p-4 sm:p-5 text-ink"
        aria-label={`Reservation for ${hold.eventName}`}
      >
<h3 className="m-0 max-w-[14ch] font-display text-[1.9rem] font-black uppercase leading-[0.92] tracking-tight">
          {hold.eventName}
        </h3>
        <p className="mb-0 mt-1.5 text-sm text-ink/75">
          {plural(hold.quantity, 'ticket')}
          {date && <>. {date}</>}
        </p>

        {terminal && (
          <div className="mt-3 flex">
            <span
              className={`stamp ${confirmed ? 'stamp-confirmed' : 'stamp-failed'} ${watchedLive.current ? 'stamp-slam' : ''}`}
            >
              {hold.status}
            </span>
          </div>
        )}

        {!terminal && (
          <div className="mt-4 flex flex-col gap-2.5">
            <div className="flex items-end justify-between gap-3">
              <div>
                <ClockFace text={formatClock(secondsLeft ?? 0)} />
                <span className="sr-only">{`${formatClock(secondsLeft ?? 0)} left to hold`}</span>
              </div>
              <p className="m-0 pb-1 text-right text-xs font-medium leading-tight text-ink/65">
                left on
                <br />
                your hold
              </p>
            </div>
            <MinuteCells secondsLeft={secondsLeft ?? 0} />
          </div>
        )}

        <div className="mt-5">
          <Stepper status={hold.status} />
        </div>

        <p role="status" className="mb-0 mt-4 text-[0.95rem] font-semibold leading-snug">
          {windowOver ? 'The hold window is over. Waiting for the final status…' : message}
        </p>
        {linkIssue && <p className="mb-0 mt-1 text-sm text-ink/60">{linkIssue}</p>}

        <div className="mt-4 flex items-center justify-between gap-3 border-t-2 border-dashed border-ink/25 pt-3">
          {!terminal && (
            <button
              type="button"
              className="btn btn-signal btn-sm flex items-center gap-1.5 font-bold shadow-sm hover:brightness-105 active:scale-[0.98] transition-all"
              disabled={paying || windowOver}
              onClick={handlePay}
            >
              {paying ? (
                <>
                  <span className="inline-block size-3 animate-spin rounded-full border-2 border-paper border-t-transparent" />
                  Processing…
                </>
              ) : (
                '💳 Pay Now'
              )}
            </button>
          )}
          <button type="button" className="btn btn-outline-ink btn-sm ml-auto" onClick={() => onInspect(hold.id)}>
            Inspect trace
          </button>
        </div>
        {payError && <p className="mb-0 mt-2 text-xs font-semibold text-vermilion">{payError}</p>}
      </article>
    </li>
  )
}
