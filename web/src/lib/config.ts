export const API_URL = (import.meta.env.VITE_API_URL ?? '/api').replace(/\/$/, '')
export const JAEGER_URL = (import.meta.env.VITE_JAEGER_URL ?? 'http://localhost:16686').replace(/\/$/, '')
export const GRAFANA_URL = (import.meta.env.VITE_GRAFANA_URL ?? 'http://localhost:3000').replace(/\/$/, '')

/** Length of the hold window, in seconds. Used when the API doesn't send an expiry. */
export const HOLD_WINDOW_SECONDS = 600
export const MAX_QUANTITY = 10
export const EVENT_POLL_MS = 2500
export const RESERVATION_POLL_MS = 1500
