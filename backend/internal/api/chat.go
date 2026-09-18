package api

import (
	"errors"
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"coveragecheck/internal/chatbot"
)

// chatLimiter is deliberately separate from and stricter than the general
// inboundLimit (30 req/s) — each chat message can trigger several real Gemini
// API calls (the function-calling loop), so it needs its own, tighter budget.
type chatLimiter struct {
	mu    sync.Mutex
	byIP  map[string]*rate.Limiter
	rps   float64
	burst int
}

func newChatLimiter(rpm float64, burst int) *chatLimiter {
	return &chatLimiter{byIP: map[string]*rate.Limiter{}, rps: rpm / 60.0, burst: burst}
}

func (c *chatLimiter) allow(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.byIP[ip]
	if !ok {
		l = rate.NewLimiter(rate.Limit(c.rps), c.burst)
		c.byIP[ip] = l
	}
	return l.Allow()
}

func (s *Server) registerChatRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/chat", s.chat)
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if s.assistant == nil || !s.assistant.Configured() {
		writeErr(w, 400, "The assistant isn't configured yet — set GEMINI_API_KEY in backend/.env and restart.")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.chatLim.allow(ip) {
		w.Header().Set("Retry-After", "10")
		writeErr(w, http.StatusTooManyRequests, "You're sending messages faster than the assistant can keep up — please wait a few seconds and try again.")
		return
	}

	var in struct {
		Messages []chatbot.ChatMessage `json:"messages"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if len(in.Messages) == 0 {
		writeErr(w, 400, "messages is required and must be non-empty")
		return
	}
	const maxLen = 4000
	total := 0
	for i, m := range in.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			writeErr(w, 400, "each message role must be \"user\" or \"assistant\"")
			return
		}
		total += len(m.Text)
		if len(m.Text) > maxLen {
			writeErr(w, 400, "message too long")
			return
		}
		_ = i
	}
	if total > 12000 {
		writeErr(w, 400, "conversation is too long — start a new chat")
		return
	}

	reply, err := s.assistant.Ask(r.Context(), in.Messages)
	if err != nil {
		if errors.Is(err, chatbot.ErrBusy) {
			w.Header().Set("Retry-After", "5")
			writeErr(w, http.StatusTooManyRequests, "The assistant is busy right now — please try again in a few seconds.")
			return
		}
		s.log.Error("chat failed", "err", err)
		writeErr(w, 502, "The assistant couldn't respond just now. Please try again.")
		return
	}
	writeJSON(w, 200, reply)
}
