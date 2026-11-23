import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, onUnauthorized } from '../lib/api'

interface Session {
  token: string
  email: string
}

interface AuthValue {
  session: Session | null
  /** Why the user was signed out, if it wasn't their choice. */
  notice: string | null
  signIn: (email: string, password: string) => Promise<void>
  register: (email: string, password: string) => Promise<void>
  signOut: () => void
  dismissNotice: () => void
}

const STORAGE_KEY = 'orderflow.session'
const EXPIRED = 'Your session ended. Sign in again to keep reserving.'

function jwtExpiryMs(token: string): number | null {
  try {
    const payload = token.split('.')[1]
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    const exp = (JSON.parse(json) as { exp?: number }).exp
    return typeof exp === 'number' ? exp * 1000 : null
  } catch {
    return null
  }
}

function readSession(): Session | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const s = JSON.parse(raw) as Session
    if (!s.token || !s.email) return null
    const exp = jwtExpiryMs(s.token)
    if (exp !== null && exp <= Date.now()) {
      localStorage.removeItem(STORAGE_KEY)
      return null
    }
    return s
  } catch {
    return null
  }
}

function writeSession(s: Session | null) {
  try {
    if (s) localStorage.setItem(STORAGE_KEY, JSON.stringify(s))
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* storage unavailable: the session lives in memory only */
  }
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(readSession)
  const [notice, setNotice] = useState<string | null>(null)

  const endSession = useCallback((reason: string | null) => {
    writeSession(null)
    setSession(null)
    setNotice(reason)
  }, [])

  // A 401 on any authenticated call ends the session.
  useEffect(() => onUnauthorized(() => endSession(EXPIRED)), [endSession])

  // Sign out when the short-lived access token runs out.
  useEffect(() => {
    if (!session) return
    const exp = jwtExpiryMs(session.token)
    if (exp === null) return
    const remaining = exp - Date.now()
    if (remaining <= 0) {
      endSession(EXPIRED)
      return
    }
    const t = window.setTimeout(() => endSession(EXPIRED), Math.min(remaining, 2 ** 31 - 1))
    return () => window.clearTimeout(t)
  }, [session, endSession])

  const signIn = useCallback(async (email: string, password: string) => {
    const token = await api.login(email, password)
    const next = { token, email }
    writeSession(next)
    setSession(next)
    setNotice(null)
  }, [])

  const register = useCallback(
    async (email: string, password: string) => {
      await api.register(email, password)
    },
    [],
  )

  const value = useMemo<AuthValue>(
    () => ({
      session,
      notice,
      signIn,
      register,
      signOut: () => endSession(null),
      dismissNotice: () => setNotice(null),
    }),
    [session, notice, signIn, register, endSession],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}
