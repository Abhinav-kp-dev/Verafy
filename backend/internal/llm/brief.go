// Package llm turns normalized coverage facts into a plain-English brief.
// The model only ever sees the field-mapped Facts object, and every number it
// returns is cross-checked against those facts before anything is stored.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"coveragecheck/internal/normalize"
)

type Brief struct {
	Status                    string             `json:"status"` // active | inactive | unknown
	DeductibleRemaining       *float64           `json:"deductibleRemaining"`
	CoveragePercentByCategory map[string]*float64 `json:"coveragePercentByCategory"`
	Flags                     []string           `json:"flags"`
	Brief                     string             `json:"brief"`
	Summary                   string             `json:"summary"` // one-line for tables
	Source                    string             `json:"source"` // llm | template | template_fallback
	Validation                string             `json:"validation"`
	Model                     string             `json:"model,omitempty"`
	Facts                     *normalize.Facts   `json:"facts"`
}

type Generator struct {
	APIKey string
	Model  string
	http   *http.Client
}

func New(apiKey, model string) *Generator {
	return &Generator{APIKey: apiKey, Model: model, http: &http.Client{Timeout: 25 * time.Second}}
}

func (g *Generator) Enabled() bool { return g.APIKey != "" }

// Generate returns a validated brief. It never fails: on any model problem it
// falls back to the deterministic template.
func (g *Generator) Generate(ctx context.Context, f *normalize.Facts) *Brief {
	tpl := Template(f)
	if !g.Enabled() {
		return tpl
	}
	out, err := g.callModel(ctx, f)
	if err != nil {
		tpl.Source = "template_fallback"
		tpl.Validation = "model error: " + err.Error()
		return tpl
	}
	if reason := validate(out, f); reason != "" {
		tpl.Source = "template_fallback"
		tpl.Validation = "rejected: " + reason
		return tpl
	}
	out.Source = "llm"
	out.Validation = "all numbers matched source response"
	out.Model = g.Model
	out.Facts = f
	out.Summary = tpl.Summary
	if out.Flags == nil {
		out.Flags = f.Flags
	}
	return out
}

// Template is the deterministic, model-free brief.
func Template(f *normalize.Facts) *Brief {
	b := &Brief{Status: f.EligibilityStatus, DeductibleRemaining: f.DeductibleRemain, CoveragePercentByCategory: map[string]*float64{}, Flags: f.Flags, Source: "template", Validation: "deterministic", Facts: f}
	var parts, short []string
	switch f.EligibilityStatus {
	case "active":
		parts = append(parts, "Active coverage")
		short = append(short, "Active")
	case "inactive":
		parts = append(parts, "Coverage is INACTIVE — do not assume benefits apply")
		short = append(short, "Inactive")
	default:
		parts = append(parts, "Eligibility status not reported by payer")
		short = append(short, "Status unknown")
	}
	if f.PlanName != "" {
		parts[0] += " under " + f.PlanName
	}
	if f.DeductibleRemain != nil {
		parts = append(parts, fmt.Sprintf("$%s deductible remaining", money(*f.DeductibleRemain)))
	}
	if f.AnnualMaximum != nil {
		parts = append(parts, fmt.Sprintf("$%s annual maximum", money(*f.AnnualMaximum)))
	}
	for _, c := range f.Categories {
		key := camel(c.Label)
		if c.Covered && c.PlanPaysPct != nil {
			b.CoveragePercentByCategory[key] = c.PlanPaysPct
			parts = append(parts, fmt.Sprintf("%s covered %d%%", c.Label, int(*c.PlanPaysPct)))
			if c.STC == "41" || c.STC == "25" || c.STC == "36" {
				short = append(short, fmt.Sprintf("%s %d%%", shortLabel(c.STC), int(*c.PlanPaysPct)))
			}
		} else {
			b.CoveragePercentByCategory[key] = nil
			parts = append(parts, fmt.Sprintf("No coverage for %s", c.Label))
			short = append(short, "Missing "+c.Label)
		}
	}
	if f.EligibilityStatus == "active" && len(f.Categories) == 0 {
		parts = append(parts, "Payer returned no benefit detail — verify manually")
	}
	b.Brief = strings.Join(parts, ". ") + "."
	b.Summary = strings.Join(short, " • ")
	return b
}

// ---- model call ----

const systemPrompt = `You write one-paragraph insurance coverage briefs for dental front-desk staff.
You will receive a JSON object of verified coverage facts. Rules:
1. Use ONLY numbers present in the facts. Never compute, round, or invent a number.
2. Copy "eligibilityStatus" into "status" unchanged.
3. Copy "deductibleRemaining" unchanged (null stays null).
4. "coveragePercentByCategory": one key per category label (camelCase), value = planPaysPct, or null if not covered.
5. "flags": copy the facts' flags array unchanged.
6. "brief": 2-4 plain sentences a receptionist can act on. Mention inactive coverage or non-covered categories first if present.
Respond with a single JSON object with exactly these keys: status, deductibleRemaining, coveragePercentByCategory, flags, brief.`

func (g *Generator) callModel(ctx context.Context, f *normalize.Facts) (*Brief, error) {
	factsJSON, _ := json.Marshal(f)
	payload := map[string]any{
		"model": g.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": string(factsJSON)},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://coveragecheck.local")
	req.Header.Set("X-Title", "Verafy")
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter http %d: %.200s", resp.StatusCode, raw)
	}
	var env struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Choices) == 0 {
		return nil, fmt.Errorf("unexpected openrouter response")
	}
	content := strings.TrimSpace(env.Choices[0].Message.Content)
	content = strings.TrimPrefix(strings.TrimSuffix(content, "```"), "```json")
	var out Brief
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return &out, nil
}

// ---- validation: every number the model produced must exist in the facts ----

var numRe = regexp.MustCompile(`\$?\d+(?:,\d{3})*(?:\.\d+)?%?`)

func validate(b *Brief, f *normalize.Facts) string {
	if b.Status != f.EligibilityStatus {
		return fmt.Sprintf("status %q != %q", b.Status, f.EligibilityStatus)
	}
	if (b.DeductibleRemaining == nil) != (f.DeductibleRemain == nil) {
		return "deductibleRemaining presence mismatch"
	}
	if b.DeductibleRemaining != nil && !inSet(*b.DeductibleRemaining, f.Numbers) {
		return fmt.Sprintf("deductibleRemaining %v not in source", *b.DeductibleRemaining)
	}
	for k, v := range b.CoveragePercentByCategory {
		if v != nil && !inSet(*v, f.Numbers) {
			return fmt.Sprintf("coverage %s=%v not in source", k, *v)
		}
	}
	if strings.TrimSpace(b.Brief) == "" {
		return "empty brief"
	}
	// every numeric token in the prose must be a source number (years like 2026 are allowed via plan dates)
	allowed := append([]float64{}, f.Numbers...)
	for _, d := range []string{f.PlanStart, f.PlanEnd} {
		for _, tok := range numRe.FindAllString(d, -1) {
			if v, ok := parseNum(tok); ok {
				allowed = append(allowed, v)
			}
		}
	}
	for _, tok := range numRe.FindAllString(b.Brief, -1) {
		v, ok := parseNum(tok)
		if !ok {
			continue
		}
		if !inSet(v, allowed) {
			return fmt.Sprintf("number %s in brief not in source", tok)
		}
	}
	return ""
}

func parseNum(tok string) (float64, bool) {
	t := strings.NewReplacer("$", "", ",", "", "%", "").Replace(tok)
	var v float64
	_, err := fmt.Sscanf(t, "%g", &v)
	return v, err == nil
}

func inSet(v float64, set []float64) bool {
	for _, s := range set {
		if math.Abs(s-v) < 0.005 {
			return true
		}
	}
	return false
}

func money(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

func camel(label string) string {
	words := strings.FieldsFunc(label, func(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == '-' })
	for i, w := range words {
		w = strings.ToLower(w)
		if i > 0 {
			w = strings.ToUpper(w[:1]) + w[1:]
		}
		words[i] = w
	}
	return strings.Join(words, "")
}

func shortLabel(stc string) string {
	switch stc {
	case "41":
		return "Preventive"
	case "25":
		return "Basic"
	case "36":
		return "Major"
	}
	return stc
}
