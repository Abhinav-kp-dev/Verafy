-- AI voice verification for payers without real-time 270/271.
-- Additive only. Enum additions run as their own statements (psql autocommit).
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'CALL_IN_PROGRESS';
ALTER TYPE review_reason ADD VALUE IF NOT EXISTS 'voice_call_failed';
ALTER TYPE review_reason ADD VALUE IF NOT EXISTS 'call_timeout';

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS verification_source TEXT NOT NULL DEFAULT 'stedi_270_271'; -- stedi_270_271 | ai_voice_call
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS call_id TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS call_transcript TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS call_started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS call_completed_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_jobs_call_id ON jobs(call_id) WHERE call_id IS NOT NULL;

ALTER TABLE payers ADD COLUMN IF NOT EXISTS provider_services_phone TEXT; -- E.164; NULL = no voice path, plain manual review
ALTER TABLE payers ADD COLUMN IF NOT EXISTS ivr_notes TEXT;
