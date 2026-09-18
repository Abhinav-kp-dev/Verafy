package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"coveragecheck/internal/db"
	"coveragecheck/internal/estimate"
	"coveragecheck/internal/llm"
	"coveragecheck/internal/queue"
)

// Nightly returns the pre-visit runner wired to this server's batch creation.
func (s *Server) Nightly() *queue.NightlyRunner {
	return &queue.NightlyRunner{
		Store: s.store, Queue: s.queue, Log: s.log,
		CreateBatch: func(ctx context.Context, label string, appts []db.Appointment) (uuid.UUID, int, error) {
			id, err := s.newAppointmentBatchTx(ctx, label, appts)
			return id, len(appts), err
		},
	}
}

// newAppointmentBatchTx mirrors newBatchTx but links each job to its appointment.
func (s *Server) newAppointmentBatchTx(ctx context.Context, label string, appts []db.Appointment) (uuid.UUID, error) {
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	batchID, err := s.store.CreateBatch(ctx, tx, s.practice.ID, label, "previsit", len(appts))
	if err != nil {
		return uuid.Nil, err
	}
	ids, err := s.store.BulkInsertJobsForAppointments(ctx, tx, &batchID, appts)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.queue.Enqueue(ctx, tx, ids, false, queue.PriorityBatch); err != nil {
		return uuid.Nil, err
	}
	return batchID, tx.Commit(ctx)
}

func (s *Server) registerPrevisitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fees", s.listFees)
	mux.HandleFunc("POST /api/fees", s.upsertFee)
	mux.HandleFunc("GET /api/appointments", s.listAppointments)
	mux.HandleFunc("POST /api/appointments", s.createAppointment)
	mux.HandleFunc("DELETE /api/appointments/{id}", s.deleteAppointment)
	mux.HandleFunc("POST /api/appointments/{id}/verify", s.verifyAppointment)
	mux.HandleFunc("POST /api/previsit/run", s.runPrevisit)
	mux.HandleFunc("GET /api/notices", s.listNotices)
	mux.HandleFunc("DELETE /api/notices", s.deleteNotices)
	mux.HandleFunc("GET /api/notices/{id}", s.getNotice)
	mux.HandleFunc("GET /api/notices/{id}/preview", s.previewNotice)
	mux.HandleFunc("POST /api/notices/{id}/send", s.sendNotice)
	mux.HandleFunc("GET /api/verifications/{id}/estimate", s.estimateForJob)
	mux.HandleFunc("POST /api/patients/{id}/contact", s.updatePatientContact)
}

// ---------- fee schedule ----------

func (s *Server) listFees(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListFeeSchedule(r.Context(), s.practice.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, items)
}

func (s *Server) upsertFee(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code        string `json:"code"`
		Description string `json:"description"`
		FeeCents    int64  `json:"feeCents"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Code) == "" || in.FeeCents < 0 {
		writeErr(w, 400, "code and a non-negative feeCents are required")
		return
	}
	if err := s.store.UpsertFee(r.Context(), s.practice.ID, strings.ToUpper(strings.TrimSpace(in.Code)), in.Description, in.FeeCents); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- appointments ----------

func dayBounds(r *http.Request) (time.Time, time.Time) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	q := r.URL.Query().Get("date")
	var day time.Time
	if q == "tomorrow" || q == "" {
		day = time.Now().In(loc).Add(24 * time.Hour)
	} else if q == "today" {
		day = time.Now().In(loc)
	} else if t, err := time.ParseInLocation("2006-01-02", q, loc); err == nil {
		day = t
	} else {
		day = time.Now().In(loc).Add(24 * time.Hour)
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	if r.URL.Query().Get("range") == "week" {
		return start, start.Add(7 * 24 * time.Hour)
	}
	return start, start.Add(24 * time.Hour)
}

func (s *Server) listAppointments(w http.ResponseWriter, r *http.Request) {
	from, to := dayBounds(r)
	if r.URL.Query().Get("range") == "all" {
		from, to = time.Now().Add(-365*24*time.Hour), time.Now().Add(365*24*time.Hour)
	}
	appts, err := s.store.ListAppointments(r.Context(), from, to)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"appointments": appts, "from": from, "to": to})
}

func (s *Server) createAppointment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PatientID      string   `json:"patientId"`
		ScheduledAt    string   `json:"scheduledAt"`
		ProcedureCodes []string `json:"procedureCodes"`
		Notes          string   `json:"notes"`
		AutoRun        *bool    `json:"autoRun"` // default true: verify + cost notice immediately
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	pid, err := uuid.Parse(in.PatientID)
	if err != nil {
		writeErr(w, 400, "patientId required")
		return
	}
	at, err := time.Parse(time.RFC3339, in.ScheduledAt)
	if err != nil {
		writeErr(w, 400, "scheduledAt must be RFC3339")
		return
	}
	if len(in.ProcedureCodes) == 0 {
		writeErr(w, 400, "at least one procedure code is required")
		return
	}
	codes := make([]string, 0, len(in.ProcedureCodes))
	for _, c := range in.ProcedureCodes {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			codes = append(codes, c)
		}
	}
	id, err := s.store.CreateAppointment(r.Context(), s.practice.ID, pid, at, codes, in.Notes)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	a, err := s.store.GetAppointment(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// Auto-run: the moment an appointment exists, verify coverage, estimate the patient's
	// share, and prepare the cost notice — no one has to remember to click "run".
	queued := false
	if in.AutoRun == nil || *in.AutoRun {
		if _, err := s.newAppointmentBatchTx(r.Context(), fmt.Sprintf("Pre-visit — %s", a.PatientName), []db.Appointment{*a}); err != nil {
			s.log.Warn("appointment created but auto-run failed", "err", err)
		} else {
			queued = true
		}
	}
	writeJSON(w, 201, map[string]any{"appointment": a, "queued": queued})
}

func (s *Server) deleteAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	if err := s.store.DeleteAppointment(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// verifyAppointment: run verification + notice for one appointment now.
func (s *Server) verifyAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	a, err := s.store.GetAppointment(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "appointment not found")
		return
	}
	batchID, err := s.newAppointmentBatchTx(r.Context(), fmt.Sprintf("Pre-visit — %s", a.PatientName), []db.Appointment{*a})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"batchId": batchID, "queued": 1})
}

// runPrevisit: the on-demand version of the nightly job. ?date=tomorrow|today|YYYY-MM-DD
func (s *Server) runPrevisit(w http.ResponseWriter, r *http.Request) {
	from, _ := dayBounds(r)
	res, err := s.Nightly().Run(r.Context(), from)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if res.Verified == 0 && res.Reminders == 0 {
		writeJSON(w, 200, map[string]any{"queued": 0, "reminders": 0, "message": "Nothing to do for " + from.Format("Mon Jan 2") + " — every appointment already has its cost notice and reminder"})
		return
	}
	var b *db.Batch
	if res.BatchID != uuid.Nil {
		b, _ = s.store.GetBatch(r.Context(), res.BatchID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": res.Verified, "reminders": res.Reminders, "batch": b, "day": from.Format("2006-01-02")})
}

// ---------- notices ----------

func (s *Server) listNotices(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNotices(r.Context(), qInt(r, "limit", 100))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	stats, _ := s.store.NoticeStats(r.Context())
	writeJSON(w, 200, map[string]any{"notices": items, "stats": stats})
}

func (s *Server) deleteNotices(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, 400, "ids is required and must be non-empty")
		return
	}
	ids := make([]uuid.UUID, 0, len(in.IDs))
	for _, s := range in.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			writeErr(w, 400, "invalid id: "+s)
			return
		}
		ids = append(ids, id)
	}
	n, err := s.store.DeleteNotices(r.Context(), ids)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"deleted": n})
}

func (s *Server) getNotice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	n, err := s.store.GetNotice(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, n)
}

// previewNotice renders the HTML body directly (for an iframe).
func (s *Server) previewNotice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	n, err := s.store.GetNotice(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head><body style="margin:0;padding:20px;background:#F8FAFC">%s</body></html>`, n.Subject, n.BodyHTML)
}

// estimateForJob: ad-hoc estimate for a verified job + a list of CDT codes (?codes=D1110,D2740).
func (s *Server) estimateForJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "job not found")
		return
	}
	codes := []string{}
	for _, c := range strings.Split(r.URL.Query().Get("codes"), ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			codes = append(codes, c)
		}
	}
	if len(codes) == 0 {
		writeErr(w, 400, "codes query param required, e.g. ?codes=D1110,D2740")
		return
	}
	var brief llm.Brief
	if len(job.NormalizedBrief) > 0 {
		_ = json.Unmarshal(job.NormalizedBrief, &brief)
	}
	fees, err := s.store.FeesForCodes(r.Context(), s.practice.ID, codes)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	procs := make([]estimate.ProcedureInput, 0, len(codes))
	for _, c := range codes {
		if f, ok := fees[c]; ok {
			procs = append(procs, estimate.ProcedureInput{Code: c, Description: f.Description, FeeCents: f.FeeCents})
		} else {
			procs = append(procs, estimate.ProcedureInput{Code: c, Description: c + " (no fee on file)", FeeCents: 0})
		}
	}
	writeJSON(w, 200, estimate.Compute(brief.Facts, procs))
}

func (s *Server) updatePatientContact(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	var in struct {
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if err := s.store.UpdatePatientContact(r.Context(), id, strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// sendNotice delivers (or re-delivers) an already-generated notice to the patient's
// CURRENT email address using the configured provider.
func (s *Server) sendNotice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	n, err := s.store.GetNotice(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "notice not found")
		return
	}
	if s.sender == nil || !s.sender.Configured() {
		writeErr(w, 400, "No email provider configured. Set SMTP_HOST/SMTP_USER/SMTP_PASS (e.g. Gmail with an App Password) or RESEND_API_KEY in backend/.env and restart.")
		return
	}
	p, err := s.store.GetPatient(r.Context(), n.PatientID)
	if err != nil {
		writeErr(w, 404, "patient not found")
		return
	}
	if p.Email == nil || *p.Email == "" {
		writeErr(w, 400, p.Name+" has no email on file. Add one on the Patients page, then send again.")
		return
	}
	to := *p.Email
	pid, sendErr := s.sender.Send(r.Context(), to, n.Subject, n.BodyText, n.BodyHTML)
	if sendErr != nil {
		_ = s.store.MarkNoticeDelivery(r.Context(), id, "failed", "", sendErr.Error())
		_ = s.store.SetNoticeRecipient(r.Context(), id, to)
		writeErr(w, 502, "Delivery failed: "+sendErr.Error())
		return
	}
	_ = s.store.SetNoticeRecipient(r.Context(), id, to)
	_ = s.store.MarkNoticeDelivery(r.Context(), id, "sent", pid, "")
	out, _ := s.store.GetNotice(r.Context(), id)
	s.log.Info("notice sent on demand", "notice", id.String()[:8], "to", to, "kind", n.Kind)
	writeJSON(w, 200, out)
}
