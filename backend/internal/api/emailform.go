package api

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/jackc/pgx/v5"

	"coveragecheck/internal/db"
	"coveragecheck/internal/voiceagent"
)

func (s *Server) registerEmailFormRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /verify-form/{token}", s.emailFormPage)
	mux.HandleFunc("POST /api/webhooks/email/facts/{token}", s.emailFormSubmit)
	mux.HandleFunc("POST /api/payers/{id}/email", s.updatePayerEmail)
}

func (s *Server) updatePayerEmail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	var in struct {
		ProviderServicesEmail string `json:"providerServicesEmail"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if err := s.store.UpdatePayerEmail(r.Context(), id, in.ProviderServicesEmail); err != nil {
		writeErr(w, 404, "payer not found")
		return
	}
	s.log.Info("payer verification email updated", "payer", id, "set", in.ProviderServicesEmail != "")
	p, _ := s.store.GetPayer(r.Context(), id)
	writeJSON(w, 200, p)
}

// emailFormPage serves the hosted verification form. No auth: the token itself, sent
// only to the payer's own provider-services address, is the credential — same trust
// model as any "click the link we emailed you" flow.
func (s *Server) emailFormPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	job, err := s.store.GetJobByEmailToken(r.Context(), token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(404)
			formStatusPage.Execute(w, formStatusData{Title: "Link not found", Message: "This verification link doesn't match any request on file. It may have been mistyped — please check the email again."})
			return
		}
		w.WriteHeader(500)
		formStatusPage.Execute(w, formStatusData{Title: "Something went wrong", Message: "Please try again in a moment."})
		return
	}
	switch job.Status {
	case db.StatusEmailPending:
		w.WriteHeader(200)
		formPage.Execute(w, formPageData{
			Token: token, PracticeName: s.cfg.ProviderName, ProviderNPI: s.cfg.ProviderNPI,
			PayerName: job.PayerName, PatientName: job.PatientName,
			PatientDOB: job.PatientDOB.Format("January 2, 2006"), MemberID: job.MemberID,
		})
	case db.StatusVerified, db.StatusGapFlagged:
		w.WriteHeader(200)
		formStatusPage.Execute(w, formStatusData{Title: "Already completed", Message: "This verification was already submitted — thank you. No further action is needed."})
	default:
		w.WriteHeader(200)
		formStatusPage.Execute(w, formStatusData{Title: "No longer needed", Message: "This request has since been resolved another way and no longer needs a response. Thank you."})
	}
}

func (s *Server) emailFormSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var ex voiceagent.Extracted
	if err := decode(r, &ex); err != nil {
		writeErr(w, 400, "invalid submission")
		return
	}
	if err := s.queue.CompleteEmailWithFacts(r.Context(), token, ex); err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type formPageData struct {
	Token, PracticeName, ProviderNPI, PayerName, PatientName, PatientDOB, MemberID string
}

type formStatusData struct{ Title, Message string }

var formStatusPage = template.Must(template.New("status").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · Verafy</title>
<style>body{font-family:-apple-system,Segoe UI,Inter,sans-serif;background:#F8FAFC;color:#1F2937;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.card{background:#fff;border:1px solid #E5E7EB;border-radius:14px;padding:40px;max-width:440px;text-align:center;box-shadow:0 4px 20px rgba(15,23,42,.06)}
h1{font-size:19px;margin:0 0 10px}p{color:#6B7280;line-height:1.6;margin:0}</style></head>
<body><div class="card"><h1>{{.Title}}</h1><p>{{.Message}}</p></div></body></html>`))

var formPage = template.Must(template.New("form").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Benefits verification · Verafy</title>
<style>
:root{--g:#0E7A5F;--g2:#0B6B52;--text:#1F2937;--muted:#6B7280;--border:#E5E7EB;--bg:#F8FAFC}
*{box-sizing:border-box}body{font-family:-apple-system,Segoe UI,Inter,sans-serif;background:var(--bg);color:var(--text);margin:0;padding:24px}
.wrap{max-width:640px;margin:0 auto}
.head{background:var(--g);color:#fff;padding:20px 26px;border-radius:14px 14px 0 0}
.head b{font-size:19px}.head div{font-size:13px;opacity:.9;margin-top:2px}
.card{background:#fff;border:1px solid var(--border);border-top:0;border-radius:0 0 14px 14px;padding:26px}
.patient{background:var(--bg);border-radius:10px;padding:14px 16px;margin-bottom:22px;font-size:14px}
.patient div{margin:3px 0}.patient b{color:var(--text)}
fieldset{border:0;padding:0;margin:0 0 20px}
legend{font-weight:600;font-size:14px;margin-bottom:10px;padding:0}
label{display:block;font-size:13px;font-weight:500;margin:12px 0 5px}
input[type=text],input[type=number],textarea,select{width:100%;padding:9px 12px;border:1px solid var(--border);border-radius:8px;font:inherit;background:#fff}
textarea{min-height:60px;resize:vertical}
.row{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.radio-row{display:flex;gap:16px;margin-top:6px}
.radio-row label{display:flex;align-items:center;gap:6px;font-weight:400;margin:0}
.help{font-size:12px;color:var(--muted);margin-top:4px}
button{background:var(--g);color:#fff;border:0;padding:13px 22px;border-radius:9px;font:inherit;font-weight:600;cursor:pointer;width:100%;margin-top:8px}
button:hover{background:var(--g2)}button:disabled{opacity:.6;cursor:wait}
#err{background:#FEE2E2;color:#991B1B;padding:12px 14px;border-radius:8px;margin-top:14px;font-size:13px;display:none}
#done{display:none;text-align:center;padding:20px}
</style></head>
<body><div class="wrap">
<div class="head"><b>{{.PracticeName}}</b><div>Dental benefits verification request</div></div>
<div class="card">
<div id="formArea">
<div class="patient">
<div><b>Patient:</b> {{.PatientName}}</div>
<div><b>Date of birth:</b> {{.PatientDOB}}</div>
<div><b>Member ID:</b> {{.MemberID}}</div>
<div><b>Requesting provider NPI:</b> {{.ProviderNPI}}</div>
<div><b>Payer:</b> {{.PayerName}}</div>
</div>
<form id="f">
<fieldset>
<legend>Coverage status</legend>
<div class="radio-row">
<label><input type="radio" name="eligibility_status" value="active" required> Active</label>
<label><input type="radio" name="eligibility_status" value="inactive"> Inactive</label>
</div>
<label>Plan name (optional)</label><input type="text" name="plan_name">
</fieldset>
<fieldset>
<legend>Deductible &amp; annual maximum</legend>
<div class="row">
<div><label>Annual deductible ($)</label><input type="number" step="0.01" name="deductible_annual"></div>
<div><label>Deductible remaining ($)</label><input type="number" step="0.01" name="deductible_remaining"></div>
</div>
<div class="row">
<div><label>Annual maximum ($)</label><input type="number" step="0.01" name="annual_maximum"></div>
<div><label>Annual max remaining ($)</label><input type="number" step="0.01" name="annual_max_remaining"></div>
</div>
<label>Office visit copay ($, if any)</label><input type="number" step="0.01" name="copay_office">
</fieldset>
<fieldset>
<legend>Coinsurance (percent the plan pays)</legend>
<div class="row">
<div><label>Preventive / diagnostic %</label><input type="number" step="1" name="preventive_pct"></div>
<div><label>Basic / restorative %</label><input type="number" step="1" name="basic_pct"></div>
</div>
<label>Major / crowns %</label><input type="number" step="1" name="major_pct">
</fieldset>
<fieldset>
<legend>Orthodontics</legend>
<div class="radio-row">
<label><input type="radio" name="orthodontics_covered" value="yes"> Covered</label>
<label><input type="radio" name="orthodontics_covered" value="no"> Not covered</label>
<label><input type="radio" name="orthodontics_covered" value=""checked> Not applicable / unsure</label>
</div>
<label>Orthodontics % (if covered)</label><input type="number" step="1" name="orthodontics_pct">
</fieldset>
<fieldset>
<legend>Other details</legend>
<label>Waiting periods (if any)</label><input type="text" name="waiting_period" placeholder="e.g. 12 months on major services">
<label>Limitations / notes on frequency, missing-tooth clause, lifetime maximums</label>
<textarea name="limitations" placeholder="One per line"></textarea>
<div class="row">
<div><label>Your name</label><input type="text" name="rep_name"></div>
<div><label>Reference number</label><input type="text" name="reference_number"></div>
</div>
<label>Anything else we should know</label><textarea name="notes"></textarea>
</fieldset>
<button type="submit" id="submitBtn">Submit verification</button>
<div id="err"></div>
</form>
</div>
<div id="done"><h2 style="color:#0E7A5F">Thank you</h2><p style="color:#6B7280">The verification has been recorded. You can close this page.</p></div>
</div>
</div>
<script>
const f = document.getElementById('f');
const num = v => v === '' || v === null ? null : Number(v);
f.addEventListener('submit', async (e) => {
  e.preventDefault();
  const btn = document.getElementById('submitBtn');
  const err = document.getElementById('err');
  err.style.display = 'none';
  btn.disabled = true; btn.textContent = 'Submitting…';
  const fd = new FormData(f);
  const orthoRaw = fd.get('orthodontics_covered');
  const body = {
    eligibility_status: fd.get('eligibility_status') || 'unknown',
    plan_name: fd.get('plan_name') || '',
    deductible_annual: num(fd.get('deductible_annual')),
    deductible_remaining: num(fd.get('deductible_remaining')),
    annual_maximum: num(fd.get('annual_maximum')),
    annual_max_remaining: num(fd.get('annual_max_remaining')),
    copay_office: num(fd.get('copay_office')),
    preventive_pct: num(fd.get('preventive_pct')),
    basic_pct: num(fd.get('basic_pct')),
    major_pct: num(fd.get('major_pct')),
    orthodontics_covered: orthoRaw === '' ? null : orthoRaw === 'yes',
    orthodontics_pct: num(fd.get('orthodontics_pct')),
    waiting_period: fd.get('waiting_period') || '',
    limitations: (fd.get('limitations') || '').split('\\n').map(s => s.trim()).filter(Boolean),
    reference_number: fd.get('reference_number') || '',
    rep_name: fd.get('rep_name') || '',
    notes: fd.get('notes') || '',
  };
  const problems = [];
  if (body.deductible_annual != null && body.deductible_remaining != null && body.deductible_remaining > body.deductible_annual) {
    problems.push('Deductible remaining ($' + body.deductible_remaining + ') can\'t be more than the annual deductible ($' + body.deductible_annual + ').');
  }
  if (body.annual_maximum != null && body.annual_max_remaining != null && body.annual_max_remaining > body.annual_maximum) {
    problems.push('Annual max remaining ($' + body.annual_max_remaining + ') can\'t be more than the annual maximum ($' + body.annual_maximum + ').');
  }
  for (const [key, label] of [['preventive_pct','Preventive %'], ['basic_pct','Basic %'], ['major_pct','Major %'], ['orthodontics_pct','Orthodontics %']]) {
    const v = body[key];
    if (v != null && (v < 0 || v > 100)) problems.push(label + ' must be between 0 and 100.');
  }
  if (problems.length) {
    err.innerHTML = problems.map(p => '&bull; ' + p).join('<br>');
    err.style.display = 'block';
    btn.disabled = false; btn.textContent = 'Submit verification';
    err.scrollIntoView({ behavior: 'smooth', block: 'center' });
    return;
  }
  try {
    const r = await fetch('/api/webhooks/email/facts/{{.Token}}', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || 'Submission failed');
    document.getElementById('formArea').style.display = 'none';
    document.getElementById('done').style.display = 'block';
  } catch (ex) {
    err.textContent = ex.message;
    err.style.display = 'block';
    err.scrollIntoView({ behavior: 'smooth', block: 'center' });
    btn.disabled = false; btn.textContent = 'Submit verification';
  }
});
</script>
</body></html>`))
