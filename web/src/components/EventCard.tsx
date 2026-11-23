import { useEffect, useRef, type CSSProperties } from 'react'
import { motion } from 'motion/react'
import type { EventItem } from '../lib/api'
import { formatEventDate, plural } from '../lib/format'

interface EventCardProps {
  event: EventItem
  /** Position in the list; drives the staggered "print" entrance. */
  index: number
  onReserve: (event: EventItem) => void
}

const MAX_CELLS = 40

function PunchStrip({ capacity, available }: { capacity: number; available: number }) {
  const cells = Math.max(1, Math.min(capacity, MAX_CELLS))
  let sold = capacity > 0 ? Math.round((1 - available / capacity) * cells) : 0
  // Never let rounding hide the real state at either end.
  if (available > 0) sold = Math.min(sold, cells - 1)
  if (available < capacity) sold = Math.max(sold, 1)

  return (
    <div
      className="punch"
      role="img"
      aria-label={`${available} of ${capacity} tickets available`}
      style={{ maxWidth: `${cells * 13}px` }}
    >
      {Array.from({ length: cells }, (_, i) => (
        <i key={i} className={i < sold ? 'sold' : undefined} />
      ))}
    </div>
  )
}

export function EventCard({ event, index, onReserve }: EventCardProps) {
  const { available, capacity } = event
  const soldOut = available <= 0
  const ratio = capacity > 0 ? available / capacity : 0
  const tier = soldOut ? 'out' : ratio <= 0.2 ? 'low' : 'ok'
  const date = formatEventDate(event.startsAt)

  // Flip the number only when it changes after first paint.
  const previous = useRef(available)
  const changed = previous.current !== available
  useEffect(() => {
    previous.current = available
  }, [available])

  return (
    <motion.li
      className="ticket-wrap list-none"
      initial={{ clipPath: 'inset(0px 0px 100% 0px)', y: -16 }}
      animate={{ clipPath: 'inset(-24px -24px -24px -24px)', y: 0 }}
      transition={{ duration: 0.65, delay: Math.min(index, 6) * 0.1, ease: [0.2, 0.8, 0.2, 1] }}
    >
      <article className="ticket on-paper" style={{ '--stub': '10.5rem' } as CSSProperties}>
        <div className="ticket-main">
          <div className="min-w-0">
            <h3 className="m-0 line-clamp-2 font-display text-[clamp(2.1rem,5vw,3.5rem)] font-black uppercase leading-[0.9] tracking-tight">
              {event.name}
            </h3>
            {date && <p className="mb-0 mt-2 text-[0.98rem] font-medium text-ink/80">{date}</p>}
          </div>

          <div className="mt-auto flex flex-col gap-3">
            <div className="flex min-w-0 flex-col gap-1.5">
              <PunchStrip capacity={capacity} available={available} />
              <p className="m-0 text-sm text-ink/70">{plural(capacity, 'ticket')} in total</p>
            </div>
            <div>
              {soldOut ? (
                <span className="stamp stamp-failed">Sold out</span>
              ) : (
                <button
                  type="button"
                  className="btn btn-ink"
                  onClick={() => onReserve(event)}
                  aria-label={`Choose tickets for ${event.name}`}
                >
                  Choose tickets
                </button>
              )}
            </div>
          </div>
        </div>

        <div className={`ticket-stub stub-${tier}`} aria-live="polite">
          <span
            key={available}
            className={`font-display text-[4.6rem] font-black leading-[0.85] tracking-tight ${changed ? 'tick-flip' : ''}`}
          >
            {Math.max(available, 0)}
          </span>
          <span className="flex flex-col text-sm font-semibold leading-tight max-[36rem]:items-start">
            <span>{soldOut ? 'left' : available === 1 ? 'ticket left' : 'tickets left'}</span>
            <span className="font-normal opacity-75">of {capacity}</span>
          </span>
        </div>
      </article>
    </motion.li>
  )
}
