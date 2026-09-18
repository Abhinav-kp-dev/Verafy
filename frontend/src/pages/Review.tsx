import { useState } from 'react'
import { api, fmtDate, fmtDOB, fmtTime, REASON_TEXT, type Job, type ReviewReason } from '../api'
import { useLiveRefresh } from '../live'
import { useSelection } from '../hooks/useSelection'
import { Drawer, SelectionBar, useToast } from '../components/ui'
import { JobDetail } from '../components/JobDetail'

const REASONS: ReviewReason[] = ['unsupported_payer', 'voice_call_failed', 'call_timeout', 'email_not_answered', 'email_response_invalid', 'ambiguous_match', 'retry_exhausted', 'payer_rejected', 'malformed_response']
const SHORT: Record<ReviewReason, string> = { unsupported_payer: 'Payer unsupported', voice_call_failed: 'AI call failed', call_timeout: 'AI call timed out', email_not_answered: 'Email not answered', email_response_invalid: 'Bad form response', ambiguous_match: 'Subscriber not matched', retry_exhausted: 'Payer unavailable', payer_rejected: 'Payer rejected', malformed_response: 'Bad response' }

export function Review() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [filter, setFilter] = useState<ReviewReason | ''>('')
  const [open, setOpen] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const toast = useToast()
  const refresh = () => api.review().then((d) => setJobs(d.jobs))
  useLiveRefresh(refresh, [], 600)

  const counts = REASONS.reduce((m, r) => ({ ...m, [r]: jobs.filter((j) => j.reviewReason === r).length }), {} as Record<string, number>)
  const shown = filter ? jobs.filter((j) => j.reviewReason === filter) : jobs
  const sel = useSelection(shown.map((j) => j.id))

  const deleteSelected = async () => {
    if (!confirm(`Permanently delete ${sel.count} review case${sel.count === 1 ? '' : 's'}? This can't be undone.`)) return
    setDeleting(true)
    try {
      const { deleted } = await api.deleteJobs([...sel.selected])
      toast.show(`Deleted ${deleted} case${deleted === 1 ? '' : 's'}`)
      sel.clear()
      refresh()
    } catch (e) { toast.show((e as Error).message, true) } finally { setDeleting(false) }
  }

  return (
    <div className="page">
      <div className="page-head"><div><h1>Manual Review</h1><p>Cases the system could not resolve automatically. Nothing lands here silently — each row carries the exact reason and the next action.</p></div></div>
      {jobs.length === 0 ? (
        <div className="card empty"><div style={{ fontSize: 34 }}>✓</div><b>Queue is clear</b><div>Every verification resolved automatically.</div></div>
      ) : (
        <div className="card">
          <div className="tabs" style={{ padding: '0 20px' }}>
            <button className={filter === '' ? 'active' : ''} onClick={() => setFilter('')}>All ({jobs.length})</button>
            {REASONS.filter((r) => counts[r] > 0).map((r) => <button key={r} className={filter === r ? 'active' : ''} onClick={() => setFilter(r)}>{SHORT[r]} ({counts[r]})</button>)}
          </div>
          <SelectionBar count={sel.count} allSelected={sel.allSelected} onToggleAll={sel.toggleAll} onClear={sel.clear} onDelete={deleteSelected} deleting={deleting} label="case" />
          <div className="tbl-wrap">
            <table className="tbl">
              <thead><tr><th className="rowcheck"></th><th>Patient</th><th>Payer</th><th>Issue</th><th>Attempts</th><th>Submitted</th><th></th></tr></thead>
              <tbody>
                {shown.map((j) => (
                  <tr key={j.id} className={sel.selected.has(j.id) ? 'selected-row' : ''}>
                    <td className="rowcheck" onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={sel.selected.has(j.id)} onChange={() => sel.toggle(j.id)} /></td>
                    <td className="clickable" onClick={() => setOpen(j.id)}><div className="cell-main">{j.patientName}</div><div className="cell-sub">DOB {fmtDOB(j.patientDob)} · <span className="num">{j.memberId}</span></div></td>
                    <td className="clickable" onClick={() => setOpen(j.id)}><div>{j.payerName}</div><div className="cell-sub">{j.stediPayerId}</div></td>
                    <td className="clickable" onClick={() => setOpen(j.id)}><div className="cell-main">{j.reviewReason ? REASON_TEXT[j.reviewReason] : '—'}</div><div className="cell-sub">{j.errorCode && <code>{j.errorCode}</code>} {j.errorMessage}{j.callTranscript && <span className="badge tone-violet" style={{ marginLeft: 6 }}>transcript</span>}</div></td>
                    <td className="num clickable" onClick={() => setOpen(j.id)}>{j.attemptCount}</td>
                    <td className="num clickable" onClick={() => setOpen(j.id)}><div>{fmtDate(j.createdAt)}</div><div className="cell-sub">{fmtTime(j.createdAt)}</div></td>
                    <td><button className="btn btn-primary btn-sm" onClick={() => setOpen(j.id)}>Review</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      {open && (
        <Drawer title="Manual review" onClose={() => setOpen(null)}>
          <JobDetail jobId={open} onResolved={(j) => { toast.show(j.status === 'MANUAL_RESOLVED' ? `Marked manually verified by ${j.resolvedBy}` : 'Re-queued for automated verification'); setOpen(null) }} />
        </Drawer>
      )}
      {toast.el}
    </div>
  )
}
