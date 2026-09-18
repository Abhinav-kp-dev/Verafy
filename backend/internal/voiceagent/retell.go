package voiceagent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retell places calls through https://api.retellai.com. The agent itself (voice,
// prompt, custom function) is configured in the Retell dashboard — see AgentSpec
// for the exact prompt and tool schema this code expects it to use.
type Retell struct {
	APIKey     string
	AgentID    string
	FromNumber string
	BaseURL    string
	http       *http.Client
}

func NewRetell(apiKey, agentID, fromNumber string) *Retell {
	return &Retell{APIKey: apiKey, AgentID: agentID, FromNumber: fromNumber, BaseURL: "https://api.retellai.com", http: &http.Client{Timeout: 20 * time.Second}}
}

func (r *Retell) Mode() string { return "live" }

// PlaceCall: POST /v2/create-phone-call. The job id rides in metadata (echoed on
// every webhook) and the 270 fields ride as dynamic variables the prompt reads.
func (r *Retell) PlaceCall(ctx context.Context, req CallRequest) (string, error) {
	payload := map[string]any{
		"from_number":       r.FromNumber,
		"to_number":         req.ToNumber,
		"override_agent_id": r.AgentID,
		"metadata":          map[string]string{"job_id": req.JobID},
		"retell_llm_dynamic_variables": map[string]string{
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
	body, _ := json.Marshal(payload)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/v2/create-phone-call", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Authorization", "Bearer "+r.APIKey)
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(hreq)
	if err != nil {
		return "", fmt.Errorf("retell: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("retell http %d: %.300s", resp.StatusCode, raw)
	}
	var out struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.CallID == "" {
		return "", fmt.Errorf("retell: response missing call_id: %.200s", raw)
	}
	return out.CallID, nil
}

// VerifySignature checks Retell's X-Retell-Signature header ("v=<ms>,d=<hex>"):
// HMAC-SHA256(rawBody + timestamp, apiKey), timestamp within 5 minutes.
func VerifySignature(rawBody []byte, apiKey, header string, now time.Time) bool {
	if apiKey == "" || header == "" {
		return false
	}
	var tsStr, digest string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "v":
			tsStr = v
		case "d":
			digest = v
		}
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || digest == "" {
		return false
	}
	if math.Abs(float64(now.UnixMilli()-ts)) > 5*60*1000 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write(rawBody)
	mac.Write([]byte(tsStr))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(digest))
}

// ---- webhook payloads (only the fields we read) ----

// WebhookEvent is the body of the agent-level webhook (call_started / call_ended / call_analyzed).
type WebhookEvent struct {
	Event string      `json:"event"`
	Call  WebhookCall `json:"call"`
}

type WebhookCall struct {
	CallID              string            `json:"call_id"`
	CallStatus          string            `json:"call_status"`
	Transcript          string            `json:"transcript"`
	DisconnectionReason string            `json:"disconnection_reason"`
	Metadata            map[string]string `json:"metadata"`
	CallAnalysis        *struct {
		CallSuccessful bool           `json:"call_successful"`
		Summary        string         `json:"call_summary"`
		Custom         map[string]any `json:"custom_analysis_data"`
	} `json:"call_analysis,omitempty"`
}

// FunctionCall is the body Retell POSTs to a custom-function URL mid-call.
type FunctionCall struct {
	Name string          `json:"name"`
	Call WebhookCall     `json:"call"`
	Args json.RawMessage `json:"args"`
}

// OutcomeFromDisconnect maps Retell's disconnection_reason to our failure outcome.
func OutcomeFromDisconnect(reason string) (Outcome, string) {
	switch reason {
	case "dial_no_answer", "dial_busy", "voicemail_reached", "dial_failed":
		return OutcomeNoAnswer, "Payer line did not answer (" + reason + ")"
	case "max_duration_reached":
		return OutcomeIVRDeadEnd, "Call hit the maximum duration without reaching benefits"
	case "user_hangup", "agent_hangup", "call_transfer":
		return OutcomeDisconnected, "Call ended before benefits were confirmed (" + reason + ")"
	case "error_llm_websocket_open", "error_llm_websocket_lost_connection", "error_llm_websocket_runtime", "error_frontend_corrupted_payload", "error_twilio", "error_no_audio_received", "error_asr", "error_retell", "error_unknown", "error_user_not_joined":
		return OutcomeError, "Voice platform error (" + reason + ")"
	}
	if reason == "" {
		reason = "unspecified"
	}
	return OutcomeDisconnected, "Call ended without a submitted verification (" + reason + ")"
}
