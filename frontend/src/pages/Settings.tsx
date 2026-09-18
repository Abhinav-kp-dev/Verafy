import { useEffect, useState } from 'react'
import { api, type ConfigInfo, type Payer, type RateLimitState } from '../api'
import { useLive } from '../live'

export function Settings() {
  const [cfg, setCfg] = useState<ConfigInfo | null>(null)
  const [payers, setPayers] = useState<Payer[]>([])
  const [tab, setTab] = useState<'clinic' | 'payers' | 'engine'>('clinic')
  const { rateLimits } = useLive()
  useEffect(() => { api.config().then(setCfg); api.payers().then(setPayers) }, [])
  const rl = (id: string): RateLimitState | undefined => rateLimits.find((r) => r.payer === id)

  return (
    <div className="page">
      <div className="page-head"><div><h1>Settings</h1><p>Manage your clinic preferences and integration</p></div></div>
      <div className="card">
        <div className="tabs" style={{ padding: '0 20px' }}>
          <button className={tab === 'clinic' ? 'active' : ''} onClick={() => setTab('clinic')}>Clinic Profile</button>
          <button className={tab === 'payers' ? 'active' : ''} onClick={() => setTab('payers')}>Payer Settings</button>
          <button className={tab === 'engine' ? 'active' : ''} onClick={() => setTab('engine')}>Verification Engine</button>
        </div>
        <div className="card-pad">
          {tab === 'clinic' && cfg && (
            <div className="form-grid" style={{ maxWidth: 720 }}>
              <div className="field"><label>Clinic Name</label><input className="input" defaultValue={cfg.practice.name} /></div>
              <div className="field"><label>Clinic Address</label><input className="input" defaultValue="123 Health St, Trivandrum, Kerala" /></div>
              <div className="field"><label>Contact Email</label><input className="input" defaultValue="sarah@riversidedental.com" /></div>
              <div className="field"><label>Phone Number</label><input className="input" defaultValue="+91 98765 43210" /></div>
              <div className="field"><label>Provider NPI (sent on every 270)</label><input className="input num" value={cfg.providerNPI} readOnly /></div>
              <div className="field"><label>Timezone</label><input className="input" defaultValue="(GMT+05:30) India Standard Time" /></div>
              <div><button className="btn btn-primary">Save Changes</button></div>
            </div>
          )}
          {tab === 'payers' && (
            <table className="tbl">
              <thead><tr><th>Payer</th><th>Stedi Payer ID</th><th>Service type</th><th>Real-time 270/271</th><th>Rate limit</th><th>Live state</th></tr></thead>
              <tbody>
                {payers.map((p) => { const s = rl(p.stediPayerId); return (
                  <tr key={p.id}>
                    <td className="cell-main">{p.name}</td>
                    <td className="num">{p.stediPayerId}</td>
                    <td>STC {p.serviceTypeCode}</td>
                    <td>{p.supportsRealtime ? <span className="badge tone-success"><span className="dot" />Supported</span> : <span className="badge tone-warning"><span className="dot" />Manual only</span>}</td>
                    <td className="num">{cfg?.payerRPS} req/s · burst {cfg?.payerBurst}</td>
                    <td className="cell-sub num">{s ? `${s.inFlight} in flight · ${s.waiting} waiting · ${s.throttledTotal} throttled` : '—'}</td>
                  </tr>) })}
              </tbody>
            </table>
          )}
          {tab === 'engine' && cfg && (
            <dl className="kv" style={{ maxWidth: 720, rowGap: 12 }}>
              <dt>Clearinghouse</dt><dd>Stedi · <code>{cfg.stediBaseURL}/eligibility-check</code></dd>
              <dt>Mode</dt><dd>{cfg.stediMode === 'live' ? <span className="badge tone-success"><span className="dot" />Live sandbox (test API key)</span> : <span className="badge tone-info"><span className="dot" />Local simulator — reproduces Stedi's documented mock scenarios in the exact 271 JSON shape</span>}</dd>
              <dt>Transaction</dt><dd>X12 270 eligibility inquiry → 271 benefit response (JSON)</dd>
              <dt>Worker pool</dt><dd className="num">{cfg.maxWorkers} concurrent workers (bounded)</dd>
              <dt>Retry policy</dt><dd className="num">{cfg.maxAttempts} attempts · exponential backoff from {cfg.retryBase} with deterministic jitter · then Manual Review</dd>
              <dt>Outbound rate limit</dt><dd className="num">{cfg.payerRPS} req/s per payer (token bucket, burst {cfg.payerBurst})</dd>
              <dt>Inbound rate limit</dt><dd>30 req/s per client on the API</dd>
              <dt>Queue</dt><dd>River (Postgres-backed) — jobs survive restarts; state changes fan out via LISTEN/NOTIFY → SSE</dd>
              <dt>Pre-visit notices</dt><dd>Nightly run at <b className="num">{String(cfg.nightlyHour ?? 18).padStart(2, '0')}:00</b> {cfg.timezone} for tomorrow's appointments → verify → estimate against the fee schedule → notice. Email delivery: {cfg.emailDelivery ? <span className="badge tone-success"><span className="dot" />on · {cfg.emailProvider}</span> : <span className="badge tone-neutral"><span className="dot" />off — notices are logged in-app</span>}{!cfg.emailDelivery && <div className="help" style={{ marginTop: 6 }}>To send for real, add to <code>backend/.env</code> and restart — Gmail: <code>SMTP_HOST=smtp.gmail.com SMTP_PORT=587 SMTP_USER=you@gmail.com SMTP_PASS=&lt;16-char App Password&gt;</code> · or <code>RESEND_API_KEY=…</code></div>}</dd>
              <dt>Coverage brief</dt><dd>{cfg.llmEnabled ? <>LLM ({cfg.llmModel}) over field-mapped facts, every number validated against the payer response; template fallback on mismatch</> : <>Deterministic template (set <code>OPENROUTER_API_KEY</code> to enable the LLM writer — numbers are validated either way)</>}</dd>
            </dl>
          )}
        </div>
      </div>
    </div>
  )
}
