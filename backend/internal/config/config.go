package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string
	CORSOrigin  string

	// Stedi
	StediMode    string // "live" | "mock"
	StediAPIKey  string
	StediBaseURL string
	ProviderNPI  string
	ProviderName string

	// LLM (OpenRouter)
	OpenRouterAPIKey string
	LLMModel         string

	// Queue / worker pool
	MaxWorkers     int
	MaxAttempts    int
	RetryBase      time.Duration // first backoff; doubles each attempt (+ jitter)
	PayerRPS       float64       // outbound per-payer rate limit
	PayerBurst     int
	MockLatencyMin time.Duration
	MockLatencyMax time.Duration
	MockFailRate   float64 // fraction of mock calls that fail transiently (synthetic-load realism)

	// Pre-visit cost notices
	PracticePhone  string
	ResendAPIKey   string // optional: real email delivery via Resend
	ResendFrom     string
	SMTPHost       string // optional: real email delivery via SMTP (e.g. smtp.gmail.com)
	SMTPPort       int
	SMTPUser       string
	SMTPPass       string
	SMTPFrom       string
	NightlyHour    int    // local hour (0-23) the pre-visit run fires for tomorrow's appointments
	Timezone       string

	// In-app assistant (Gemini)
	GeminiAPIKey  string
	GeminiModel   string
	GeminiBaseURL string // override for local testing; defaults to the real Gemini API
	GeminiRPM     float64 // outbound requests/minute to the Gemini API (shared across all chat sessions)
	ChatRPM       float64 // inbound chat messages/minute allowed per client IP
	ChatBurst     int
}

func Load() (*Config, error) {
	c := &Config{
		Port:             env("PORT", "8080"),
		DatabaseURL:      env("DATABASE_URL", ""),
		CORSOrigin:       env("CORS_ORIGIN", "http://localhost:5173"),
		StediMode:        env("STEDI_MODE", "mock"),
		StediAPIKey:      env("STEDI_API_KEY", ""),
		StediBaseURL:     env("STEDI_BASE_URL", "https://healthcare.us.stedi.com/2026-06-01"),
		ProviderNPI:      env("PROVIDER_NPI", "1999999984"),
		ProviderName:     env("PROVIDER_NAME", "Riverside Dental Care"),
		OpenRouterAPIKey: env("OPENROUTER_API_KEY", ""),
		LLMModel:         env("LLM_MODEL", "openai/gpt-4o-mini"),
		MaxWorkers:       envInt("MAX_WORKERS", 20),
		MaxAttempts:      envInt("MAX_ATTEMPTS", 3),
		RetryBase:        envDur("RETRY_BASE", 15*time.Second),
		PayerRPS:         envFloat("PAYER_RPS", 5),
		PayerBurst:       envInt("PAYER_BURST", 5),
		MockLatencyMin:   envDur("MOCK_LATENCY_MIN", 250*time.Millisecond),
		MockLatencyMax:   envDur("MOCK_LATENCY_MAX", 900*time.Millisecond),
		MockFailRate:     envFloat("MOCK_FAIL_RATE", 0.03),
		PracticePhone:    env("PRACTICE_PHONE", "+91 98765 43210"),
		ResendAPIKey:     env("RESEND_API_KEY", ""),
		ResendFrom:       env("RESEND_FROM", ""),
		SMTPHost:         env("SMTP_HOST", ""),
		SMTPPort:         envInt("SMTP_PORT", 587),
		SMTPUser:         env("SMTP_USER", ""),
		SMTPPass:         env("SMTP_PASS", ""),
		SMTPFrom:         env("SMTP_FROM", ""),
		NightlyHour:      envInt("NIGHTLY_HOUR", 18),
		Timezone:         env("TIMEZONE", "Asia/Kolkata"),
		GeminiAPIKey:     env("GEMINI_API_KEY", ""),
		GeminiModel:      env("GEMINI_MODEL", "gemini-2.5-flash"),
		GeminiBaseURL:    env("GEMINI_BASE_URL", ""),
		GeminiRPM:        envFloat("GEMINI_RPM", 8),
		ChatRPM:          envFloat("CHAT_RPM", 6),
		ChatBurst:        envInt("CHAT_BURST", 3),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.StediMode == "live" && c.StediAPIKey == "" {
		return nil, fmt.Errorf("STEDI_MODE=live requires STEDI_API_KEY")
	}
	if c.StediMode != "live" && c.StediMode != "mock" {
		return nil, fmt.Errorf("STEDI_MODE must be live or mock, got %q", c.StediMode)
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
