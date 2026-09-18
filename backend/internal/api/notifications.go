package api

import (
	"net/http"
	"sort"
	"time"

	"coveragecheck/internal/db"
)

// Notification is derived entirely from real state — new manual-review cases,
// failed email deliveries, and the queue's own pause state — never a fabricated
// event. There is no separate notifications table: everything here is a view
// over data that already exists for another reason.
type Notification struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // manual_review | delivery_failed | queue_paused
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
	Link      string    `json:"link"`
}

func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications", s.listNotifications)
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var out []Notification

	if jobs, _, err := s.store.ListJobs(ctx, db.JobFilter{Status: string(db.StatusNeedsReview), Limit: 15}); err == nil {
		for _, j := range jobs {
			out = append(out, Notification{
				ID: "review-" + j.ID.String(), Kind: "manual_review",
				Title:     j.PatientName + " needs manual review",
				Detail:    reasonText(j.ReviewReason) + " · " + j.PayerName,
				CreatedAt: j.UpdatedAt, Link: "/review",
			})
		}
	}

	if notices, err := s.store.ListNotices(ctx, 30); err == nil {
		for _, n := range notices {
			if n.DeliveryStatus != "failed" {
				continue
			}
			detail := "Could not send"
			if n.DeliveryError != nil {
				detail = *n.DeliveryError
			}
			out = append(out, Notification{
				ID: "notice-" + n.ID.String(), Kind: "delivery_failed",
				Title:     "Email to " + n.PatientName + " failed",
				Detail:    detail,
				CreatedAt: n.CreatedAt, Link: "/previsit",
			})
		}
	}

	if st, err := s.queue.Status(ctx); err == nil && st.Paused {
		ts := time.Now()
		if st.PausedAt != nil {
			ts = *st.PausedAt
		}
		out = append(out, Notification{
			ID: "queue-paused", Kind: "queue_paused",
			Title:     "Verification queue is paused",
			Detail:    "No new checks are being processed until it's resumed.",
			CreatedAt: ts, Link: "/settings",
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 25 {
		out = out[:25]
	}
	if out == nil {
		out = []Notification{}
	}
	writeJSON(w, 200, out)
}

func reasonText(r *db.ReviewReason) string {
	if r == nil {
		return "Needs review"
	}
	switch *r {
	case db.ReasonUnsupportedPayer:
		return "Payer doesn't support electronic checks"
	case db.ReasonAmbiguousMatch:
		return "Subscriber could not be matched"
	case db.ReasonRetryExhausted:
		return "Payer unavailable after retries"
	case db.ReasonMalformedResponse:
		return "Payer response could not be parsed"
	case db.ReasonPayerRejected:
		return "Payer rejected the request"
	case db.ReasonVoiceCallFailed:
		return "AI phone call could not complete verification"
	case db.ReasonCallTimeout:
		return "AI phone call timed out"
	case db.ReasonEmailNotAnswered:
		return "Verification email was not answered"
	case db.ReasonEmailInvalid:
		return "Emailed verification form had invalid data"
	default:
		return string(*r)
	}
}
