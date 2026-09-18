import { useState } from 'react'
import { api, type Stats } from '../api'
import { useLiveRefresh } from '../live'
import { Icon, StatCard } from '../components/ui'

const SEG = [
  ['verified', 'Verified', 'var(--green-500)'], ['manualResolved', 'Manually verified', 'var(--accent)'], ['gapFlagged', 'Coverage gap', 'var(--amber)'],
  ['needsReview', 'Needs review', 'var(--red)'], ['inProgress', 'In progress', 'var(--blue)'],
] as const

export function Reports() {
  const [s, setS] = useState<Stats | null>(null)
  useLiveRefresh(() => { api.stats().then(setS) }, [], 1500)
  if (!s) return <div className="page"><div className="empty">Loading…</div></div>

  const total = Math.max(s.total, 1)
  let acc = 0
  const arcs = SEG.map(([k, label, color]) => { const v = s[k]; const start = acc / total; acc += v; return { k, label, color, v, start, end: acc / total } })
  const maxPayer = Math.max(...s.byPayer.map((p) => p.count), 1)
  const maxDay = Math.max(...s.last7Days.map((d) => d.count), 1)

  return (
    <div className="page">
      <div className="page-head"><div><h1>Reports &amp; Analytics</h1><p>Gain insights into your verification activity</p></div><a className="btn btn-outline" href={api.reportPdfUrl()}><Icon name="download" size={15} /> Download PDF</a></div>
      <div className="grid grid-4">
        <StatCard icon="doc" tone="success" value={s.total.toLocaleString()} label="Total Verifications" />
        <StatCard icon="shield" tone="info" value={`${s.successRate.toFixed(0)}%`} label="Automated success rate" sub="verified or gap-flagged without a human" />
        <StatCard icon="alert" tone="error" value={s.needsReview} label="Needs Review" sub={Object.entries(s.reviewReasons).map(([k, v]) => `${v} ${k.replace('_', ' ')}`).join(' · ') || undefined} />
        <StatCard icon="clock" tone="warning" value={`${s.avgProcessingSeconds < 60 ? s.avgProcessingSeconds.toFixed(1) + ' s' : (s.avgProcessingSeconds / 60).toFixed(1) + ' min'}`} label="Avg. queue → result (24h)" sub="vs 20–30 min manual call" />
      </div>
      <div className="grid grid-2">
        <div className="card card-pad">
          <h3 style={{ marginBottom: 16 }}>Verification Status</h3>
          <div style={{ display: 'flex', gap: 24, alignItems: 'center' }}>
            <svg viewBox="0 0 120 120" width="180" height="180">
              {arcs.map((a) => a.v > 0 && <circle key={a.k} cx="60" cy="60" r="46" fill="none" stroke={a.color} strokeWidth="18" strokeDasharray={`${(a.end - a.start) * 289} 289`} strokeDashoffset={-a.start * 289} transform="rotate(-90 60 60)" />)}
              <text x="60" y="57" textAnchor="middle" fontSize="20" fontWeight="700" fill="var(--text)" className="num">{s.total}</text>
              <text x="60" y="73" textAnchor="middle" fontSize="9" fill="var(--muted)">Total</text>
            </svg>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 13 }}>
              {arcs.map((a) => <div key={a.k} style={{ display: 'flex', alignItems: 'center', gap: 8 }}><i style={{ width: 10, height: 10, borderRadius: 3, background: a.color }} />{a.label}<b className="num" style={{ marginLeft: 'auto', paddingLeft: 16 }}>{a.v} ({((a.v / total) * 100).toFixed(0)}%)</b></div>)}
            </div>
          </div>
        </div>
        <div className="card card-pad">
          <h3 style={{ marginBottom: 16 }}>Verifications by Payer</h3>
          {s.byPayer.map((p) => (
            <div key={p.payer} className="bar-row"><span>{p.payer}</span><div className="bar"><div style={{ width: `${(p.count / maxPayer) * 100}%` }} /></div><b className="num">{p.count}</b></div>
          ))}
        </div>
      </div>
      <div className="card card-pad">
        <h3 style={{ marginBottom: 16 }}>Last 7 days</h3>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 12, alignItems: 'end', height: 140 }}>
          {s.last7Days.map((d) => (
            <div key={d.day} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6, height: '100%', justifyContent: 'flex-end' }}>
              <b className="num" style={{ fontSize: 12 }}>{d.count}</b>
              <div style={{ width: '100%', background: 'var(--green-500)', borderRadius: 6, height: `${(d.count / maxDay) * 100}%`, minHeight: d.count ? 4 : 0 }} />
              <span className="cell-sub">{new Date(d.day).toLocaleDateString('en-US', { weekday: 'short' })}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
