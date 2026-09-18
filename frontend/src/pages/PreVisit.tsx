import { useEffect, useState } from 'react'
import { api, fmtDate, fmtTime, money, NOTICE_META, type Appointment, type Batch, type FeeItem, type NoticeKind, type NoticeStatus, type Patient, type PatientNotice } from '../api'
import { useLive, useLiveRefresh } from '../live'
import { BatchProgress, Drawer, Icon, SelectionBar, StatCard, StatusBadge, useToast } from '../components/ui'
import { EstimateCard } from '../components/EstimateCard'
import { useSelection } from '../hooks/useSelection'
import { JobDetail } from '../components/JobDetail'

type DayKey = 'today' | 'tomorrow'

export function PreVisit() {
  const [day, setDay] = useState<DayKey>('tomorrow')
  const [appts, setAppts] = useState<Appointment[]>([])
  const [notices, setNotices] = useState<PatientNotice[]>([])
  const [stats, setStats] = useState<Record<string, number>>({})
  const [openNotice, setOpenNotice] = useState<PatientNotice | null>(null)
  const [openJob, setOpenJob] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState(false)
  const [runBatchId, setRunBatchId] = useState<string | null>(null)
  const live = useLive()
  const toast = useToast()

  useLiveRefresh(() => { api.appointments(day).then((d) => setAppts(d.appointments)) }, [day], 700)
  useLiveRefresh(() => { refreshNotices() }, [], 900)

  const runBatch: Batch | undefined = runBatchId ? live.batches[runBatchId] : undefined
  const inFlight = appts.filter((a) => a.jobStatus && ['QUEUED', 'PROCESSING', 'RETRYING', 'CALL_IN_PROGRESS', 'EMAIL_PENDING'].includes(a.jobStatus)).length
  const needVerify = appts.filter((a) => !a.noticeId && (!a.jobStatus || ['VERIFIED', 'COVERAGE_GAP_FLAGGED'].includes(a.jobStatus))).length
  const needReminder = appts.filter((a) => a.noticeId && !a.reminderId).length
  const pending = needVerify + needReminder
  const costNotices = notices.filter((n) => n.kind === 'cost_estimate')
  const reminders = notices.filter((n) => n.kind === 'reminder')
  const readyNotices = (stats.logged ?? 0) + (stats.skipped_no_provider ?? 0) + (stats.sent ?? 0)
  const totalPatientShare = costNotices.reduce((s, n) => s + n.estimate.patientPaysCents, 0)
  const [logFilter, setLogFilter] = useState<'all' | NoticeKind>('all')
  const shownNotices = logFilter === 'all' ? notices : notices.filter((n) => n.kind === logFilter)
  const [deletingNotices, setDeletingNotices] = useState(false)
  const refreshNotices = () => api.notices(100).then((d) => { setNotices(d.notices); setStats(d.stats) })
  const noticeSel = useSelection(shownNotices.map((n) => n.id))
  const deleteSelectedNotices = async () => {
    if (!confirm(`Permanently delete ${noticeSel.count} notice${noticeSel.count === 1 ? '' : 's'} from the log? This can't be undone.`)) return
    setDeletingNotices(true)
    try {
      const { deleted } = await api.deleteNotices([...noticeSel.selected])
      toast.show(`Deleted ${deleted} notice${deleted === 1 ? '' : 's'}`)
      noticeSel.clear()
      refreshNotices()
    } catch (e) { toast.show((e as Error).message, true) } finally { setDeletingNotices(false) }
  }

  const run = async () => {
    setBusy(true)
    try {
      const r = await api.runPrevisit(day)
      if (r.queued === 0 && r.reminders === 0) { toast.show(r.message ?? 'Nothing to run'); return }
      setRunBatchId(r.batch?.id ?? null)
      const parts = []
      if (r.queued) parts.push(`${r.queued} to verify + estimate`)
      if (r.reminders) parts.push(`${r.reminders} day-before reminder${r.reminders === 1 ? '' : 's'}`)
      toast.show(`Pre-visit run started: ${parts.join(' · ')}`)
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }

  return (
    <div className="page">
      <div className="page-head">
        <div><h1>Pre-Visit Notices</h1><p>When an appointment is booked, coverage is verified and the patient's share is estimated and sent right away. The night before, a reminder goes out with the same numbers — so nobody is surprised at the front desk.</p></div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <div className="tabs" style={{ border: 0 }}>
            <button className={day === 'today' ? 'active' : ''} onClick={() => setDay('today')}>Today</button>
            <button className={day === 'tomorrow' ? 'active' : ''} onClick={() => setDay('tomorrow')}>Tomorrow</button>
          </div>
          <button className="btn btn-ghost" onClick={() => setAdding(true)}>+ Appointment</button>
          <button className="btn btn-primary" disabled={busy || pending === 0} onClick={run}><Icon name="zap" size={16} /> Run pre-visit check{pending ? ` (${needVerify ? `${needVerify} verify` : ''}${needVerify && needReminder ? ' · ' : ''}${needReminder ? `${needReminder} remind` : ''})` : ''}</button>
        </div>
      </div>

      <div className="grid grid-4">
        <StatCard icon="clock" tone="info" value={appts.length} label={`Appointments ${day}`} sub={inFlight ? `${inFlight} verifying now` : pending ? `${pending} not yet checked` : 'all checked'} />
        <StatCard icon="doc" tone="success" value={readyNotices} label="Cost notices prepared" sub={stats.skipped_no_recipient ? `${stats.skipped_no_recipient} patient${stats.skipped_no_recipient === 1 ? '' : 's'} missing an email` : stats.sent ? `${stats.sent} delivered by email` : 'sent on booking · logged in-app'} />
        <StatCard icon="mail" tone="info" value={reminders.length} label="Reminders prepared" sub={needReminder ? `${needReminder} due at the nightly run` : 'sent the evening before'} />
        <StatCard icon="shield" tone="success" value={money(totalPatientShare)} label="Patient share known in advance" sub="collect at check-in, not after" />
      </div>

      {runBatch && (
        <div className="card card-pad">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
            <div><b>{runBatch.label}</b><div className="cell-sub">Verification → estimate → notice, per appointment, through the same queue</div></div>
            {runBatch.queued + runBatch.processing + runBatch.retrying > 0 ? <span className="badge tone-info pulse"><span className="dot" />Running</span> : <span className="badge tone-success"><span className="dot" />Complete</span>}
          </div>
          <BatchProgress b={runBatch} />
        </div>
      )}

      <div className="card">
        <div className="card-head"><h3>Schedule — {day}</h3><span className="cell-sub num">{appts.length} appointments</span></div>
        <div className="tbl-wrap">
          <table className="tbl">
            <thead><tr><th>Time</th><th>Patient</th><th>Planned treatment</th><th>Verification</th><th>Patient share</th><th>Cost notice</th><th>Reminder</th><th></th></tr></thead>
            <tbody>
              {appts.length === 0 && <tr><td colSpan={8} className="empty">No appointments {day}. Add one, or run <code>go run ./cmd/seedprevisit</code>.</td></tr>}
              {appts.map((a) => {
                const n = notices.find((x) => x.id === a.noticeId)
                const rm = notices.find((x) => x.id === a.reminderId)
                return (
                  <tr key={a.id}>
                    <td className="num">{fmtTime(a.scheduledAt)}</td>
                    <td><div className="cell-main">{a.patientName}</div><div className="cell-sub">{a.payerName}{a.patientEmail ? ` · ${a.patientEmail}` : <span style={{ color: 'var(--warning-text)' }}> · no email</span>}</div></td>
                    <td><div className="num">{a.procedureCodes.join(', ')}</div>{a.notes && <div className="cell-sub">{a.notes}</div>}</td>
                    <td>{a.jobStatus ? <span className="clickable" onClick={() => a.jobId && setOpenJob(a.jobId)}><StatusBadge status={a.jobStatus} /></span> : <span className="cell-sub">Not run</span>}</td>
                    <td className="num" style={{ fontWeight: 600 }}>{n ? money(n.estimate.patientPaysCents) : '—'}</td>
                    <td>{n ? <span className={`badge tone-${NOTICE_META[n.deliveryStatus as NoticeStatus]?.tone ?? 'neutral'}`}><span className="dot" />{NOTICE_META[n.deliveryStatus as NoticeStatus]?.label ?? n.deliveryStatus}</span>
                      : a.jobStatus === 'NEEDS_MANUAL_REVIEW' ? <span className="cell-sub">Needs staff review first</span> : <span className="cell-sub">—</span>}</td>
                    <td>{rm ? <span className={`badge tone-${NOTICE_META[rm.deliveryStatus as NoticeStatus]?.tone ?? 'neutral'} clickable`} onClick={() => setOpenNotice(rm)}><span className="dot" />{NOTICE_META[rm.deliveryStatus as NoticeStatus]?.label ?? rm.deliveryStatus}</span>
                      : n ? <span className="cell-sub">Due at nightly run</span> : <span className="cell-sub">—</span>}</td>
                    <td style={{ whiteSpace: 'nowrap' }}>
                      {n ? <button className="btn btn-outline btn-sm" onClick={() => setOpenNotice(n)}>Preview notice</button>
                        : !a.jobStatus || ['VERIFIED', 'COVERAGE_GAP_FLAGGED', 'NEEDS_MANUAL_REVIEW', 'MANUAL_RESOLVED'].includes(a.jobStatus)
                          ? <button className="btn btn-ghost btn-sm" disabled={busy} onClick={async () => { await api.verifyAppointment(a.id); toast.show(`Queued pre-visit check for ${a.patientName}`) }}>Run now</button>
                          : null}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <div className="card-head"><h3>Notice log</h3><span className="cell-sub">Every notice is written here before any delivery attempt — this is the audit record</span></div>
        <div className="tabs" style={{ padding: '0 20px' }}>
          <button className={logFilter === 'all' ? 'active' : ''} onClick={() => setLogFilter('all')}>All ({notices.length})</button>
          <button className={logFilter === 'cost_estimate' ? 'active' : ''} onClick={() => setLogFilter('cost_estimate')}>Cost estimates ({costNotices.length})</button>
          <button className={logFilter === 'reminder' ? 'active' : ''} onClick={() => setLogFilter('reminder')}>Reminders ({reminders.length})</button>
        </div>
        {shownNotices.length > 0 && (
          <SelectionBar count={noticeSel.count} allSelected={noticeSel.allSelected} onToggleAll={noticeSel.toggleAll} onClear={noticeSel.clear} onDelete={deleteSelectedNotices} deleting={deletingNotices} label="notice" />
        )}
        <div className="tbl-wrap">
          <table className="tbl">
            <thead><tr><th className="rowcheck"></th><th>Prepared</th><th>Type</th><th>Patient</th><th>Visit</th><th>Subject</th><th>Patient share</th><th>Status</th><th></th></tr></thead>
            <tbody>
              {shownNotices.length === 0 && <tr><td colSpan={9} className="empty">No notices yet — add an appointment or run the pre-visit check.</td></tr>}
              {shownNotices.map((n) => (
                <tr key={n.id} className={noticeSel.selected.has(n.id) ? 'selected-row' : ''}>
                  <td className="rowcheck" onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={noticeSel.selected.has(n.id)} onChange={() => noticeSel.toggle(n.id)} /></td>
                  <td className="num clickable" onClick={() => setOpenNotice(n)}><div>{fmtDate(n.createdAt)}</div><div className="cell-sub">{fmtTime(n.createdAt)}</div></td>
                  <td className="clickable" onClick={() => setOpenNotice(n)}>{n.kind === 'reminder' ? <span className="badge tone-neutral">Reminder</span> : <span className="badge tone-success">Cost estimate</span>}</td>
                  <td><div className="cell-main">{n.patientName}</div><div className="cell-sub">{n.recipient ?? 'no email on file'}</div></td>
                  <td className="num">{n.scheduledAt ? `${fmtDate(n.scheduledAt)} ${fmtTime(n.scheduledAt)}` : '—'}</td>
                  <td className="cell-sub clickable" style={{ maxWidth: 320 }} onClick={() => setOpenNotice(n)}>{n.subject}</td>
                  <td className="num clickable" style={{ fontWeight: 600 }} onClick={() => setOpenNotice(n)}>{money(n.estimate.patientPaysCents)}</td>
                  <td className="clickable" onClick={() => setOpenNotice(n)}><span className={`badge tone-${NOTICE_META[n.deliveryStatus]?.tone ?? 'neutral'}`}><span className="dot" />{NOTICE_META[n.deliveryStatus]?.label ?? n.deliveryStatus}</span></td>
                  <td><button className="btn btn-ghost btn-sm" onClick={() => setOpenNotice(n)}>Open</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {openNotice && <NoticeDrawer n={openNotice} onClose={() => setOpenNotice(null)} onOpenJob={(id) => { setOpenNotice(null); setOpenJob(id) }} />}
      {openJob && <Drawer title="Verification detail" onClose={() => setOpenJob(null)}><JobDetail jobId={openJob} /></Drawer>}
      {adding && <AddAppointment onClose={() => setAdding(false)} onCreated={(queued) => { setAdding(false); api.appointments(day).then((d) => setAppts(d.appointments)); toast.show(queued ? 'Appointment added — verifying coverage and preparing the cost notice now' : 'Appointment added') }} />}
      {toast.el}
    </div>
  )
}

function NoticeDrawer({ n: initial, onClose, onOpenJob }: { n: PatientNotice; onClose: () => void; onOpenJob: (id: string) => void }) {
  const [n, setN] = useState(initial)
  const [tab, setTab] = useState<'email' | 'estimate' | 'text'>('email')
  const [sending, setSending] = useState(false)
  const [sendMsg, setSendMsg] = useState<{ text: string; err?: boolean } | null>(null)
  const meta = NOTICE_META[n.deliveryStatus]
  const send = async () => {
    setSending(true); setSendMsg(null)
    try { const r = await api.sendNotice(n.id); setN(r); setSendMsg({ text: `Sent to ${r.recipient}` }) }
    catch (e) { setSendMsg({ text: (e as Error).message, err: true }) } finally { setSending(false) }
  }
  return (
    <Drawer title={<>{n.kind === 'reminder' ? 'Day-before reminder' : 'Cost notice'} — {n.patientName}</>} onClose={onClose}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div>
          <div className="cell-main" style={{ fontSize: 15 }}>{n.subject}</div>
          <div className="cell-sub">To: {n.recipient ?? <span style={{ color: 'var(--warning-text)' }}>no email on file</span>} · {n.payerName}{n.scheduledAt && ` · visit ${fmtDate(n.scheduledAt)} ${fmtTime(n.scheduledAt)}`}</div>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <span className={`badge tone-${meta?.tone ?? 'neutral'}`}><span className="dot" />{meta?.label ?? n.deliveryStatus}</span>
          <button className="btn btn-primary btn-sm" disabled={sending} onClick={send}><Icon name="mail" size={14} /> {n.deliveryStatus === 'sent' ? 'Resend' : 'Send now'}</button>
        </div>
      </div>
      {sendMsg && <div className={`notice ${sendMsg.err ? 'error' : 'info'}`}>{sendMsg.err ? '✕ ' : '✓ '}{sendMsg.text}</div>}
      {n.deliveryStatus === 'skipped_no_provider' && <div className="notice info">✓ Notice prepared and logged. Email delivery is off — configure Gmail SMTP or Resend in <code>backend/.env</code> (see Settings → Verification Engine), restart, then press <b>Send now</b>. The content below is exactly what the patient will receive.</div>}
      {n.deliveryStatus === 'skipped_no_recipient' && <div className="notice warn">⚠ No email on file for this patient. Add one on the Patients page (Edit contact), then press <b>Send now</b>.</div>}
      {n.deliveryStatus === 'failed' && <div className="notice error">Delivery failed: {n.deliveryError}</div>}
      {n.deliveryStatus === 'sent' && <div className="notice info">✓ Delivered to {n.recipient}{n.sentAt && ` at ${fmtTime(n.sentAt)}`}{n.providerId && ` · message id ${n.providerId}`}</div>}
      <div className="tabs">
        <button className={tab === 'email' ? 'active' : ''} onClick={() => setTab('email')}>Email preview</button>
        <button className={tab === 'estimate' ? 'active' : ''} onClick={() => setTab('estimate')}>Estimate breakdown</button>
        <button className={tab === 'text' ? 'active' : ''} onClick={() => setTab('text')}>Plain text</button>
      </div>
      {tab === 'email' && <iframe title="Email preview" src={api.noticePreviewUrl(n.id)} style={{ width: '100%', height: 620, border: '1px solid var(--border)', borderRadius: 10, background: 'var(--surface-alt)' }} />}
      {tab === 'estimate' && <EstimateCard e={n.estimate} />}
      {tab === 'text' && <pre className="raw" style={{ background: 'var(--surface-alt)', color: 'var(--text)', whiteSpace: 'pre-wrap' }}>{n.bodyText}</pre>}
      <div><button className="link" onClick={() => onOpenJob(n.jobId)}>Open the verification this estimate is based on →</button></div>
    </Drawer>
  )
}

function AddAppointment({ onClose, onCreated }: { onClose: () => void; onCreated: (queued: boolean) => void }) {
  const [patients, setPatients] = useState<Patient[]>([])
  const [fees, setFees] = useState<FeeItem[]>([])
  const [patientId, setPatientId] = useState('')
  const [when, setWhen] = useState(() => {
    const d = new Date(Date.now() + 24 * 3600 * 1000); d.setHours(10, 0, 0, 0)
    const p = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}` // local, not UTC
  })
  const [codes, setCodes] = useState<string[]>([])
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => { api.patients().then((p) => { setPatients(p); setPatientId(p[0]?.id ?? '') }); api.fees().then(setFees) }, [])
  const total = codes.reduce((s, c) => s + (fees.find((f) => f.code === c)?.feeCents ?? 0), 0)
  const submit = async () => {
    setBusy(true); setErr('')
    try { const r = await api.createAppointment({ patientId, scheduledAt: new Date(when).toISOString(), procedureCodes: codes, notes }); onCreated(r.queued) }
    catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Drawer title="Add appointment" onClose={onClose}>
      <div className="field"><label>Patient</label><select className="input" value={patientId} onChange={(e) => setPatientId(e.target.value)}>{patients.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.payerName}</option>)}</select></div>
      <div className="field"><label>Date &amp; time</label><input className="input" type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} /></div>
      <div className="field">
        <label>Planned procedures <span className="help">(from the practice fee schedule)</span></label>
        <div style={{ maxHeight: 260, overflow: 'auto', border: '1px solid var(--border)', borderRadius: 10 }}>
          <table className="tbl"><tbody>
            {fees.map((f) => (
              <tr key={f.code} className="clickable" onClick={() => setCodes(codes.includes(f.code) ? codes.filter((c) => c !== f.code) : [...codes, f.code])}>
                <td style={{ width: 30 }}><input type="checkbox" readOnly checked={codes.includes(f.code)} /></td>
                <td><span className="num" style={{ color: 'var(--muted)', marginRight: 8 }}>{f.code}</span>{f.description}</td>
                <td className="num" style={{ textAlign: 'right' }}>{money(f.feeCents)}</td>
              </tr>
            ))}
          </tbody></table>
        </div>
        <span className="help">{codes.length} selected · total fees {money(total)}</span>
      </div>
      <div className="field"><label>Notes</label><input className="input" value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="e.g. Crown #19" /></div>
      {(() => { const p = patients.find((x) => x.id === patientId); return p && !p.email ? <div className="notice warn">⚠ {p.name} has no email on file — the cost notice will be prepared and logged but not sent. Add an email on the Patients page.</div> : null })()}
      <div className="notice info">⚡ On save: coverage is verified, the patient's share is estimated, and the cost notice is prepared automatically. The day-before reminder goes out at the nightly run.</div>
      {err && <div className="notice error">{err}</div>}
      <div><button className="btn btn-primary" disabled={busy || !patientId || codes.length === 0} onClick={submit}>Save appointment</button></div>
    </Drawer>
  )
}
