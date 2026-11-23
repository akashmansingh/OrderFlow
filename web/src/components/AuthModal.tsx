import { useId, useState, type FormEvent } from 'react'
import { useAuth } from '../context/AuthContext'
import { ApiError, errorMessage } from '../lib/api'
import { Modal } from './Modal'

export type AuthTab = 'login' | 'register'

interface AuthModalProps {
  open: boolean
  initialTab: AuthTab
  onClose: () => void
}

export function AuthModal({ open, initialTab, onClose }: AuthModalProps) {
  return (
    <Modal open={open} onClose={onClose} label="Sign in or create an account">
      <AuthForm initialTab={initialTab} onDone={onClose} onCancel={onClose} />
    </Modal>
  )
}

function AuthForm({ initialTab, onDone, onCancel }: { initialTab: AuthTab; onDone: () => void; onCancel: () => void }) {
  const { signIn, register } = useAuth()
  const [tab, setTab] = useState<AuthTab>(initialTab)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const uid = useId()

  const isLogin = tab === 'login'

  function switchTab(next: AuthTab) {
    setTab(next)
    setError(null)
    setSuccess(null)
    setEmail('')
    setPassword('')
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    setPending(true)
    setError(null)
    setSuccess(null)
    try {
      if (isLogin) {
        await signIn(email.trim(), password)
        onDone()
      } else {
        await register(email.trim(), password)
        setSuccess('Account created successfully! Please enter your details to sign in.')
        setTab('login')
        setEmail('')
        setPassword('')
      }
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) setError('That email and password don’t match. Check them and try again.')
        else if (err.status === 409) setError('An account with that email already exists. Sign in instead.')
        else if (err.status === 429) setError('Too many attempts. Wait a minute, then try again.')
        else setError(err.message)
      } else {
        setError(errorMessage(err))
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="on-paper overflow-hidden rounded-lg bg-paper text-ink shadow-[6px_7px_0_var(--color-cobalt-deep)]">
      <div role="tablist" aria-label="Account" className="grid grid-cols-2 border-b-2 border-dashed border-ink/30">
        {(
          [
            ['login', 'Sign in'],
            ['register', 'Create account'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            id={`${uid}-tab-${id}`}
            aria-selected={tab === id}
            aria-controls={`${uid}-panel`}
            onClick={() => switchTab(id)}
            className={`min-h-14 cursor-pointer border-0 font-display text-xl sm:text-2xl font-extrabold tracking-wide transition-colors ${
              tab === id ? 'bg-marigold text-ink' : 'bg-transparent text-ink/55 hover:text-ink'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <form
        id={`${uid}-panel`}
        role="tabpanel"
        aria-labelledby={`${uid}-tab-${tab}`}
        onSubmit={submit}
        className="flex flex-col gap-4 p-5 sm:p-6"
      >
        <p className="m-0 text-[0.95rem] text-ink/75">
          {isLogin
            ? 'Welcome back. Sign in to see what’s on sale.'
            : 'Make an account to reserve tickets. Payments here are simulated, so no card is needed.'}
        </p>

        <label className="flex flex-col gap-1.5 text-sm font-semibold" htmlFor={`${uid}-email`}>
          Email
          <input
            id={`${uid}-email`}
            type="email"
            required
            autoFocus
            autoComplete={isLogin ? 'username' : 'email'}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="h-12 rounded-[5px] border-2 border-ink/25 bg-white/60 px-3.5 text-base font-normal text-ink outline-none transition-colors placeholder:text-ink/35 focus:border-cobalt"
            placeholder="you@example.com"
          />
        </label>

        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between">
            <label className="text-sm font-semibold" htmlFor={`${uid}-password`}>
              Password
            </label>
            <button
              type="button"
              onClick={() => setShowPassword((s) => !s)}
              className="cursor-pointer border-0 bg-transparent p-0 text-sm font-semibold text-cobalt underline underline-offset-2"
              aria-pressed={showPassword}
            >
              {showPassword ? 'Hide' : 'Show'}
            </button>
          </div>
          <input
            id={`${uid}-password`}
            type={showPassword ? 'text' : 'password'}
            required
            autoComplete={isLogin ? 'current-password' : 'new-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="h-12 rounded-[5px] border-2 border-ink/25 bg-white/60 px-3.5 text-base font-normal text-ink outline-none transition-colors focus:border-cobalt"
          />
        </div>

        {success && (
          <p role="status" className="m-0 rounded-[5px] bg-signal px-3.5 py-2.5 text-sm font-semibold text-paper">
            {success}
          </p>
        )}

        {error && (
          <p role="alert" className="m-0 rounded-[5px] bg-vermilion px-3.5 py-2.5 text-sm font-semibold text-ink">
            {error}
          </p>
        )}

        <div className="mt-1 flex items-center justify-end gap-2">
          <button type="button" className="btn btn-outline-ink max-sm:flex-1" onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className="btn btn-ink min-w-40 max-sm:flex-1" disabled={pending}>
            {pending ? (isLogin ? 'Signing in…' : 'Creating account…') : isLogin ? 'Sign in' : 'Create account'}
          </button>
        </div>
      </form>
    </div>
  )
}
