import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, fmtDate, fmtDOB, type Job, type Patient, type Payer } from '../api'
import { useLiveRefresh } from '../live'
import { useSelection } from '../hooks/useSelection'
import { Drawer, Icon, SelectionBar, StatusBadge, useToast } from '../components/ui'

export function Patients() {
  const [list, setList] = useState<Patient[]>([])
  const [search, setSearch] = useState('')
  const [payers, setPayers] = useState<Payer[]>([])
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Patient | null>(null)
  const [contact, setContact] = useState({ email: '', phone: '' })
  const [form, setForm] = useState({ name: '', dob: '', memberId: '', payerId: '', email: '', phone: '' })
  const [busy, setBusy] = useState(false)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const nav = useNavigate()
  const toast = useToast()

  const refresh = () => api.patients(search).then(setList)
  useLiveRefresh(refresh, [search], 1500)
  useEffect(() => { api.payers().then((p) => { setPayers(p); setForm((f) => ({ ...f, payerId: p[0]?.id ?? '' })) }) }, [])
  const sel = useSelection(list.map((p) => p.id))

  const verify = async (p: Patient) => {
    try { const r = await api.verify({ patientId: p.id }); toast.show(`Queued verification for ${p.name}`); void r } catch (e) { toast.show((e as Error).message, true) }
  }
  const add = async () => {
    setBusy(true)
    try { await api.createPatient(form); setAdding(false); setForm({ ...form, name: '', dob: '', memberId: '', email: '', phone: '' }); toast.show(form.email ? 'Patient added — pre-visit notices will go to ' + form.email : 'Patient added (no email: notices will be logged but not sent)'); refresh() }
    catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }

  const deleteOne = async (p: Patient) => {
    if (!confirm(`Permanently delete ${p.name}? This also removes their verification history, appointments, and pre-visit notices. This can't be undone.`)) return
    setDeletingIds((prev) => new Set(prev).add(p.id))
    try {
      await api.deletePatients([p.id])
      toast.show(`Deleted ${p.name}`)
      refresh()
    } catch (e) { toast.show((e as Error).message, true) } finally { setDeletingIds((prev) => { const n = new Set(prev); n.delete(p.id); return n }) }
  }
  const deleteSelected = async () => {
    if (!confirm(`Permanently delete ${sel.count} patient${sel.count === 1 ? '' : 's'}? This also removes their verification history, appointments, and pre-visit notices. This can't be undone.`)) return
    setBusy(true)
    try {
      const { deleted } = await api.deletePatients([...sel.selected])
      toast.show(`Deleted ${deleted} patient${deleted === 1 ? '' : 's'}`)
      sel.clear()
      refresh()
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }

  return (
    <div className="page">
      <div className="page-head"><div><h1>Patients</h1><p>Manage your patient records</p></div><button className="btn btn-primary" onClick={() => setAdding(true)}>+ Add Patient</button></div>
      <div className="card">
        <div className="card-head">
          <div className="search" style={{ maxWidth: 380 }}><Icon name="search" size={16} /><input placeholder="Search by name or member ID…" value={search} onChange={(e) => setSearch(e.target.value)} /></div>
          <span className="cell-sub num">{list.length} patients</span>
        </div>
        {list.length > 0 && (
          <SelectionBar count={sel.count} allSelected={sel.allSelected} onToggleAll={sel.toggleAll} onClear={sel.clear} onDelete={deleteSelected} deleting={busy} label="patient" />
        )}
        <div className="tbl-wrap">
          <table className="tbl">
            <thead><tr><th className="rowcheck"></th><th>Name</th><th>DOB</th><th>Member ID</th><th>Primary Payer</th><th>Last Verified</th><th>Actions</th></tr></thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id} className={sel.selected.has(p.id) ? 'selected-row' : ''}>
                  <td className="rowcheck"><input type="checkbox" checked={sel.selected.has(p.id)} onChange={() => sel.toggle(p.id)} /></td>
                  <td><div className="cell-main">{p.name}</div><div className="cell-sub">{p.email ?? <span style={{ color: '#B45309' }}>no email — notices won't be sent</span>}</div></td>
                  <td className="num">{fmtDOB(p.dob)}</td>
                  <td className="num">{p.memberId}</td>
                  <td><div>{p.payerName}</div><div className="cell-sub">{p.stediPayerId}</div></td>
                  <td>{p.lastStatus ? <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}><StatusBadge status={p.lastStatus as Job['status']} /><span className="cell-sub num">{fmtDate(p.lastVerified)}</span></div> : <span className="cell-sub">Never</span>}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="btn btn-ghost btn-sm" onClick={() => verify(p)}>Verify</button>{' '}
                    <button className="btn btn-ghost btn-sm" onClick={() => { setEditing(p); setContact({ email: p.email ?? '', phone: p.phone ?? '' }) }}>Edit contact</button>{' '}
                    <button className="btn btn-ghost btn-sm" onClick={() => nav(`/history?search=${encodeURIComponent(p.memberId)}`)}>History</button>{' '}
                    <button className="btn btn-danger btn-sm" disabled={deletingIds.has(p.id)} onClick={() => deleteOne(p)} title="Delete patient"><Icon name="trash" size={13} /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {adding && (
        <Drawer title="Add patient" onClose={() => setAdding(false)}>
          <div className="notice warn">In test mode, Stedi only returns data for its documented mock subscribers. Other member IDs will land in Manual Review as "subscriber not found" — which is the correct real-world behaviour.</div>
          <div className="field"><label>Full name</label><input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></div>
          <div className="form-grid">
            <div className="field"><label>Date of birth</label><input className="input" type="date" value={form.dob} onChange={(e) => setForm({ ...form, dob: e.target.value })} /></div>
            <div className="field"><label>Member ID</label><input className="input" value={form.memberId} onChange={(e) => setForm({ ...form, memberId: e.target.value })} /></div>
          </div>
          <div className="field"><label>Payer</label><select className="input" value={form.payerId} onChange={(e) => setForm({ ...form, payerId: e.target.value })}>{payers.map((p) => <option key={p.id} value={p.id}>{p.name} ({p.stediPayerId})</option>)}</select></div>
          <div className="form-grid">
            <div className="field"><label>Email <span className="help">(for pre-visit cost notices &amp; reminders)</span></label><input className="input" type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} placeholder="patient@example.com" /></div>
            <div className="field"><label>Phone <span className="help">(optional)</span></label><input className="input" type="tel" value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} placeholder="+1 555 0100" /></div>
          </div>
          <div><button className="btn btn-primary" disabled={busy || !form.name || !form.dob || !form.memberId} onClick={add}>Save patient</button></div>
        </Drawer>
      )}
      {editing && (
        <Drawer title={<>Edit contact — {editing.name}</>} onClose={() => setEditing(null)}>
          <div className="notice info">Pre-visit cost notices and day-before reminders are sent to this email. Existing notices can be re-sent from Pre-Visit Notices → Send now.</div>
          <div className="field"><label>Email</label><input className="input" type="email" value={contact.email} onChange={(e) => setContact({ ...contact, email: e.target.value })} placeholder="patient@example.com" /></div>
          <div className="field"><label>Phone</label><input className="input" type="tel" value={contact.phone} onChange={(e) => setContact({ ...contact, phone: e.target.value })} placeholder="+1 555 0100" /></div>
          <div><button className="btn btn-primary" disabled={busy} onClick={async () => {
            setBusy(true)
            try { await api.updateContact(editing.id, contact); toast.show(`Contact updated for ${editing.name}`); setEditing(null); refresh() }
            catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
          }}>Save</button></div>
        </Drawer>
      )}
      {toast.el}
    </div>
  )
}
