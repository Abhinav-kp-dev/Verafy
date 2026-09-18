package notify

import (
	"bytes"
	"fmt"
	"html"
)

// VerificationRequestInput is what the emailed "please fill this in" message needs.
type VerificationRequestInput struct {
	PracticeName string
	PayerName    string
	PatientName  string
	PatientDOB   string // pre-formatted, e.g. "August 3, 1994"
	MemberID     string
	ProviderNPI  string
	FormURL      string
}

// RenderVerificationRequest builds the email sent to a payer with no real-time EDI
// and no phone line on file, asking their provider-services team to complete a short
// hosted form instead of a phone call.
func RenderVerificationRequest(in VerificationRequestInput) Rendered {
	subject := fmt.Sprintf("Benefits verification request — %s (Member ID %s)", in.PatientName, in.MemberID)

	var t bytes.Buffer
	fmt.Fprintf(&t, "Hello,\n\nThis is an automated benefits-verification request from %s.\n\n", in.PracticeName)
	fmt.Fprintf(&t, "We don't have an electronic (270/271) connection with %s, so we're asking your provider services team to confirm dental benefits for one patient using the short form linked below.\n\n", in.PayerName)
	fmt.Fprintf(&t, "Patient: %s\nDate of birth: %s\nMember ID: %s\nRequesting provider NPI: %s\n\n", in.PatientName, in.PatientDOB, in.MemberID, in.ProviderNPI)
	fmt.Fprintf(&t, "Please complete the form here: %s\n\n", in.FormURL)
	t.WriteString("It takes about two minutes and only asks for what's needed to verify this one patient's benefits — no login required.\n\n")
	fmt.Fprintf(&t, "Thank you,\n%s\n", in.PracticeName)

	esc := html.EscapeString
	var h bytes.Buffer
	h.WriteString(`<div style="font-family:Inter,Segoe UI,Arial,sans-serif;max-width:560px;margin:0 auto;color:#1F2937;line-height:1.5">`)
	fmt.Fprintf(&h, `<div style="background:#0E7A5F;color:#fff;padding:18px 22px;border-radius:12px 12px 0 0"><div style="font-size:18px;font-weight:700">%s</div><div style="font-size:13px;opacity:.9">Benefits verification request</div></div>`, esc(in.PracticeName))
	h.WriteString(`<div style="border:1px solid #E5E7EB;border-top:0;padding:22px;border-radius:0 0 12px 12px">`)
	fmt.Fprintf(&h, `<p>Hello,</p><p>We don't have an electronic (270/271) connection with <b>%s</b>, so we're asking your provider services team to confirm dental benefits for one patient using the short form below.</p>`, esc(in.PayerName))
	h.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:14px;margin:14px 0;background:#F8FAFC;border-radius:10px;overflow:hidden"><tbody>`)
	for _, row := range [][2]string{{"Patient", in.PatientName}, {"Date of birth", in.PatientDOB}, {"Member ID", in.MemberID}, {"Requesting provider NPI", in.ProviderNPI}} {
		fmt.Fprintf(&h, `<tr><td style="padding:8px 14px;color:#6B7280">%s</td><td style="padding:8px 14px;font-weight:600">%s</td></tr>`, esc(row[0]), esc(row[1]))
	}
	h.WriteString(`</tbody></table>`)
	fmt.Fprintf(&h, `<p style="text-align:center;margin:22px 0"><a href="%s" style="background:#0E7A5F;color:#fff;padding:12px 22px;border-radius:9px;text-decoration:none;font-weight:600;display:inline-block">Complete benefits verification</a></p>`, esc(in.FormURL))
	h.WriteString(`<p style="font-size:13px;color:#6B7280">Takes about two minutes. No login required — the link is unique to this request.</p>`)
	fmt.Fprintf(&h, `<p style="font-size:13px">Thank you,<br><b>%s</b></p>`, esc(in.PracticeName))
	h.WriteString(`</div></div>`)

	return Rendered{Subject: subject, Text: t.String(), HTML: h.String()}
}
