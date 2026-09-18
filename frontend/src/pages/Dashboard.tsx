import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, fmtDate, fmtDOB, fmtTime, type Job, type Payer, type Stats } from '../api'
import { useLive, useLiveRefresh } from '../live'
import { Drawer, Icon, StatCard, StatusBadge } from '../components/ui'
import { JobDetail } from '../components/JobDetail'

export function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [recent, setRecent] = useState<Job[]>([])
  const [payers, setPayers] = useState<Payer[]>([])
  const [open, setOpen] = useState<string | null>(null)
  const { rateLimits, connected } = useLive()
  const nav = useNavigate()

  useLiveRefresh(() => {
    api.stats().then(setStats).catch(() => {})
    api.jobs({ limit: 6 }).then((d) => setRecent(d.jobs)).catch(() => {})
  }, [])
  useLiveRefresh(() => { api.payers().then(setPayers).catch(() => {}) }, [], 60000)

  const q = stats?.byStatus ?? {}
  const throttling = rateLimits.filter((r) => r.throttlingNow)

  return (
    <div className="page">
      <div className="hero">
        <div><h1>Welcome back, Dr. Sarah!</h1><p>Verify insurance coverage in seconds, not hours.</p></div>
        <div className="quote">"Less paperwork.<br /><u>More smiles.</u>"</div>
      </div>

      <div className="grid grid-4">
        <StatCard icon="doc" tone="success" value={stats?.total ?? '—'} label="Total Verifications" sub={stats ? `${stats.last7Days.reduce((a, d) => a + d.count, 0)} in the last 7 days` : undefined} />
        <StatCard icon="check" tone="info" value={(stats?.verified ?? 0) + (stats?.manualResolved ?? 0)} label="Verified" sub={stats ? `${stats.successRate.toFixed(0)}% success rate` : undefined} />
        <StatCard icon="clock" tone="warning" value={stats?.inProgress ?? '—'} label="In Progress" sub={<Link to="/batch">View queue →</Link>} />
        <StatCard icon="alert" tone="error" value={stats?.needsReview ?? '—'} label="Needs Review" sub={<Link to="/review">Action required →</Link>} alert={(stats?.needsReview ?? 0) > 0} />
      </div>

      <div className="grid" style={{ gridTemplateColumns: '1fr 1fr 300px' }}>
        <div className="card card-pad" style={{ display: 'flex', gap: 14 }}>
          <div className="ico tone-success" style={{ width: 44, height: 44, borderRadius: 12, display: 'grid', placeItems: 'center', flex: 'none' }}><Icon name="user" /></div>
          <div style={{ flex: 1 }}>
            <h3>Verify a Single Patient</h3><p className="help" style={{ margin: '4px 0 14px' }}>Enter patient details to check insurance eligibility</p>
            <button className="btn btn-primary" onClick={() => nav('/verify')}>Start Verification <Icon name="arrow" size={16} /></button>
          </div>
        </div>
        <div className="card card-pad" style={{ display: 'flex', gap: 14 }}>
          <div className="ico tone-info" style={{ width: 44, height: 44, borderRadius: 12, display: 'grid', placeItems: 'center', flex: 'none' }}><Icon name="doc" /></div>
          <div style={{ flex: 1 }}>
            <h3>Upload a Patient List</h3><p className="help" style={{ margin: '4px 0 14px' }}>Verify multiple patients at once (CSV)</p>
            <button className="btn btn-outline" onClick={() => nav('/batch')}><Icon name="upload" size={16} /> Upload CSV</button>
          </div>
        </div>
        <div className="card card-pad">
          <h3 style={{ marginBottom: 12 }}>How it works</h3>
          <ol className="how" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
            <li><span className="n">1</span><div><b>Submit patient details</b><span>Every check is queued instantly — the UI never blocks</span></div></li>
            <li><span className="n">2</span><div><b>We verify with the payer</b><span>Real-time X12 270/271 via the Stedi clearinghouse</span></div></li>
            <li><span className="n">3</span><div><b>Get structured results</b><span>Plain-English brief, validated against the payer response</span></div></li>
          </ol>
        </div>
      </div>

      <div className="grid" style={{ gridTemplateColumns: '1fr 300px' }}>
        <div className="card">
          <div className="card-head"><h3>Recent Verifications</h3><Link className="link" to="/history">View all →</Link></div>
          <div className="tbl-wrap">
            <table className="tbl">
              <thead><tr><th>Patient</th><th>Payer</th><th>Submitted</th><th>Status</th><th>Coverage Summary</th><th></th></tr></thead>
              <tbody>
                {recent.length === 0 && <tr><td colSpan={6} className="empty">No verifications yet — start one above.</td></tr>}
                {recent.map((j) => (
                  <tr key={j.id} className="clickable" onClick={() => setOpen(j.id)}>
                    <td><div className="cell-main">{j.patientName}</div><div className="cell-sub">DOB: {fmtDOB(j.patientDob)}</div></td>
                    <td><div>{j.payerName}</div><div className="cell-sub">Payer ID: {j.stediPayerId}</div></td>
                    <td className="num"><div>{fmtDate(j.createdAt)}</div><div className="cell-sub">{fmtTime(j.createdAt)}</div></td>
                    <td><StatusBadge status={j.status} job={j} /></td>
                    <td className="cell-sub" style={{ maxWidth: 260 }}>{j.normalizedBrief?.summary ?? (j.status === 'NEEDS_MANUAL_REVIEW' ? 'Manual verification required' : '—')}</td>
                    <td><button className="btn btn-ghost btn-sm">View</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div className="card card-pad">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
              <h3>Queue Status</h3>
              <span className={`badge ${connected ? 'tone-success' : 'tone-error'}`}><span className="dot" />{connected ? 'Live' : 'Offline'}</span>
            </div>
            <div className="queue-rows num">
              <div className="queue-row"><span className="ico tone-neutral">◷</span>Queued<b>{q.QUEUED ?? 0}</b></div>
              <div className="queue-row"><span className="ico tone-info">⟳</span>Processing<b>{q.PROCESSING ?? 0}</b></div>
              <div className="queue-row"><span className="ico tone-violet">↻</span>Retrying<b>{q.RETRYING ?? 0}</b></div>
              <div className="queue-row"><span className="ico tone-error">!</span>Needs Review<b>{q.NEEDS_MANUAL_REVIEW ?? 0}</b></div>
            </div>
            {throttling.length > 0 && (
              <div className="notice blue" style={{ marginTop: 12 }}>
                <span>⏱</span><div>Processing throttled to respect {throttling.map((t) => payers.find((p) => p.stediPayerId === t.payer)?.name ?? t.payer).join(', ')} rate limit — expected, not stuck.</div>
              </div>
            )}
          </div>
          <div className="card card-pad">
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}><h3>Supported Payers</h3><Link className="link" to="/settings">View all</Link></div>
            <p className="help" style={{ margin: '0 0 12px' }}>{payers.filter((p) => p.supportsRealtime).length} real-time · 200+ dental payers via Stedi</p>
            <div className="payer-logos">
              {payers.map((p) => <span key={p.id} className={`payer-chip ${p.supportsRealtime ? '' : 'off'}`} title={p.supportsRealtime ? 'Real-time 270/271' : 'Manual verification only'}>{p.name}</span>)}
            </div>
          </div>
        </div>
      </div>

      <div className="footer-note"><span>"Technology should make care simpler, not harder."</span><span>Verafy · Built for healthier smiles · DSOLVE 2026</span></div>

      {open && <Drawer title="Verification detail" onClose={() => setOpen(null)}><JobDetail jobId={open} /></Drawer>}
    </div>
  )
}
