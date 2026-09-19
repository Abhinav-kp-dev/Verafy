# Verafy

### **DSOLVE 2026** · DRISHTI · College of Engineering Trivandrum (CET)

**BUILD. SOLVE. DEMONSTRATE.**

|                   |                                                         |
| ----------------- | ------------------------------------------------------- |
| **Problem:**      | Problem 3 — Insurance Verification Automation            |
| **Team Name:**    | `[Your Team Name]`                                       |
| **Team Members:** | `[Name 1]` · `[Name 2]` · `[Name 3]` · `[Name 4]`         |
| **Institution:**  | `[College / University]`                                 |
| **Live Demo:**    | `[Demo link goes here]`                                  |
| **Pitch Video:**  | `[Social media pitch video link]`                        |

---

## Table of Contents

- [Problem Statement](#problem-statement)
- [Our Solution](#our-solution)
- [Key Features](#key-features)
- [Screenshots & Demo](#screenshots--demo)
- [Tech Stack](#tech-stack)
- [Getting Started](#getting-started)
- [Usage / Demo Script](#usage--demo-script)
- [Limitations & Future Scope](#limitations--future-scope)
- [Technical Deep Dive](#technical-deep-dive)
- [Team](#team)
- [Submission Checklist](#submission-checklist)

---

## Problem Statement

> ## Problem 3: Insurance Verification Automation
>
> Develop a solution to simplify and automate insurance verification for dental
> practices.
>
> Currently, dental practices often need to manually contact insurance companies to
> verify whether a patient's insurance is active and what coverage is available.
> This process can take 20–30 minutes per patient, creating significant
> administrative effort and delays.
>
> The solution should explore ways to automate or significantly reduce this manual
> process by quickly verifying insurance eligibility and presenting the relevant
> information to the dental practice in a simple and usable format.

### Why this matters

A front desk verifying insurance by phone loses 20–30 minutes *per patient* — for a practice seeing 30 patients a day, that's multiple staff-hours spent on hold, every single day. It delays same-day bookings, creates billing surprises when coverage turns out to be inactive, and is the single most common source of preventable claim denials in dental practices. Every practice does this, every day, and almost none of it is automated end-to-end — because the moment a payer doesn't support electronic verification, every existing tool just tells staff to "call the payer," and the whole problem comes right back.

---

## Our Solution

**Verafy is not a form that wraps an eligibility API.** It's a real orchestration engine that verifies insurance through **three separate channels**, feeding into one identical, validated data pipeline — so a practice always gets a structured, trustworthy answer, no matter how the payer actually gives it up:

1. **Electronic (X12 270/271)** via a live Stedi clearinghouse connection — the fast path, when the payer supports it.
2. **AI voice call** — when a payer has no electronic connection, an AI agent phones their provider-services line, asks the same benefits questions a receptionist would, and extracts the answer through a structured, validated tool call.
3. **Emailed hosted form** — when a payer has no phone line either, a one-time link to a short, self-contained form is emailed to their provider-services team; their submission runs through the *exact same* validation and data pipeline as the phone call or the electronic response.

Every path — electronic, AI voice, or human-filled form — produces the same typed `Facts` object, the same AI-written plain-English brief (numerically validated against source data before it's ever shown to staff), the same dashboard entry, and the same downloadable PDF. Nothing is fabricated: every number shown is traceable back to where it came from, and every failure mode (no answer, a dropped call, a payer that never got back to us) is handled explicitly and surfaced to staff — never silently dropped.

---

## Key Features

- **Instant electronic verification** — `POST /api/verifications` returns `202 Accepted` in milliseconds; a durable Postgres-backed queue (River) processes it and streams the result back live via Server-Sent Events.
- **AI voice verification for payers with no EDI** — an AI agent (Bolna, India-ready; Retell supported) calls the payer's provider-services line with the same patient/provider data a 270 would carry, follows a fixed question script, and submits the collected benefits through a validated tool call.
- **Emailed hosted-form verification** — the third fallback channel for payers with no phone line: a hosted form (no login) collects the same data a human would give over the phone, validated the same way.
- **Graceful early-hangup recovery** — if a call ends before the agent formally submits its findings, an LLM salvage pass reads the transcript and recovers whatever was actually confirmed, so a cut-short call still surfaces real (flagged, partial) data instead of nothing.
- **AI-written coverage brief with hallucination prevention** — every number the model writes is checked against the source facts before it's trusted; anything that doesn't match falls back to a deterministic, template-written brief instead.
- **Full state machine with retry & manual-review fallback** — exponential backoff with jitter for transient payer errors, and a dedicated Manual Review queue with an audit trail for anything that genuinely needs a human.
- **Batch upload & synthetic load testing** — bulk-verify a CSV of patients in one call, or enqueue 10,000 synthetic jobs to prove the pipeline holds up under load.
- **Live operational dashboard** — real-time job counters, batch progress, per-payer rate-limit state, a live notification center, queue pause/resume/purge controls, and one-click PDF export of any patient record or practice-wide report.
- **Dark/light theme, pre-visit cost estimator emails, and an in-app AI assistant** round out the day-to-day front-desk experience.

---

## Screenshots & Demo

| Screenshot                                            | Description                          |
| ----------------------------------------------------- | ------------------------------------ |
| `[Screenshot 1]`                                       | Dashboard — live verification counters, recent activity |
| `[Screenshot 2]`                                       | AI voice call in progress for a no-EDI payer |
| `[Screenshot 3]`                                       | Hosted email verification form |
| `[Screenshot 4]`                                       | Manual Review with call transcript / form audit trail |
| **Pitch Video**                                        | `[Link to your >30s social pitch video]` |

---

## Tech Stack

| Layer            | Technology                                                             | Why we chose it |
| ---------------- | ------------------------------------------------------------------------ | ---------------- |
| Frontend         | React 19 + Vite + TypeScript                                            | Fast dev loop, typed API contracts shared conceptually with the Go backend, no framework overhead for a dashboard-heavy UI |
| Backend          | Go 1.22+, standard `net/http`                                            | Strong concurrency primitives for a worker-pool architecture; no framework magic to fight when building an explicit state machine |
| Queue            | [River](https://riverqueue.com) (Postgres-backed)                       | Durable job queue with zero extra infrastructure — no Redis, jobs survive restarts, scheduled/delayed jobs built in (used for every watchdog timer) |
| Database         | PostgreSQL (pgx driver)                                                  | `LISTEN/NOTIFY` gives free real-time dashboard updates without a separate pub/sub system; strong transactional guarantees for atomic status transitions |
| Electronic Eligibility | [Stedi](https://stedi.com) X12 270/271 clearinghouse             | Real, production-grade EDI eligibility checks with a documented mock-sandbox mode for offline development |
| AI Voice Agent   | [Bolna](https://bolna.ai) (India-ready), Retell (alternative)            | Bolna's default line dials +91 numbers directly and supports a custom-LLM-style structured tool call, matching the exact data-collection schema our validation pipeline expects |
| LLM / AI         | Google Gemini + OpenRouter (model-agnostic)                              | Gemini powers the in-app assistant and coverage briefs; OpenRouter gives a model-agnostic fallback and powers the early-hangup transcript-salvage pass |
| Email Delivery   | SMTP (Gmail) or Resend                                                   | Either works with zero extra signup for a hackathon judge to reproduce; same integration sends pre-visit notices and the verification-form links |
| PDF Generation   | [go-pdf/fpdf](https://github.com/go-pdf/fpdf)                            | Server-side PDF rendering with no headless-browser dependency |
| Infra / Hosting  | Self-hosted binary (Go single binary) + Postgres; ngrok for public webhook exposure during development | Zero-dependency deploy — one binary, one database, works anywhere Go and Postgres run |

---

## Getting Started

### Prerequisites

- Go **1.22+**
- Node **20+**
- PostgreSQL **14+**
- Optional, for the full experience: a [Stedi](https://stedi.com) test API key, a [Bolna](https://bolna.ai) account, and Gmail (or [Resend](https://resend.com)) SMTP credentials — the app runs fully in **mock mode** with none of these

### Installation

**1. Database**

```bash
# Homebrew (macOS)
brew install postgresql@17 && brew services start postgresql@17
createdb verafy

# or Docker
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=dev postgres:16
```

**2. Backend**

```bash
cd backend
cp .env.example .env        # edit DATABASE_URL if needed
make migrate                # run all SQL migrations
make seed                   # seed practice + payer directory
make seedprevisit           # seed fee schedule + tomorrow's appointments
make run                    # API on :8080 (starts in mock mode, no key needed)
```

**3. Frontend**

```bash
cd frontend
cp .env.example .env        # VITE_API_URL=http://localhost:8080
npm install
npm run dev                 # http://localhost:5173
```

Open the dashboard → **Verify Insurance → Falcon Dent** → watch it go `Queued → Processing → Verified` in real time.

### Environment Variables

The full annotated list lives in [`backend/.env.example`](./backend/.env.example) (30+ vars covering every optional integration). The essentials:

| Variable       | Description                                  | Example                                     |
| -------------- | --------------------------------------------- | -------------------------------------------- |
| `DATABASE_URL` | PostgreSQL connection string                  | `postgres://user@localhost:5432/verafy`      |
| `PORT`         | Port the backend listens on                   | `8080`                                       |
| `STEDI_MODE`   | `mock` (default, no key needed) or `live`     | `mock`                                       |
| `VOICE_MODE`   | `mock` (default, simulated calls) or `live`   | `mock`                                       |
| `OPENROUTER_API_KEY` | Powers the AI brief writer + transcript salvage (optional — falls back to a deterministic template) | `sk-or-v1-...` |

> Every integration (Stedi, Bolna/Retell, Gemini, SMTP/Resend) has a working **mock mode** — nothing above is required to run and demo the full state machine locally. Never commit real keys: `backend/.env` is already gitignored.

---

## Usage / Demo Script

*This doubles as the live demo runbook (3–5 min).*

1. **Boot** — `make run` (backend) + `npm run dev` (frontend), both start in mock mode with zero API keys.
2. **The fast path** — submit a patient against an electronic payer (e.g. Falcon Dent / Ameritas) and watch it go `Queued → Processing → Verified` in under a second, with a live AI-written coverage brief appearing on the dashboard.
3. **The wow moment** — submit a patient against a payer with no electronic connection (e.g. Delta Dental) and show the dashboard flip to **"Calling Payer"** — the AI agent is placing a real phone call, asking the same benefits questions a receptionist would, live.
4. **The trust close** — open the resulting record and show the **call transcript** alongside the extracted, validated benefits — every figure the AI reports is checked against what was actually said before it's trusted. Then show the same outcome happening through the **emailed hosted form** for a payer with no phone line at all — same validation, same pipeline, same dashboard.
5. **Wrap-up** — Manual Review only ever contains the genuine dead-ends (no answer, no response, a payer that outright rejected the request) — with a full audit trail. In production, this closes the loop that every existing EDI-only tool leaves open: from "verify what you can electronically" to "verify everything, one way or another."

---

## Limitations & Future Scope

### Known Limitations

- Sandbox-only: runs against Stedi's test environment and a demo Bolna/SMTP setup, not production payer connections or production telephony minutes.
- Single-practice / single-tenant data model — no multi-tenant auth yet.
- Claims (X12 837) and remittance (X12 835) are out of scope; this solves eligibility verification, not the full revenue cycle.
- AI voice calling currently defaults to India-based telephony (Bolna); US-market production telephony (a properly connected Twilio/Plivo number with a local caller ID) needs additional carrier setup.
- Free-tier ngrok tunnels rotate on restart, so publicly-reachable webhook URLs (voice + email) need re-registering with the telephony/email provider after a tunnel restart during development.

### Future Scope

- Production Stedi agreement + real payer connections, replacing the sandbox.
- Multi-tenant support for dental service organizations (DSOs) managing many locations from one account.
- HIPAA-compliant production hosting and a formal security/compliance review.
- Claims submission and remittance tracking (X12 837/835) to extend from eligibility into the full revenue cycle.
- A payer-side self-serve portal, so payers can register their own provider-services contact once instead of being called/emailed by every practice that uses Verafy.
- Native US telephony (properly connected local caller ID) alongside the existing India-ready Bolna integration.

---

## Technical Deep Dive

<details>
<summary>Architecture, state machine, and full API reference (click to expand)</summary>

### Architecture

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

### AI Voice Verification

1. The worker sees `supports_realtime = false` **and** a `provider_services_phone` on the payer.
2. It asks the voice platform to dial that line, passing the same fields a 270 carries (patient name, DOB, member ID, provider NPI, payer name, IVR hints), and parks the job in `CALL_IN_PROGRESS`.
3. The agent follows a fixed question script — active/inactive, deductible + remaining, annual max + remaining, coinsurance for preventive/basic/major, orthodontics, waiting periods, rep name + reference number.
4. As soon as it has the numbers it calls the `submit_verification_facts` tool → our webhook validates completeness + plausibility, maps the result into the same `Facts` object the 271 normalizer produces, runs the normal brief pipeline, and finalizes to `VERIFIED` / `COVERAGE_GAP_FLAGGED`.
5. No answer, IVR dead end, dropped call, or a watchdog timeout → a transcript-salvage pass first tries to recover real confirmed data before falling to `NEEDS_MANUAL_REVIEW`.

**Provider:** [Bolna](https://bolna.ai) by default — its default line dials +91 numbers directly; connect a Plivo number for a +91 caller ID (Twilio's Indian numbers are inbound-only under TRAI rules). Retell is supported via `VOICE_PROVIDER=retell`. **Mock mode** (`VOICE_MODE=mock`, default) places no real calls — a deterministic stand-in drives the identical completion path for offline demos.

### Email-Form Verification

1. The worker sees no EDI and no phone line, but a `provider_services_email` on the payer.
2. It generates a one-time token, emails a link (`{EMAIL_FORM_BASE_URL}/verify-form/{token}`), and parks the job in `EMAIL_PENDING`.
3. The form (`GET /verify-form/{token}`, no login) asks the exact same questions the voice agent asks.
4. On submit, the same `Validate`/`ToFacts` gate and brief pipeline runs, finalizing to `VERIFIED`/`COVERAGE_GAP_FLAGGED`.
5. An invalid submission is correctable, not a dead end — the job stays in `EMAIL_PENDING` for resubmission on the same link. Only a genuinely unanswered request (`EMAIL_RESPONSE_TIMEOUT`, default 72h) escalates to Manual Review.

### AI Brief & Hallucination Prevention

1. A typed **Facts** object is extracted deterministically (271 field-mapping, or the voice/email tool-call schema) — no model involved in this step.
2. Gemini/OpenRouter receives **only** the Facts object and must return a fixed JSON schema.
3. Every number in the model's prose is cross-checked against source facts; any mismatch is rejected.
4. On rejection → a deterministic, template-written brief is used instead, marked `template_fallback`.

### Repository Layout

```
backend/
  cmd/server           API + River workers + SSE hub (single binary)
  cmd/seed             practice, payers, Stedi test patients
  cmd/seedprevisit     fee schedule + tomorrow's appointments
  cmd/reset            wipe jobs/batches before a demo (keeps patients)
  cmd/loadtest         synthetic load runner with live counters
  internal/api         HTTP handlers, CORS, rate limiting, notifications, PDF + voice/email webhooks
  internal/queue       River worker (state machine), retry logic, voice/email dispatch + watchdogs
  internal/voiceagent  Bolna + Retell clients, mock stand-in, facts validation, agent spec
  internal/stedi       X12 270/271 live client + mock simulator
  internal/normalize   271 → typed Facts field mapper (no AI)
  internal/llm         Gemini/OpenRouter brief writer + numeric validation + template fallback
  internal/chatbot     In-app assistant (Gemini)
  internal/ratelimit   per-payer token buckets
  internal/events      Postgres LISTEN/NOTIFY → coalesced SSE
  internal/db          pgx store; atomic status transitions + batch counters
  internal/estimate    deductible → coinsurance → annual-max math, unit-tested
  internal/notify      email rendering (HTML + text) + SMTP/Resend delivery
  internal/pdfreport   patient record + practice report PDF rendering
  migrations/          versioned SQL migrations

frontend/              React 19 + Vite + TypeScript
  Dashboard, Verify, Batch Upload, Patients, History,
  Manual Review, Pre-Visit Notices, Reports, Settings
```

### Test Patients & Scenarios (mock mode)

| Patient | Payer | Member ID | Expected Result |
|---|---|---|---|
| Falcon Dent | Ameritas | `007007007` | ✅ VERIFIED |
| Elephant Dent | MetLife | `88877788` | ✅ VERIFIED (PPO + deductible) |
| Beaver Dent | UnitedHealthcare | `404404404` | ⚠️ COVERAGE_GAP_FLAGGED |
| Jane Doe | UnitedHealthcare | `UHCAAA75` | 🚨 NEEDS_MANUAL_REVIEW (subscriber not found) |
| Olivia Bennett | Delta Dental | `E788123456` | ☎️ CALL_IN_PROGRESS → VERIFIED via AI voice call |
| any patient | Principal Dental | any | ✉️ EMAIL_PENDING → submit the hosted form → VERIFIED |

### Full API Reference

| Method | Path | Description |
|---|---|---|
| POST | `/api/verifications` | Single check → `202 {jobId}` |
| GET | `/api/verifications` | History with filters |
| GET | `/api/verifications/{id}` | Job + attempts + raw 271 + brief |
| POST | `/api/batches` / `/api/batches/csv` | Bulk / CSV verification |
| GET | `/api/review` · POST `/api/review/{id}/resolve` | Manual review queue + resolution |
| GET | `/api/events` | SSE stream (jobs, batches, rate-limits) |
| GET | `/api/stats` · `/api/config` | Dashboard stats + engine configuration |
| GET | `/api/patients/{id}/pdf` · `/api/reports/pdf` | PDF export |
| POST | `/api/payers/{id}/voice` · `/api/payers/{id}/email` | Configure a payer's AI call line / verification email |
| GET | `/api/voice/agent-spec` | Prompt + tool schema to configure the voice agent |
| POST | `/api/webhooks/bolna/*` · `/api/webhooks/voice*` | Voice provider webhooks (authenticated) |
| GET | `/verify-form/{token}` · POST `/api/webhooks/email/facts/{token}` | Hosted form + submission |

### Running Tests

```bash
cd backend && go test ./...
```

### Scale

- Batch of 10,000 patients → `202 Accepted` in **~100 ms**
- Interactive single checks always preempt batch processing
- Measured: a front-desk check completed in **0.66 s** while 2,900 batch jobs were queued behind it

</details>

---

## Team

| Name       | Role(s)                          | GitHub      | Email         |
| ---------- | --------------------------------- | ----------- | ------------- |
| `[Name 1]` | `[e.g. Full-stack / Backend]`     | `[@handle]` | `[email]`     |
| `[Name 2]` | `[e.g. AI / Voice integration]`   | `[@handle]` | `[email]`     |
| `[Name 3]` | `[e.g. Frontend / Design]`        | `[@handle]` | `[email]`     |

---

## Submission Checklist

- [x] Clean, runnable source code committed to this **public** repo
- [ ] `README.md` fully filled in — team name, members, institution, live demo link, pitch video link still need real values above
- [ ] Pitch video (>30s, English) posted on a team member's social profile tagging **@DrishtiCET** & **@CareStack**, link added above
- [x] All secrets/API keys removed from the repo (`.env` is gitignored; only `.env.example` with placeholder values is committed)
- [ ] Quick-start verified from a fresh clone (`git clone` → run)

---

**License:** MIT
