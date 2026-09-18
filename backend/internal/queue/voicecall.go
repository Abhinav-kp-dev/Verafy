package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"coveragecheck/internal/db"
	"coveragecheck/internal/voiceagent"
)

// dispatchVoiceCall is the unsupported-payer branch of VerifyWorker.Work when the
// payer has a provider-services line: place the call, park the job in
// CALL_IN_PROGRESS, arm the watchdog, and return. The webhook (or mock) finishes it.
func (w *VerifyWorker) dispatchVoiceCall(ctx context.Context, log *slog.Logger, jobID uuid.UUID, attempt int, payer *db.Payer, patient *db.Patient) error {
	d := w.d
	req := voiceagent.CallRequest{
		JobID:           jobID.String(),
		ToNumber:        *payer.ProviderServicesPhone,
		PatientName:     patient.Name,
		PatientDOB:      patient.DOB.Format("2006-01-02"),
		MemberID:        patient.MemberID,
		PayerName:       payer.Name,
		ProviderName:    d.Cfg.ProviderName,
		ProviderNPI:     d.Cfg.ProviderNPI,
		ServiceTypeCode: payer.ServiceTypeCode,
	}
	if payer.IVRNotes != nil {
		req.IVRNotes = *payer.IVRNotes
	}
	callID, err := d.Voice.PlaceCall(ctx, req)
	if err != nil {
		// Could not even dial: this is the voice equivalent of a transport failure.
		log.Warn("voice call dispatch failed -> manual review", "err", err)
		return d.Store.MarkNeedsReview(ctx, jobID, db.ReasonVoiceCallFailed, nil, "voice_dispatch_failed",
			fmt.Sprintf("Could not place the verification call to %s: %v", payer.Name, err))
	}
	if err := d.Store.MarkCallInProgress(ctx, jobID, callID); err != nil {
		return err
	}
	timeout := d.Cfg.VoiceCallTimeout
	if timeout <= 0 {
		timeout = voiceagent.DefaultCallTimeout
	}
	if _, err := w.q.Client.Insert(ctx, VoiceTimeoutArgs{JobID: jobID, CallID: callID},
		&river.InsertOpts{ScheduledAt: time.Now().Add(timeout), MaxAttempts: 3, Priority: PriorityInteractive}); err != nil {
		log.Error("could not arm voice watchdog", "err", err)
	}
	log.Info("voice call placed", "call", callID, "to", req.ToNumber, "mode", d.Voice.Mode(), "attempt", attempt)
	return nil
}

// ---------- completion (called by the Retell webhook or the mock) ----------

var _ voiceagent.Completer = (*Queue)(nil)

// CompleteWithFacts validates what the agent collected, runs it through the same
// brief pipeline as an EDI result, and finalizes the job. Idempotent: a job that is
// no longer CALL_IN_PROGRESS is left untouched.
func (q *Queue) CompleteWithFacts(ctx context.Context, callID string, ex voiceagent.Extracted, transcript string) error {
	d := q.deps
	job, err := d.Store.GetJobByCallID(ctx, callID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			d.Log.Warn("voice facts for unknown call", "call", callID)
			return nil
		}
		return err
	}
	log := d.Log.With("job", job.ID.String()[:8], "call", callID)
	if job.Status != db.StatusCallInProgress {
		log.Info("voice facts arrived for a finished job; keeping transcript only", "status", job.Status)
		return d.Store.AttachCallTranscript(ctx, callID, transcript)
	}
	if verr := voiceagent.Validate(ex); verr != nil {
		log.Warn("voice facts rejected -> manual review", "reason", verr)
		return d.Store.MarkVoiceFailed(ctx, job.ID, db.ReasonVoiceCallFailed, "incomplete_facts",
			"The agent reached the payer but the collected data did not pass validation: "+verr.Error(), transcript)
	}
	return q.finishWithFacts(ctx, job, callID, ex, transcript, false)
}

// finishWithFacts runs already-validated facts through the same brief pipeline an
// EDI result gets, and finalizes the job. partial marks a result salvaged from a
// transcript after an early hangup rather than a clean tool-call submission — it's
// still real, confirmed data, just tagged so staff know the call didn't finish.
func (q *Queue) finishWithFacts(ctx context.Context, job *db.Job, callID string, ex voiceagent.Extracted, transcript string, partial bool) error {
	d := q.deps
	log := d.Log.With("job", job.ID.String()[:8], "call", callID)
	facts := voiceagent.ToFacts(ex, job.PayerName)
	if partial {
		facts.Flags = append(facts.Flags, "call_ended_early_partial_data")
		facts.Messages = append(facts.Messages, "Call ended before completion; these details were confirmed up to that point.")
	}
	brief := d.LLM.Generate(ctx, facts)
	briefJSON, _ := json.Marshal(brief)
	raw, _ := json.Marshal(map[string]any{"source": db.SourceVoiceCall, "callId": callID, "submitted": ex, "partial": partial})
	status := db.StatusVerified
	if facts.HasGap {
		status = db.StatusGapFlagged
	}
	if err := d.Store.MarkVoiceResult(ctx, job.ID, status, raw, briefJSON, transcript); err != nil {
		return err
	}
	log.Info("verified by voice call", "status", status, "eligibility", facts.EligibilityStatus, "brief_source", brief.Source, "partial", partial)
	if apptID, _ := d.Store.JobAppointmentID(ctx, job.ID); apptID != nil {
		if err := q.EnqueueNotice(ctx, job.ID); err != nil {
			log.Warn("could not enqueue cost notice", "err", err)
		}
	}
	return nil
}

// CompleteWithFailure handles a call that ended without the agent submitting facts
// (early hangup, dropped call, refused rep, etc). Before giving up, it tries to
// salvage whatever was actually confirmed from the transcript — ending a call early
// should surface partial progress, not silently discard it. Only if nothing usable
// was said (or salvage is unavailable/fails validation) does this fall through to
// manual review, same as before.
func (q *Queue) CompleteWithFailure(ctx context.Context, callID string, outcome voiceagent.Outcome, detail, transcript string) error {
	d := q.deps
	job, err := d.Store.GetJobByCallID(ctx, callID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if job.Status != db.StatusCallInProgress {
		return d.Store.AttachCallTranscript(ctx, callID, transcript)
	}
	log := d.Log.With("job", job.ID.String()[:8], "call", callID)

	if d.Salvage != nil && d.Salvage.Enabled() && strings.TrimSpace(transcript) != "" {
		if salvaged, serr := d.Salvage.Extract(ctx, transcript); serr == nil {
			if verr := voiceagent.Validate(salvaged); verr == nil {
				log.Info("call ended early but salvaged usable facts from transcript", "outcome", outcome)
				return q.finishWithFacts(ctx, job, callID, salvaged, transcript, true)
			} else {
				log.Info("call ended early; transcript salvage had nothing usable", "reason", verr)
			}
		} else {
			log.Warn("transcript salvage failed", "err", serr)
		}
	}

	log.Warn("voice call failed -> manual review", "outcome", outcome)
	return d.Store.MarkVoiceFailed(ctx, job.ID, db.ReasonVoiceCallFailed, string(outcome), detail, transcript)
}

// ---------- watchdog ----------

// VoiceTimeoutArgs is scheduled at dispatch time; it fires once the call should be over.
type VoiceTimeoutArgs struct {
	JobID  uuid.UUID `json:"job_id"`
	CallID string    `json:"call_id"`
}

func (VoiceTimeoutArgs) Kind() string { return "voice_call_timeout" }

type VoiceTimeoutWorker struct {
	river.WorkerDefaults[VoiceTimeoutArgs]
	d *Deps
}

func (w *VoiceTimeoutWorker) Timeout(*river.Job[VoiceTimeoutArgs]) time.Duration {
	return 30 * time.Second
}

func (w *VoiceTimeoutWorker) Work(ctx context.Context, rj *river.Job[VoiceTimeoutArgs]) error {
	job, err := w.d.Store.GetJob(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	// Only the call we armed for counts: a re-run that placed a newer call has its own watchdog.
	if job.Status != db.StatusCallInProgress || job.CallID == nil || *job.CallID != rj.Args.CallID {
		return nil
	}
	w.d.Log.Warn("voice call watchdog fired -> manual review", "job", job.ID.String()[:8], "call", rj.Args.CallID)
	return w.d.Store.MarkVoiceFailed(ctx, job.ID, db.ReasonCallTimeout, "call_timeout",
		fmt.Sprintf("No result from the payer call after %s. The call may have dropped or is still on hold.", w.d.Cfg.VoiceCallTimeout.Truncate(time.Second)), "")
}
