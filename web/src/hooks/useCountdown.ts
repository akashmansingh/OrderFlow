import { useEffect, useState } from 'react'

/** Whole seconds left until `deadlineMs`, ticking while `active`. */
export function useCountdown(deadlineMs: number | null, active: boolean): number | null {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!active || deadlineMs === null) return
    setNow(Date.now())
    const id = window.setInterval(() => setNow(Date.now()), 250)
    return () => window.clearInterval(id)
  }, [active, deadlineMs])

  if (deadlineMs === null) return null
  return Math.max(0, Math.ceil((deadlineMs - now) / 1000))
}
