import { API_URL } from './config'

/* ------------------------------------------------------------------
   Types
------------------------------------------------------------------- */
export interface EventItem {
  id: string
  name: string
  startsAt: string
  capacity: number
  available: number
}

export type ReservationStatus = 'held' | 'confirmed' | 'cancelled' | 'expired' | (string & {})

export interface Reservation {
  id: string
  eventId: string
  quantity: number
  status: ReservationStatus
  expiresAt?: number
  createdAt?: number
  traceparent?: string
}

export interface CallRecord {
  id: number
  at: number
  method: string
  path: string
  status: number
  ms: number
  traceparent?: string
  idempotencyKey?: string
}

const TERMINAL = new Set(['confirmed', 'cancelled', 'expired'])
export const isTerminal = (status: string): boolean => TERMINAL.has(status)

/* ------------------------------------------------------------------
   Errors
------------------------------------------------------------------- */
export class ApiError extends Error {
  status: number
  retryAfter?: number
  constructor(status: number, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.retryAfter = retryAfter
  }
}

export const isAbort = (e: unknown): boolean => e instanceof DOMException && e.name === 'AbortError'

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : 'Something went wrong.'
}

/* ------------------------------------------------------------------
   Call log (feeds the trace inspector)
------------------------------------------------------------------- */
let callSeq = 0
let callSnapshot: CallRecord[] = []
const listeners = new Set<() => void>()

function pushCall(rec: Omit<CallRecord, 'id' | 'at'>) {
  callSnapshot = [{ ...rec, id: ++callSeq, at: Date.now() }, ...callSnapshot].slice(0, 30)
  listeners.forEach((l) => l())
}

export const callLog = {
  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },
  getSnapshot: () => callSnapshot,
}

/* ------------------------------------------------------------------
   401 handling — AuthContext registers a handler that signs the user out.
------------------------------------------------------------------- */
let unauthorizedHandler: (() => void) | null = null
export function onUnauthorized(fn: () => void): () => void {
  unauthorizedHandler = fn
  return () => {
    if (unauthorizedHandler === fn) unauthorizedHandler = null
  }
}

/* ------------------------------------------------------------------
   Transport
------------------------------------------------------------------- */
interface RequestOptions {
  token?: string | null
  body?: unknown
  headers?: Record<string, string>
  /** Skip the call log (used for polling). */
  quiet?: boolean
  signal?: AbortSignal
}

interface RawResponse {
  data: unknown
  status: number
  traceparent?: string
}

function messageFrom(data: unknown): string | undefined {
  if (typeof data === 'string' && data.trim()) return data.trim().slice(0, 200)
  if (data && typeof data === 'object') {
    const r = data as Record<string, unknown>
    for (const key of ['error', 'message', 'detail']) {
      const v = r[key]
      if (typeof v === 'string' && v) return v
      if (v && typeof v === 'object' && typeof (v as Record<string, unknown>).message === 'string') {
        return (v as Record<string, string>).message
      }
    }
  }
  return undefined
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value) return undefined
  const n = Number(value)
  if (Number.isFinite(n)) return Math.max(1, Math.ceil(n))
  const t = Date.parse(value)
  return Number.isNaN(t) ? undefined : Math.max(1, Math.ceil((t - Date.now()) / 1000))
}

async function request(method: string, path: string, opts: RequestOptions = {}): Promise<RawResponse> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers }
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'

  const started = performance.now()
  let res: Response
  try {
    res = await fetch(`${API_URL}${path}`, {
      method,
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    })
  } catch (e) {
    if (isAbort(e)) throw e
    if (!opts.quiet) {
      pushCall({
        method,
        path,
        status: 0,
        ms: Math.round(performance.now() - started),
        idempotencyKey: headers['Idempotency-Key'],
      })
    }
    throw new ApiError(0, "Can't reach the API. Check that the backend is running.")
  }

  const ms = Math.round(performance.now() - started)
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  const traceparent = res.headers.get('traceparent') ?? undefined

  if (!opts.quiet) {
    pushCall({ method, path, status: res.status, ms, traceparent, idempotencyKey: headers['Idempotency-Key'] })
  }

  if (!res.ok) {
    if (res.status === 401 && opts.token) unauthorizedHandler?.()
    throw new ApiError(
      res.status,
      messageFrom(data) ?? (res.statusText || `Request failed (${res.status})`),
      parseRetryAfter(res.headers.get('retry-after')),
    )
  }
  return { data, status: res.status, traceparent }
}

/* ------------------------------------------------------------------
   Normalisers — tolerant of snake_case / camelCase and list envelopes.
------------------------------------------------------------------- */
const asRecord = (v: unknown): Record<string, unknown> | null =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
const str = (v: unknown): string | undefined => (typeof v === 'string' && v ? v : undefined)
const num = (v: unknown): number | undefined => {
  const n = typeof v === 'string' ? Number(v) : v
  return typeof n === 'number' && Number.isFinite(n) ? n : undefined
}
const time = (v: unknown): number | undefined => {
  const s = str(v)
  if (!s) return undefined
  const t = Date.parse(s)
  return Number.isNaN(t) ? undefined : t
}

function toEvent(raw: unknown): (EventItem & { availableKnown: boolean }) | null {
  const r = asRecord(raw)
  if (!r) return null
  const id = str(r.id) ?? str(r.event_id)
  if (!id) return null
  const capacity = num(r.capacity) ?? num(r.total_capacity) ?? 0
  const available = num(r.available) ?? num(r.available_tickets) ?? num(r.availability) ?? num(r.remaining)
  return {
    id,
    name: str(r.name) ?? 'Untitled event',
    startsAt: str(r.starts_at) ?? str(r.startsAt) ?? '',
    capacity,
    available: available ?? capacity,
    availableKnown: available !== undefined,
  }
}

function stripFlag(e: EventItem & { availableKnown: boolean }): EventItem {
  return { id: e.id, name: e.name, startsAt: e.startsAt, capacity: e.capacity, available: e.available }
}

function toReservation(raw: unknown, fallbackTraceparent?: string): Reservation {
  const r = asRecord(raw)
  if (!r) throw new ApiError(500, 'The API returned an unexpected reservation response.')
  const id = str(r.id) ?? str(r.reservation_id)
  if (!id) throw new ApiError(500, 'The reservation response had no id.')
  let status = (str(r.status) ?? 'held').toLowerCase()
  if (status === 'canceled') status = 'cancelled'
  return {
    id,
    eventId: str(r.event_id) ?? str(r.eventId) ?? '',
    quantity: num(r.quantity) ?? 1,
    status,
    expiresAt: time(r.expires_at) ?? time(r.expiresAt) ?? time(r.hold_expires_at) ?? time(r.held_until),
    createdAt: time(r.created_at) ?? time(r.createdAt),
    traceparent: str(r.traceparent) ?? fallbackTraceparent,
  }
}

function listFrom(data: unknown): unknown[] {
  if (Array.isArray(data)) return data
  const r = asRecord(data)
  if (r) for (const k of ['events', 'data', 'items', 'results']) if (Array.isArray(r[k])) return r[k] as unknown[]
  return []
}

/* ------------------------------------------------------------------
   Endpoints
------------------------------------------------------------------- */
export const api = {
  async register(email: string, password: string): Promise<void> {
    await request('POST', '/v1/auth/register', { body: { email, password } })
  },

  async login(email: string, password: string): Promise<string> {
    const { data } = await request('POST', '/v1/auth/login', { body: { email, password } })
    const r = asRecord(data)
    const token = str(r?.access_token) ?? str(r?.accessToken) ?? str(r?.token)
    if (!token) throw new ApiError(500, 'The login response had no access token.')
    return token
  },

  async listEvents(token: string, signal?: AbortSignal): Promise<EventItem[]> {
    const { data } = await request('GET', '/v1/events', { token, signal, quiet: true })
    const parsed = listFrom(data)
      .map(toEvent)
      .filter((e): e is NonNullable<typeof e> => e !== null)

    // If the list endpoint omits live availability, read it per event.
    return Promise.all(
      parsed.map(async (e) => {
        if (e.availableKnown) return stripFlag(e)
        try {
          const one = toEvent((await request('GET', `/v1/events/${e.id}`, { token, signal, quiet: true })).data)
          return stripFlag(one ?? e)
        } catch (err) {
          if (isAbort(err)) throw err
          return stripFlag(e)
        }
      }),
    )
  },

  async createEvent(token: string, input: { name: string; startsAt: string; capacity: number }): Promise<EventItem> {
    const { data } = await request('POST', '/v1/events', {
      token,
      body: { name: input.name, starts_at: input.startsAt, capacity: input.capacity },
    })
    const e = toEvent(data)
    if (!e) throw new ApiError(500, 'The API returned an unexpected event response.')
    return stripFlag(e)
  },

  async reserve(
    token: string,
    eventId: string,
    quantity: number,
    idempotencyKey: string,
  ): Promise<Reservation> {
    const { data, traceparent } = await request('POST', `/v1/events/${eventId}/reservations`, {
      token,
      body: { quantity },
      headers: { 'Idempotency-Key': idempotencyKey },
    })
    const reservation = toReservation(data, traceparent)
    return { ...reservation, eventId: reservation.eventId || eventId, traceparent: traceparent ?? reservation.traceparent }
  },

  async getReservation(token: string, id: string, signal?: AbortSignal): Promise<Reservation> {
    const { data } = await request('GET', `/v1/reservations/${id}`, { token, signal, quiet: true })
    return toReservation(data)
  },
}
