import { useRef, useState } from 'react'
import { API, api, fmtDate, fmtTime, type Batch, type Job, type Patient, type Payer } from '../api'
import { useLive, useLiveRefresh } from '../live'
import { BatchProgress, Drawer, Icon, SelectionBar, StatusBadge, useToast } from '../components/ui'
import { useSelection } from '../hooks/useSelection'
import { JobDetail } from '../components/JobDetail'

export function BatchUpload() {
  const [batches, setBatches] = useState<Batch[]>([])
  const [patients, setPatients] = useState<Patient[]>([])
  const [payers, setPayers] = useState<Payer[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [active, setActive] = useState<string | null>(null)
  const [jobs, setJobs] = useState<Job[]>([])
  const [filter, setFilter] = useState('')
  const [over, setOver] = useState(false)
  const [count, setCount] = useState(10000)
  const [busy, setBusy] = useState(false)
  const [openJob, setOpenJob] = useState<string | null>(null)
  const [deletingJobs, setDeletingJobs] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const live = useLive()
  const toast = useToast()

  useLiveRefresh(() => { api.batches(20).then((b) => { setBatches(b); if (!active && b[0]) setActive(b[0].id) }) }, [])
  useLiveRefresh(() => { api.patients().then(setPatients) }, [], 5000)
  useLiveRefresh(() => { api.payers().then(setPayers) }, [], 60000)
  const payerName = (id: string) => payers.find((p) => p.stediPayerId === id)?.name ?? id
  const refreshBatchJobs = () => { if (active) api.batch(active, filter, 200).then((d) => setJobs(d.jobs)) }
  useLiveRefresh(refreshBatchJobs, [active, filter], 600)
  const jobSel = useSelection(jobs.map((j) => j.id))

  const deleteSelectedJobs = async () => {
    if (!confirm(`Permanently delete ${jobSel.count} job record${jobSel.count === 1 ? '' : 's'} from this batch? This can't be undone.`)) return
    setDeletingJobs(true)
    try {
      const { deleted } = await api.deleteJobs([...jobSel.selected])
      toast.show(`Deleted ${deleted} job${deleted === 1 ? '' : 's'}`)
      jobSel.clear()
      refreshBatchJobs()
      api.batches(20).then(setBatches)
    } catch (e) { toast.show((e as Error).message, true) } finally { setDeletingJobs(false) }
  }

  const current = active ? (live.batches[active] ?? batches.find((b) => b.id === active)) : undefined
  const allBatches = batches.map((b) => live.batches[b.id] ?? b)

  const upload = async (file: File) => {
    setBusy(true)
    try {
      const r = await api.uploadCSV(file)
      setActive(r.batch.id)
      const skipped = r.skipped ?? []
      toast.show(`${r.batch.totalJobs} patients queued from ${file.name}${skipped.length ? ` (${skipped.length} rows skipped)` : ''}`)
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }
  const runSelected = async (all: boolean) => {
    setBusy(true)
    try {
      const b = await api.createBatch(all ? { all: true } : { patientIds: [...selected] })
      setActive(b.id); setSelected(new Set())
      toast.show(`Batch of ${b.totalJobs} accepted — 202 returned instantly, workers are on it`)
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }
  const runSynthetic = async () => {
    setBusy(true)
    try {
      const b = await api.synthetic(count)
      setActive(b.id)
      toast.show(`${b.totalJobs.toLocaleString()} synthetic jobs enqueued — watch the queue drain under the per-payer rate limit`)
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }

  const throttling = live.rateLimits.filter((r) => r.throttlingNow)

  return (
    <div className="page">
      <div className="page-head"><div><h1>Batch Upload</h1><p>Upload a CSV or select patients to verify many at once. Every job is queued, rate-limited per payer, retried on transient failure, and escalated when a human is needed.</p></div></div>

      <div className="grid grid-2">
        <div className="card card-pad">
          <h3 style={{ marginBottom: 12 }}>Upload a patient list</h3>
          <div className={`dropzone ${over ? 'over' : ''}`}
            onDragOver={(e) => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
            onDrop={(e) => { e.preventDefault(); setOver(false); const f = e.dataTransfer.files[0]; if (f) upload(f) }}>
            <Icon name="upload" size={28} />
            <b>Drag and drop your CSV file here</b>
            <div style={{ margin: '8px 0 12px' }}>or</div>
            <button className="btn btn-outline btn-sm" disabled={busy} onClick={() => fileRef.current?.click()}>Choose File</button>
            <input ref={fileRef} type="file" accept=".csv" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) upload(f); e.target.value = '' }} />
            <div className="help" style={{ marginTop: 12 }}>Columns: name, dob, member_id, payer (payer ID or name) · <a className="link" href={`${API}/api/batches/csv-template`}>Download sample template</a></div>
          </div>
        </div>

        <div className="card card-pad">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h3>Select from patient records</h3>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn btn-ghost btn-sm" disabled={busy || selected.size === 0} onClick={() => runSelected(false)}>Run {selected.size || ''} selected</button>
              <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => runSelected(true)}>Run all {patients.length}</button>
            </div>
          </div>
          <div style={{ maxHeight: 220, overflow: 'auto', border: '1px solid var(--border)', borderRadius: 10 }}>
            <table className="tbl">
              <tbody>
                {patients.map((p) => (
                  <tr key={p.id} className="clickable" onClick={() => { const s = new Set(selected); s.has(p.id) ? s.delete(p.id) : s.add(p.id); setSelected(s) }}>
                    <td style={{ width: 30 }}><input type="checkbox" readOnly checked={selected.has(p.id)} /></td>
                    <td><div className="cell-main">{p.name}</div><div className="cell-sub">{p.payerName} · {p.memberId}</div></td>
                    <td className="cell-sub">{p.lastStatus ? <StatusBadge status={p.lastStatus as Job['status']} /> : 'never verified'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginTop: 14, paddingTop: 14, borderTop: '1px solid var(--border)' }}>
            <span style={{ fontSize: 13 }}><b>Synthetic load test</b> <span className="help">— cycles seeded patients to prove the queue at scale</span></span>
            <input className="input num" type="number" min={100} max={50000} step={100} value={count} onChange={(e) => setCount(Number(e.target.value))} style={{ width: 110, marginLeft: 'auto' }} />
            <button className="btn btn-ghost btn-sm" disabled={busy} onClick={runSynthetic}><Icon name="zap" size={14} /> Enqueue</button>
          </div>
        </div>
      </div>

      {current && (
        <div className="card">
          <div className="card-head">
            <div><h3>{current.label}</h3><div className="cell-sub">Batch {current.id.slice(0, 8)} · {current.kind} · created {fmtDate(current.createdAt)} {fmtTime(current.createdAt)}</div></div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <span className="badge tone-neutral num">{current.totalJobs.toLocaleString()} jobs</span>
              {current.queued + current.processing + current.retrying > 0
                ? <span className="badge tone-info pulse"><span className="dot" />Processing</span>
                : <span className="badge tone-success"><span className="dot" />Complete</span>}
            </div>
          </div>
          <div className="card-pad">
            <BatchProgress b={current} />
            {throttling.length > 0 && (
              <div className="notice blue" style={{ marginTop: 14 }}><span>⏱</span><div>
                <b>Processing throttled to respect {throttling.map((t) => payerName(t.payer)).join(', ')}'s rate limit — this is expected, not stuck.</b>
                <div style={{ fontSize: 12, marginTop: 2 }}>{throttling.map((t) => `${payerName(t.payer)}: ${t.waiting} waiting · ${t.inFlight} in flight · ${t.rps} req/s cap`).join(' | ')}</div>
              </div></div>
            )}
          </div>
          <div className="tabs" style={{ padding: '0 20px' }}>
            {[['', 'All'], ['QUEUED', 'Queued'], ['PROCESSING', 'Processing'], ['RETRYING', 'Retrying'], ['CALL_IN_PROGRESS', 'Calling payer'], ['VERIFIED', 'Verified'], ['COVERAGE_GAP_FLAGGED', 'Coverage gap'], ['NEEDS_MANUAL_REVIEW', 'Needs review']].map(([v, l]) => (
              <button key={v} className={filter === v ? 'active' : ''} onClick={() => setFilter(v)}>{l}</button>
            ))}
          </div>
          {jobs.length > 0 && (
            <SelectionBar count={jobSel.count} allSelected={jobSel.allSelected} onToggleAll={jobSel.toggleAll} onClear={jobSel.clear} onDelete={deleteSelectedJobs} deleting={deletingJobs} label="job" />
          )}
          <div className="tbl-wrap" style={{ maxHeight: 420, overflowY: 'auto' }}>
            <table className="tbl">
              <thead><tr><th className="rowcheck"></th><th>Patient</th><th>Payer</th><th>Status</th><th>Attempts</th><th>Summary</th></tr></thead>
              <tbody>
                {jobs.length === 0 && <tr><td colSpan={6} className="empty">No jobs in this state right now.</td></tr>}
                {jobs.map((j) => (
                  <tr key={j.id} className={jobSel.selected.has(j.id) ? 'selected-row' : ''}>
                    <td className="rowcheck" onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={jobSel.selected.has(j.id)} onChange={() => jobSel.toggle(j.id)} /></td>
                    <td className="clickable" onClick={() => setOpenJob(j.id)}><div className="cell-main">{j.patientName}</div><div className="cell-sub num">{j.memberId}</div></td>
                    <td className="clickable" onClick={() => setOpenJob(j.id)}>{j.payerName}</td>
                    <td className="clickable" onClick={() => setOpenJob(j.id)}><StatusBadge status={j.status} job={j} /></td>
                    <td className="num clickable" onClick={() => setOpenJob(j.id)}>{j.attemptCount}</td>
                    <td className="cell-sub clickable" onClick={() => setOpenJob(j.id)}>{j.normalizedBrief?.summary ?? j.errorMessage ?? '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {allBatches.length > 1 && (
        <div className="card">
          <div className="card-head"><h3>Recent batches</h3></div>
          <table className="tbl">
            <thead><tr><th>Batch</th><th>Kind</th><th>Progress</th><th>Created</th></tr></thead>
            <tbody>
              {allBatches.map((b) => (
                <tr key={b.id} className="clickable" onClick={() => setActive(b.id)} style={{ background: b.id === active ? 'var(--green-50)' : undefined }}>
                  <td><div className="cell-main">{b.label}</div><div className="cell-sub num">{b.totalJobs.toLocaleString()} jobs</div></td>
                  <td><span className="badge tone-neutral">{b.kind}</span></td>
                  <td style={{ width: 320 }}><BatchProgress b={b} showLegend={false} /></td>
                  <td className="cell-sub num">{fmtDate(b.createdAt)} {fmtTime(b.createdAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {openJob && <Drawer title="Verification detail" onClose={() => setOpenJob(null)}><JobDetail jobId={openJob} /></Drawer>}
      {toast.el}
    </div>
  )
}
