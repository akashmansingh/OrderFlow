// Mock OrderFlow API for developing the UI without Docker.
// Mirrors the real contract: auth, events, idempotent reservations, 409 sold
// out, 429 rate limits, a `traceparent` header, and an async held -> settled saga.
//
//   MOCK_PORT          default 8787
//   MOCK_PAYMENT       approve (default) | reject | random | stall
//   MOCK_HOLD_SECONDS  default 600 (use e.g. 20 with MOCK_PAYMENT=stall to see expiry)
//   MOCK_SETTLE_MS     default 3500 (time the "worker" takes to settle payment)
import { createServer } from 'node:http'
import { randomBytes, randomUUID } from 'node:crypto'

const PORT = Number(process.env.MOCK_PORT ?? 8787)
const PAYMENT = process.env.MOCK_PAYMENT ?? 'approve'
const AUTO_SETTLE = process.env.AUTO_SETTLE === 'true'
const HOLD_MS = Number(process.env.MOCK_HOLD_SECONDS ?? 600) * 1000
const SETTLE_MS = Number(process.env.MOCK_SETTLE_MS ?? 3500)

const users = new Map() // email -> password
const events = new Map()
const reservations = new Map()
const idempotent = new Map() // `${user}:${key}` -> { status, body, traceparent }
const hits = new Map() // rate-limit buckets

const b64 = (o) => Buffer.from(JSON.stringify(o)).toString('base64url')
const makeToken = (email) =>
  `${b64({ alg: 'HS256', typ: 'JWT' })}.${b64({ sub: email, exp: Math.floor(Date.now() / 1000) + 15 * 60 })}.mock`

function userFrom(req) {
  const m = /^Bearer (.+)$/.exec(req.headers.authorization ?? '')
  if (!m) return null
  try {
    const p = JSON.parse(Buffer.from(m[1].split('.')[1], 'base64url').toString())
    return p.exp * 1000 > Date.now() ? p.sub : null
  } catch {
    return null
  }
}

function limited(bucket, max) {
  const now = Date.now()
  const recent = (hits.get(bucket) ?? []).filter((t) => now - t < 60_000)
  if (recent.length >= max) {
    hits.set(bucket, recent)
    return Math.ceil((60_000 - (now - recent[0])) / 1000)
  }
  recent.push(now)
  hits.set(bucket, recent)
  return 0
}

const traceparent = () => `00-${randomBytes(16).toString('hex')}-${randomBytes(8).toString('hex')}-01`

function send(res, status, body, headers = {}) {
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Expose-Headers': 'traceparent, Retry-After',
    ...headers,
  })
  res.end(body === undefined ? '' : JSON.stringify(body))
}

const readBody = (req) =>
  new Promise((resolve) => {
    let raw = ''
    req.on('data', (c) => (raw += c))
    req.on('end', () => {
      try {
        resolve(raw ? JSON.parse(raw) : {})
      } catch {
        resolve(null)
      }
    })
  })

const publicEvent = (e) => ({ id: e.id, name: e.name, starts_at: e.starts_at, capacity: e.capacity, available: e.available })
const publicReservation = (r) => ({
  id: r.id,
  event_id: r.event_id,
  customer_id: r.customer_id,
  quantity: r.quantity,
  status: r.status,
  created_at: r.created_at,
  expires_at: r.expires_at,
  traceparent: r.traceparent,
})

function settle(r) {
  const finish = (status) => {
    if (r.status !== 'held') return
    r.status = status
    if (status !== 'confirmed') events.get(r.event_id).available += r.quantity
    console.log(`[saga] ${r.id.slice(0, 8)} -> ${status}`)
  }
  setTimeout(() => {
    if (PAYMENT === 'stall') return
    const approve = PAYMENT === 'approve' || (PAYMENT === 'random' && Math.random() > 0.35)
    finish(approve ? 'confirmed' : 'cancelled')
  }, SETTLE_MS + Math.random() * 1500)
  setTimeout(() => finish('expired'), HOLD_MS)
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x')
  const path = url.pathname
  const method = req.method

  if (method === 'OPTIONS') {
    return send(res, 204, undefined, {
      'Access-Control-Allow-Headers': 'Authorization, Content-Type, Idempotency-Key',
      'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
    })
  }
  console.log(method, path)

  if (path === '/healthz' || path === '/readyz') return send(res, 200, { status: 'ok' })

  if (method === 'POST' && path === '/v1/auth/register') {
    const b = await readBody(req)
    if (!b?.email || !b?.password) return send(res, 400, { error: 'email and password are required' })
    if (users.has(b.email)) return send(res, 409, { error: 'email already registered' })
    users.set(b.email, b.password)
    return send(res, 201, { id: randomUUID(), email: b.email })
  }

  if (method === 'POST' && path === '/v1/auth/login') {
    const wait = limited(`login:${req.socket.remoteAddress}`, 5)
    if (wait) return send(res, 429, { error: 'too many requests' }, { 'Retry-After': String(wait) })
    const b = await readBody(req)
    if (!b || users.get(b.email) !== b.password) return send(res, 401, { error: 'invalid credentials' })
    return send(res, 200, { access_token: makeToken(b.email), token_type: 'Bearer', expires_in: 900 })
  }

  const user = userFrom(req)
  if (!path.startsWith('/v1/')) return send(res, 404, { error: 'not found' })
  if (!user) return send(res, 401, { error: 'missing or invalid bearer token' })

  if (path === '/v1/events' && method === 'GET') return send(res, 200, [...events.values()].map(publicEvent))

  if (path === '/v1/events' && method === 'POST') {
    const b = await readBody(req)
    if (!b?.name || !b?.starts_at || !(b?.capacity > 0)) return send(res, 400, { error: 'name, starts_at and capacity are required' })
    const e = { id: randomUUID(), name: b.name, starts_at: b.starts_at, capacity: b.capacity, available: b.capacity }
    events.set(e.id, e)
    return send(res, 201, publicEvent(e))
  }

  let m = /^\/v1\/events\/([^/]+)$/.exec(path)
  if (m && method === 'GET') {
    const e = events.get(m[1])
    return e ? send(res, 200, publicEvent(e)) : send(res, 404, { error: 'event not found' })
  }

  m = /^\/v1\/events\/([^/]+)\/reservations$/.exec(path)
  if (m && method === 'POST') {
    const wait = limited(`reserve:${user}`, 10)
    if (wait) return send(res, 429, { error: 'rate limit exceeded' }, { 'Retry-After': String(wait) })
    const key = req.headers['idempotency-key']
    if (!key) return send(res, 400, { error: 'Idempotency-Key header is required' })
    const b = await readBody(req)
    const e = events.get(m[1])
    if (!e) return send(res, 404, { error: 'event not found' })
    if (!Number.isInteger(b?.quantity) || b.quantity < 1 || b.quantity > 10) return send(res, 400, { error: 'quantity must be 1-10' })

    const replay = idempotent.get(`${user}:${key}`)
    if (replay) return send(res, replay.status, replay.body, { traceparent: replay.traceparent })

    if (b.quantity > e.available) {
      const body = { error: 'insufficient inventory' }
      return send(res, 409, body)
    }
    e.available -= b.quantity
    const now = Date.now()
    const r = {
      id: randomUUID(),
      event_id: e.id,
      customer_id: user,
      quantity: b.quantity,
      status: 'held',
      created_at: new Date(now).toISOString(),
      expires_at: new Date(now + HOLD_MS).toISOString(),
      traceparent: traceparent(),
      owner: user,
    }
    reservations.set(r.id, r)
    if (AUTO_SETTLE) settle(r)
    setTimeout(() => {
      if (r.status === 'held') {
        r.status = 'expired'
        events.get(r.event_id).available += r.quantity
        console.log(`[saga] ${r.id.slice(0, 8)} -> expired`)
      }
    }, HOLD_MS)
    const body = publicReservation(r)
    idempotent.set(`${user}:${key}`, { status: 201, body, traceparent: r.traceparent })
    return send(res, 201, body, { traceparent: r.traceparent })
  }

  m = /^\/v1\/reservations\/([^/]+)\/pay$/.exec(path)
  if (m && method === 'POST') {
    const r = reservations.get(m[1])
    if (!r || r.owner !== user) return send(res, 404, { error: 'reservation not found' })
    if (r.status === 'confirmed') return send(res, 200, publicReservation(r))
    if (r.status !== 'held') return send(res, 409, { error: 'reservation is not held' })
    settle(r)
    return send(res, 200, publicReservation(r))
  }

  m = /^\/v1\/reservations\/([^/]+)$/.exec(path)
  if (m && method === 'GET') {
    const r = reservations.get(m[1])
    return r && r.owner === user ? send(res, 200, publicReservation(r)) : send(res, 404, { error: 'reservation not found' })
  }

  send(res, 404, { error: 'not found' })
})

server.listen(PORT, () => {
  console.log(`Mock OrderFlow API on http://localhost:${PORT}  (payment=${PAYMENT}, hold=${HOLD_MS / 1000}s)`)
})
