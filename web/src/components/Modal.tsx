import { useEffect, useRef, type ReactNode } from 'react'

interface ModalProps {
  open: boolean
  onClose: () => void
  /** Accessible name for the dialog. */
  label: string
  variant?: 'center' | 'drawer'
  children: ReactNode
}

/**
 * Native <dialog>: focus trap, Esc to close and inert background come for free.
 * Children only mount while open, so forms reset every time.
 */
export function Modal({ open, onClose, label, variant = 'center', children }: ModalProps) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  return (
    <dialog
      ref={ref}
      aria-label={label}
      className={`modal modal-${variant}`}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      {open ? children : null}
    </dialog>
  )
}
