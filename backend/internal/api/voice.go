package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"coveragecheck/internal/voiceagent"
)

func (s *Server) registerVoiceRoutes(mux *http.ServeMux) {
	// Bolna (default): unsigned callbacks, authenticated with VOICE_WEBHOOK_SECRET
	mux.HandleFunc("POST /api/webhooks/bolna/facts", s.bolnaFacts)         // custom function, fires mid-call
	mux.HandleFunc("POST /api/webhooks/bolna/execution", s.bolnaExecution) // "push all execution data" webhook
	// Retell: HMAC-signed with the API key
	mux.HandleFunc("POST /api/webhooks/voice/facts", s.retellFacts)
	mux.HandleFunc("POST /api/webhooks/voice", s.retellEvent)

	mux.HandleFunc("GET /api/voice/agent-spec", s.voiceAgentSpec)
	mux.HandleFunc("POST /api/payers/{id}/voice", s.updatePayerVoice)
}

// updatePayerVoice is how staff put a payer on the voice path: give it a phone line.
func (s *Server) updatePayerVoice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	var in struct {
		ProviderServicesPhone string `json:"providerServicesPhone"`
		IVRNotes              string `json:"ivrNotes"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	phone := normalizeE164(in.ProviderServicesPhone)
	if in.ProviderServicesPhone != "" && phone == "" {
		writeErr(w, 400, "phone must be in international format, e.g. +919876543210")
		return
	}
	if err := s.store.UpdatePayerVoice(r.Context(), id, phone, strings.TrimSpace(in.IVRNotes)); err != nil {
		writeErr(w, 404, "payer not found")
		return
	}
	s.log.Info("payer voice line updated", "payer", id, "phone", phone != "")
	p, _ := s.store.GetPayer(r.Context(), id)
	writeJSON(w, 200, p)
}

// normalizeE164 strips spaces/dashes/parens and requires a leading + and 8-15 digits.
func normalizeE164(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r == '+' && b.Len() == 0 {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if !strings.HasPrefix(out, "+") || len(out) < 9 || len(out) > 16 {
		return ""
	}
	return out
}

func (s *Server) readBody(w http.ResponseWriter, r *http.Request, provider string) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, 400, "unreadable body")
		return nil, false
	}
	if s.cfg.VoiceMode != "live" || s.cfg.VoiceProvider != provider {
		writeErr(w, 404, "this webhook is only active when VOICE_MODE=live and VOICE_PROVIDER="+provider)
		return nil, false
	}
	return raw, true
}

// ---------- Bolna ----------

// bolnaAuthorized accepts the shared secret as a Bearer token (set as api_token on
// the custom function) or as ?token= (the execution webhook URL has no header config).
func (s *Server) bolnaAuthorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" || got == r.Header.Get("Authorization") {
		got = r.URL.Query().Get("token")
	}
	return voiceagent.CheckSecret(s.cfg.VoiceWebhookSecret, got)
}

func (s *Server) bolnaFacts(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readBody(w, r, "bolna")
	if !ok {
		return
	}
	if !s.bolnaAuthorized(r) {
		s.log.Warn("bolna function rejected: bad secret", "ip", r.RemoteAddr)
		writeErr(w, 401, "unauthorized")
		return
	}
	fc, err := voiceagent.ParseBolnaFunctionCall(raw)
	if err != nil || fc.ExecutionID == "" {
		writeErr(w, 400, "expected execution_id plus the submit_verification_facts fields")
		return
	}
	// The transcript is not available mid-call; the execution webhook attaches it later.
	if err := s.queue.CompleteWithFacts(r.Context(), fc.ExecutionID, fc.Extracted, ""); err != nil {
		s.log.Error("bolna facts", "execution", fc.ExecutionID, "err", err)
		writeJSON(w, 200, map[string]string{"result": "There was a problem recording that on our side. Please ask for the reference number and end the call."})
		return
	}
	if verr := voiceagent.Validate(fc.Extracted); verr != nil {
		writeJSON(w, 200, map[string]string{"result": "Recorded, but one item did not validate: " + verr.Error() + ". Confirm it with the representative if possible, then end the call."})
		return
	}
	writeJSON(w, 200, map[string]string{"result": "Verification recorded successfully. Thank the representative and end the call."})
}

func (s *Server) bolnaExecution(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readBody(w, r, "bolna")
	if !ok {
		return
	}
	if !s.bolnaAuthorized(r) {
		s.log.Warn("bolna webhook rejected: bad secret", "ip", r.RemoteAddr)
		writeErr(w, 401, "unauthorized")
		return
	}
	var ex voiceagent.BolnaExecution
	if err := json.Unmarshal(raw, &ex); err != nil || ex.ID == "" {
		writeErr(w, 400, "expected an execution object")
		return
	}
	if !voiceagent.BolnaTerminal(ex.Status) {
		w.WriteHeader(http.StatusNoContent) // queued / ringing / in-progress / call-disconnected: not final yet
		return
	}
	outcome, detail := voiceagent.OutcomeFromBolna(ex)
	// CompleteWithFailure is a no-op for state if facts already finished the job; it
	// then only attaches the transcript, which is exactly what we want on "completed".
	if err := s.queue.CompleteWithFailure(r.Context(), ex.ID, outcome, detail, ex.Transcript); err != nil {
		s.log.Error("bolna execution", "execution", ex.ID, "status", ex.Status, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Retell ----------

func (s *Server) retellVerified(w http.ResponseWriter, r *http.Request, raw []byte) bool {
	if !voiceagent.VerifySignature(raw, s.cfg.RetellAPIKey, r.Header.Get("X-Retell-Signature"), time.Now()) {
		s.log.Warn("retell webhook rejected: bad signature", "path", r.URL.Path, "ip", r.RemoteAddr)
		writeErr(w, 401, "invalid signature")
		return false
	}
	return true
}

func (s *Server) retellFacts(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readBody(w, r, "retell")
	if !ok || !s.retellVerified(w, r, raw) {
		return
	}
	var fc voiceagent.FunctionCall
	if err := json.Unmarshal(raw, &fc); err != nil || fc.Call.CallID == "" {
		writeErr(w, 400, "expected {name, call, args}")
		return
	}
	if fc.Name != "" && fc.Name != voiceagent.FunctionName {
		writeErr(w, 400, "unknown function "+fc.Name)
		return
	}
	var ex voiceagent.Extracted
	if err := json.Unmarshal(fc.Args, &ex); err != nil {
		writeErr(w, 400, "args did not match the submit_verification_facts schema")
		return
	}
	if err := s.queue.CompleteWithFacts(r.Context(), fc.Call.CallID, ex, fc.Call.Transcript); err != nil {
		s.log.Error("retell facts", "call", fc.Call.CallID, "err", err)
		writeJSON(w, 200, map[string]string{"result": "There was a problem recording that on our side. Please ask for the reference number and end the call."})
		return
	}
	if verr := voiceagent.Validate(ex); verr != nil {
		writeJSON(w, 200, map[string]string{"result": "Recorded, but one item did not validate: " + verr.Error() + ". Confirm it with the representative if possible, then end the call."})
		return
	}
	writeJSON(w, 200, map[string]string{"result": "Verification recorded successfully. Thank the representative and end the call."})
}

func (s *Server) retellEvent(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readBody(w, r, "retell")
	if !ok || !s.retellVerified(w, r, raw) {
		return
	}
	var ev voiceagent.WebhookEvent
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Call.CallID == "" {
		writeErr(w, 400, "expected {event, call}")
		return
	}
	ctx := r.Context()
	switch ev.Event {
	case "call_ended":
		outcome, detail := voiceagent.OutcomeFromDisconnect(ev.Call.DisconnectionReason)
		if err := s.queue.CompleteWithFailure(ctx, ev.Call.CallID, outcome, detail, ev.Call.Transcript); err != nil {
			s.log.Error("retell call_ended", "call", ev.Call.CallID, "err", err)
		}
	case "call_analyzed":
		if err := s.store.AttachCallTranscript(ctx, ev.Call.CallID, ev.Call.Transcript); err != nil {
			s.log.Error("retell call_analyzed", "call", ev.Call.CallID, "err", err)
		}
	default:
		s.log.Info("retell event ignored", "event", ev.Event, "call", ev.Call.CallID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- spec ----------

func (s *Server) voiceAgentSpec(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimSuffix(r.URL.Query().Get("baseUrl"), "/")
	writeJSON(w, 200, map[string]any{
		"mode":        s.cfg.VoiceMode,
		"provider":    s.cfg.VoiceProvider,
		"callTimeout": s.cfg.VoiceCallTimeout.String(),
		"spec":        voiceagent.SpecFor(s.cfg.VoiceProvider, base, s.cfg.VoiceWebhookSecret != ""),
	})
}
