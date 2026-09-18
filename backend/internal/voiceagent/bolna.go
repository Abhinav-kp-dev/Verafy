package voiceagent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Bolna (bolna.ai) is the India-native provider: its default numbers can dial +91
// directly, and an Exotel/Plivo number can be connected for a +91 caller ID.
// Its webhooks are not signed, so both callbacks are authenticated with a shared
// secret we configure on the agent (Bearer token on the function, ?token= on the
// execution webhook).
type Bolna struct {
	APIKey     string
	AgentID    string
	FromNumber string // optional: a connected Exotel/Plivo number; empty = Bolna default line
	BaseURL    string
	http       *http.Client
}

func NewBolna(apiKey, agentID, fromNumber, baseURL string) *Bolna {
	if baseURL == "" {
		baseURL = "https://api.bolna.ai"
	}
	return &Bolna{APIKey: apiKey, AgentID: agentID, FromNumber: fromNumber, BaseURL: baseURL, http: &http.Client{Timeout: 20 * time.Second}}
}

func (b *Bolna) Mode() string { return "live" }

// PlaceCall: POST /call. Every key in user_data is available to the prompt as
// {{key}}, and job_id rides along so the function call can echo it back.
func (b *Bolna) PlaceCall(ctx context.Context, req CallRequest) (string, error) {
	payload := map[string]any{
		"agent_id":               b.AgentID,
		"recipient_phone_number": req.ToNumber,
		"user_data": map[string]string{
			"job_id":            req.JobID,
			"patient_name":      req.PatientName,
			"patient_dob":       req.PatientDOB,
			"member_id":         req.MemberID,
			"payer_name":        req.PayerName,
			"provider_name":     req.ProviderName,
			"provider_npi":      req.ProviderNPI,
			"service_type_code": req.ServiceTypeCode,
			"ivr_notes":         req.IVRNotes,
		},
	}
	if b.FromNumber != "" {
		payload["from_phone_number"] = b.FromNumber
	}
	body, _ := json.Marshal(payload)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/call", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Authorization", "Bearer "+b.APIKey)
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(hreq)
	if err != nil {
		return "", fmt.Errorf("bolna: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("bolna http %d: %.300s", resp.StatusCode, raw)
	}
	var out struct {
		ExecutionID string `json:"execution_id"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ExecutionID == "" {
		return "", fmt.Errorf("bolna: response missing execution_id: %.200s", raw)
	}
	return out.ExecutionID, nil
}

// CheckSecret is the constant-time comparison used for Bolna's unsigned callbacks.
func CheckSecret(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// BolnaFunctionCall is the body our own param mapping makes Bolna send mid-call:
// the facts fields at top level plus the two identifiers.
type BolnaFunctionCall struct {
	ExecutionID string
	JobID       string
	Extracted
}

// ParseBolnaFunctionCall tolerates Bolna's string substitution: numbers may arrive
// as "80", "$1,500" or "null", booleans as "true"/"yes", arrays as JSON or a
// comma-separated string. Anything unparseable is treated as "not provided".
func ParseBolnaFunctionCall(raw []byte) (BolnaFunctionCall, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return BolnaFunctionCall{}, err
	}
	fc := BolnaFunctionCall{ExecutionID: looseString(m["execution_id"]), JobID: looseString(m["job_id"])}
	e := &fc.Extracted
	e.EligibilityStatus = looseString(m["eligibility_status"])
	e.PlanName = looseString(m["plan_name"])
	e.DeductibleAnnual = looseNum(m["deductible_annual"])
	e.DeductibleRemaining = looseNum(m["deductible_remaining"])
	e.AnnualMaximum = looseNum(m["annual_maximum"])
	e.AnnualMaxRemaining = looseNum(m["annual_max_remaining"])
	e.CopayOffice = looseNum(m["copay_office"])
	e.PreventivePct = looseNum(m["preventive_pct"])
	e.BasicPct = looseNum(m["basic_pct"])
	e.MajorPct = looseNum(m["major_pct"])
	e.OrthoCovered = looseBool(m["orthodontics_covered"])
	e.OrthoPct = looseNum(m["orthodontics_pct"])
	e.WaitingPeriod = looseString(m["waiting_period"])
	e.Limitations = looseList(m["limitations"])
	e.ReferenceNumber = looseString(m["reference_number"])
	e.RepName = looseString(m["rep_name"])
	e.Notes = looseString(m["notes"])
	return fc, nil
}

func looseString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(t)
		if strings.EqualFold(s, "null") || strings.EqualFold(s, "none") || strings.EqualFold(s, "n/a") {
			return ""
		}
		return s
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func looseNum(v any) *float64 {
	switch t := v.(type) {
	case float64:
		return &t
	case string:
		s := strings.NewReplacer("$", "", ",", "", "%", "", " ", "").Replace(looseString(t))
		if s == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return &f
		}
	}
	return nil
}

func looseBool(v any) *bool {
	switch t := v.(type) {
	case bool:
		return &t
	case string:
		switch strings.ToLower(looseString(t)) {
		case "true", "yes", "y", "covered", "1":
			b := true
			return &b
		case "false", "no", "n", "not covered", "0":
			b := false
			return &b
		}
	}
	return nil
}

func looseList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s := looseString(x); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		s := looseString(t)
		if s == "" {
			return nil
		}
		if strings.HasPrefix(s, "[") {
			var arr []any
			if json.Unmarshal([]byte(s), &arr) == nil {
				return looseList(arr)
			}
		}
		var out []string
		for _, part := range strings.Split(s, ";") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

// BolnaExecution is the post-call webhook body (same shape as GET /executions/{id}).
type BolnaExecution struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	ErrorMessage  string         `json:"error_message"`
	Transcript    string         `json:"transcript"`
	Summary       string         `json:"summary"`
	ExtractedData map[string]any `json:"extracted_data"`
	Voicemail     bool           `json:"answered_by_voice_mail"`
	Telephony     struct {
		HangupBy     string `json:"hangup_by"`
		HangupReason string `json:"hangup_reason"`
		Duration     any    `json:"duration"` // Bolna sends this as a number, docs say string — accept either
		ToNumber     string `json:"to_number"`
		FromNumber   string `json:"from_number"`
	} `json:"telephony_data"`
}

// BolnaTerminal reports whether an execution status is final. Non-terminal
// statuses (queued, ringing, in-progress, call-disconnected) carry incomplete data
// and are ignored; "call-disconnected" in particular precedes "completed".
func BolnaTerminal(status string) bool {
	switch status {
	case "completed", "no-answer", "busy", "failed", "canceled", "stopped", "error", "balance-low":
		return true
	}
	return false
}

// OutcomeFromBolna maps a terminal execution to our failure outcome (used only when
// no facts were submitted during the call).
func OutcomeFromBolna(e BolnaExecution) (Outcome, string) {
	switch e.Status {
	case "no-answer", "busy":
		return OutcomeNoAnswer, "Payer line did not answer (" + e.Status + ")"
	case "failed", "error", "canceled", "stopped", "balance-low":
		msg := e.ErrorMessage
		if msg == "" {
			msg = e.Status
		}
		return OutcomeError, "Voice platform could not complete the call (" + msg + ")"
	}
	if e.Voicemail {
		return OutcomeNoAnswer, "Payer line went to voicemail"
	}
	reason := e.Telephony.HangupReason
	if reason == "" {
		reason = "hangup by " + e.Telephony.HangupBy
	}
	return OutcomeDisconnected, "Call ended without a submitted verification (" + reason + ")"
}
