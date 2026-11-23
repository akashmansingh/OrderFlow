import { useState, useSyncExternalStore } from 'react'
import type { Hold } from '../hooks/useHolds'
import { callLog, type CallRecord } from '../lib/api'
import { GRAFANA_URL, JAEGER_URL } from '../lib/config'
import { parseTraceparent } from '../lib/format'
import { Modal } from './Modal'

interface TraceInspectorProps {
  open: boolean
  onClose: () => void
  holds: Hold[]
  selectedId: string | null
  onSelect: (id: string) => void
}

const jaegerTrace = (traceId: string) => `${JAEGER_URL}/trace/${traceId}`

function CopyField({ label, value, large }: { label: string; value: string; large?: boolean }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard blocked: the value stays selectable */
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <span className="text-sm font-semibold text-mist">{label}</span>
        <button
          type="button"
          onClick={copy}
          className="cursor-pointer border-0 bg-transparent p-0 text-sm font-semibold text-marigold underline underline-offset-2"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <code
        className={`block select-all break-all rounded-[5px] bg-white/[0.07] px-3 py-2.5 font-mono text-paper ${
          large ? 'text-[0.95rem] font-medium' : 'text-[0.8rem]'
        }`}
      >
        {value}
      </code>
    </div>
  )
}

function statusTone(status: number): string {
  if (status >= 200 && status < 300) return 'text-signal'
  if (status >= 400 && status < 500) return 'text-marigold'
  return 'text-vermilion'
}

function CallRow({ call }: { call: CallRecord }) {
  const trace = parseTraceparent(call.traceparent)
  return (
    <li className="flex flex-col gap-1 border-t border-white/10 py-2.5 first:border-t-0">
      <div className="flex items-center gap-2 font-mono text-[0.8rem]">
        <span className="font-medium text-mist">{call.method}</span>
        <span className="min-w-0 flex-1 truncate text-paper" title={call.path}>
          {call.path}
        </span>
        <span className={`font-medium ${statusTone(call.status)}`}>{call.status || 'failed'}</span>
        <span className="text-mist/70">{call.ms}ms</span>
      </div>
      {trace && (
        <a
          className="w-fit font-mono text-[0.72rem] text-marigold underline underline-offset-2"
          href={jaegerTrace(trace.traceId)}
          target="_blank"
          rel="noreferrer"
        >
          trace {trace.traceId.slice(0, 12)}
        </a>
      )}
    </li>
  )
}

export function TraceInspector({ open, onClose, holds, selectedId, onSelect }: TraceInspectorProps) {
  const calls = useSyncExternalStore(callLog.subscribe, callLog.getSnapshot)
  const hold = holds.find((h) => h.id === selectedId) ?? holds[0] ?? null
  const trace = parseTraceparent(hold?.traceparent)

  return (
    <Modal open={open} onClose={onClose} label="Trace inspector" variant="drawer">
      <div className="flex h-full flex-col bg-ink text-paper shadow-[-6px_0_0_var(--color-cobalt-deep)]">
        <div className="flex items-center justify-between gap-3 border-b border-white/10 px-6 py-4">
          <h2 className="m-0 font-display text-[1.9rem] font-extrabold leading-none tracking-wide">Trace inspector</h2>
          <button type="button" className="btn btn-outline-light btn-sm" onClick={onClose}>
            Close
          </button>
        </div>

        <div className="flex flex-1 flex-col gap-7 overflow-y-auto px-6 py-6">
          <section className="flex flex-col gap-4" aria-label="Selected reservation">
            {hold ? (
              <>
                {holds.length > 1 ? (
                  <label className="flex flex-col gap-1.5 text-sm font-semibold text-mist">
                    Reservation
                    <select
                      value={hold.id}
                      onChange={(e) => onSelect(e.target.value)}
                      className="h-11 rounded-[5px] border-2 border-white/20 bg-ink px-3 text-[0.95rem] font-normal text-paper"
                    >
                      {holds.map((h) => (
                        <option key={h.id} value={h.id}>
                          {h.eventName} ({h.quantity}) {h.id.slice(0, 8)}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : (
                  <p className="m-0 text-[0.95rem] text-mist">
                    <span className="font-semibold text-paper">{hold.eventName}</span>, {hold.quantity}{' '}
                    {hold.quantity === 1 ? 'ticket' : 'tickets'}
                  </p>
                )}

                <CopyField label="Idempotency-Key sent" value={hold.idempotencyKey} />
                {hold.traceparent ? (
                  <CopyField label="traceparent returned" value={hold.traceparent} />
                ) : (
                  <p className="m-0 text-sm text-mist">
                    The API didn’t return a <code className="font-mono text-paper">traceparent</code> header for this
                    reservation. If the API is on another origin, expose it with{' '}
                    <code className="font-mono text-paper">Access-Control-Expose-Headers</code>.
                  </p>
                )}
                {trace && <CopyField label="Trace ID" value={trace.traceId} large />}

                <div className="flex flex-wrap gap-2">
                  {trace ? (
                    <a className="btn btn-paper btn-sm" href={jaegerTrace(trace.traceId)} target="_blank" rel="noreferrer">
                      Open trace in Jaeger
                    </a>
                  ) : (
                    <button type="button" className="btn btn-paper btn-sm" disabled>
                      Open trace in Jaeger
                    </button>
                  )}
                  <a className="btn btn-outline-light btn-sm" href={GRAFANA_URL} target="_blank" rel="noreferrer">
                    Open Grafana dashboards
                  </a>
                </div>
              </>
            ) : (
              <>
                <p className="m-0 text-[0.95rem] text-mist">
                  Reserve tickets and the request key, trace ID and Jaeger link show up here.
                </p>
                <div>
                  <a className="btn btn-outline-light btn-sm" href={GRAFANA_URL} target="_blank" rel="noreferrer">
                    Open Grafana dashboards
                  </a>
                </div>
              </>
            )}
          </section>

          <section aria-labelledby="calls-heading">
            <h3 id="calls-heading" className="m-0 mb-2 font-display text-[1.5rem] font-extrabold tracking-wide">
              Recent API calls
            </h3>
            {calls.length === 0 ? (
              <p className="m-0 text-sm text-mist">
                Nothing yet. Sign-ins, new events and reservations are listed here. Background refreshes are left out.
              </p>
            ) : (
              <ul className="m-0 list-none p-0">
                {calls.map((c) => (
                  <CallRow key={c.id} call={c} />
                ))}
              </ul>
            )}
          </section>
        </div>
      </div>
    </Modal>
  )
}
