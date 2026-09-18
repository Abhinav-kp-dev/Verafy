-- Email-form verification: a third channel alongside Stedi EDI and AI voice calls.
-- A payer with no real-time EDI and no phone line, but an email on file, gets sent a
-- link to a hosted form; the insurance rep fills it in and the submission runs
-- through the exact same facts pipeline as a voice call.
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'EMAIL_PENDING';
ALTER TYPE review_reason ADD VALUE IF NOT EXISTS 'email_not_answered';
ALTER TYPE review_reason ADD VALUE IF NOT EXISTS 'email_response_invalid';

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS email_token TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS email_sent_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_email_token ON jobs(email_token) WHERE email_token IS NOT NULL;

ALTER TABLE payers ADD COLUMN IF NOT EXISTS provider_services_email TEXT; -- NULL = no email path
