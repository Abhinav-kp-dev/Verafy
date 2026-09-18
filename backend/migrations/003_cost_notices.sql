-- Pre-visit cost estimate + patient notice. Purely additive: new tables and one
-- nullable column. Nothing existing is altered.

ALTER TABLE patients ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE patients ADD COLUMN IF NOT EXISTS phone TEXT;

-- Practice fee schedule: what the practice charges per CDT procedure.
-- Example rates for the demo; pricing is practice-specific in production.
CREATE TABLE IF NOT EXISTS fee_schedule (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id  UUID NOT NULL REFERENCES practices(id),
    code         TEXT NOT NULL,          -- CDT code, e.g. D2740
    description  TEXT NOT NULL,
    fee_cents    BIGINT NOT NULL CHECK (fee_cents >= 0),
    UNIQUE (practice_id, code)
);

-- Scheduled visits with the procedures planned for that visit.
CREATE TABLE IF NOT EXISTS appointments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id     UUID NOT NULL REFERENCES practices(id),
    patient_id      UUID NOT NULL REFERENCES patients(id),
    scheduled_at    TIMESTAMPTZ NOT NULL,
    procedure_codes TEXT[] NOT NULL DEFAULT '{}',
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_appointments_scheduled_at ON appointments(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_appointments_patient ON appointments(patient_id);

-- Every notice generated for a patient. Written BEFORE any delivery attempt so
-- the app always has the exact content, whether or not an email provider is configured.
CREATE TABLE IF NOT EXISTS patient_notices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID NOT NULL REFERENCES jobs(id),
    appointment_id  UUID REFERENCES appointments(id),
    patient_id      UUID NOT NULL REFERENCES patients(id),
    channel         TEXT NOT NULL DEFAULT 'email',      -- email | sms
    recipient       TEXT,                               -- email address / phone (may be null -> logged only)
    subject         TEXT NOT NULL,
    body_text       TEXT NOT NULL,
    body_html       TEXT NOT NULL,
    estimate        JSONB NOT NULL,                     -- estimate.Result
    delivery_status TEXT NOT NULL DEFAULT 'logged',     -- logged | sent | failed | skipped_no_recipient | skipped_no_provider
    delivery_error  TEXT,
    provider_id     TEXT,                               -- id returned by the email provider
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_patient_notices_created ON patient_notices(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_patient_notices_job ON patient_notices(job_id);

-- Link a job to the appointment it was run for (null for ad-hoc checks).
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS appointment_id UUID REFERENCES appointments(id);
