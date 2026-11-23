import { useCallback, useEffect, useRef, useState } from 'react'
import { api, errorMessage, isAbort, type EventItem } from '../lib/api'
import { EVENT_POLL_MS } from '../lib/config'

export type EventsStatus = 'idle' | 'loading' | 'ready' | 'error'

/** Polls the event list so the Available badge stays live. */
export function useEvents(token: string | null) {
  const [events, setEvents] = useState<EventItem[]>([])
  const [status, setStatus] = useState<EventsStatus>('idle')
  const [error, setError] = useState<string | null>(null)
  const refreshRef = useRef<() => void>(() => {})

  useEffect(() => {
    if (!token) {
      setEvents([])
      setStatus('idle')
      setError(null)
      refreshRef.current = () => {}
      return
    }

    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()
    setStatus('loading')

    const tick = async () => {
      window.clearTimeout(timer)
      try {
        const list = await api.listEvents(token, controller.signal)
        if (cancelled) return
        list.sort((a, b) => a.startsAt.localeCompare(b.startsAt) || a.name.localeCompare(b.name))
        setEvents(list)
        setStatus('ready')
        setError(null)
      } catch (e) {
        if (cancelled || isAbort(e)) return
        setError(errorMessage(e))
        // Keep showing the last good list if we have one.
        setStatus((prev) => (prev === 'ready' ? 'ready' : 'error'))
      }
      if (!cancelled) timer = window.setTimeout(tick, document.hidden ? 8000 : EVENT_POLL_MS)
    }

    refreshRef.current = () => void tick()
    void tick()

    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [token])

  const refresh = useCallback(() => refreshRef.current(), [])
  return { events, status, error, refresh }
}
