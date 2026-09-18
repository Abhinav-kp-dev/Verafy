import { useEffect, useState } from 'react'
import { api, type ConfigInfo, type Payer, type RateLimitState } from '../api'
import { useLive } from '../live'
import { Icon, useToast } from '../components/ui'

export function Settings() {
  const [cfg, setCfg] = useState<ConfigInfo | null>(null)
  const [payers, setPayers] = useState<Payer[]>([])
  const [tab, setTab] = useState<'clinic' | 'payers' | 'engine'>('clinic')
  const { rateLimits } = useLive()
  const toast = useToast()
  const [editing, setEditing] = useState<Payer | null>(null)
  const [voice, setVoice] = useState({ providerServicesPhone: '', ivrNotes: '' })
  const [editingEmail, setEditingEmail] = useState<Payer | null>(null)
  const [email, setEmail] = useState('')
  const [saving, setSaving] = useState(false)
  useEffect(() => { api.config().then(setCfg); api.payers().then(setPayers) }, [])
  const saveVoice = async () => {
    if (!editing) return
    setSaving(true)
    try {
      const p = await api.updatePayerVoice(editing.id, voice)
      setPayers((list) => list.map((x) => (x.id === p.id ? p : x)))
      toast.show(p.providerServicesPhone ? `${p.name}: the AI agent will now call ${p.providerServicesPhone}` : `${p.name}: voice line removed — back to plain manual review`)
      setEditing(null)
    } catch (e) { toast.show((e as Error).message, true) } finally { setSaving(false) }
  }
  const saveEmail = async () => {
    if (!editingEmail) return
    setSaving(true)
    try {
      const p = await api.updatePayerEmail(editingEmail.id, email)
      setPayers((list) => list.map((x) => (x.id === p.id ? p : x)))
      toast.show(p.providerServicesEmail ? `${p.name}: verification forms will be emailed to ${p.providerServicesEmail}` : `${p.name}: email removed — back to plain manual review`)
      setEditingEmail(null)
    } catch (e) { toast.show((e as Error).message, true) } finally { setSaving(false) }
  }
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
            <>
            <div className="notice blue" style={{ marginBottom: 14 }}><span>☎</span><div>Payers marked <b>No EDI</b> can't be checked electronically. Give one an <b>AI call line</b> and the voice agent will phone it, or a <b>verification email</b> to send a hosted form instead (the call takes priority if both are set) — leave both empty and those checks go straight to Manual Review.</div></div>
            <table className="tbl">
              <thead><tr><th>Payer</th><th>Stedi Payer ID</th><th>Service type</th><th>Real-time 270/271</th><th>AI call line</th><th>Verification email</th><th>Rate limit</th><th>Live state</th></tr></thead>
              <tbody>
                {payers.map((p) => { const s = rl(p.stediPayerId); return (
                  <tr key={p.id}>
                    <td className="cell-main">{p.name}</td>
                    <td className="num">{p.stediPayerId}</td>
                    <td>STC {p.serviceTypeCode}</td>
                    <td>{p.supportsRealtime ? <span className="badge tone-success"><span className="dot" />Supported</span> : <span className="badge tone-warning"><span className="dot" />No EDI</span>}</td>
                    <td>
                      {p.supportsRealtime ? <span className="cell-sub">—</span> : editing?.id === p.id ? (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 260 }}>
                          <input className="input num" placeholder="+919876543210" value={voice.providerServicesPhone} onChange={(e) => setVoice({ ...voice, providerServicesPhone: e.target.value })} />
                          <input className="input" placeholder="IVR hints, e.g. press 2 then 1; say 'representative'" value={voice.ivrNotes} onChange={(e) => setVoice({ ...voice, ivrNotes: e.target.value })} />
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button className="btn btn-primary btn-sm" disabled={saving} onClick={saveVoice}>Save</button>
                            <button className="btn btn-ghost btn-sm" disabled={saving} onClick={() => setEditing(null)}>Cancel</button>
                          </div>
                        </div>
                      ) : (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          {p.providerServicesPhone
                            ? <span className="badge tone-violet"><Icon name="phone" size={12} /> {p.providerServicesPhone}</span>
                            : <span className="cell-sub">none — manual review</span>}
                          <button className="link" onClick={() => { setEditing(p); setVoice({ providerServicesPhone: p.providerServicesPhone ?? '', ivrNotes: p.ivrNotes ?? '' }) }}>{p.providerServicesPhone ? 'Edit' : 'Add line'}</button>
                        </div>
                      )}
                    </td>
                    <td>
                      {p.supportsRealtime ? <span className="cell-sub">—</span> : editingEmail?.id === p.id ? (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 240 }}>
                          <input className="input" type="email" placeholder="providerservices@payer.com" value={email} onChange={(e) => setEmail(e.target.value)} />
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button className="btn btn-primary btn-sm" disabled={saving} onClick={saveEmail}>Save</button>
                            <button className="btn btn-ghost btn-sm" disabled={saving} onClick={() => setEditingEmail(null)}>Cancel</button>
                          </div>
                        </div>
                      ) : (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          {p.providerServicesEmail
                            ? <span className="badge tone-violet"><Icon name="mail" size={12} /> {p.providerServicesEmail}</span>
                            : <span className="cell-sub">none — manual review</span>}
                          <button className="link" onClick={() => { setEditingEmail(p); setEmail(p.providerServicesEmail ?? '') }}>{p.providerServicesEmail ? 'Edit' : 'Add email'}</button>
                        </div>
                      )}
                    </td>
                    <td className="num">{cfg?.payerRPS} req/s · burst {cfg?.payerBurst}</td>
                    <td className="cell-sub num">{s ? `${s.inFlight} in flight · ${s.waiting} waiting · ${s.throttledTotal} throttled` : '—'}</td>
                  </tr>) })}
              </tbody>
            </table>
            </>
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
              <dt>Payers without real-time EDI</dt><dd>
                An AI voice agent calls the payer's provider-services line with the same member ID, DOB and NPI a 270 would carry, collects benefits through a structured tool call, and the result rejoins the normal brief pipeline tagged <code>ai_voice_call</code>. Watchdog: <b className="num">{cfg.voiceCallTimeout ?? '10m'}</b>, then Manual Review with the transcript.
                {' '}{cfg.voiceMode === 'live'
                  ? <span className="badge tone-success"><span className="dot" />live · {cfg.voiceProvider ?? 'bolna'}</span>
                  : <span className="badge tone-info"><span className="dot" />simulated — no calls placed</span>}
                {cfg.voiceMode !== 'live' && <div className="help" style={{ marginTop: 6 }}>To place real calls (India-ready via Bolna): <code>VOICE_MODE=live VOICE_PROVIDER=bolna BOLNA_API_KEY=… BOLNA_AGENT_ID=… VOICE_WEBHOOK_SECRET=…</code>, build the agent from <code>GET /api/voice/agent-spec?baseUrl=&lt;public URL&gt;</code>, and add each payer's line under Payer Settings.</div>}
              </dd>
              <dt>Payers with no phone line</dt><dd>
                A short hosted form is emailed to the payer's provider services team with the patient's details; their submission runs through the exact same facts pipeline as a phone call, tagged <code>email_form</code>. Response window: <b className="num">{cfg.emailResponseTimeout ?? '72h'}</b>, then Manual Review.
                {' '}{cfg.emailFormEnabled
                  ? <span className="badge tone-success"><span className="dot" />email delivery on</span>
                  : <span className="badge tone-neutral"><span className="dot" />off — configure SMTP or Resend to send</span>}
                <div className="help" style={{ marginTop: 6 }}>Forms are hosted at <code>{cfg.emailFormBaseURL}/verify-form/…</code> — set <code>EMAIL_FORM_BASE_URL</code> to your public origin before going live so the emailed link is reachable.</div>
              </dd>
            </dl>
          )}
        </div>
      </div>
      {toast.el}
    </div>
  )
}
