-- Notice kinds (cost estimate vs. day-before reminder). Additive only.
ALTER TABLE patient_notices ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'cost_estimate';
CREATE INDEX IF NOT EXISTS idx_patient_notices_appt_kind ON patient_notices(appointment_id, kind);
