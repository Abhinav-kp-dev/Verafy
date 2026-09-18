// Package chatbot implements the in-app assistant: a Gemini-backed chat endpoint
// with function calling into Verafy's own data (patients, verifications,
// manual review, stats) so it can answer real questions, not just app-usage FAQs.
//
// REST contract verified against Google's own function-calling cookbook example
// (generativelanguage.googleapis.com/v1beta/models/{model}:generateContent):
// tools use "functionDeclarations"; a function-response turn uses role "function"
// with a "functionResponse" part; the model's call comes back as a "functionCall"
// part with "args". We default to gemini-2.0-flash — confirmed current and
// undeprecated on generateContent — rather than gambling on very recently
// released models whose exact API surface we could not fully verify.
package chatbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// ---------- wire types (generateContent REST contract) ----------

type part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type content struct {
	Role  string `json:"role,omitempty"` // "user" | "model" | "function"
	Parts []part `json:"parts"`
}

type functionDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type tool struct {
	FunctionDeclarations []functionDecl `json:"functionDeclarations"`
}

type genConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type generateRequest struct {
	Contents          []content `json:"contents"`
	Tools             []tool    `json:"tools,omitempty"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	GenerationConfig  genConfig `json:"generationConfig"`
}

type generateResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ---------- client ----------

type Client struct {
	apiKey  string
	model   string
	baseURL string // overridable in tests; defaults to the real Gemini API
	http    *http.Client
	limiter *rate.Limiter // outbound requests/sec to Gemini, shared across all sessions
}

func New(apiKey, model string, rpm float64) *Client {
	return NewWithBaseURL(apiKey, model, "", rpm)
}

// NewWithBaseURL overrides the API host — used to point at a local mock server
// for testing without a real Gemini key. Empty baseURL uses the real API.
func NewWithBaseURL(apiKey, model, baseURL string, rpm float64) *Client {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	}
	if rpm <= 0 {
		rpm = 8
	}
	return &Client{
		apiKey: apiKey, model: model, baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(rpm/60.0), max(1, int(rpm/4))),
	}
}

func (c *Client) Configured() bool { return c != nil && c.apiKey != "" }

// ErrBusy is returned when the outbound Gemini rate limit would make the caller
// wait longer than is reasonable for a chat request.
var ErrBusy = fmt.Errorf("assistant is handling a lot of requests right now")

func (c *Client) call(ctx context.Context, req generateRequest) (*generateResponse, error) {
	// Never block a user-facing request indefinitely on our own outbound limiter.
	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := c.limiter.Wait(waitCtx); err != nil {
		return nil, ErrBusy
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s:generateContent", c.baseURL, c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gemini: unparseable response (http %d): %.200s", resp.StatusCode, raw)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("gemini http %d: %s", out.Error.Code, out.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini http %d: %.300s", resp.StatusCode, raw)
	}
	return &out, nil
}
