import { useEffect, useState, type ReactNode } from 'react'
import { STATUS_META, type Batch, type Job, type JobStatus } from '../api'
import { useLive } from '../live'

export function StatusBadge({ status, job }: { status: JobStatus; job?: Job }) {
  const m = STATUS_META[status]
  const { maxAttempts } = useLive()
  const [, force] = useState(0)
  useEffect(() => {
    if (status !== 'RETRYING' || !job?.nextAttemptAt) return
    const t = setInterval(() => force((x) => x + 1), 1000)
    return () => clearInterval(t)
  }, [status, job?.nextAttemptAt])
  let label: string = m.label
  if (status === 'RETRYING' && job) {
    const secs = job.nextAttemptAt ? Math.max(0, Math.round((new Date(job.nextAttemptAt).getTime() - Date.now()) / 1000)) : 0
    label = `Retry ${job.attemptCount}/${maxAttempts} · ${secs > 0 ? `next in ${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}` : 'retrying…'}`
  }
  const live = status === 'PROCESSING' || status === 'RETRYING'
  return <span className={`badge tone-${m.tone} ${live ? 'pulse' : ''}`}><span className="dot" />{label}</span>
}

export function Icon({ name, size = 18 }: { name: string; size?: number }) {
  const p = { width: size, height: size, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
  switch (name) {
    case 'home': return <svg {...p}><path d="M3 11.5 12 4l9 7.5" /><path d="M5 10v10h14V10" /></svg>
    case 'shield': return <svg {...p}><path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" /><path d="m9 12 2 2 4-4" /></svg>
    case 'upload': return <svg {...p}><path d="M12 16V4" /><path d="m7 9 5-5 5 5" /><path d="M4 20h16" /></svg>
    case 'users': return <svg {...p}><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0" /><circle cx="17" cy="9" r="2.5" /><path d="M16 14.5a5 5 0 0 1 5.5 5.5" /></svg>
    case 'history': return <svg {...p}><path d="M3 12a9 9 0 1 0 3-6.7" /><path d="M3 4v5h5" /><path d="M12 7v5l3 2" /></svg>
    case 'review': return <svg {...p}><rect x="4" y="3" width="16" height="18" rx="2" /><path d="M8 8h8M8 12h8M8 16h5" /></svg>
    case 'chart': return <svg {...p}><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></svg>
    case 'settings': return <svg {...p}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></svg>
    case 'doc': return <svg {...p}><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" /><path d="M14 3v6h6M9 13h6M9 17h6" /></svg>
    case 'check': return <svg {...p}><circle cx="12" cy="12" r="9" /><path d="m8.5 12 2.5 2.5 4.5-5" /></svg>
    case 'clock': return <svg {...p}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></svg>
    case 'alert': return <svg {...p}><circle cx="12" cy="12" r="9" /><path d="M12 8v4M12 16h.01" /></svg>
    case 'user': return <svg {...p}><circle cx="12" cy="8" r="4" /><path d="M4 21a8 8 0 0 1 16 0" /></svg>
    case 'search': return <svg {...p}><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
    case 'bell': return <svg {...p}><path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15z" /><path d="M10 21h4" /></svg>
    case 'tooth': return <svg {...p} strokeWidth={0} fill="currentColor"><path d="M12 2.5c-1.6 0-2.4.9-3.6.9S6.3 2.5 4.9 2.5C3 2.5 2 4.3 2 6.5c0 3 1.6 4.8 2.2 7.3.6 2.6.9 7.7 2.7 7.7 1.4 0 1.7-3.3 2.4-5.3.4-1.1 1-1.7 2.7-1.7s2.3.6 2.7 1.7c.7 2 1 5.3 2.4 5.3 1.8 0 2.1-5.1 2.7-7.7.6-2.5 2.2-4.3 2.2-7.3 0-2.2-1-4-2.9-4-1.4 0-2.3.9-3.5.9S13.6 2.5 12 2.5z" /></svg>
    case 'arrow': return <svg {...p}><path d="M5 12h14M13 6l6 6-6 6" /></svg>
    case 'zap': return <svg {...p}><path d="M13 2 4 14h7l-1 8 9-12h-7z" /></svg>
    case 'mail': return <svg {...p}><rect x="3" y="5" width="18" height="14" rx="2" /><path d="m3 7 9 6 9-6" /></svg>
    case 'trash': return <svg {...p}><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" /><path d="M10 11v6M14 11v6" /></svg>
    case 'chat': return <svg {...p}><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" /></svg>
    case 'close': return <svg {...p}><path d="M18 6 6 18M6 6l12 12" /></svg>
    case 'send': return <svg {...p}><path d="m22 2-7 20-4-9-9-4Z" /><path d="M22 2 11 13" /></svg>
    default: return null
  }
}

export function StatCard({ icon, tone, value, label, sub, alert }: { icon: string; tone: string; value: ReactNode; label: string; sub?: ReactNode; alert?: boolean }) {
  return (
    <div className={`card stat ${alert ? 'alert' : ''}`}>
      <div className={`ico tone-${tone}`}><Icon name={icon} size={20} /></div>
      <div><b className="num">{value}</b><span>{label}</span>{sub && <small className={alert ? 'tone-error' : ''} style={{ background: 'none', color: alert ? 'var(--red)' : 'var(--green-900)' }}>{sub}</small>}</div>
    </div>
  )
}

export function BatchProgress({ b, showLegend = true }: { b: Batch; showLegend?: boolean }) {
  const t = Math.max(b.totalJobs, 1)
  const pct = (n: number) => `${(n / t) * 100}%`
  const done = b.verified + b.gapFlagged + b.needsReview + b.manualResolved
  return (
    <div>
      <div className="progress" title={`${done}/${b.totalJobs} complete`}>
        <div className="seg-verified" style={{ width: pct(b.verified) }} />
        <div className="seg-resolved" style={{ width: pct(b.manualResolved) }} />
        <div className="seg-gap" style={{ width: pct(b.gapFlagged) }} />
        <div className="seg-review" style={{ width: pct(b.needsReview) }} />
        <div className="seg-retrying" style={{ width: pct(b.retrying) }} />
        <div className="seg-processing" style={{ width: pct(b.processing) }} />
        <div className="seg-queued" style={{ width: pct(b.queued) }} />
      </div>
      {showLegend && (
        <div className="legend num">
          <span><i className="seg-queued" />Queued {b.queued}</span>
          <span><i className="seg-processing" />Processing {b.processing}</span>
          <span><i className="seg-retrying" />Retrying {b.retrying}</span>
          <span><i className="seg-verified" />Verified {b.verified}</span>
          <span><i className="seg-gap" />Coverage gap {b.gapFlagged}</span>
          <span><i className="seg-review" />Needs review {b.needsReview}</span>
          {b.manualResolved > 0 && <span><i className="seg-resolved" />Manually verified {b.manualResolved}</span>}
          <span style={{ marginLeft: 'auto', fontWeight: 600, color: 'var(--text)' }}>{Math.round((done / t) * 100)}%</span>
        </div>
      )}
    </div>
  )
}

export function Toast({ msg, err, onDone }: { msg: string; err?: boolean; onDone: () => void }) {
  useEffect(() => { const t = setTimeout(onDone, 4000); return () => clearTimeout(t) }, [onDone])
  return <div className={`toast ${err ? 'err' : ''}`}>{msg}</div>
}

export function useToast() {
  const [toast, setToast] = useState<{ msg: string; err?: boolean } | null>(null)
  const el = toast ? <Toast msg={toast.msg} err={toast.err} onDone={() => setToast(null)} /> : null
  return { show: (msg: string, err = false) => setToast({ msg, err }), el }
}

export function SelectionBar({ count, allSelected, onToggleAll, onClear, onDelete, deleting, label = 'row' }:
  { count: number; allSelected: boolean; onToggleAll: () => void; onClear: () => void; onDelete: () => void; deleting?: boolean; label?: string }) {
  return (
    <div className="selection-bar">
      <label className="selection-check"><input type="checkbox" checked={allSelected} onChange={onToggleAll} /> Select all on this page</label>
      {count > 0 && (
        <>
          <span className="cell-sub num">{count} {label}{count === 1 ? '' : 's'} selected</span>
          <button className="btn btn-ghost btn-sm" onClick={onClear}>Clear</button>
          <button className="btn btn-danger btn-sm" disabled={deleting} onClick={onDelete}>
            <Icon name="trash" size={14} /> Delete {count} selected
          </button>
        </>
      )}
    </div>
  )
}

export function Drawer({ title, onClose, children }: { title: ReactNode; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])
  return (
    <div className="drawer-bg" onClick={onClose}>
      <div className="drawer" onClick={(e) => e.stopPropagation()}>
        <div className="drawer-head"><h3>{title}</h3><button className="x" onClick={onClose} aria-label="Close">×</button></div>
        <div className="drawer-body">{children}</div>
      </div>
    </div>
  )
}
