package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 40
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

// ---------- practices / payers / patients ----------

func (s *Store) DefaultPractice(ctx context.Context) (*Practice, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, name FROM practices ORDER BY created_at LIMIT 1`)
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Practice])
	if err != nil {
		return nil, fmt.Errorf("default practice: %w", err)
	}
	return p, nil
}

func (s *Store) ListPayers(ctx context.Context) ([]Payer, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, name, stedi_payer_id, supports_realtime, service_type_code, plan_type FROM payers ORDER BY name`)
	return pgx.CollectRows(rows, pgx.RowToStructByName[Payer])
}

func (s *Store) GetPayer(ctx context.Context, id uuid.UUID) (*Payer, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, name, stedi_payer_id, supports_realtime, service_type_code, plan_type FROM payers WHERE id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Payer])
}

func (s *Store) FindPayerByStediID(ctx context.Context, stediID string) (*Payer, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, name, stedi_payer_id, supports_realtime, service_type_code, plan_type FROM payers WHERE stedi_payer_id=$1 LIMIT 1`, stediID)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Payer])
}

const patientSelect = `
SELECT p.id, p.practice_id, p.name, p.dob, p.member_id, p.payer_id, p.created_at, p.email, p.phone,
       py.name AS payer_name, py.stedi_payer_id,
       lj.completed_at AS last_verified, lj.status::text AS last_status
FROM patients p
JOIN payers py ON py.id = p.payer_id
LEFT JOIN LATERAL (
    SELECT j.completed_at, j.status FROM jobs j
    WHERE j.patient_id = p.id AND j.status IN ('VERIFIED','COVERAGE_GAP_FLAGGED','MANUAL_RESOLVED','NEEDS_MANUAL_REVIEW')
    ORDER BY j.created_at DESC LIMIT 1
) lj ON true`

func (s *Store) ListPatients(ctx context.Context, search string, limit int) ([]Patient, error) {
	q := patientSelect + ` WHERE ($1 = '' OR p.name ILIKE '%' || $1 || '%' OR p.member_id ILIKE '%' || $1 || '%') ORDER BY p.created_at DESC, p.name LIMIT $2`
	rows, _ := s.Pool.Query(ctx, q, search, limit)
	return pgx.CollectRows(rows, pgx.RowToStructByName[Patient])
}

func (s *Store) GetPatient(ctx context.Context, id uuid.UUID) (*Patient, error) {
	rows, _ := s.Pool.Query(ctx, patientSelect+` WHERE p.id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Patient])
}

func (s *Store) AllPatientIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id FROM patients ORDER BY created_at`)
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

type NewPatient struct {
	PracticeID uuid.UUID
	Name       string
	DOB        time.Time
	MemberID   string
	PayerID    uuid.UUID
	Email      string // optional
	Phone      string // optional
}

func (s *Store) CreatePatient(ctx context.Context, np NewPatient) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO patients (practice_id, name, dob, member_id, payer_id, email, phone) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,'')) RETURNING id`,
		np.PracticeID, np.Name, np.DOB, np.MemberID, np.PayerID, np.Email, np.Phone).Scan(&id)
	return id, err
}

// UpsertPatient returns an existing patient with the same member_id+payer, or creates one.
func (s *Store) UpsertPatient(ctx context.Context, np NewPatient) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx,
		`SELECT id FROM patients WHERE member_id=$1 AND payer_id=$2 AND practice_id=$3 LIMIT 1`,
		np.MemberID, np.PayerID, np.PracticeID).Scan(&id)
	if err == nil {
		if np.Email != "" || np.Phone != "" {
			if _, uerr := s.Pool.Exec(ctx, `UPDATE patients SET email=COALESCE(NULLIF($2,''),email), phone=COALESCE(NULLIF($3,''),phone) WHERE id=$1`, id, np.Email, np.Phone); uerr != nil {
				return uuid.Nil, uerr
			}
		}
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	return s.CreatePatient(ctx, np)
}

// ---------- batches ----------

func (s *Store) CreateBatch(ctx context.Context, tx pgx.Tx, practiceID uuid.UUID, label, kind string, total int) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO batches (practice_id, label, kind, total_jobs, queued) VALUES ($1,$2,$3,$4,$4) RETURNING id`,
		practiceID, label, kind, total).Scan(&id)
	return id, err
}

// BulkInsertJobs inserts N QUEUED jobs with COPY and returns their IDs in input order.
func (s *Store) BulkInsertJobs(ctx context.Context, tx pgx.Tx, batchID *uuid.UUID, patients []struct{ PatientID, PayerID uuid.UUID }) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, len(patients))
	rows := make([][]any, len(patients))
	for i, p := range patients {
		ids[i] = uuid.New()
		rows[i] = []any{ids[i], batchID, p.PatientID, p.PayerID}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"jobs"}, []string{"id", "batch_id", "patient_id", "payer_id"}, pgx.CopyFromRows(rows))
	return ids, err
}

const batchSelect = `SELECT id, practice_id, label, kind, total_jobs, queued, processing, verified, gap_flagged, retrying, needs_review, manual_resolved, created_at, updated_at FROM batches`

func (s *Store) GetBatch(ctx context.Context, id uuid.UUID) (*Batch, error) {
	rows, _ := s.Pool.Query(ctx, batchSelect+` WHERE id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Batch])
}

func (s *Store) GetBatches(ctx context.Context, ids []uuid.UUID) ([]Batch, error) {
	rows, _ := s.Pool.Query(ctx, batchSelect+` WHERE id = ANY($1)`, ids)
	return pgx.CollectRows(rows, pgx.RowToStructByName[Batch])
}

func (s *Store) ListBatches(ctx context.Context, limit int) ([]Batch, error) {
	rows, _ := s.Pool.Query(ctx, batchSelect+` ORDER BY created_at DESC LIMIT $1`, limit)
	return pgx.CollectRows(rows, pgx.RowToStructByName[Batch])
}

// ---------- jobs ----------

const jobSelect = `
SELECT j.id, j.batch_id, j.patient_id, j.payer_id, j.status, j.attempt_count, j.next_attempt_at,
       j.raw_response, j.normalized_brief, j.review_reason, j.error_code, j.error_message,
       j.resolved_by, j.resolution_note, j.started_at, j.completed_at, j.resolved_at, j.created_at, j.updated_at,
       p.name AS patient_name, p.dob AS patient_dob, p.member_id, py.name AS payer_name, py.stedi_payer_id
FROM jobs j
JOIN patients p ON p.id = j.patient_id
JOIN payers py ON py.id = j.payer_id`

func (s *Store) GetJob(ctx context.Context, id uuid.UUID) (*Job, error) {
	rows, _ := s.Pool.Query(ctx, jobSelect+` WHERE j.id=$1`, id)
	return pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[Job])
}

type JobFilter struct {
	Status  string
	BatchID *uuid.UUID
	Payer   *uuid.UUID
	Search  string
	Limit   int
	Offset  int
}

func (s *Store) ListJobs(ctx context.Context, f JobFilter) ([]Job, int, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	where := ` WHERE ($1 = '' OR j.status::text = $1)
	  AND ($2::uuid IS NULL OR j.batch_id = $2)
	  AND ($3::uuid IS NULL OR j.payer_id = $3)
	  AND ($4 = '' OR p.name ILIKE '%' || $4 || '%' OR p.member_id ILIKE '%' || $4 || '%')`
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM jobs j JOIN patients p ON p.id=j.patient_id`+where,
		f.Status, f.BatchID, f.Payer, f.Search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, _ := s.Pool.Query(ctx, jobSelect+where+` ORDER BY j.created_at DESC LIMIT $5 OFFSET $6`,
		f.Status, f.BatchID, f.Payer, f.Search, f.Limit, f.Offset)
	jobs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Job])
	return jobs, total, err
}

func (s *Store) ListAttempts(ctx context.Context, jobID uuid.UUID) ([]JobAttempt, error) {
	rows, _ := s.Pool.Query(ctx, `SELECT id, job_id, attempt_number, status, error_code, error_message, attempted_at FROM job_attempts WHERE job_id=$1 ORDER BY attempt_number`, jobID)
	return pgx.CollectRows(rows, pgx.RowToStructByName[JobAttempt])
}

// DeleteJobs removes verification records (and their attempts + any pre-visit notice)
// and keeps each parent batch's denormalized counters consistent. Deleting a job that
// still has a River task queued is safe: the worker already treats a missing job row
// as "already gone" (see queue.VerifyWorker.Work).
func (s *Store) DeleteJobs(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	deleted := 0
	for _, id := range ids {
		ok, err := deleteJobTx(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if ok {
			deleted++
		}
	}
	return deleted, tx.Commit(ctx)
}

// deleteJobTx removes one job (its attempts, any pre-visit notice, and the job row
// itself) and keeps the parent batch's denormalized counters consistent. Returns
// false, nil if the job no longer exists (already deleted). Shared by DeleteJobs and
// DeletePatients so a patient delete cascades through the exact same accounting.
func deleteJobTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var status JobStatus
	var batchID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT status, batch_id FROM jobs WHERE id=$1 FOR UPDATE`, id).Scan(&status, &batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM patient_notices WHERE job_id=$1`, id); err != nil {
		return false, fmt.Errorf("delete notices for job %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM job_attempts WHERE job_id=$1`, id); err != nil {
		return false, fmt.Errorf("delete attempts for job %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, id); err != nil {
		return false, fmt.Errorf("delete job %s: %w", id, err)
	}
	if batchID != nil {
		if col, ok := batchColumn[status]; ok {
			q := fmt.Sprintf(`UPDATE batches SET %s = GREATEST(%s - 1, 0), total_jobs = GREATEST(total_jobs - 1, 0), updated_at = now() WHERE id=$1`, col, col)
			if _, err := tx.Exec(ctx, q, *batchID); err != nil {
				return false, fmt.Errorf("adjust batch counters for %s: %w", *batchID, err)
			}
		}
	}
	return true, nil
}

// DeletePatients removes patients along with every job (and its attempts/notices,
// with batch counters adjusted), appointment, and notice that references them.
// A destructive, cascading delete — the caller is expected to have confirmed with
// the user, since this cannot be undone.
func (s *Store) DeletePatients(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	deleted := 0
	for _, pid := range ids {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id=$1)`, pid).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			continue
		}
		rows, err := tx.Query(ctx, `SELECT id FROM jobs WHERE patient_id=$1`, pid)
		if err != nil {
			return 0, fmt.Errorf("list jobs for patient %s: %w", pid, err)
		}
		jobIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return 0, err
		}
		for _, jid := range jobIDs {
			if _, err := deleteJobTx(ctx, tx, jid); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM appointments WHERE patient_id=$1`, pid); err != nil {
			return 0, fmt.Errorf("delete appointments for patient %s: %w", pid, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM patients WHERE id=$1`, pid); err != nil {
			return 0, fmt.Errorf("delete patient %s: %w", pid, err)
		}
		deleted++
	}
	return deleted, tx.Commit(ctx)
}

// ---------- state machine ----------

// transition atomically moves a job to a new status and keeps the parent batch's
// denormalized counters in sync. Returns the previous status.
func transition(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, to JobStatus) (JobStatus, error) {
	var old JobStatus
	var batchID *uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE jobs j SET status=$2, updated_at=now()
		FROM (SELECT status AS old_status FROM jobs WHERE id=$1 FOR UPDATE) o
		WHERE j.id=$1
		RETURNING o.old_status, j.batch_id`, jobID, to).Scan(&old, &batchID)
	if err != nil {
		return "", fmt.Errorf("transition %s: %w", to, err)
	}
	if batchID != nil && old != to {
		oldCol, ok1 := batchColumn[old]
		newCol, ok2 := batchColumn[to]
		if !ok1 || !ok2 {
			return "", fmt.Errorf("unknown status column %s -> %s", old, to)
		}
		q := fmt.Sprintf(`UPDATE batches SET %s = GREATEST(%s - 1, 0), %s = %s + 1, updated_at = now() WHERE id=$1`, oldCol, oldCol, newCol, newCol)
		if _, err := tx.Exec(ctx, q, *batchID); err != nil {
			return "", fmt.Errorf("batch counters: %w", err)
		}
	}
	return old, nil
}

// MarkProcessing: QUEUED/RETRYING -> PROCESSING, increments attempt_count, records an attempt row.
func (s *Store) MarkProcessing(ctx context.Context, jobID uuid.UUID) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := transition(ctx, tx, jobID, StatusProcessing); err != nil {
		return 0, err
	}
	var attempt int
	if err := tx.QueryRow(ctx, `UPDATE jobs SET attempt_count = attempt_count + 1, started_at = COALESCE(started_at, now()), next_attempt_at = NULL WHERE id=$1 RETURNING attempt_count`, jobID).Scan(&attempt); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO job_attempts (job_id, attempt_number, status) VALUES ($1,$2,'PROCESSING')`, jobID, attempt); err != nil {
		return 0, err
	}
	return attempt, tx.Commit(ctx)
}

// MarkResult: PROCESSING -> VERIFIED | COVERAGE_GAP_FLAGGED
func (s *Store) MarkResult(ctx context.Context, jobID uuid.UUID, status JobStatus, raw, brief json.RawMessage) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := transition(ctx, tx, jobID, status); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET raw_response=$2, normalized_brief=$3, completed_at=now(), error_code=NULL, error_message=NULL WHERE id=$1`, jobID, raw, brief); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET status=$2 WHERE job_id=$1 AND attempt_number=(SELECT attempt_count FROM jobs WHERE id=$1)`, jobID, string(status)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkRetrying: PROCESSING -> RETRYING with the scheduled next attempt time.
func (s *Store) MarkRetrying(ctx context.Context, jobID uuid.UUID, nextAt time.Time, code, msg string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := transition(ctx, tx, jobID, StatusRetrying); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET next_attempt_at=$2, error_code=$3, error_message=$4 WHERE id=$1`, jobID, nextAt, code, msg); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET status='FAILED_TRANSIENT', error_code=$2, error_message=$3 WHERE job_id=$1 AND attempt_number=(SELECT attempt_count FROM jobs WHERE id=$1)`, jobID, code, msg); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkNeedsReview: * -> NEEDS_MANUAL_REVIEW with a reason.
func (s *Store) MarkNeedsReview(ctx context.Context, jobID uuid.UUID, reason ReviewReason, raw json.RawMessage, code, msg string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := transition(ctx, tx, jobID, StatusNeedsReview); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET review_reason=$2, raw_response=COALESCE($3, raw_response), error_code=$4, error_message=$5, completed_at=now(), next_attempt_at=NULL WHERE id=$1`,
		jobID, reason, raw, nullIfEmpty(code), nullIfEmpty(msg)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET status='NEEDS_MANUAL_REVIEW', error_code=$2, error_message=$3 WHERE job_id=$1 AND attempt_number=(SELECT attempt_count FROM jobs WHERE id=$1)`, jobID, nullIfEmpty(code), nullIfEmpty(msg)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkResolved: NEEDS_MANUAL_REVIEW -> MANUAL_RESOLVED (staff action).
func (s *Store) MarkResolved(ctx context.Context, jobID uuid.UUID, resolvedBy, note string, brief json.RawMessage) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := transition(ctx, tx, jobID, StatusManualResolve)
	if err != nil {
		return err
	}
	if old != StatusNeedsReview {
		return fmt.Errorf("job is %s, only NEEDS_MANUAL_REVIEW jobs can be resolved", old)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET resolved_by=$2, resolution_note=$3, resolved_at=now(), normalized_brief=COALESCE($4, normalized_brief) WHERE id=$1`, jobID, resolvedBy, note, brief); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RequeueFromReview: NEEDS_MANUAL_REVIEW -> QUEUED (staff asks the system to try again).
func (s *Store) RequeueFromReview(ctx context.Context, jobID uuid.UUID) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := transition(ctx, tx, jobID, StatusQueued)
	if err != nil {
		return err
	}
	if old != StatusNeedsReview {
		return fmt.Errorf("job is %s, only NEEDS_MANUAL_REVIEW jobs can be re-queued", old)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET review_reason=NULL, attempt_count=0, error_code=NULL, error_message=NULL, completed_at=NULL WHERE id=$1`, jobID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------- stats ----------

type Stats struct {
	Total          int                `json:"total"`
	Verified       int                `json:"verified"`
	GapFlagged     int                `json:"gapFlagged"`
	InProgress     int                `json:"inProgress"`
	NeedsReview    int                `json:"needsReview"`
	ManualResolved int                `json:"manualResolved"`
	SuccessRate    float64            `json:"successRate"`
	AvgProcessSecs float64            `json:"avgProcessingSeconds"`
	ByStatus       map[string]int     `json:"byStatus"`
	ByPayer        []PayerCount       `json:"byPayer"`
	Last7Days      []DayCount         `json:"last7Days"`
	ReviewReasons  map[string]int     `json:"reviewReasons"`
}

type PayerCount struct {
	Payer string `json:"payer"`
	Count int    `json:"count"`
}

type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

func (s *Store) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{ByStatus: map[string]int{}, ReviewReasons: map[string]int{}}
	rows, err := s.Pool.Query(ctx, `SELECT status::text, count(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		st.ByStatus[k] = n
		st.Total += n
	}
	rows.Close()
	st.Verified = st.ByStatus["VERIFIED"]
	st.GapFlagged = st.ByStatus["COVERAGE_GAP_FLAGGED"]
	st.NeedsReview = st.ByStatus["NEEDS_MANUAL_REVIEW"]
	st.ManualResolved = st.ByStatus["MANUAL_RESOLVED"]
	st.InProgress = st.ByStatus["QUEUED"] + st.ByStatus["PROCESSING"] + st.ByStatus["RETRYING"]
	done := st.Verified + st.GapFlagged + st.NeedsReview + st.ManualResolved
	if done > 0 {
		st.SuccessRate = float64(st.Verified+st.GapFlagged+st.ManualResolved) / float64(done) * 100
	}
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - created_at))),0) FROM jobs WHERE completed_at IS NOT NULL AND created_at > now() - interval '1 day'`).Scan(&st.AvgProcessSecs)

	prows, err := s.Pool.Query(ctx, `SELECT py.name, count(*) FROM jobs j JOIN payers py ON py.id=j.payer_id GROUP BY py.name ORDER BY count(*) DESC`)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var pc PayerCount
		if err := prows.Scan(&pc.Payer, &pc.Count); err != nil {
			return nil, err
		}
		st.ByPayer = append(st.ByPayer, pc)
	}
	prows.Close()

	drows, err := s.Pool.Query(ctx, `SELECT to_char(d, 'YYYY-MM-DD'), COALESCE(c,0) FROM generate_series(current_date - 6, current_date, '1 day') d
		LEFT JOIN (SELECT created_at::date dd, count(*) c FROM jobs GROUP BY 1) x ON x.dd = d ORDER BY d`)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var dc DayCount
		if err := drows.Scan(&dc.Day, &dc.Count); err != nil {
			return nil, err
		}
		st.Last7Days = append(st.Last7Days, dc)
	}
	drows.Close()

	rrows, err := s.Pool.Query(ctx, `SELECT review_reason::text, count(*) FROM jobs WHERE status='NEEDS_MANUAL_REVIEW' AND review_reason IS NOT NULL GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var k string
		var n int
		if err := rrows.Scan(&k, &n); err != nil {
			return nil, err
		}
		st.ReviewReasons[k] = n
	}
	rrows.Close()
	return st, nil
}
