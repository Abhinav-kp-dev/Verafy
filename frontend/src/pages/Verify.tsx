import { useEffect, useState } from 'react'
import { api, type Patient, type Payer } from '../api'
import { Icon, useToast } from '../components/ui'
import { JobDetail } from '../components/JobDetail'

export function Verify() {
  const [step, setStep] = useState(1)
  const [payers, setPayers] = useState<Payer[]>([])
  const [patients, setPatients] = useState<Patient[]>([])
  const [form, setForm] = useState({ firstName: '', lastName: '', dob: '', memberId: '', payerId: '', savePatient: true })
  const [existing, setExisting] = useState('')
  const [jobId, setJobId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const toast = useToast()

  useEffect(() => {
    api.payers().then((p) => { setPayers(p); setForm((f) => ({ ...f, payerId: f.payerId || p[0]?.id || '' })) })
    api.patients().then(setPatients)
  }, [])

  const pickExisting = (id: string) => {
    setExisting(id)
    const p = patients.find((x) => x.id === id)
    if (!p) return
    const [first, ...rest] = p.name.split(' ')
    setForm({ firstName: first, lastName: rest.join(' '), dob: p.dob.slice(0, 10), memberId: p.memberId, payerId: p.payerId, savePatient: false })
  }

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm({ ...form, [k]: e.target.type === 'checkbox' ? (e.target as HTMLInputElement).checked : e.target.value })

  const payer = payers.find((p) => p.id === form.payerId)
  const step1ok = form.firstName && form.lastName && form.dob && form.memberId
  const submit = async () => {
    setBusy(true)
    try {
      const body = existing
        ? { patientId: existing }
        : { name: `${form.firstName} ${form.lastName}`.trim(), dob: form.dob, memberId: form.memberId, payerId: form.payerId, savePatient: form.savePatient }
      const r = await api.verify(body)
      setJobId(r.jobId)
      toast.show('Verification queued — watching for the payer response…')
    } catch (e) { toast.show((e as Error).message, true) } finally { setBusy(false) }
  }

  if (jobId) {
    return (
      <div className="page">
        <div className="page-head"><div><h1>Verification in progress</h1><p>Queued instantly · processed by the worker pool · every state change pushed live.</p></div>
          <button className="btn btn-ghost" onClick={() => { setJobId(null); setStep(1); setExisting('') }}>Verify another patient</button></div>
        <div className="card card-pad" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}><JobDetail jobId={jobId} /></div>
        {toast.el}
      </div>
    )
  }

  return (
    <div className="page">
      <div className="page-head"><div><h1>Verify Patient Insurance</h1><p>Enter patient and insurance details to check eligibility</p></div></div>
      <div className="card card-pad" style={{ maxWidth: 820 }}>
        <div className="steps">
          {['Patient Details', 'Insurance Details', 'Review & Submit'].map((s, i) => (
            <div key={s} style={{ display: 'contents' }}>
              <div className={`step ${step === i + 1 ? 'active' : step > i + 1 ? 'done' : ''}`}><span className="n">{step > i + 1 ? '✓' : i + 1}</span>{s}</div>
              {i < 2 && <div className="step-line" />}
            </div>
          ))}
        </div>

        {step === 1 && (
          <>
            <div className="field" style={{ marginBottom: 16 }}>
              <label>Existing patient (optional)</label>
              <select className="input" value={existing} onChange={(e) => pickExisting(e.target.value)}>
                <option value="">— Enter details manually —</option>
                {patients.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.payerName} · {p.memberId}</option>)}
              </select>
              <span className="help">Seeded patients match Stedi's documented test subscribers exactly (the sandbox rejects any other data).</span>
            </div>
            <h3 style={{ margin: '8px 0 12px', fontSize: 14 }}>Patient Information</h3>
            <div className="form-grid">
              <div className="field"><label>First Name <i>*</i></label><input className="input" value={form.firstName} onChange={set('firstName')} placeholder="Emily" /></div>
              <div className="field"><label>Last Name <i>*</i></label><input className="input" value={form.lastName} onChange={set('lastName')} placeholder="Carter" /></div>
              <div className="field"><label>Date of Birth <i>*</i></label><input className="input" type="date" value={form.dob} onChange={set('dob')} /></div>
              <div className="field"><label>Member ID <i>*</i></label><input className="input" value={form.memberId} onChange={set('memberId')} placeholder="A123456789" /></div>
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 20 }}><button className="btn btn-primary" disabled={!step1ok} onClick={() => setStep(2)}>Next <Icon name="arrow" size={16} /></button></div>
          </>
        )}

        {step === 2 && (
          <>
            <h3 style={{ margin: '8px 0 12px', fontSize: 14 }}>Insurance Information</h3>
            <div className="form-grid">
              <div className="field"><label>Payer <i>*</i></label>
                <select className="input" value={form.payerId} onChange={set('payerId')} disabled={!!existing}>
                  {payers.map((p) => <option key={p.id} value={p.id}>{p.name} ({p.stediPayerId}){p.supportsRealtime ? '' : ' — manual only'}</option>)}
                </select></div>
              <div className="field"><label>Plan Type</label><input className="input" value={payer?.planType ?? ''} readOnly /></div>
              <div className="field"><label>Service type</label><input className="input" value={payer ? `STC ${payer.serviceTypeCode} — ${payer.serviceTypeCode === '35' ? 'Dental Care' : 'Health Benefit Plan Coverage'}` : ''} readOnly /></div>
              <div className="field"><label>Relationship to Subscriber</label><select className="input" defaultValue="self"><option value="self">Self</option></select></div>
            </div>
            {payer && !payer.supportsRealtime && <div className="notice warn" style={{ marginTop: 14 }}>⚠ {payer.name} does not support electronic 270/271 checks — this request will be routed straight to Manual Review with call instructions.</div>}
            {!existing && <label style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 16, fontSize: 13 }}><input type="checkbox" checked={form.savePatient} onChange={set('savePatient')} />Save as new patient record</label>}
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 20 }}><button className="btn btn-ghost" onClick={() => setStep(1)}>Back</button><button className="btn btn-primary" onClick={() => setStep(3)}>Next <Icon name="arrow" size={16} /></button></div>
          </>
        )}

        {step === 3 && (
          <>
            <h3 style={{ margin: '8px 0 12px', fontSize: 14 }}>Review & Submit</h3>
            <dl className="kv">
              <dt>Patient</dt><dd>{form.firstName} {form.lastName}</dd>
              <dt>Date of birth</dt><dd className="num">{form.dob}</dd>
              <dt>Member ID</dt><dd className="num">{form.memberId}</dd>
              <dt>Payer</dt><dd>{payer?.name} · Payer ID {payer?.stediPayerId}</dd>
              <dt>Transaction</dt><dd>X12 270 eligibility inquiry → 271 response (STC {payer?.serviceTypeCode})</dd>
            </dl>
            <div className="notice info" style={{ marginTop: 16 }}>⚡ The request is accepted instantly and worked asynchronously. You'll see it move Queued → Processing → result in real time.</div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 20 }}><button className="btn btn-ghost" onClick={() => setStep(2)}>Back</button><button className="btn btn-primary" disabled={busy} onClick={submit}>Verify coverage <Icon name="zap" size={16} /></button></div>
          </>
        )}
      </div>
      {toast.el}
    </div>
  )
}
