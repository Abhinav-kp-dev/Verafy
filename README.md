# Verafy — Dental Insurance Verification Engine

> **Turn a 20-minute phone call into a sub-10-second automated check.**

Verafy is a full-stack insurance eligibility verification platform built for dental practices. It is **not** a form that wraps an API — it is a real orchestration engine with a durable queue, explicit state machine, per-payer rate limiting, exponential-backoff retry, and a dedicated manual-review workflow, all wired to a live X12 270/271 clearinghouse (Stedi).

---

## Features

- ⚡ **Instant response** — `POST /api/verifications` returns `202 Accepted` in milliseconds; work is queued and streamed back via Server-Sent Events
- 🔄 **State machine** — `QUEUED → PROCESSING → VERIFIED | COVERAGE_GAP_FLAGGED | RETRYING → NEEDS_MANUAL_REVIEW`
- 🧑‍💼 **Manual review workflow** — staff resolves with outcome + audit note; every action is timestamped
- 📦 **Batch upload** — CSV or JSON; bulk-inserts with `COPY`, enqueues in chunks of 500, returns in ~100 ms for any size
- 🚦 **Per-payer rate limiting** — token bucket (configurable RPS/burst); UI shows throttling notice when engaged
- 🔁 **Retry with backoff** — exponential + deterministic jitter; exhausted budget → manual review queue
- 📊 **Live dashboard** — Postgres `LISTEN/NOTIFY` → coalesced SSE → real-time job counters, batch progress, rate-limit state
- 🤖 **AI brief** — Gemini-powered plain-English coverage summary with post-hoc numeric validation; falls back to a deterministic template on any mismatch
- 📬 **Pre-visit cost notices** — estimates patient out-of-pocket share (deductible → coinsurance → annual max) and emails it before the appointment
- ☎️ **AI voice verification for payers with no EDI** — when a payer can't be checked electronically, a voice agent phones its provider-services line with the same member ID/DOB/NPI a 270 would carry, asks a fixed question script, records the answers through a structured tool call, and the result rejoins the normal pipeline tagged `ai_voice_call` (India-ready via Bolna; Retell supported)
- 🩹 **Graceful early-hangup recovery** — if the call ends before the agent formally submits its findings, an LLM salvage pass reads the transcript and recovers whatever was actually confirmed up to that point, so a cut-short call still surfaces real (flagged, partial) data instead of an empty Manual Review case
- ✉️ **Email-form verification** — a third channel alongside EDI and voice: a payer with no phone line but an email gets sent a link to a short hosted form (no login) asking the same benefit questions; their submission runs through the identical facts pipeline and shows up tagged `email_form`
- 🔔 **Live notification center** — derived entirely from real state (new manual-review cases, failed email deliveries, a paused queue) — never a fabricated event
- ⏯️ **Queue control** — pause/resume the verification queue and purge everything still waiting, from one toggle in the topbar
- 📄 **PDF export** — download a single patient's record or the full practice report as a print-ready PDF, generated server-side
- 🌗 **Dark / light theme** — full token-based theming with a one-click toggle, persisted per browser and defaulting to OS preference
- 🔬 **Synthetic load test** — enqueue 10,000 jobs from the UI or CLI to prove throughput

---

## Architecture

```
Front desk ──► API (202 Accepted) ──► Postgres-backed queue (River)
                                            │
                    bounded worker pool ◄───┘   per-payer token bucket
                          │
               Stedi  X12 270 ──► 271 (JSON)
                          │
       deterministic field mapper ──► typed Facts
                          │
         Gemini brief (validated) ──► plain-English summary
                          │
VERIFIED · COVERAGE_GAP_FLAGGED · RETRYING · NEEDS_MANUAL_REVIEW
                          │                         │
      Postgres LISTEN/NOTIFY ──► SSE ──► live dashboard
                          │
   appointment jobs ──► send_cost_notice ──► patient email notice
```

### State Machine

```
QUEUED ──► PROCESSING ──► VERIFIED                         (terminal)
                      ├──► COVERAGE_GAP_FLAGGED            (terminal — active but gaps)
                      ├──► RETRYING ──► PROCESSING         (transient payer error — backoff)
                      │             └► NEEDS_MANUAL_REVIEW (retry budget exhausted)
                      ├──► CALL_IN_PROGRESS                (no EDI, payer has a phone line — AI agent dials)
                      │        ├► VERIFIED | COVERAGE_GAP_FLAGGED   (facts submitted mid-call, validated)
                      │        └► NEEDS_MANUAL_REVIEW              (voice_call_failed | call_timeout, transcript kept)
                      ├──► EMAIL_PENDING                   (no EDI, no phone — payer has an email — form is sent)
                      │        ├► VERIFIED | COVERAGE_GAP_FLAGGED   (valid form submitted)
                      │        ├► EMAIL_PENDING                    (invalid submission — stays open, same link resubmittable)
                      │        └► NEEDS_MANUAL_REVIEW              (email_not_answered — response window elapsed)
                      └──► NEEDS_MANUAL_REVIEW             (rejected / unsupported payer, no phone or email)
NEEDS_MANUAL_REVIEW ──► MANUAL_RESOLVED                    (staff action, audited)
NEEDS_MANUAL_REVIEW ──► QUEUED                             ("Call payer with AI agent" / re-run)
```

### Repository Layout

```
backend/
  cmd/server        API + River workers + SSE hub (single binary)
  cmd/seed          practice, payers, Stedi test patients
  cmd/seedprevisit  fee schedule + tomorrow's appointments
  cmd/reset         wipe jobs/batches before a demo (keeps patients)
  cmd/loadtest      synthetic load runner with live counters
  internal/api      HTTP handlers, CORS, inbound rate limit, notifications, PDF + voice webhooks
  internal/queue    River worker (state machine) + retry logic + voice dispatch/completion/watchdog
  internal/voiceagent  Bolna + Retell clients, mock stand-in, facts validation, agent spec
                       (Extracted/Validate/ToFacts are shared by the email-form channel too)
  internal/stedi    X12 270/271 live client + mock simulator
  internal/normalize  271 → typed Facts field mapper (no AI)
  internal/llm      Gemini brief writer + numeric validation + template fallback
  internal/chatbot  In-app assistant (Gemini)
  internal/ratelimit  per-payer token buckets
  internal/events   Postgres LISTEN/NOTIFY → coalesced SSE
  internal/db       pgx store; atomic status transitions + batch counters
  internal/estimate deductible → coinsurance → annual-max math, unit-tested
  internal/notify   email rendering (HTML + text) + SMTP delivery
  internal/pdfreport  patient record + practice report PDF rendering (fpdf)
  migrations/       versioned SQL migrations

frontend/           React 19 + Vite + TypeScript
  Dashboard, Verify, Batch Upload, Patients, History,
  Manual Review, Pre-Visit Notices, Reports, Settings
  Notification center, queue control toggle, dark/light theme — all in the shared topbar
```

**Stack:** Go 1.22+ · [River](https://riverqueue.com) (Postgres-backed queue, no Redis) · pgx · `golang.org/x/time/rate` · SSE · React 19 · Vite · TypeScript

---

## Quick Start (Local, ~5 min)

**Requirements:** Go 1.22+, Node 20+, PostgreSQL 14+

### 1. Database

```bash
# Homebrew (macOS)
brew install postgresql@17 && brew services start postgresql@17
createdb verafy

# or Docker
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=dev postgres:16
```

### 2. Backend

```bash
cd backend
cp .env.example .env        # edit DATABASE_URL if needed
make migrate                # run all SQL migrations
make seed                   # seed practice + payer directory
make seedprevisit           # seed fee schedule + tomorrow's appointments
make run                    # API on :8080 (starts in mock mode, no key needed)
```

### 3. Frontend

```bash
cd frontend
cp .env.example .env        # VITE_API_URL=http://localhost:8080
npm install
npm run dev                 # http://localhost:5173
```

Open the dashboard → **Verify Insurance → Falcon Dent** → watch it go `Queued → Processing → Verified` in real-time.

Upload test patients via **Batch Upload → Choose File** (select `seed-data/patients.csv`) or:

```bash
curl -F file=@seed-data/patients.csv localhost:8080/api/batches/csv
```

---

## AI Voice Verification (payers with no real-time EDI)

Some payers (e.g. Delta Dental in test mode) have no 270/271 path. Instead of parking those checks in Manual Review, Verafy can place a **real phone call**:

1. The worker sees `supports_realtime = false` **and** a `provider_services_phone` on the payer.
2. It asks the voice platform to dial that line, passing the same fields a 270 carries (patient name, DOB, member ID, provider NPI, payer name, IVR hints) as per-call variables, and parks the job in `CALL_IN_PROGRESS` (live badge "Calling Payer").
3. The agent follows a fixed question script — active/inactive, deductible + remaining, annual max + remaining, coinsurance for preventive/basic/major, orthodontics, waiting periods, rep name + reference number.
4. As soon as it has the numbers it calls the `submit_verification_facts` tool → our webhook validates them (completeness + plausibility), maps them into the same `Facts` object the 271 normalizer produces, runs the normal brief pipeline, and finalizes to `VERIFIED` / `COVERAGE_GAP_FLAGGED` with `verification_source = ai_voice_call`.
5. No answer, IVR dead end, dropped call, refused rep or a watchdog timeout (`VOICE_CALL_TIMEOUT`, default 10 min) → first, if a transcript exists, a salvage pass tries to recover real confirmed data (see below); only if there's genuinely nothing usable does it fall to `NEEDS_MANUAL_REVIEW` with reason `voice_call_failed` / `call_timeout` and the transcript attached. A late webhook can never overwrite a finished job.

**Provider:** [Bolna](https://bolna.ai) by default — India-native, its default line dials +91 numbers directly (the callee sees a +1 caller ID; connect a Plivo number in Bolna and set `BOLNA_FROM_NUMBER` for a +91 caller ID — Twilio's Indian numbers are inbound-only under TRAI rules and won't work here). Retell is supported via `VOICE_PROVIDER=retell`.

**Mock mode** (`VOICE_MODE=mock`, the default) places no calls: a deterministic stand-in drives the exact same completion path, so the whole flow is demoable offline. Scenario is chosen by the member ID's last digit — `…0` no answer, `…9` IVR dead end, `…5` inactive coverage, anything else active. Transcripts in mock mode are clearly labelled as generated.

**Ending a call early:** staff (or a demo) can hang up mid-conversation without losing everything. Whatever the representative had already confirmed — active/inactive, a deductible, a coinsurance percentage — is recovered from the transcript by a dedicated LLM pass (`internal/voiceagent.TranscriptExtractor`, reusing `OPENROUTER_API_KEY`) and run through the exact same completeness/plausibility gate (`voiceagent.Validate`) a normal tool call gets. If that passes, the job finalizes to `VERIFIED`/`COVERAGE_GAP_FLAGGED` same as always, just tagged with a `call_ended_early_partial_data` flag and a note in the brief — never silently upgraded to look like a complete call. If nothing usable was said before the hangup, it still correctly falls to Manual Review.

### Going live with Bolna (~15 min)

1. Sign up at bolna.ai, copy the API key. On the **Calling** tab, set Telephony Provider to **Plivo** (not Twilio — see above) and connect a Plivo account with an Indian number.
2. Expose the backend publicly (e.g. `ngrok http 8080`) and open `GET /api/voice/agent-spec?baseUrl=https://<your-ngrok>.ngrok.app` — it returns the prompt, the custom-function definition in Bolna's own format (URL, `api_token`, `param` mapping), and the execution-webhook URL. Note ngrok's free tier issues a new URL every restart — both the Tools-tab function URL and the Extractions-tab webhook URL need updating if the tunnel restarts.
3. In Bolna: create an agent, paste the prompt into the **Agent** tab's Canvas (replacing anything the setup wizard drafted), add the custom function verbatim on the **Tools** tab, and set **Extractions → Push all execution data to webhook** to the returned URL.
4. Copy the agent's real ID (visible in its dashboard entry) into `.env` as `BOLNA_AGENT_ID` — a mismatch here is the most common setup mistake and shows up as the call asking generic questions instead of running your script.
5. `backend/.env`: `VOICE_MODE=live VOICE_PROVIDER=bolna BOLNA_API_KEY=… BOLNA_AGENT_ID=… VOICE_WEBHOOK_SECRET=<long random string>` (the same secret you pasted into the function's `api_token`; the webhook URL carries it as `?token=`).
6. Trial accounts only call **verified numbers** — add both your Bolna-connected number and every destination number you'll dial (from the trial-plan banner in the dashboard) before testing.
7. **Settings → Payer Settings → Add line** on any "No EDI" payer and enter the number to dial. For a demo, that can be a colleague's phone answering as the rep.
8. Verify a patient on that payer and watch Dashboard → "Calling payer", then the drawer: brief, facts, transcript.

---

## Email-Form Verification (payers with no phone line)

The third channel: a payer with no real-time EDI and no phone line, but a provider-services email, gets sent a link to a short hosted form instead.

1. The worker sees `supports_realtime = false`, no `provider_services_phone`, and a `provider_services_email` on the payer.
2. It generates a one-time token, emails a link (`{EMAIL_FORM_BASE_URL}/verify-form/{token}`) via whatever `notify.Sender` is configured (SMTP or Resend — the same one pre-visit notices use), and parks the job in `EMAIL_PENDING`.
3. The form itself is a single self-contained page (`GET /verify-form/{token}`, no login) asking the exact same questions the voice agent asks — active/inactive, deductible, annual max, coinsurance, orthodontics, waiting periods, rep name + reference number.
4. On submit, the form POSTs JSON to `/api/webhooks/email/facts/{token}`, which validates it through the same `voiceagent.Validate`/`ToFacts` gate a phone call's tool-call goes through, runs the normal brief pipeline, and finalizes to `VERIFIED`/`COVERAGE_GAP_FLAGGED` tagged `verification_source = email_form`.
5. An invalid submission (e.g. "remaining" exceeding the total, a percentage out of range) is **not** a dead end — unlike a one-shot phone call, a form can just be corrected. Basic consistency checks run client-side first with an inline, scrollable error message; anything that still fails server-side validation returns a clear reason and leaves the job in `EMAIL_PENDING` so the same link can be resubmitted. Only a genuinely unanswered request — no valid submission within `EMAIL_RESPONSE_TIMEOUT` (default 72h, watched by a River-scheduled job) — escalates to `NEEDS_MANUAL_REVIEW` (`email_not_answered`). A submission to an already-*completed* link is rejected, not silently ignored.

**Priority when a payer has both a phone and an email on file:** the AI voice call wins — email is the fallback for payers you haven't (or can't) set up a phone line for.

**Going live:** set `EMAIL_FORM_BASE_URL` to your public origin (e.g. behind the same ngrok tunnel used for voice, or your real domain in production) so the emailed link is reachable, and configure SMTP or Resend delivery (see "Enabling Email" below) — no separate signup needed, it reuses the existing email delivery integration. Add a payer's address under **Settings → Payer Settings → Add email**.

---

## Environment Variables

### Backend (`backend/.env`)

| Variable | Default | Required | Description |
|---|---|---|---|
| `DATABASE_URL` | — | ✅ | PostgreSQL connection string |
| `PORT` | `8080` | | API port |
| `CORS_ORIGIN` | `http://localhost:5173` | | Allowed frontend origin |
| `STEDI_MODE` | `mock` | | `mock` (no key) or `live` (real X12 calls) |
| `STEDI_API_KEY` | — | if `live` | Stedi test API key from [stedi.com](https://stedi.com) |
| `PROVIDER_NPI` | `1999999984` | | Your practice's 10-digit NPI |
| `PROVIDER_NAME` | `Riverside Dental Care` | | Sent on every 270 request |
| `GEMINI_API_KEY` | — | for AI | Google AI Studio key |
| `GEMINI_MODEL` | `gemini-2.5-flash` | | Gemini model for briefs + chatbot |
| `OPENROUTER_API_KEY` | — | optional | Alternative LLM via OpenRouter; also powers the voice early-hangup transcript salvage |
| `LLM_MODEL` | `openai/gpt-4o-mini` | | Model used with OpenRouter |
| `MAX_WORKERS` | `20` | | Bounded worker pool size |
| `MAX_ATTEMPTS` | `3` | | Retry budget before manual review |
| `RETRY_BASE` | `15s` | | First backoff (doubles each attempt + jitter) |
| `PAYER_RPS` | `5` | | Outbound requests/sec per payer |
| `PAYER_BURST` | `5` | | Token bucket burst size |
| `SMTP_HOST` | — | for email | e.g. `smtp.gmail.com` |
| `SMTP_PORT` | `587` | | SMTP port |
| `SMTP_USER` | — | for email | Your Gmail address |
| `SMTP_PASS` | — | for email | Gmail App Password (16 chars) |
| `NIGHTLY_HOUR` | `18` | | Hour for the pre-visit nightly run |
| `TIMEZONE` | `Asia/Kolkata` | | Timezone for scheduling |
| `VOICE_MODE` | `mock` | | `mock` (no calls; simulated completion) or `live` |
| `VOICE_PROVIDER` | `bolna` | | `bolna` (India-ready) or `retell` |
| `VOICE_CALL_TIMEOUT` | `10m` | | Watchdog before a stuck call goes to Manual Review |
| `VOICE_MOCK_DELAY` | `8s` | | Mock only: simulated call length |
| `BOLNA_API_KEY` / `BOLNA_AGENT_ID` | — | if live+bolna | From the Bolna dashboard |
| `BOLNA_FROM_NUMBER` | — | optional | Connected Exotel/Plivo number for a +91 caller ID |
| `VOICE_WEBHOOK_SECRET` | — | if live+bolna | Shared secret for Bolna's (unsigned) callbacks |
| `RETELL_API_KEY` / `RETELL_AGENT_ID` / `RETELL_FROM_NUMBER` | — | if live+retell | Retell credentials (webhooks are HMAC-signed with the key) |
| `EMAIL_FORM_BASE_URL` | `http://localhost:8080` | for real links | Public origin the emailed verification-form link points to |
| `EMAIL_RESPONSE_TIMEOUT` | `72h` | | Watchdog before an unanswered email goes to Manual Review |

### Frontend (`frontend/.env`)

| Variable | Default | Description |
|---|---|---|
| `VITE_API_URL` | `http://localhost:8080` | Backend API base URL |

---

## Going Live (Real Stedi Sandbox)

1. Create a free account at [stedi.com](https://stedi.com) → Developer Settings → API Keys → **Generate** (Mode = Test)
2. In `backend/.env`:
   ```
   STEDI_MODE=live
   STEDI_API_KEY=<your-test-key>
   ```
3. Restart the backend — no other changes needed

---

## Test Patients & Scenarios

| Patient | Payer | Member ID | Expected Result |
|---|---|---|---|
| Falcon Dent | Ameritas | `007007007` | ✅ VERIFIED |
| Elephant Dent | MetLife | `88877788` | ✅ VERIFIED (PPO + deductible) |
| Beaver Dent | UnitedHealthcare | `404404404` | ⚠️ COVERAGE_GAP_FLAGGED |
| Aardvark Dent | Anthem BCBS CA | `AFK987654321` | ⚠️ COVERAGE_GAP_FLAGGED (ortho) |
| Jane Doe | UnitedHealthcare | `UHCAAA42` | 🔄 RETRYING ×3 → NEEDS_MANUAL_REVIEW |
| Jane Doe | UnitedHealthcare | `UHCAAA75` | 🚨 NEEDS_MANUAL_REVIEW (subscriber not found) |
| Olivia Bennett | Delta Dental | `E788123456` | ☎️ CALL_IN_PROGRESS → VERIFIED via AI voice call (mock: active) |
| any patient | Delta / Guardian | ends in `0` / `9` / `5` | ☎️ mock: no answer / IVR dead end → Manual Review · inactive → COVERAGE_GAP_FLAGGED |
| any patient | Principal Dental | any | ✉️ EMAIL_PENDING → submit the form at `/verify-form/{token}` → VERIFIED |
| any voice call | Delta / Guardian | ended before facts submitted | 🩹 transcript salvage → VERIFIED/COVERAGE_GAP_FLAGGED with `call_ended_early_partial_data`, or Manual Review if nothing was confirmed |

---

## AI Brief & Hallucination Prevention

1. `internal/normalize` extracts a typed **Facts** object from the raw 271 using plain Go — no model involved
2. Gemini receives **only** the Facts object and must return a fixed JSON schema
3. `internal/llm.validate` rejects output if any number in the prose is not present in source facts
4. On rejection → deterministic template used, marked `template_fallback` with reason
5. Unit-tested in `internal/llm/brief_test.go`

---

## Enabling Email (Gmail, 2 min)

1. Google Account → Security → 2-Step Verification → **App passwords** → create one for "Verafy" → copy the 16-char password
2. In `backend/.env`:
   ```
   SMTP_HOST=smtp.gmail.com
   SMTP_PORT=587
   SMTP_USER=you@gmail.com
   SMTP_PASS=xxxx xxxx xxxx xxxx
   ```
3. Restart the backend

Settings → Verification Engine shows *Email delivery: on · smtp (smtp.gmail.com)* when configured.

---

## API Reference

| Method | Path | Description |
|---|---|---|
| POST | `/api/verifications` | Single check → `202 {jobId}` |
| GET | `/api/verifications` | History with filters |
| GET | `/api/verifications/{id}` | Job + attempts + raw 271 + brief |
| POST | `/api/batches` | Bulk check `{patientIds}` or `{all:true}` |
| POST | `/api/batches/csv` | Multipart CSV upload |
| POST | `/api/batches/synthetic` | Synthetic load `{count}` |
| GET | `/api/batches/{id}` | Batch counters + jobs |
| GET | `/api/review` | Manual review queue |
| POST | `/api/review/{id}/resolve` | Resolve with outcome + note |
| GET | `/api/events` | SSE stream (jobs, batches, rate-limits) |
| GET/POST | `/api/appointments` | Appointment scheduling |
| GET | `/api/notices` | Pre-visit notice log |
| POST | `/api/notices/{id}/send` | Send/resend a notice |
| GET/POST | `/api/fees` | Practice fee schedule |
| GET | `/api/stats` | Dashboard stats |
| GET | `/api/config` | Engine configuration |
| GET | `/api/payers` | Payer directory |
| GET | `/api/patients` | Patient list |
| GET | `/api/notifications` | Live notification feed (manual review, delivery failures, queue state) |
| GET | `/api/queue/status` | Current pause state of the verification queue |
| POST | `/api/queue/pause` | Pause the queue — in-flight jobs finish, no new work starts |
| POST | `/api/queue/resume` | Resume a paused queue |
| POST | `/api/queue/purge` | Delete every job still queued/retrying (River task + domain row) |
| GET | `/api/patients/{id}/pdf` | Download one patient's record as a PDF |
| GET | `/api/reports/pdf` | Download the practice report as a PDF |
| POST | `/api/payers/{id}/voice` | Set/clear a payer's AI call line + IVR notes |
| GET | `/api/voice/agent-spec` | Prompt, tool schema and webhook URLs to configure the voice agent (`?baseUrl=`) |
| POST | `/api/webhooks/bolna/facts` | Bolna custom function (mid-call facts) — Bearer `VOICE_WEBHOOK_SECRET` |
| POST | `/api/webhooks/bolna/execution` | Bolna execution webhook (call ended) — `?token=VOICE_WEBHOOK_SECRET` |
| POST | `/api/webhooks/voice/facts` · `/api/webhooks/voice` | Retell equivalents, `X-Retell-Signature` verified |
| POST | `/api/payers/{id}/email` | Set/clear a payer's verification email |
| GET | `/verify-form/{token}` | Hosted verification form (no auth — the token is the credential) |
| POST | `/api/webhooks/email/facts/{token}` | Form submission → validated → same facts pipeline as voice/EDI |

---

## Running Tests

```bash
cd backend && go test ./...
```

---

## Scale

- Batch of 10,000 patients → `202 Accepted` in **~100 ms**
- Worker pool drains queue with per-payer rate limiting
- Interactive single checks (priority 1) always preempt batch (priority 2)
- Measured: front-desk check completed in **0.66 s** while 2,900 batch jobs were queued
- Raise `PAYER_RPS=20` for faster demo (~100 s instead of ~7 min for 10k jobs)

---

## Out of Scope (Deliberately)

Claims (837) / remittance (835), HIPAA production hosting, multi-tenant auth, production Stedi billing. Sandbox only, by design.

---

## License

MIT
