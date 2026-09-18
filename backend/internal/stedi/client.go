package stedi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Client performs a 270 inquiry and returns the 271 response.
type Client interface {
	Check(ctx context.Context, req Request) (*Response, []byte, error)
	Mode() string
}

// ---------- live ----------

type LiveClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewLive(baseURL, apiKey string) *LiveClient {
	return &LiveClient{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *LiveClient) Mode() string { return "live" }

func (c *LiveClient) Check(ctx context.Context, req Request) (*Response, []byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, &CallError{Kind: ErrKindMalformed, Code: "marshal", Message: err.Error()}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/eligibility-check", bytes.NewReader(body))
	if err != nil {
		return nil, nil, &CallError{Kind: ErrKindMalformed, Code: "request", Message: err.Error()}
	}
	httpReq.Header.Set("Authorization", "Key "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, nil, &CallError{Kind: ErrKindTransient, Code: "timeout", Message: err.Error()}
		}
		return nil, nil, &CallError{Kind: ErrKindTransient, Code: "network", Message: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	switch {
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		return nil, raw, &CallError{Kind: ErrKindTransient, Code: fmt.Sprintf("http_%d", resp.StatusCode), Message: snippet(raw), Raw: raw}
	case resp.StatusCode >= 400:
		return nil, raw, &CallError{Kind: ErrKindRejected, Code: fmt.Sprintf("http_%d", resp.StatusCode), Message: snippet(raw), Raw: raw}
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, &CallError{Kind: ErrKindMalformed, Code: "parse", Message: err.Error(), Raw: raw}
	}
	return &out, raw, nil
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
