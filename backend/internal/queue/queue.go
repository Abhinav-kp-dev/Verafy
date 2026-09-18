// Package queue runs the verification state machine on top of River (Postgres-backed jobs).
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"coveragecheck/internal/config"
	"coveragecheck/internal/db"
	"coveragecheck/internal/llm"
	"coveragecheck/internal/normalize"
	"coveragecheck/internal/ratelimit"
	"coveragecheck/internal/stedi"
	"coveragecheck/internal/voiceagent"
)

// VerifyArgs is the River job payload: just a pointer to our domain job.
type VerifyArgs struct {
	JobID     uuid.UUID `json:"job_id"`
	Synthetic bool      `json:"synthetic"`
}

func (VerifyArgs) Kind() string { return "verify_eligibility" }

type Deps struct {
	Cfg     *config.Config
	Store   *db.Store
	Stedi   stedi.Client
	LLM     *llm.Generator
	Limiter *ratelimit.PayerLimiter
	Voice   voiceagent.Client
	Salvage *voiceagent.TranscriptExtractor // best-effort facts recovery when a call ends without a tool call
	Log     *slog.Logger
}

type VerifyWorker struct {
	river.WorkerDefaults[VerifyArgs]
	d *Deps
	q *Queue
}

func (w *VerifyWorker) Timeout(*river.Job[VerifyArgs]) time.Duration { return 90 * time.Second }

// NextRetry: exponential backoff with deterministic jitter (so the time we show in
// the UI is exactly the time River will actually retry).
func (w *VerifyWorker) NextRetry(job *river.Job[VerifyArgs]) time.Time {
	return time.Now().Add(backoff(w.d.Cfg.RetryBase, job.Attempt, job.Args.JobID))
}

func backoff(base time.Duration, attempt int, id uuid.UUID) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(base) * math.Pow(2, float64(attempt-1))
	h := fnv.New32a()
	h.Write(id[:])
	jitter := float64(h.Sum32()%1000) / 1000 * 0.25 * d
	return time.Duration(d + jitter)
}

func (w *VerifyWorker) Work(ctx context.Context, rj *river.Job[VerifyArgs]) error {
	d := w.d
	jobID := rj.Args.JobID
	log := d.Log.With("job", jobID.String()[:8], "attempt", rj.Attempt)

	job, err := d.Store.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn("job row missing; dropping")
			return nil
		}
		return err
	}
	switch job.Status {
	case db.StatusVerified, db.StatusGapFlagged, db.StatusNeedsReview, db.StatusManualResolve:
		log.Info("job already terminal; skipping", "status", job.Status)
		return nil
	case db.StatusCallInProgress:
		log.Info("voice call already in progress; skipping", "call", job.CallID)
		return nil
	}
	payer, err := d.Store.GetPayer(ctx, job.PayerID)
	if err != nil {
		return err
	}
	patient, err := d.Store.GetPatient(ctx, job.PatientID)
	if err != nil {
		return err
	}

	if !payer.SupportsRealtime {
		if payer.VoiceEnabled() && d.Voice != nil {
			// No EDI path, but we know the payer's phone line: the AI agent makes the call.
			attempt, err := d.Store.MarkProcessing(ctx, jobID)
			if err != nil {
				return err
			}
			log.Info("payer has no real-time eligibility -> AI voice call", "to", *payer.ProviderServicesPhone)
			return w.dispatchVoiceCall(ctx, log, jobID, attempt, payer, patient)
		}
		log.Info("payer does not support real-time eligibility -> manual review")
		return d.Store.MarkNeedsReview(ctx, jobID, db.ReasonUnsupportedPayer, nil, "unsupported_payer",
			fmt.Sprintf("%s does not support electronic (270/271) eligibility checks and has no phone line on file. Verify by phone or payer portal.", payer.Name))
	}

	attempt, err := d.Store.MarkProcessing(ctx, jobID)
	if err != nil {
		return err
	}

	release, err := d.Limiter.Acquire(ctx, payer.StediPayerID)
	if err != nil {
		return err
	}
	defer release()

	first, last := splitName(patient.Name)
	req := stedi.Request{
		PayerID:    payer.StediPayerID,
		Provider:   stedi.Provider{Name: stedi.Name{Organization: d.Cfg.ProviderName}, NPI: d.Cfg.ProviderNPI},
		Subscriber: stedi.Subscriber{Name: stedi.Name{Person: &stedi.PersonName{FirstName: first, LastName: last}}, MemberID: patient.MemberID, DateOfBirth: patient.DOB.Format("2006-01-02")},
		Encounter:  stedi.Encounter{Services: []stedi.Service{{Value: payer.ServiceTypeCode, System: "STC"}}},
	}
	callCtx := ctx
	if rj.Args.Synthetic {
		callCtx = stedi.WithSynthetic(ctx)
	}
	resp, raw, callErr := d.Stedi.Check(callCtx, req)

	// ---- transport / HTTP level errors ----
	if callErr != nil {
		var ce *stedi.CallError
		if !errors.As(callErr, &ce) {
			ce = &stedi.CallError{Kind: stedi.ErrKindTransient, Code: "unknown", Message: callErr.Error()}
		}
		return w.handleFailure(ctx, log, rj, jobID, attempt, ce.Kind, ce.Code, ce.Message, raw)
	}

	// ---- payer AAA rejections inside a 200 ----
	if len(resp.Errors) > 0 {
		e := resp.Errors[0]
		msg := e.Description
		if e.FollowupAction != "" {
			msg += " — " + e.FollowupAction
		}
		return w.handleFailure(ctx, log, rj, jobID, attempt, stedi.ClassifyAAA(e.Code), "AAA_"+e.Code, msg, raw)
	}

	// ---- success: deterministic extraction -> validated brief ----
	facts := normalize.Extract(resp)
	brief := d.LLM.Generate(ctx, facts)
	briefJSON, _ := json.Marshal(brief)
	status := db.StatusVerified
	if facts.HasGap {
		status = db.StatusGapFlagged
	}
	log.Info("verified", "status", status, "eligibility", facts.EligibilityStatus, "brief_source", brief.Source)
	if err := d.Store.MarkResult(ctx, jobID, status, raw, briefJSON); err != nil {
		return err
	}
	// Chain the pre-visit cost notice for appointment-linked jobs. Best effort: a failure
	// here must never undo a verification that already succeeded.
	if w.q != nil {
		if apptID, _ := d.Store.JobAppointmentID(ctx, jobID); apptID != nil {
			if err := w.q.EnqueueNotice(ctx, jobID); err != nil {
				log.Warn("could not enqueue cost notice", "err", err)
			}
		}
	}
	return nil
}

func (w *VerifyWorker) handleFailure(ctx context.Context, log *slog.Logger, rj *river.Job[VerifyArgs], jobID uuid.UUID, attempt int, kind stedi.ErrorKind, code, msg string, raw []byte) error {
	d := w.d
	switch kind {
	case stedi.ErrKindTransient:
		if attempt >= d.Cfg.MaxAttempts || rj.Attempt >= rj.MaxAttempts {
			log.Warn("retry budget exhausted -> manual review", "code", code)
			return d.Store.MarkNeedsReview(ctx, jobID, db.ReasonRetryExhausted, raw, code,
				fmt.Sprintf("Payer did not respond after %d attempts (%s). Last error: %s", attempt, code, msg))
		}
		nextAt := w.NextRetry(rj)
		log.Warn("transient failure -> retrying", "code", code, "next", nextAt.Format(time.Kitchen))
		if err := d.Store.MarkRetrying(ctx, jobID, nextAt, code, msg); err != nil {
			return err
		}
		return fmt.Errorf("transient %s: %s", code, msg) // River schedules the retry at NextRetry()
	case stedi.ErrKindRejected:
		reason := db.ReasonPayerRejected
		switch code {
		case "AAA_72", "AAA_73", "AAA_75":
			reason = db.ReasonAmbiguousMatch
		}
		log.Warn("payer rejected -> manual review", "code", code, "reason", reason)
		return d.Store.MarkNeedsReview(ctx, jobID, reason, raw, code, msg)
	default:
		log.Error("malformed response -> manual review", "code", code)
		return d.Store.MarkNeedsReview(ctx, jobID, db.ReasonMalformedResponse, raw, code, msg)
	}
}

func splitName(full string) (string, string) {
	first, last := full, ""
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == ' ' {
			first, last = full[:i], full[i+1:]
			break
		}
	}
	return first, last
}

// ---------- client / enqueue ----------

type Queue struct {
	Client *river.Client[pgx.Tx]
	deps   *Deps
}

func New(pool *pgxpool.Pool, deps *Deps, nd *NoticeDeps) (*Queue, error) {
	q := &Queue{deps: deps}
	workers := river.NewWorkers()
	river.AddWorker(workers, &VerifyWorker{d: deps, q: q})
	river.AddWorker(workers, &VoiceTimeoutWorker{d: deps})
	if nd != nil {
		river.AddWorker(workers, &NoticeWorker{d: deps, nd: nd})
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:      map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: deps.Cfg.MaxWorkers}},
		Workers:     workers,
		MaxAttempts: deps.Cfg.MaxAttempts,
		Logger:      deps.Log,
	})
	if err != nil {
		return nil, err
	}
	q.Client = client
	return q, nil
}

func (q *Queue) Start(ctx context.Context) error { return q.Client.Start(ctx) }
func (q *Queue) Stop(ctx context.Context) error  { return q.Client.Stop(ctx) }

// Priorities: interactive single checks must never wait behind a bulk run.
const (
	PriorityInteractive = 1
	PriorityBatch       = 2
	PrioritySynthetic   = 4
)

// Enqueue inserts River jobs for the given domain job IDs in chunks of 500.
func (q *Queue) Enqueue(ctx context.Context, tx pgx.Tx, jobIDs []uuid.UUID, synthetic bool, priority int) error {
	const chunk = 500
	for i := 0; i < len(jobIDs); i += chunk {
		end := min(i+chunk, len(jobIDs))
		params := make([]river.InsertManyParams, 0, end-i)
		for _, id := range jobIDs[i:end] {
			params = append(params, river.InsertManyParams{
				Args:       VerifyArgs{JobID: id, Synthetic: synthetic},
				InsertOpts: &river.InsertOpts{MaxAttempts: q.deps.Cfg.MaxAttempts, Priority: priority},
			})
		}
		if _, err := q.Client.InsertManyFastTx(ctx, tx, params); err != nil {
			return fmt.Errorf("enqueue chunk %d: %w", i/chunk, err)
		}
	}
	return nil
}
