-- CoverageCheck domain schema
-- Applied on top of River's own migration tables (river_job, etc.)

CREATE TABLE IF NOT EXISTS practices (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    stedi_payer_id      TEXT NOT NULL,
    supports_realtime   BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS patients (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id UUID NOT NULL REFERENCES practices(id),
    name        TEXT NOT NULL,
    dob         DATE NOT NULL,
    member_id   TEXT NOT NULL,
    payer_id    UUID NOT NULL REFERENCES payers(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS batches (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id   UUID NOT NULL REFERENCES practices(id),
    total_jobs    INT NOT NULL DEFAULT 0,
    queued        INT NOT NULL DEFAULT 0,
    processing    INT NOT NULL DEFAULT 0,
    verified      INT NOT NULL DEFAULT 0,
    gap_flagged   INT NOT NULL DEFAULT 0,
    retrying      INT NOT NULL DEFAULT 0,
    needs_review  INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$ BEGIN
    CREATE TYPE job_status AS ENUM (
    'QUEUED',
    'PROCESSING',
    'VERIFIED',
    'COVERAGE_GAP_FLAGGED',
    'RETRYING',
    'NEEDS_MANUAL_REVIEW',
    'MANUAL_RESOLVED'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE review_reason AS ENUM (
    'unsupported_payer',
    'ambiguous_match',
    'retry_exhausted',
    'malformed_response'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id          UUID REFERENCES batches(id),
    patient_id        UUID NOT NULL REFERENCES patients(id),
    payer_id          UUID NOT NULL REFERENCES payers(id),
    river_job_id      BIGINT,
    status            job_status NOT NULL DEFAULT 'QUEUED',
    attempt_count     INT NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ,
    raw_response      JSONB,
    normalized_brief  JSONB,
    review_reason     review_reason,
    resolved_by       TEXT,
    resolution_note   TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS job_attempts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID NOT NULL REFERENCES jobs(id),
    attempt_number  INT NOT NULL,
    status          TEXT NOT NULL,
    error_code      TEXT,
    error_message   TEXT,
    attempted_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_jobs_batch_id ON jobs(batch_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_job_attempts_job_id ON job_attempts(job_id);

-- Notify channel for SSE fan-out: every job state change publishes here
CREATE OR REPLACE FUNCTION notify_job_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify(
        'job_changes',
        json_build_object(
            'job_id', NEW.id,
            'batch_id', NEW.batch_id,
            'status', NEW.status,
            'attempt_count', NEW.attempt_count
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_job_change ON jobs;
CREATE TRIGGER trg_notify_job_change
    AFTER INSERT OR UPDATE OF status ON jobs
    FOR EACH ROW EXECUTE FUNCTION notify_job_change();
