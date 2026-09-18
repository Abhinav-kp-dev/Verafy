import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, fmtDate, fmtTime, STATUS_META, type Job, type JobStatus, type Payer } from '../api'
import { useLiveRefresh } from '../live'
import { useSelection } from '../hooks/useSelection'
import { Drawer, SelectionBar, StatusBadge, useToast } from '../components/ui'
import { JobDetail } from '../components/JobDetail'

const PAGE = 25

export function History() {
  const [params, setParams] = useSearchParams()
  const [jobs, setJobs] = useState<Job[]>([])
  const [total, setTotal] = useState(0)
  const [payers, setPayers] = useState<Payer[]>([])
  const [open, setOpen] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const status = params.get('status') ?? ''
  const payerId = params.get('payerId') ?? ''
  const search = params.get('search') ?? ''
  const page = Number(params.get('page') ?? 0)
  const upd = (k: string, v: string) => { const p = new URLSearchParams(params); v ? p.set(k, v) : p.delete(k); if (k !== 'page') p.delete('page'); setParams(p) }
  const toast = useToast()

  useEffect(() => { api.payers().then(setPayers) }, [])
  const refresh = () => api.jobs({ status, payerId, search, limit: PAGE, offset: page * PAGE }).then((d) => { setJobs(d.jobs); setTotal(d.total) })
  useLiveRefresh(refresh, [status, payerId, search, page], 800)
  const sel = useSelection(jobs.map((j) => j.id))

  const deleteSelected = async () => {
    if (!confirm(`Permanently delete ${sel.count} verification record${sel.count === 1 ? '' : 's'}? This also removes any pre-visit notice tied to them. This can't be undone.`)) return
    setDeleting(true)
    try {
      const { deleted } = await api.deleteJobs([...sel.selected])
      toast.show(`Deleted ${deleted} record${deleted === 1 ? '' : 's'}`)
      sel.clear()
      refresh()
    } catch (e) { toast.show((e as Error).message, true) } finally { setDeleting(false) }
  }

  return (
    <div className="page">
      <div className="page-head"><div><h1>Verification History</h1><p>View all past verification requests</p></div></div>
      <div className="card">
        <div className="card-head" style={{ gap: 10, flexWrap: 'wrap' }}>
          <select className="input" style={{ width: 180 }} value={status} onChange={(e) => upd('status', e.target.value)}>
            <option value="">All Status</option>
            {(Object.keys(STATUS_META) as JobStatus[]).map((s) => <option key={s} value={s}>{STATUS_META[s].label}</option>)}
          </select>
          <select className="input" style={{ width: 220 }} value={payerId} onChange={(e) => upd('payerId', e.target.value)}>
            <option value="">All Payers</option>{payers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
          <input className="input" style={{ width: 260 }} placeholder="Search patient or member ID…" value={search} onChange={(e) => upd('search', e.target.value)} />
          <span className="cell-sub num" style={{ marginLeft: 'auto' }}>{total.toLocaleString()} results</span>
        </div>
        {jobs.length > 0 && (
          <SelectionBar count={sel.count} allSelected={sel.allSelected} onToggleAll={sel.toggleAll} onClear={sel.clear} onDelete={deleteSelected} deleting={deleting} label="record" />
        )}
        <div className="tbl-wrap">
          <table className="tbl">
            <thead><tr><th className="rowcheck"></th><th>ID</th><th>Patient</th><th>Payer</th><th>Date &amp; Time</th><th>Status</th><th>Attempts</th><th>Summary</th><th></th></tr></thead>
            <tbody>
              {jobs.length === 0 && <tr><td colSpan={9} className="empty">No verifications match these filters.</td></tr>}
              {jobs.map((j) => (
                <tr key={j.id} className={sel.selected.has(j.id) ? 'selected-row' : ''}>
                  <td className="rowcheck" onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={sel.selected.has(j.id)} onChange={() => sel.toggle(j.id)} /></td>
                  <td className="cell-sub num clickable" onClick={() => setOpen(j.id)}>#VC-{j.id.slice(0, 6).toUpperCase()}</td>
                  <td className="clickable" onClick={() => setOpen(j.id)}><div className="cell-main">{j.patientName}</div><div className="cell-sub num">{j.memberId}</div></td>
                  <td className="clickable" onClick={() => setOpen(j.id)}>{j.payerName}</td>
                  <td className="num clickable" onClick={() => setOpen(j.id)}><div>{fmtDate(j.createdAt)}</div><div className="cell-sub">{fmtTime(j.createdAt)}</div></td>
                  <td className="clickable" onClick={() => setOpen(j.id)}><StatusBadge status={j.status} job={j} /></td>
                  <td className="num clickable" onClick={() => setOpen(j.id)}>{j.attemptCount}</td>
                  <td className="cell-sub clickable" style={{ maxWidth: 280 }} onClick={() => setOpen(j.id)}>{j.normalizedBrief?.summary ?? j.errorMessage ?? '—'}</td>
                  <td><button className="btn btn-ghost btn-sm" onClick={() => setOpen(j.id)}>View</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {total > PAGE && (
          <div className="card-head" style={{ borderTop: '1px solid var(--border)', borderBottom: 0 }}>
            <span className="cell-sub">Page {page + 1} of {Math.ceil(total / PAGE)}</span>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn btn-ghost btn-sm" disabled={page === 0} onClick={() => upd('page', String(page - 1))}>Previous</button>
              <button className="btn btn-ghost btn-sm" disabled={(page + 1) * PAGE >= total} onClick={() => upd('page', String(page + 1))}>Next</button>
            </div>
          </div>
        )}
      </div>
      {open && <Drawer title="Verification detail" onClose={() => setOpen(null)}><JobDetail jobId={open} /></Drawer>}
      {toast.el}
    </div>
  )
}
