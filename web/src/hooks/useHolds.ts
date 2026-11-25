import { useCallback, useEffect, useState } from 'react'
import { isTerminal, type PaymentInfo, type ReservationStatus } from '../lib/api'

/** A reservation this browser is tracking, plus what the inspector needs to show. */
export interface Hold {
  id: string
  eventId: string
  eventName: string
  eventStartsAt: string
  quantity: number
  status: ReservationStatus
  createdAt: number
  expiresAt: number
  idempotencyKey: string
  traceparent?: string
  /** Only set when the backend runs in checkout mode. */
  payment?: PaymentInfo
}

const MAX_TRACKED = 8
const storageKey = (email: string) => `orderflow.holds.${email}`

function read(email: string | null): Hold[] {
  if (!email) return []
  try {
    const raw = localStorage.getItem(storageKey(email))
    const parsed = raw ? (JSON.parse(raw) as Hold[]) : []
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

/**
 * Holds persist per user so a refresh resumes polling. Mount this under a
 * component keyed by the user's email so the lists never mix.
 */
export function useHolds(email: string | null) {
  const [holds, setHolds] = useState<Hold[]>(() => read(email))

  useEffect(() => {
    if (!email) return
    try {
      localStorage.setItem(storageKey(email), JSON.stringify(holds))
    } catch {
      /* ignore quota / privacy-mode errors */
    }
  }, [email, holds])

  const add = useCallback((hold: Hold) => {
    setHolds((prev) => [hold, ...prev.filter((h) => h.id !== hold.id)].slice(0, MAX_TRACKED))
  }, [])

  const update = useCallback((id: string, patch: Partial<Pick<Hold, 'status' | 'expiresAt' | 'payment'>>) => {
    setHolds((prev) => {
      const current = prev.find((h) => h.id === id)
      if (!current) return prev
      const changed = (Object.keys(patch) as (keyof typeof patch)[]).some((k) => {
        const next = patch[k]
        if (next === undefined) return false
        // payment is an object, so compare by value or every poll would re-render.
        return k === 'payment' ? JSON.stringify(next) !== JSON.stringify(current[k]) : next !== current[k]
      })
      if (!changed) return prev
      return prev.map((h) => (h.id === id ? { ...h, ...patch } : h))
    })
  }, [])

  const clearFinished = useCallback(() => {
    setHolds((prev) => prev.filter((h) => !isTerminal(h.status)))
  }, [])

  return { holds, add, update, clearFinished }
}
