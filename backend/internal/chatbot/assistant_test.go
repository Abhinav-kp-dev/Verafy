package chatbot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"coveragecheck/internal/db"
)

// scriptedGemini plays back a fixed sequence of generateContent responses,
// asserting the request shape at each step matches the real API contract
// (role "function" for tool results, functionCall/functionResponse/args).
func scriptedGemini(t *testing.T, responses ...generateResponse) *httptest.Server {
	t.Helper()
	call := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "" {
			t.Errorf("missing x-goog-api-key header")
		}
		body, _ := io.ReadAll(r.Body)
		var req generateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("bad request json: %v", err)
		}
		if call >= 1 {
			// every round after the first must carry the prior function results back
			// on a content entry with role "function"
			last := req.Contents[len(req.Contents)-1]
			if last.Role != "function" {
				t.Errorf("round %d: expected last content role %q, got %q", call, "function", last.Role)
			}
			if last.Parts[0].FunctionResponse == nil {
				t.Errorf("round %d: expected a functionResponse part", call)
			}
		}
		if call >= len(responses) {
			t.Fatalf("unexpected extra call %d", call)
		}
		resp := responses[call]
		call++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func newTestAssistant(t *testing.T, srv *httptest.Server) *Assistant {
	t.Helper()
	c := New("test-key", "gemini-test", 6000) // high RPM: tests must not be rate-limited
	c.baseURL = srv.URL
	a := NewAssistant(c, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Tests exercise the orchestration loop (rounds, history, rate limiting), not
	// live DB queries — dispatch is faked so no real *db.Store is required.
	a.dispatch = func(ctx context.Context, store *db.Store, name string, args map[string]any) map[string]any {
		return map[string]any{"total": 0}
	}
	return a
}

func TestPlainTextReply_NoToolsNeeded(t *testing.T) {
	srv := scriptedGemini(t, generateResponse{Candidates: []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	}{{Content: content{Role: "model", Parts: []part{{Text: "Batch Upload lets you verify many patients from a CSV."}}}, FinishReason: "STOP"}}})
	defer srv.Close()

	a := newTestAssistant(t, srv)
	reply, err := a.Ask(context.Background(), []ChatMessage{{Role: "user", Text: "how does batch upload work?"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Batch Upload lets you verify many patients from a CSV." {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}
	if len(reply.ToolsUsed) != 0 {
		t.Fatalf("expected no tools used, got %v", reply.ToolsUsed)
	}
}

func TestFunctionCallingLoop_OneRoundTrip(t *testing.T) {
	// Round 1: model asks to call get_dashboard_stats.
	// Round 2: model answers in text using the (mocked, empty-store) tool result.
	srv := scriptedGemini(t,
		generateResponse{Candidates: []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		}{{Content: content{Role: "model", Parts: []part{{FunctionCall: &functionCall{Name: "get_dashboard_stats", Args: map[string]any{}}}}}, FinishReason: "STOP"}}},
		generateResponse{Candidates: []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		}{{Content: content{Role: "model", Parts: []part{{Text: "You have 0 verifications on record."}}}, FinishReason: "STOP"}}},
	)
	defer srv.Close()

	a := newTestAssistant(t, srv)
	reply, err := a.Ask(context.Background(), []ChatMessage{{Role: "user", Text: "how many verifications have we run?"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "You have 0 verifications on record." {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}
	if len(reply.ToolsUsed) != 1 || reply.ToolsUsed[0] != "get_dashboard_stats" {
		t.Fatalf("expected [get_dashboard_stats], got %v", reply.ToolsUsed)
	}
}

func TestToolRoundBudget_StopsLooping(t *testing.T) {
	// Model keeps calling a tool forever; the loop must stop at maxToolRounds
	// rather than looping (and spending Gemini quota) indefinitely.
	resps := make([]generateResponse, 0, maxToolRounds)
	for i := 0; i < maxToolRounds; i++ {
		resps = append(resps, generateResponse{Candidates: []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		}{{Content: content{Role: "model", Parts: []part{{FunctionCall: &functionCall{Name: "get_dashboard_stats", Args: map[string]any{}}}}}}}})
	}
	srv := scriptedGemini(t, resps...)
	defer srv.Close()

	a := newTestAssistant(t, srv)
	reply, err := a.Ask(context.Background(), []ChatMessage{{Role: "user", Text: "loop forever"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolsUsed) != maxToolRounds {
		t.Fatalf("expected exactly %d tool calls, got %d", maxToolRounds, len(reply.ToolsUsed))
	}
}

func TestHistoryTruncation(t *testing.T) {
	long := make([]ChatMessage, 0, 30)
	for i := 0; i < 30; i++ {
		long = append(long, ChatMessage{Role: "user", Text: "msg"})
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req generateRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		if len(req.Contents) != maxHistory {
			t.Errorf("expected %d contents after truncation, got %d", maxHistory, len(req.Contents))
		}
		json.NewEncoder(w).Encode(generateResponse{Candidates: []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		}{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}}})
	}))
	defer srv.Close()
	a := newTestAssistant(t, srv)
	if _, err := a.Ask(context.Background(), long); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestOutboundRateLimiter_BlocksBurst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(generateResponse{Candidates: []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		}{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}}})
	}))
	defer srv.Close()

	c := New("k", "m", 60) // 1 req/sec, burst 15 (max(1,int(60/4)))
	c.baseURL = srv.URL
	for i := 0; i < 15; i++ { // drain the burst
		if _, err := c.call(context.Background(), generateRequest{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	start := time.Now()
	if _, err := c.call(context.Background(), generateRequest{}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Fatalf("expected the limiter to make the caller wait for a token, returned in %v", time.Since(start))
	}
}

func TestGeminiHTTPError_Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 429, "message": "Resource exhausted"}})
	}))
	defer srv.Close()
	c := New("k", "m", 6000)
	c.baseURL = srv.URL
	_, err := c.call(context.Background(), generateRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
}
