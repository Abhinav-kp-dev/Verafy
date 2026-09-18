package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"coveragecheck/internal/db"
	"coveragecheck/internal/notify"
	"coveragecheck/internal/voiceagent"
)

// dispatchEmailVerification is the unsupported-payer branch of VerifyWorker.Work when
// the payer has no phone line but does have a provider-services email: send a link to
// the hosted verification form, park the job in EMAIL_PENDING, arm the watchdog, and
// return. The form's own submission (POST /api/webhooks/email/facts/{token}) finishes it.
func (w *VerifyWorker) dispatchEmailVerification(ctx context.Context, log *slog.Logger, jobID uuid.UUID, payer *db.Payer, patient *db.Patient) error {
	d := w.d
	token, err := newFormToken()
	if err != nil {
		return err
	}
	formURL := strings.TrimSuffix(d.Cfg.EmailFormBaseURL, "/") + "/verify-form/" + token
	rendered := notify.RenderVerificationRequest(notify.VerificationRequestInput{
		PracticeName: d.Cfg.ProviderName,
		PayerName:    payer.Name,
		PatientName:  patient.Name,
		PatientDOB:   patient.DOB.Format("January 2, 2006"),
		MemberID:     patient.MemberID,
		ProviderNPI:  d.Cfg.ProviderNPI,
		FormURL:      formURL,
	})
	if _, err := d.EmailSender.Send(ctx, *payer.ProviderServicesEmail, rendered.Subject, rendered.Text, rendered.HTML); err != nil {
		log.Warn("verification email dispatch failed -> manual review", "err", err)
		return d.Store.MarkNeedsReview(ctx, jobID, db.ReasonEmailNotAnswered, nil, "email_dispatch_failed",
			fmt.Sprintf("Could not send the verification email to %s: %v", payer.Name, err))
	}
	if err := d.Store.MarkEmailPending(ctx, jobID, token); err != nil {
		return err
	}
	timeout := d.Cfg.EmailResponseTimeout
	if timeout <= 0 {
		timeout = 72 * time.Hour
	}
	if _, err := w.q.Client.Insert(ctx, EmailTimeoutArgs{JobID: jobID, Token: token},
		&river.InsertOpts{ScheduledAt: time.Now().Add(timeout), MaxAttempts: 3, Priority: PriorityInteractive}); err != nil {
		log.Error("could not arm email watchdog", "err", err)
	}
	log.Info("verification email sent", "to", *payer.ProviderServicesEmail, "timeout", timeout)
	return nil
}

func newFormToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------- completion (called by the hosted form's submit handler) ----------

// CompleteEmailWithFacts validates a submitted form, runs it through the same brief
// pipeline every other channel gets, and finalizes the job. Idempotent: a job that is
// no longer EMAIL_PENDING (already submitted, or timed out) is left untouched.
func (q *Queue) CompleteEmailWithFacts(ctx context.Context, token string, ex voiceagent.Extracted) error {
	d := q.deps
	job, err := d.Store.GetJobByEmailToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("unknown or expired verification link")
		}
		return err
	}
	log := d.Log.With("job", job.ID.String()[:8], "token", token[:8])
	if job.Status != db.StatusEmailPending {
		return fmt.Errorf("this verification request has already been resolved")
	}
	if verr := voiceagent.Validate(ex); verr != nil {
		// Unlike a phone call, a form can simply be corrected and resubmitted — a typo
		// shouldn't permanently burn the link. Leave the job in EMAIL_PENDING; only the
		// watchdog timeout (no valid submission within the response window) escalates
		// this to Manual Review.
		log.Info("email form submission invalid; left pending for correction", "reason", verr)
		return fmt.Errorf("some required information was missing or invalid: %s", verr.Error())
	}
	return q.finishWithFacts(ctx, job, token, db.StatusEmailPending, db.SourceEmailForm, ex, "", false)
}

// ---------- watchdog ----------

// EmailTimeoutArgs is scheduled at dispatch time; it fires once the response window has elapsed.
type EmailTimeoutArgs struct {
	JobID uuid.UUID `json:"job_id"`
	Token string    `json:"token"`
}

func (EmailTimeoutArgs) Kind() string { return "email_verification_timeout" }

type EmailTimeoutWorker struct {
	river.WorkerDefaults[EmailTimeoutArgs]
	d *Deps
}

func (w *EmailTimeoutWorker) Timeout(*river.Job[EmailTimeoutArgs]) time.Duration {
	return 30 * time.Second
}

func (w *EmailTimeoutWorker) Work(ctx context.Context, rj *river.Job[EmailTimeoutArgs]) error {
	job, err := w.d.Store.GetJob(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	// Only the request we armed for counts: a re-run that sent a newer email has its own watchdog.
	if job.Status != db.StatusEmailPending || job.EmailToken == nil || *job.EmailToken != rj.Args.Token {
		return nil
	}
	w.d.Log.Warn("verification email timed out -> manual review", "job", job.ID.String()[:8])
	return w.d.Store.MarkChannelFailed(ctx, job.ID, db.StatusEmailPending, db.ReasonEmailNotAnswered, "email_timeout",
		fmt.Sprintf("No response to the verification email after %s.", w.d.Cfg.EmailResponseTimeout.Truncate(time.Hour)), "")
}
