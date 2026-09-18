package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"coveragecheck/internal/db"
	"coveragecheck/internal/estimate"
	"coveragecheck/internal/llm"
	"coveragecheck/internal/notify"
)

// NoticeArgs: second job kind, chained after a successful verification that was
// run for a scheduled appointment.
type NoticeArgs struct {
	JobID      uuid.UUID `json:"job_id"`
	NoticeKind string    `json:"notice_kind,omitempty"` // "" | cost_estimate | reminder
}

func (NoticeArgs) Kind() string { return "send_cost_notice" }

type NoticeDeps struct {
	Sender        notify.Sender
	PracticePhone string
}

type NoticeWorker struct {
	river.WorkerDefaults[NoticeArgs]
	d  *Deps
	nd *NoticeDeps
}

func (w *NoticeWorker) Timeout(*river.Job[NoticeArgs]) time.Duration { return 60 * time.Second }

func (w *NoticeWorker) Work(ctx context.Context, rj *river.Job[NoticeArgs]) error {
	d := w.d
	log := d.Log.With("notice_for_job", rj.Args.JobID.String()[:8])

	job, err := d.Store.GetJob(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if job.Status != db.StatusVerified && job.Status != db.StatusGapFlagged {
		log.Info("job not in a verified state; no notice", "status", job.Status)
		return nil
	}
	kind := rj.Args.NoticeKind
	if kind == "" {
		kind = "cost_estimate"
	}
	apptID, err := d.Store.JobAppointmentID(ctx, job.ID)
	if err != nil || apptID == nil {
		return nil
	}
	// idempotent: one notice per appointment per kind
	if _, err := d.Store.NoticeForAppointment(ctx, *apptID, kind); err == nil {
		log.Info("notice already exists; skipping", "kind", kind)
		return nil
	}
	appt, err := d.Store.GetAppointment(ctx, *apptID)
	if err != nil {
		return err
	}
	practice, err := d.Store.DefaultPractice(ctx)
	if err != nil {
		return err
	}
	if kind == "reminder" {
		return w.workReminder(ctx, log, job, appt, practice, apptID)
	}

	// ---- estimate from the already-validated facts ----
	var brief llm.Brief
	if len(job.NormalizedBrief) > 0 {
		_ = json.Unmarshal(job.NormalizedBrief, &brief)
	}
	fees, err := d.Store.FeesForCodes(ctx, practice.ID, appt.ProcedureCodes)
	if err != nil {
		return err
	}
	procs := make([]estimate.ProcedureInput, 0, len(appt.ProcedureCodes))
	for _, code := range appt.ProcedureCodes {
		fee, ok := fees[code]
		if !ok {
			procs = append(procs, estimate.ProcedureInput{Code: code, Description: code + " (no fee on file)", FeeCents: 0})
			continue
		}
		procs = append(procs, estimate.ProcedureInput{Code: code, Description: fee.Description, FeeCents: fee.FeeCents})
	}
	est := estimate.Compute(brief.Facts, procs)
	estJSON, _ := json.Marshal(est)

	rendered := notify.Render(notify.RenderInput{
		PatientName: appt.PatientName, PracticeName: practice.Name, PracticePhone: w.nd.PracticePhone,
		ScheduledAt: appt.ScheduledAt, PlanName: est.PlanName, PayerName: appt.PayerName, Estimate: est,
	})

	return w.persistAndDeliver(ctx, log, "cost_estimate", job, appt, apptID, rendered, estJSON, est.PatientPaysCents)
}

// workReminder sends the day-before reminder using the estimate already sent in the
// cost notice, so the two emails never disagree. Falls back to a fresh estimate only
// if no cost notice exists yet.
func (w *NoticeWorker) workReminder(ctx context.Context, log *slog.Logger, job *db.Job, appt *db.Appointment, practice *db.Practice, apptID *uuid.UUID) error {
	d := w.d
	var est estimate.Result
	if prior, err := d.Store.NoticeForAppointment(ctx, *apptID, "cost_estimate"); err == nil && len(prior.Estimate) > 0 {
		_ = json.Unmarshal(prior.Estimate, &est)
	} else {
		var brief llm.Brief
		if len(job.NormalizedBrief) > 0 {
			_ = json.Unmarshal(job.NormalizedBrief, &brief)
		}
		fees, err := d.Store.FeesForCodes(ctx, practice.ID, appt.ProcedureCodes)
		if err != nil {
			return err
		}
		procs := make([]estimate.ProcedureInput, 0, len(appt.ProcedureCodes))
		for _, code := range appt.ProcedureCodes {
			if fee, ok := fees[code]; ok {
				procs = append(procs, estimate.ProcedureInput{Code: code, Description: fee.Description, FeeCents: fee.FeeCents})
			} else {
				procs = append(procs, estimate.ProcedureInput{Code: code, Description: code + " (no fee on file)"})
			}
		}
		est = estimate.Compute(brief.Facts, procs)
	}
	estJSON, _ := json.Marshal(est)
	rendered := notify.RenderReminder(notify.RenderInput{
		PatientName: appt.PatientName, PracticeName: practice.Name, PracticePhone: w.nd.PracticePhone,
		ScheduledAt: appt.ScheduledAt, PlanName: est.PlanName, PayerName: appt.PayerName, Estimate: est,
	})
	return w.persistAndDeliver(ctx, log, "reminder", job, appt, apptID, rendered, estJSON, est.PatientPaysCents)
}

func (w *NoticeWorker) persistAndDeliver(ctx context.Context, log *slog.Logger, kind string, job *db.Job, appt *db.Appointment, apptID *uuid.UUID, rendered notify.Rendered, estJSON []byte, patientPays int64) error {
	d := w.d
	// ---- log first, always ----
	status := "logged"
	if appt.PatientEmail == nil || *appt.PatientEmail == "" {
		status = "skipped_no_recipient"
	} else if w.nd.Sender == nil || !w.nd.Sender.Configured() {
		status = "skipped_no_provider"
	}
	noticeID, err := d.Store.InsertNotice(ctx, db.NewNotice{
		JobID: job.ID, AppointmentID: apptID, PatientID: job.PatientID, Kind: kind, Channel: "email", Recipient: appt.PatientEmail,
		Subject: rendered.Subject, BodyText: rendered.Text, BodyHTML: rendered.HTML, Estimate: estJSON, Status: status,
	})
	if err != nil {
		return err
	}
	log.Info("notice generated", "kind", kind, "patient", appt.PatientName, "patient_pays", patientPays, "status", status)

	// ---- optional delivery ----
	if status != "logged" {
		return nil
	}
	pid, sendErr := w.nd.Sender.Send(ctx, *appt.PatientEmail, rendered.Subject, rendered.Text, rendered.HTML)
	if sendErr != nil {
		log.Warn("delivery failed", "err", sendErr)
		_ = d.Store.MarkNoticeDelivery(ctx, noticeID, "failed", "", sendErr.Error())
		return nil // never retry the whole job for a delivery failure; the notice is logged
	}
	return d.Store.MarkNoticeDelivery(ctx, noticeID, "sent", pid, "")
}

// EnqueueNotice schedules the notice job for a verification. Safe to call for any job:
// the worker itself checks whether the job was for an appointment.
func (q *Queue) EnqueueNotice(ctx context.Context, jobID uuid.UUID) error {
	_, err := q.Client.Insert(ctx, NoticeArgs{JobID: jobID, NoticeKind: "cost_estimate"}, &river.InsertOpts{MaxAttempts: 3, Priority: PriorityInteractive})
	return err
}

// EnqueueReminder schedules the day-before reminder for an already-verified appointment job.
func (q *Queue) EnqueueReminder(ctx context.Context, jobID uuid.UUID) error {
	_, err := q.Client.Insert(ctx, NoticeArgs{JobID: jobID, NoticeKind: "reminder"}, &river.InsertOpts{MaxAttempts: 3, Priority: PriorityInteractive})
	return err
}

// ---------- nightly scheduler ----------

// RunNightly enqueues verification for every appointment on the given calendar day (local time).
// Called both by the ticker (real nightly behaviour) and by the on-demand API endpoint (demo).
type NightlyRunner struct {
	Store *db.Store
	Queue *Queue
	Log   interface{ Info(string, ...any) }
	// CreateBatch is provided by the api package so batch creation stays in one place.
	CreateBatch func(ctx context.Context, label string, appts []db.Appointment) (uuid.UUID, int, error)
}

// RunResult summarises one pre-visit run.
type RunResult struct {
	BatchID   uuid.UUID
	Verified  int // appointments queued for verification + cost notice
	Reminders int // appointments that already had a cost notice and got a reminder
}

func (n *NightlyRunner) Run(ctx context.Context, day time.Time) (RunResult, error) {
	loc := day.Location()
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	appts, err := n.Store.ListAppointments(ctx, start, end)
	if err != nil {
		return RunResult{}, err
	}
	var res RunResult
	todo := appts[:0:0]
	for _, a := range appts {
		status := ""
		if a.JobStatus != nil {
			status = *a.JobStatus
		}
		switch {
		case status == string(db.StatusQueued), status == string(db.StatusProcessing), status == string(db.StatusRetrying):
			// already in flight — never double-queue
		case a.NoticeID != nil && a.ReminderID == nil && a.JobID != nil:
			// has its cost estimate: send the day-before reminder
			if err := n.Queue.EnqueueReminder(ctx, *a.JobID); err == nil {
				res.Reminders++
			}
		case a.NoticeID == nil && (status == "" || status == string(db.StatusVerified) || status == string(db.StatusGapFlagged)):
			// never checked, or verified but the notice is missing: verify + cost notice
			todo = append(todo, a)
		default:
			// NEEDS_MANUAL_REVIEW / MANUAL_RESOLVED without a notice: staff owns it; re-running
			// nightly would only duplicate review items. "Run now" in the UI re-checks on demand.
		}
	}
	if len(todo) > 0 {
		label := fmt.Sprintf("Pre-visit run — %s (%d appointments)", start.Format("Mon Jan 2"), len(todo))
		id, count, err := n.CreateBatch(ctx, label, todo)
		if err != nil {
			return res, err
		}
		res.BatchID, res.Verified = id, count
	}
	n.Log.Info("pre-visit run", "day", start.Format("2006-01-02"), "verify", res.Verified, "reminders", res.Reminders)
	return res, nil
}

// StartTicker runs the nightly job for "tomorrow" once per day at the configured hour.
func (n *NightlyRunner) StartTicker(ctx context.Context, hour int, loc *time.Location) {
	go func() {
		for {
			now := time.Now().In(loc)
			next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, loc)
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				tomorrow := time.Now().In(loc).Add(24 * time.Hour)
				if _, err := n.Run(ctx, tomorrow); err != nil {
					n.Log.Info("nightly run failed", "err", err.Error())
				}
			}
		}
	}()
}
