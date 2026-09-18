// Package notify renders pre-visit cost notices and (optionally) delivers them.
// Rendering is deterministic; delivery goes through Resend when RESEND_API_KEY is set.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"coveragecheck/internal/estimate"
)

type Practice struct {
	Name  string
	Phone string
}

type RenderInput struct {
	PatientName   string
	PracticeName  string
	PracticePhone string
	ScheduledAt   time.Time
	PlanName      string
	PayerName     string
	Estimate      estimate.Result
}

type Rendered struct {
	Subject string
	Text    string
	HTML    string
}

func money(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	s := fmt.Sprintf("$%d.%02d", c/100, c%100)
	if neg {
		return "-" + s
	}
	return s
}

func firstName(full string) string {
	if i := strings.IndexByte(full, ' '); i > 0 {
		return full[:i]
	}
	return full
}

// Render builds the patient-facing message from an estimate.
func Render(in RenderInput) Rendered {
	e := in.Estimate
	when := in.ScheduledAt.Format("Monday, January 2 at 3:04 PM")
	fn := firstName(in.PatientName)
	plan := in.PlanName
	if plan == "" {
		plan = in.PayerName
	}

	var subject string
	switch {
	case e.EligibilityStatus != "active":
		subject = fmt.Sprintf("Action needed before your visit on %s — insurance not active", in.ScheduledAt.Format("Jan 2"))
	case e.PatientPaysCents == 0:
		subject = fmt.Sprintf("Good news: your visit on %s is fully covered", in.ScheduledAt.Format("Jan 2"))
	default:
		subject = fmt.Sprintf("Your estimated cost for %s: %s", in.ScheduledAt.Format("Jan 2"), money(e.PatientPaysCents))
	}

	// ---- text ----
	var t strings.Builder
	fmt.Fprintf(&t, "Hi %s,\n\n", fn)
	fmt.Fprintf(&t, "We've checked your insurance ahead of your appointment at %s on %s.\n\n", in.PracticeName, when)
	if e.EligibilityStatus != "active" {
		fmt.Fprintf(&t, "Your insurance company reported that your coverage with %s is currently NOT ACTIVE. ", in.PayerName)
		t.WriteString("Please bring updated insurance details, or plan to pay the full amount below at your visit. Call us if you think this is a mistake.\n\n")
	} else {
		fmt.Fprintf(&t, "Your plan: %s (%s) — active.\n\n", plan, in.PayerName)
	}
	t.WriteString("Planned treatment:\n")
	for _, li := range e.Lines {
		desc := li.Description
		if desc == "" {
			desc = li.Code
		}
		if li.Covered {
			fmt.Fprintf(&t, "  • %s — %s. Insurance pays %s, you pay %s", desc, money(li.FeeCents), money(li.InsurancePays), money(li.PatientPays))
			if li.DeductibleApplied > 0 {
				fmt.Fprintf(&t, " (includes %s toward your deductible)", money(li.DeductibleApplied))
			}
			t.WriteString("\n")
		} else {
			fmt.Fprintf(&t, "  • %s — %s. Not covered by your plan; you pay %s\n", desc, money(li.FeeCents), money(li.PatientPays))
		}
	}
	fmt.Fprintf(&t, "\nTotal for the visit: %s\nInsurance is expected to pay: %s\nYOUR ESTIMATED SHARE: %s\n\n", money(e.TotalFeeCents), money(e.InsurancePaysCents), money(e.PatientPaysCents))
	if e.DeductibleStartCents != nil && *e.DeductibleStartCents > 0 {
		fmt.Fprintf(&t, "You had %s of your annual deductible remaining; %s of it applies to this visit.\n", money(*e.DeductibleStartCents), money(e.DeductibleUsedCents))
	}
	if e.PatientPaysCents > 0 {
		t.WriteString("You can pay your share at check-in. We accept cards and offer payment plans — just ask.\n")
	}
	fmt.Fprintf(&t, "\n%s\n\n", e.Disclaimer)
	fmt.Fprintf(&t, "Questions? Call %s at %s.\n\n— The team at %s\n", in.PracticeName, in.PracticePhone, in.PracticeName)

	// ---- html ----
	esc := html.EscapeString
	var h bytes.Buffer
	h.WriteString(`<div style="font-family:Inter,Segoe UI,Arial,sans-serif;max-width:560px;margin:0 auto;color:#1F2937;line-height:1.5">`)
	fmt.Fprintf(&h, `<div style="background:#0E7A5F;color:#fff;padding:18px 22px;border-radius:12px 12px 0 0"><div style="font-size:18px;font-weight:700">%s</div><div style="font-size:13px;opacity:.9">Pre-visit insurance summary</div></div>`, esc(in.PracticeName))
	h.WriteString(`<div style="border:1px solid #E5E7EB;border-top:0;padding:22px;border-radius:0 0 12px 12px">`)
	fmt.Fprintf(&h, `<p>Hi %s,</p><p>We've checked your insurance ahead of your appointment on <b>%s</b>.</p>`, esc(fn), esc(when))
	if e.EligibilityStatus != "active" {
		fmt.Fprintf(&h, `<div style="background:#FEE2E2;color:#991B1B;padding:12px 14px;border-radius:10px;margin:14px 0"><b>Your coverage with %s is currently not active.</b> Please bring updated insurance details or plan to pay the full amount below. Call us if you think this is a mistake.</div>`, esc(in.PayerName))
	} else {
		fmt.Fprintf(&h, `<p style="color:#6B7280;font-size:13px">Plan: %s (%s) — <span style="color:#0E7A5F;font-weight:600">active</span></p>`, esc(plan), esc(in.PayerName))
	}
	h.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:14px;margin:12px 0"><thead><tr style="color:#6B7280;font-size:12px;text-align:left"><th style="padding:6px 0">Treatment</th><th style="text-align:right">Fee</th><th style="text-align:right">Insurance</th><th style="text-align:right">You</th></tr></thead><tbody>`)
	for _, li := range e.Lines {
		desc := li.Description
		if desc == "" {
			desc = li.Code
		}
		note := ""
		if li.DeductibleApplied > 0 {
			note = fmt.Sprintf(`<div style="font-size:12px;color:#6B7280">includes %s deductible</div>`, money(li.DeductibleApplied))
		} else if !li.Covered {
			note = `<div style="font-size:12px;color:#B91C1C">not covered</div>`
		}
		fmt.Fprintf(&h, `<tr style="border-top:1px solid #E5E7EB"><td style="padding:8px 0">%s%s</td><td style="text-align:right">%s</td><td style="text-align:right">%s</td><td style="text-align:right;font-weight:600">%s</td></tr>`,
			esc(desc), note, money(li.FeeCents), money(li.InsurancePays), money(li.PatientPays))
	}
	h.WriteString(`</tbody></table>`)
	fmt.Fprintf(&h, `<div style="background:#E8F5F0;border-radius:10px;padding:14px 16px;margin:14px 0"><div style="display:flex;justify-content:space-between"><span>Insurance is expected to pay</span><b>%s</b></div><div style="display:flex;justify-content:space-between;font-size:18px;margin-top:6px"><span>Your estimated share</span><b style="color:#0E7A5F">%s</b></div></div>`, money(e.InsurancePaysCents), money(e.PatientPaysCents))
	if e.PatientPaysCents > 0 {
		h.WriteString(`<p>You can pay your share at check-in. We accept cards and offer payment plans — just ask.</p>`)
	}
	fmt.Fprintf(&h, `<p style="font-size:12px;color:#6B7280">%s</p>`, esc(e.Disclaimer))
	fmt.Fprintf(&h, `<p style="font-size:13px">Questions? Call <b>%s</b> at %s.</p>`, esc(in.PracticeName), esc(in.PracticePhone))
	h.WriteString(`</div></div>`)

	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}
}

// RenderReminder builds the day-before reminder. The estimate is the one already
// sent in the cost notice (so the numbers never change between the two emails).
func RenderReminder(in RenderInput) Rendered {
	e := in.Estimate
	fn := firstName(in.PatientName)
	when := in.ScheduledAt.Format("Monday, January 2 at 3:04 PM")
	subject := fmt.Sprintf("Reminder: your appointment tomorrow at %s", in.ScheduledAt.Format("3:04 PM"))
	if e.PatientPaysCents > 0 {
		subject += fmt.Sprintf(" — estimated share %s", money(e.PatientPaysCents))
	}

	var t strings.Builder
	fmt.Fprintf(&t, "Hi %s,\n\nThis is a reminder of your appointment at %s on %s.\n\n", fn, in.PracticeName, when)
	if e.EligibilityStatus != "active" {
		fmt.Fprintf(&t, "Heads-up: your insurance company reported your coverage with %s as NOT ACTIVE. Please bring updated insurance details or plan to pay the full amount (%s).\n\n", in.PayerName, money(e.TotalFeeCents))
	} else {
		fmt.Fprintf(&t, "Your insurance is active. Based on the coverage check we ran earlier, your estimated share for tomorrow is %s (insurance is expected to cover %s).\n\n", money(e.PatientPaysCents), money(e.InsurancePaysCents))
	}
	t.WriteString("What to bring:\n  • A photo ID and your insurance card\n  • Your preferred payment method for your share\n\n")
	t.WriteString("Need to reschedule? Reply to this email or call us — we'd appreciate 24 hours' notice.\n\n")
	fmt.Fprintf(&t, "%s\n\nSee you tomorrow,\nThe team at %s · %s\n", e.Disclaimer, in.PracticeName, in.PracticePhone)

	esc := html.EscapeString
	var h bytes.Buffer
	h.WriteString(`<div style="font-family:Inter,Segoe UI,Arial,sans-serif;max-width:560px;margin:0 auto;color:#1F2937;line-height:1.5">`)
	fmt.Fprintf(&h, `<div style="background:#0F172A;color:#fff;padding:18px 22px;border-radius:12px 12px 0 0"><div style="font-size:18px;font-weight:700">%s</div><div style="font-size:13px;opacity:.9">Appointment reminder</div></div>`, esc(in.PracticeName))
	h.WriteString(`<div style="border:1px solid #E5E7EB;border-top:0;padding:22px;border-radius:0 0 12px 12px">`)
	fmt.Fprintf(&h, `<p>Hi %s,</p><p>This is a reminder of your appointment <b>tomorrow, %s</b>.</p>`, esc(fn), esc(when))
	if e.EligibilityStatus != "active" {
		fmt.Fprintf(&h, `<div style="background:#FEE2E2;color:#991B1B;padding:12px 14px;border-radius:10px;margin:14px 0"><b>Your coverage with %s was reported as not active.</b> Please bring updated insurance details or plan to pay the full amount (%s).</div>`, esc(in.PayerName), money(e.TotalFeeCents))
	} else {
		fmt.Fprintf(&h, `<div style="background:#E8F5F0;border-radius:10px;padding:14px 16px;margin:14px 0"><div style="display:flex;justify-content:space-between"><span>Insurance is expected to cover</span><b>%s</b></div><div style="display:flex;justify-content:space-between;font-size:18px;margin-top:6px"><span>Your estimated share</span><b style="color:#0E7A5F">%s</b></div></div>`, money(e.InsurancePaysCents), money(e.PatientPaysCents))
	}
	h.WriteString(`<p><b>What to bring:</b> a photo ID, your insurance card, and your preferred payment method.</p>`)
	h.WriteString(`<p style="font-size:13px;color:#6B7280">Need to reschedule? Reply to this email or call us — we'd appreciate 24 hours' notice.</p>`)
	fmt.Fprintf(&h, `<p style="font-size:12px;color:#6B7280">%s</p>`, esc(e.Disclaimer))
	fmt.Fprintf(&h, `<p style="font-size:13px">See you tomorrow,<br><b>%s</b> · %s</p>`, esc(in.PracticeName), esc(in.PracticePhone))
	h.WriteString(`</div></div>`)
	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}
}

// ---------- delivery ----------

type Sender interface {
	Configured() bool
	Send(ctx context.Context, to, subject, text, htmlBody string) (providerID string, err error)
}

type ResendSender struct {
	APIKey string
	From   string
	http   *http.Client
}

func NewResend(apiKey, from string) *ResendSender {
	if from == "" {
		from = "Verafy <onboarding@resend.dev>"
	}
	return &ResendSender{APIKey: apiKey, From: from, http: &http.Client{Timeout: 15 * time.Second}}
}

func (r *ResendSender) Configured() bool { return r != nil && r.APIKey != "" }

func (r *ResendSender) Send(ctx context.Context, to, subject, text, htmlBody string) (string, error) {
	body, _ := json.Marshal(map[string]any{"from": r.From, "to": []string{to}, "subject": subject, "text": text, "html": htmlBody})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("resend http %d: %.200s", resp.StatusCode, raw)
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.ID, nil
}

// ---------- SMTP (e.g. Gmail with an App Password, or any relay) ----------

type SMTPSender struct {
	Host, User, Pass, From string
	Port                   int
}

func NewSMTP(host string, port int, user, pass, from string) *SMTPSender {
	if port == 0 {
		port = 587
	}
	if from == "" {
		from = user
	}
	return &SMTPSender{Host: host, Port: port, User: user, Pass: pass, From: from}
}

func (m *SMTPSender) Configured() bool { return m != nil && m.Host != "" && m.From != "" }

func (m *SMTPSender) Send(ctx context.Context, to, subject, text, htmlBody string) (string, error) {
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return "", err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
			return "", fmt.Errorf("starttls: %w", err)
		}
	}
	if m.User != "" {
		if err := c.Auth(smtp.PlainAuth("", m.User, m.Pass, m.Host)); err != nil {
			return "", fmt.Errorf("smtp auth: %w", err)
		}
	}
	fromAddr := m.From
	if i := strings.Index(fromAddr, "<"); i >= 0 {
		fromAddr = strings.TrimSuffix(fromAddr[i+1:], ">")
	}
	if err := c.Mail(fromAddr); err != nil {
		return "", err
	}
	if err := c.Rcpt(to); err != nil {
		return "", err
	}
	w, err := c.Data()
	if err != nil {
		return "", err
	}
	msgID := fmt.Sprintf("<%d.coveragecheck@%s>", time.Now().UnixNano(), m.Host)
	boundary := fmt.Sprintf("cc-%d", time.Now().UnixNano())
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		m.From, to, mime.QEncoding.Encode("utf-8", subject), msgID, time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, text)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, htmlBody)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	_ = c.Quit()
	return msgID, nil
}
