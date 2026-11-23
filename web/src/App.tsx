import { MotionConfig } from 'motion/react'
import { AuthProvider, useAuth } from './context/AuthContext'
import { Dashboard } from './components/Dashboard'

/** Hand-inked edges for the status stamps (referenced from index.css). */
function InkFilter() {
  return (
    <svg width="0" height="0" aria-hidden="true" focusable="false" className="absolute">
      <filter id="ink-rough" x="-5%" y="-5%" width="110%" height="110%">
        <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" seed="4" result="noise" />
        <feDisplacementMap in="SourceGraphic" in2="noise" scale="2.4" result="wobbly" />
        <feColorMatrix
          in="noise"
          type="matrix"
          values="0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  14 0 0 0 -4.9"
          result="speckle"
        />
        <feComposite in="wobbly" in2="speckle" operator="in" />
      </filter>
    </svg>
  )
}

function Shell() {
  const { session } = useAuth()
  return <Dashboard key={session?.email ?? 'signed-out'} />
}

export default function App() {
  return (
    <AuthProvider>
      <MotionConfig reducedMotion="user">
        <InkFilter />
        <Shell />
      </MotionConfig>
    </AuthProvider>
  )
}
