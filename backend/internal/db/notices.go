package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------- fee schedule ----------

type FeeScheduleItem struct {
	ID          uuid.UUID `db:"id" json:"id"`
	PracticeID  uuid.UUID `db:"practice_id" json:"practiceId"`
	Code        string    `db:"code" json:"code"`
	Description string    `db:"description" json:"description"`
	FeeCents    int64     `db:"fee_cents" json:"feeCents"`
}

func (s *Store) ListFeeSchedule(ctx context.Context, practiceID uuid.UUID) ([]FeeScheduleItem, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, practice_id, code, description, fee_cents FROM fee_schedule WHERE practice_id=$1 ORDER BY code`, practiceID)
	return pgx.CollectRows(rows, pgx.RowToStructByName[FeeScheduleItem])
}

func (s *Store) FeesForCodes(ctx context.Context, practiceID uuid.UUID, codes []string) (map[string]FeeScheduleItem, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, practice_id, code, description, fee_cents FROM fee_schedule WHERE practice_id=$1 AND code = ANY($2)`, practiceID, codes)
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[FeeScheduleItem])
	if err != nil {
		return nil, err
	}
	out := make(map[string]FeeScheduleItem, len(items))
	for _, it := range items {
		out[it.Code] = it
	}
	return out, nil
}

func (s *Store) UpsertFee(ctx context.Context, practiceID uuid.UUID, code, desc string, feeCents int64) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO fee_schedule (practice_id, code, description, fee_cents) VALUES ($1,$2,$3,$4)
		ON CONFLICT (practice_id, code) DO UPDATE SET description=EXCLUDED.description, fee_cents=EXCLUDED.fee_cents`, practiceID, code, desc, feeCents)
	return err
}

// ---------- appointments ----------

type Appointment struct {
	ID             uuid.UUID `db:"id" json:"id"`
	PracticeID     uuid.UUID `db:"practice_id" json:"practiceId"`
	PatientID      uuid.UUID `db:"patient_id" json:"patientId"`
	ScheduledAt    time.Time `db:"scheduled_at" json:"scheduledAt"`
	ProcedureCodes []string  `db:"procedure_codes" json:"procedureCodes"`
	Notes          *string   `db:"notes" json:"notes,omitempty"`
	CreatedAt      time.Time `db:"created_at" json:"createdAt"`
	// joined
	PatientName  string  `db:"patient_name" json:"patientName"`
	PatientEmail *string `db:"patient_email" json:"patientEmail,omitempty"`
	PayerName    string  `db:"payer_name" json:"payerName"`
	PayerID      uuid.UUID `db:"payer_id" json:"payerId"`
	MemberID     string  `db:"member_id" json:"memberId"`
	// latest job for this appointment (if any)
	JobID        *uuid.UUID `db:"job_id" json:"jobId,omitempty"`
	JobStatus    *string    `db:"job_status" json:"jobStatus,omitempty"`
	NoticeID     *uuid.UUID `db:"notice_id" json:"noticeId,omitempty"`
	NoticeStatus *string    `db:"notice_status" json:"noticeStatus,omitempty"`
	ReminderID     *uuid.UUID `db:"reminder_id" json:"reminderId,omitempty"`
	ReminderStatus *string    `db:"reminder_status" json:"reminderStatus,omitempty"`
}

const appointmentSelect = `
SELECT a.id, a.practice_id, a.patient_id, a.scheduled_at, a.procedure_codes, a.notes, a.created_at,
       p.name AS patient_name, p.email AS patient_email, py.name AS payer_name, p.payer_id, p.member_id,
       j.id AS job_id, j.status::text AS job_status, n.id AS notice_id, n.delivery_status AS notice_status,
       rm.id AS reminder_id, rm.delivery_status AS reminder_status
FROM appointments a
JOIN patients p ON p.id = a.patient_id
JOIN payers py ON py.id = p.payer_id
LEFT JOIN LATERAL (SELECT id, status FROM jobs WHERE appointment_id = a.id ORDER BY created_at DESC LIMIT 1) j ON true
LEFT JOIN LATERAL (SELECT id, delivery_status FROM patient_notices WHERE appointment_id = a.id AND kind = 'cost_estimate' ORDER BY created_at DESC LIMIT 1) n ON true
LEFT JOIN LATERAL (SELECT id, delivery_status FROM patient_notices WHERE appointment_id = a.id AND kind = 'reminder' ORDER BY created_at DESC LIMIT 1) rm ON true`

func (s *Store) ListAppointments(ctx context.Context, from, to time.Time) ([]Appointment, error) {
	rows, _ := s.Pool.Query(ctx, appointmentSelect+` WHERE a.scheduled_at >= $1 AND a.scheduled_at < $2 ORDER BY a.scheduled_at`, from, to)
	return pgx.CollectRows(rows, pgx.RowToStructByName[Appointment])
}

func (s *Store) GetAppointment(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	rows, _ := s.Pool.Query(ctx, appointmentSelect+` WHERE a.id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Appointment])
}

func (s *Store) CreateAppointment(ctx context.Context, practiceID, patientID uuid.UUID, at time.Time, codes []string, notes string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `INSERT INTO appointments (practice_id, patient_id, scheduled_at, procedure_codes, notes) VALUES ($1,$2,$3,$4,NULLIF($5,'')) RETURNING id`,
		practiceID, patientID, at, codes, notes).Scan(&id)
	return id, err
}

func (s *Store) DeleteAppointment(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM appointments WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM jobs WHERE appointment_id=$1)`, id)
	return err
}

// BulkInsertJobsForAppointments inserts QUEUED jobs linked to appointments. Same COPY path as BulkInsertJobs.
func (s *Store) BulkInsertJobsForAppointments(ctx context.Context, tx pgx.Tx, batchID *uuid.UUID, appts []Appointment) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, len(appts))
	rows := make([][]any, len(appts))
	for i, a := range appts {
		ids[i] = uuid.New()
		rows[i] = []any{ids[i], batchID, a.PatientID, a.PayerID, a.ID}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"jobs"}, []string{"id", "batch_id", "patient_id", "payer_id", "appointment_id"}, pgx.CopyFromRows(rows))
	return ids, err
}

func (s *Store) JobAppointmentID(ctx context.Context, jobID uuid.UUID) (*uuid.UUID, error) {
	var id *uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT appointment_id FROM jobs WHERE id=$1`, jobID).Scan(&id)
	return id, err
}

func (s *Store) UpdatePatientContact(ctx context.Context, id uuid.UUID, email, phone string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE patients SET email=NULLIF($2,''), phone=NULLIF($3,'') WHERE id=$1`, id, email, phone)
	return err
}

// ---------- patient notices ----------

type PatientNotice struct {
	ID             uuid.UUID       `db:"id" json:"id"`
	JobID          uuid.UUID       `db:"job_id" json:"jobId"`
	AppointmentID  *uuid.UUID      `db:"appointment_id" json:"appointmentId,omitempty"`
	PatientID      uuid.UUID       `db:"patient_id" json:"patientId"`
	Kind           string          `db:"kind" json:"kind"`
	Channel        string          `db:"channel" json:"channel"`
	Recipient      *string         `db:"recipient" json:"recipient,omitempty"`
	Subject        string          `db:"subject" json:"subject"`
	BodyText       string          `db:"body_text" json:"bodyText"`
	BodyHTML       string          `db:"body_html" json:"bodyHtml"`
	Estimate       json.RawMessage `db:"estimate" json:"estimate"`
	DeliveryStatus string          `db:"delivery_status" json:"deliveryStatus"`
	DeliveryError  *string         `db:"delivery_error" json:"deliveryError,omitempty"`
	ProviderID     *string         `db:"provider_id" json:"providerId,omitempty"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	SentAt         *time.Time      `db:"sent_at" json:"sentAt,omitempty"`
	// joined
	PatientName   string     `db:"patient_name" json:"patientName"`
	PayerName     string     `db:"payer_name" json:"payerName"`
	ScheduledAt   *time.Time `db:"scheduled_at" json:"scheduledAt,omitempty"`
	JobStatus     string     `db:"job_status" json:"jobStatus"`
}

const noticeSelect = `
SELECT n.id, n.job_id, n.appointment_id, n.patient_id, n.kind, n.channel, n.recipient, n.subject, n.body_text, n.body_html, n.estimate,
       n.delivery_status, n.delivery_error, n.provider_id, n.created_at, n.sent_at,
       p.name AS patient_name, py.name AS payer_name, a.scheduled_at, j.status::text AS job_status
FROM patient_notices n
JOIN patients p ON p.id = n.patient_id
JOIN payers py ON py.id = p.payer_id
JOIN jobs j ON j.id = n.job_id
LEFT JOIN appointments a ON a.id = n.appointment_id`

func (s *Store) ListNotices(ctx context.Context, limit int) ([]PatientNotice, error) {
	rows, _ := s.Pool.Query(ctx, noticeSelect+` ORDER BY n.created_at DESC LIMIT $1`, limit)
	return pgx.CollectRows(rows, pgx.RowToStructByName[PatientNotice])
}

func (s *Store) GetNotice(ctx context.Context, id uuid.UUID) (*PatientNotice, error) {
	rows, _ := s.Pool.Query(ctx, noticeSelect+` WHERE n.id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[PatientNotice])
}

func (s *Store) NoticeForJob(ctx context.Context, jobID uuid.UUID) (*PatientNotice, error) {
	rows, _ := s.Pool.Query(ctx, noticeSelect+` WHERE n.job_id=$1 AND n.kind='cost_estimate' ORDER BY n.created_at DESC LIMIT 1`, jobID)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[PatientNotice])
}

func (s *Store) NoticeForAppointment(ctx context.Context, apptID uuid.UUID, kind string) (*PatientNotice, error) {
	rows, _ := s.Pool.Query(ctx, noticeSelect+` WHERE n.appointment_id=$1 AND n.kind=$2 ORDER BY n.created_at DESC LIMIT 1`, apptID, kind)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[PatientNotice])
}

type NewNotice struct {
	JobID         uuid.UUID
	AppointmentID *uuid.UUID
	PatientID     uuid.UUID
	Kind          string // cost_estimate | reminder
	Channel       string
	Recipient     *string
	Subject       string
	BodyText      string
	BodyHTML      string
	Estimate      json.RawMessage
	Status        string
}

func (s *Store) InsertNotice(ctx context.Context, n NewNotice) (uuid.UUID, error) {
	var id uuid.UUID
	if n.Kind == "" {
		n.Kind = "cost_estimate"
	}
	err := s.Pool.QueryRow(ctx, `INSERT INTO patient_notices (job_id, appointment_id, patient_id, kind, channel, recipient, subject, body_text, body_html, estimate, delivery_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		n.JobID, n.AppointmentID, n.PatientID, n.Kind, n.Channel, n.Recipient, n.Subject, n.BodyText, n.BodyHTML, n.Estimate, n.Status).Scan(&id)
	return id, err
}

func (s *Store) MarkNoticeDelivery(ctx context.Context, id uuid.UUID, status string, providerID, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE patient_notices SET delivery_status=$2, provider_id=NULLIF($3,''), delivery_error=NULLIF($4,''), sent_at=CASE WHEN $2='sent' THEN now() ELSE sent_at END WHERE id=$1`,
		id, status, providerID, errMsg)
	return err
}

func (s *Store) NoticeStats(ctx context.Context) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT delivery_status, count(*) FROM patient_notices WHERE kind='cost_estimate' GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, nil
}

func (s *Store) SetNoticeRecipient(ctx context.Context, id uuid.UUID, to string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE patient_notices SET recipient=$2 WHERE id=$1`, id, to)
	return err
}

// DeleteNotices removes notice records by id (patient_notices has no dependents).
func (s *Store) DeleteNotices(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	ct, err := s.Pool.Exec(ctx, `DELETE FROM patient_notices WHERE id = ANY($1)`, ids)
	return int(ct.RowsAffected()), err
}
