import { money, type Estimate } from '../api'

export function EstimateCard({ e, compact = false }: { e: Estimate; compact?: boolean }) {
  const inactive = e.eligibilityStatus !== 'active'
  return (
    <div className="card" style={{ overflow: 'hidden' }}>
      <div className="card-head" style={{ background: 'var(--surface-alt)' }}>
        <div><h3>Pre-visit cost estimate</h3><div className="cell-sub">{e.planName ? `${e.planName} · ` : ''}deductible first → coinsurance → annual maximum</div></div>
        {inactive
          ? <span className="badge tone-error"><span className="dot" />Coverage not active</span>
          : e.patientPaysCents === 0 ? <span className="badge tone-success"><span className="dot" />Fully covered</span> : null}
      </div>
      <div className="tbl-wrap">
        <table className="tbl">
          <thead><tr><th>Procedure</th><th style={{ textAlign: 'right' }}>Fee</th>{!compact && <th style={{ textAlign: 'right' }}>Deductible</th>}<th style={{ textAlign: 'right' }}>Insurance pays</th><th style={{ textAlign: 'right' }}>Patient pays</th></tr></thead>
          <tbody>
            {e.lines.map((li, i) => (
              <tr key={i}>
                <td>
                  <div className="cell-main"><span className="num" style={{ color: 'var(--muted)', marginRight: 8 }}>{li.code}</span>{li.description}</div>
                  <div className="cell-sub">{li.covered && li.planPaysPct != null ? `${li.category} · plan pays ${li.planPaysPct}%` : (li.note ?? 'Not covered')}{li.overAnnualMaxCents > 0 && <span style={{ color: 'var(--warning-text)' }}> · {money(li.overAnnualMaxCents)} over annual max</span>}</div>
                </td>
                <td className="num" style={{ textAlign: 'right' }}>{money(li.feeCents)}</td>
                {!compact && <td className="num" style={{ textAlign: 'right' }}>{li.deductibleAppliedCents ? money(li.deductibleAppliedCents) : '—'}</td>}
                <td className="num" style={{ textAlign: 'right' }}>{money(li.insurancePaysCents)}</td>
                <td className="num" style={{ textAlign: 'right', fontWeight: 600, color: li.covered ? 'inherit' : 'var(--error-text)' }}>{money(li.patientPaysCents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="card-pad" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12, borderTop: '1px solid var(--border)' }}>
        <div className="cov"><span>Total fees</span><b className="num">{money(e.totalFeeCents)}</b></div>
        <div className="cov"><span>Insurance expected to pay</span><b className="num">{money(e.insurancePaysCents)}</b>{e.annualMaximumCents != null && <span>of {money(e.annualMaximumCents)} annual max</span>}</div>
        <div className="cov" style={{ background: 'var(--green-50)', borderColor: 'var(--green-300)' }}><span>Patient's estimated share</span><b className="num" style={{ color: 'var(--accent)', fontSize: 22 }}>{money(e.patientPaysCents)}</b>{e.deductibleRemainingStartCents != null && e.deductibleRemainingStartCents > 0 && <span>includes {money(e.deductibleUsedCents)} deductible</span>}</div>
      </div>
      {!compact && <div className="card-pad" style={{ paddingTop: 0 }}><p className="help" style={{ margin: 0 }}>{e.disclaimer}</p></div>}
    </div>
  )
}
