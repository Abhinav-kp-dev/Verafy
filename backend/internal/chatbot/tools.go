package chatbot

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"coveragecheck/internal/db"
)

// Tools are deliberately read-only: the assistant can look things up, never
// delete/resend/create anything. That stays a human action in the UI.

func toolDeclarations() []functionDecl {
	obj := func(props map[string]any, required ...string) any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	return []functionDecl{
		{Name: "search_patients", Description: "Find patients by name or member ID. Use this whenever the user asks about a patient by name.",
			Parameters: obj(map[string]any{"query": str("Patient name or member ID, or part of one")}, "query")},
		{Name: "get_patient_detail", Description: "Get full detail for one patient by their exact patient ID (from search_patients), including recent verification jobs.",
			Parameters: obj(map[string]any{"patientId": str("The patient's UUID")}, "patientId")},
		{Name: "get_verification_detail", Description: "Get the full status, coverage brief, and any error for one verification job by its ID.",
			Parameters: obj(map[string]any{"jobId": str("The verification job's UUID")}, "jobId")},
		{Name: "list_manual_review", Description: "List everything currently sitting in the Manual Review queue, with the reason each case needs a human."},
		{Name: "get_dashboard_stats", Description: "Get overall practice stats: total verifications, success rate, breakdown by status, average processing time."},
		{Name: "list_recent_notices", Description: "List recent pre-visit cost-estimate or reminder emails, optionally filtered by patient name.",
			Parameters: obj(map[string]any{"patientName": str("Optional: filter to one patient's name")})},
		{Name: "get_schedule", Description: "List appointments for today or tomorrow, with their verification and cost-notice status.",
			Parameters: obj(map[string]any{"day": str(`Either "today" or "tomorrow"`)}, "day")},
	}
}

// dispatch executes one tool call and returns a JSON-able result map. Never returns
// an error to the model for "not found" cases — it returns an explicit empty/absent
// result so the model reports that honestly instead of guessing.
func dispatch(ctx context.Context, store *db.Store, name string, args map[string]any) map[string]any {
	argStr := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }

	switch name {
	case "search_patients":
		q := argStr("query")
		patients, err := store.ListPatients(ctx, q, 8)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		out := make([]map[string]any, 0, len(patients))
		for _, p := range patients {
			out = append(out, map[string]any{
				"patientId": p.ID.String(), "name": p.Name, "memberId": p.MemberID,
				"payer": p.PayerName, "lastStatus": strPtrOr(p.LastStatus, "never verified"),
				"hasEmail": p.Email != nil && *p.Email != "",
			})
		}
		if len(out) == 0 {
			return map[string]any{"results": []any{}, "note": "no patients matched"}
		}
		return map[string]any{"results": out}

	case "get_patient_detail":
		id, err := uuid.Parse(argStr("patientId"))
		if err != nil {
			return map[string]any{"error": "invalid patientId"}
		}
		p, err := store.GetPatient(ctx, id)
		if err != nil {
			return map[string]any{"error": "patient not found"}
		}
		jobs, _, _ := store.ListJobs(ctx, db.JobFilter{Search: p.MemberID, Limit: 5})
		recent := make([]map[string]any, 0, len(jobs))
		for _, j := range jobs {
			if j.PatientID != p.ID {
				continue
			}
			recent = append(recent, map[string]any{"jobId": j.ID.String(), "status": string(j.Status), "createdAt": j.CreatedAt.Format("2006-01-02 15:04")})
		}
		return map[string]any{
			"name": p.Name, "dob": p.DOB.Format("2006-01-02"), "memberId": p.MemberID, "payer": p.PayerName,
			"email": strPtrOr(p.Email, "(none on file)"), "phone": strPtrOr(p.Phone, "(none on file)"),
			"lastStatus": strPtrOr(p.LastStatus, "never verified"), "recentVerifications": recent,
		}

	case "get_verification_detail":
		id, err := uuid.Parse(argStr("jobId"))
		if err != nil {
			return map[string]any{"error": "invalid jobId"}
		}
		j, err := store.GetJob(ctx, id)
		if err != nil {
			return map[string]any{"error": "verification not found"}
		}
		out := map[string]any{
			"patient": j.PatientName, "payer": j.PayerName, "status": string(j.Status),
			"attempts": j.AttemptCount, "createdAt": j.CreatedAt.Format("2006-01-02 15:04"),
		}
		if j.ReviewReason != nil {
			out["reviewReason"] = string(*j.ReviewReason)
		}
		if j.ErrorMessage != nil {
			out["error"] = *j.ErrorMessage
		}
		if len(j.NormalizedBrief) > 0 {
			out["coverageBriefRaw"] = string(j.NormalizedBrief)
		}
		return out

	case "list_manual_review":
		jobs, total, err := store.ListJobs(ctx, db.JobFilter{Status: string(db.StatusNeedsReview), Limit: 15})
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		out := make([]map[string]any, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, map[string]any{
				"patient": j.PatientName, "payer": j.PayerName,
				"reason": reasonOr(j.ReviewReason), "jobId": j.ID.String(),
			})
		}
		return map[string]any{"total": total, "cases": out}

	case "get_dashboard_stats":
		st, err := store.Stats(ctx)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{
			"total": st.Total, "verified": st.Verified, "coverageGapFlagged": st.GapFlagged,
			"inProgress": st.InProgress, "needsManualReview": st.NeedsReview,
			"successRatePct": st.SuccessRate, "avgProcessingSeconds": st.AvgProcessSecs,
			"byStatus": st.ByStatus,
		}

	case "list_recent_notices":
		notices, err := store.ListNotices(ctx, 100)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		filter := strings.ToLower(argStr("patientName"))
		out := make([]map[string]any, 0, 10)
		for _, n := range notices {
			if filter != "" && !strings.Contains(strings.ToLower(n.PatientName), filter) {
				continue
			}
			out = append(out, map[string]any{
				"patient": n.PatientName, "kind": n.Kind, "subject": n.Subject,
				"deliveryStatus": n.DeliveryStatus, "recipient": strPtrOr(n.Recipient, "(no email on file)"),
				"createdAt": n.CreatedAt.Format("2006-01-02 15:04"),
			})
			if len(out) >= 10 {
				break
			}
		}
		return map[string]any{"notices": out}

	case "get_schedule":
		loc, err := time.LoadLocation("Asia/Kolkata")
		if err != nil {
			loc = time.UTC
		}
		day := time.Now().In(loc)
		if strings.EqualFold(argStr("day"), "tomorrow") {
			day = day.Add(24 * time.Hour)
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		appts, err := store.ListAppointments(ctx, start, start.Add(24*time.Hour))
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		out := make([]map[string]any, 0, len(appts))
		for _, a := range appts {
			out = append(out, map[string]any{
				"time": a.ScheduledAt.In(loc).Format("15:04"), "patient": a.PatientName, "payer": a.PayerName,
				"procedures": a.ProcedureCodes, "verificationStatus": strPtrOr(a.JobStatus, "not run yet"),
				"costNoticeStatus": strPtrOr(a.NoticeStatus, "not sent yet"),
			})
		}
		return map[string]any{"day": start.Format("2006-01-02"), "appointments": out}

	default:
		return map[string]any{"error": "unknown tool: " + name}
	}
}

func strPtrOr(p *string, def string) string {
	if p == nil || *p == "" {
		return def
	}
	return *p
}

func reasonOr(r *db.ReviewReason) string {
	if r == nil {
		return "unspecified"
	}
	return string(*r)
}
