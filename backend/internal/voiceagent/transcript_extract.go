package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TranscriptExtractor salvages whatever benefit facts were actually confirmed in a
// call that ended without the agent calling submit_verification_facts — most often
// because staff hung up early, mid-question. It never guesses: anything not clearly
// stated in the transcript comes back null, and the result still passes through the
// same Validate() gate as a normal tool call before it's trusted.
type TranscriptExtractor struct {
	APIKey string
	Model  string
	http   *http.Client
}

func NewTranscriptExtractor(apiKey, model string) *TranscriptExtractor {
	return &TranscriptExtractor{APIKey: apiKey, Model: model, http: &http.Client{Timeout: 30 * time.Second}}
}

func (t *TranscriptExtractor) Enabled() bool { return t.APIKey != "" }

const transcriptExtractPrompt = `You read transcripts of automated phone calls verifying dental insurance benefits. The call may have ended early (staff hung up, representative was cut off) before every question was answered.

Extract ONLY what was explicitly confirmed by the representative before the call ended. Never guess, infer, or carry a number forward from a different question. If a value was never stated, use null.

Respond with a single JSON object with exactly these keys: eligibility_status (one of "active","inactive","unknown"), plan_name, deductible_annual, deductible_remaining, annual_maximum, annual_max_remaining, copay_office, preventive_pct, basic_pct, major_pct, orthodontics_covered (true/false/null), orthodontics_pct, waiting_period, limitations (array of strings), reference_number, rep_name, notes.

If the call ended before the representative confirmed whether coverage is active or inactive, eligibility_status must be "unknown".`

// Extract asks the model to salvage facts from a raw transcript. Returns an error
// only on transport/parse failure — an empty-but-valid result (e.g. all nulls) is a
// successful call whose facts will simply fail the downstream completeness check.
func (t *TranscriptExtractor) Extract(ctx context.Context, transcript string) (Extracted, error) {
	var out Extracted
	if strings.TrimSpace(transcript) == "" {
		return out, fmt.Errorf("empty transcript")
	}
	payload := map[string]any{
		"model": t.Model,
		"messages": []map[string]string{
			{"role": "system", "content": transcriptExtractPrompt},
			{"role": "user", "content": transcript},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Verafy")
	resp, err := t.http.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("openrouter http %d: %.300s", resp.StatusCode, raw)
	}
	var env struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Choices) == 0 {
		return out, fmt.Errorf("unexpected openrouter response")
	}
	content := strings.TrimSpace(env.Choices[0].Message.Content)
	content = strings.TrimPrefix(strings.TrimSuffix(content, "```"), "```json")
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return out, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return out, nil
}
