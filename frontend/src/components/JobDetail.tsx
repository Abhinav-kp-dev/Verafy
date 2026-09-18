import { useEffect, useState } from 'react'
import { api, fmtDOB, fmtTime, money, NOTICE_META, REASON_TEXT, type Job, type JobAttempt, type PatientNotice } from '../api'
import { EstimateCard } from './EstimateCard'
import { useLive } from '../live'
import { StatusBadge } from './ui'

export function JobDetail({ jobId, onResolved }: { jobId: string; onResolved?: (j: Job) => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [attempts, setAttempts] = useState<JobAttempt[]>([])
  const [showRaw, setShowRaw] = useState(false)
  const [note, setNote] = useState('')
  const [by, setBy] = useState('Dr. Sarah Lee')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [notice, setNotice] = useState<PatientNotice | null>(null)
  const { tick, lastChanges, maxAttempts } = useLive()

  const load = () => api.job(jobId).then((d) => {
    setJob(d.job); setAttempts(d.attempts)
    api.notices(200).then((r) => setNotice(r.notices.find((n) => n.jobId === jobId && n.kind === 'cost_estimate') ?? null)).catch(() => {})
  }).catch((e) => setErr(e.message))
  useEffect(() => { load() }, [jobId]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (lastChanges.some((c) => c.job_id === jobId)) load()
  }, [tick]) // eslint-disable-line react-hooks/exhaustive-deps

  if (err) return <div className="notice error">{err}</div>
  if (!job) return <div className="empty">Loading…</div>
  const b = job.normalizedBrief
  const f = b?.facts

  const resolve = async (outcome: 'verified' | 'requeue') => {
    setBusy(true); setErr('')
    try {
      const j = await api.resolve(job.id, { outcome, resolvedBy: by, note })
      setJob(j); onResolved?.(j)
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
        <div>
          <h2 style={{ fontSize: 20 }}>{job.patientName}</h2>
          <div className="cell-sub">DOB {fmtDOB(job.patientDob)} · Member ID <span className="num">{job.memberId}</span></div>
          <div className="cell-sub">{job.payerName} · Payer ID {job.stediPayerId}</div>
        </div>
        <StatusBadge status={job.status} job={job} />
      </div>

      {job.status === 'NEEDS_MANUAL_REVIEW' && (
        <div className="notice error">
          <span>⚠</span>
          <div>
            <b>{job.reviewReason ? REASON_TEXT[job.reviewReason] : 'Needs manual verification'}</b>
            {job.errorMessage && <div style={{ marginTop: 4 }}>{job.errorCode && <code>{job.errorCode}</code>} {job.errorMessage}</div>}
            <div style={{ marginTop: 6, fontSize: 12 }}>
              Next step: call the payer with member ID <b className="num">{job.memberId}</b> and DOB <b>{fmtDOB(job.patientDob)}</b>, then record the outcome below.
            </div>
          </div>
        </div>
      )}
      {job.status === 'RETRYING' && (
        <div className="notice warn"><span>⟳</span><div><b>Transient payer error — retrying automatically.</b> {job.errorCode && <code>{job.errorCode}</code>} {job.errorMessage}<div style={{ fontSize: 12, marginTop: 4 }}>Attempt {job.attemptCount} of {maxAttempts}. If the payer keeps failing, this will be routed to Manual Review — never dropped silently.</div></div></div>
      )}
      {job.status === 'MANUAL_RESOLVED' && (
        <div className="notice info"><span>✓</span><div><b>Manually verified by {job.resolvedBy}</b> at {fmtTime(job.resolvedAt)}{job.resolutionNote && <div>{job.resolutionNote}</div>}</div></div>
      )}

      {b && job.status !== 'MANUAL_RESOLVED' && (
        <div className="card card-pad" style={{ background: '#FAFBFC' }}>
          <div className="cell-sub" style={{ marginBottom: 6 }}>Coverage brief · {b.source === 'llm' ? `AI-written (${b.model}) — ${b.validation}` : b.source === 'template_fallback' ? `template (model output ${b.validation})` : 'deterministic template'}</div>
          <div style={{ fontSize: 15, lineHeight: 1.6 }}>{b.brief}</div>
        </div>
      )}

      {f && (
        <>
          <div className="grid grid-3">
            <div className="cov"><span>Eligibility</span><b style={{ color: f.eligibilityStatus === 'active' ? 'var(--green-900)' : 'var(--red)' }}>{f.eligibilityStatus.toUpperCase()}</b><span>{f.planName || f.insuranceType || ''}</span></div>
            <div className="cov"><span>Deductible remaining</span><b className="num">{f.deductibleRemaining == null ? '—' : `$${f.deductibleRemaining}`}</b><span>{f.deductibleAnnual != null ? `of $${f.deductibleAnnual} annual` : ''}</span></div>
            <div className="cov"><span>Annual maximum</span><b className="num">{f.annualMaximum == null ? '—' : `$${f.annualMaximum}`}</b><span>{f.planStart ? `Plan ${f.planStart} → ${f.planEnd || 'open'}` : ''}</span></div>
          </div>
          {f.categories.length > 0 && (
            <div>
              <div className="cell-sub" style={{ marginBottom: 8 }}>Coverage by category (plan pays)</div>
              <div className="cov-grid">
                {f.categories.map((c) => (
                  <div key={c.stc} className={`cov ${c.covered ? '' : 'no'}`}>
                    <span>{c.label}</span>
                    <b className="num">{c.covered && c.planPaysPct != null ? `${c.planPaysPct}%` : 'Not covered'}</b>
                    {c.covered && c.patientCoinsurancePct != null && <span>patient pays {c.patientCoinsurancePct}% · {c.network || 'network n/a'}</span>}
                  </div>
                ))}
              </div>
            </div>
          )}
          {f.limitations.length > 0 && <div className="cell-sub">Limitations: {f.limitations.join(' · ')}</div>}
          {f.flags.length > 0 && <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>{f.flags.map((fl) => <span key={fl} className="badge tone-neutral">{fl}</span>)}</div>}
        </>
      )}

      {job.status === 'NEEDS_MANUAL_REVIEW' && (
        <div className="card card-pad">
          <h3 style={{ marginBottom: 12 }}>Resolve</h3>
          <div className="form-grid">
            <div className="field"><label>Resolved by</label><input className="input" value={by} onChange={(e) => setBy(e.target.value)} /></div>
            <div className="field"><label>Note (what the payer confirmed)</label><input className="input" value={note} onChange={(e) => setNote(e.target.value)} placeholder="e.g. Active PPO, confirmed by phone, ref #…" /></div>
          </div>
          {err && <div className="notice error" style={{ marginTop: 10 }}>{err}</div>}
          <div style={{ display: 'flex', gap: 10, marginTop: 14 }}>
            <button className="btn btn-primary" disabled={busy} onClick={() => resolve('verified')}>Mark manually verified</button>
            <button className="btn btn-ghost" disabled={busy} onClick={() => resolve('requeue')}>Re-run automated check</button>
          </div>
        </div>
      )}

      {notice && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div className="cell-sub">Pre-visit notice · <span className={`badge tone-${NOTICE_META[notice.deliveryStatus]?.tone ?? 'neutral'}`}><span className="dot" />{NOTICE_META[notice.deliveryStatus]?.label ?? notice.deliveryStatus}</span> · patient share <b className="num">{money(notice.estimate.patientPaysCents)}</b></div>
            <a className="link" href={api.noticePreviewUrl(notice.id)} target="_blank" rel="noreferrer">Open email preview ↗</a>
          </div>
          <EstimateCard e={notice.estimate} compact />
        </div>
      )}

      <div>
        <div className="cell-sub" style={{ marginBottom: 8 }}>Audit trail</div>
        <table className="tbl">
          <thead><tr><th>#</th><th>Outcome</th><th>Error</th><th>Time</th></tr></thead>
          <tbody>
            <tr><td className="num">–</td><td>QUEUED</td><td></td><td className="num">{fmtTime(job.createdAt)}</td></tr>
            {attempts.map((a) => (
              <tr key={a.id}><td className="num">{a.attemptNumber}</td><td>{a.status}</td><td className="cell-sub">{a.errorCode ? <><code>{a.errorCode}</code> {a.errorMessage}</> : ''}</td><td className="num">{fmtTime(a.attemptedAt)}</td></tr>
            ))}
            {job.resolvedAt && <tr><td>–</td><td>MANUAL_RESOLVED by {job.resolvedBy}</td><td></td><td className="num">{fmtTime(job.resolvedAt)}</td></tr>}
          </tbody>
        </table>
      </div>

      {job.rawResponse != null && (
        <div>
          <button className="link" onClick={() => setShowRaw(!showRaw)}>{showRaw ? 'Hide' : 'Show'} raw 271 response (X12 → JSON via Stedi)</button>
          {showRaw && <pre className="raw">{JSON.stringify(job.rawResponse, null, 2)}</pre>}
        </div>
      )}
    </>
  )
}
