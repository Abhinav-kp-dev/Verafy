// Package voiceagent places real outbound phone calls to payers that have no
// real-time 270/271 path, via a telephony-native voice-AI platform (Retell), and
// maps what the agent collects back into the same Facts shape the EDI path
// produces. Nothing here is simulated in live mode: a call is a call.
package voiceagent

import (
	"context"
	"time"
)

// CallRequest carries the same subscriber/provider data that would go into an
// X12 270, plus the payer's phone line and any IVR hints.
type CallRequest struct {
	JobID           string
	ToNumber        string // payer provider-services line, E.164
	PatientName     string
	PatientDOB      string // YYYY-MM-DD
	MemberID        string
	PayerName       string
	ProviderName    string
	ProviderNPI     string
	ServiceTypeCode string
	IVRNotes        string
}

// Extracted is exactly what the agent submits through the submit_verification_facts
// tool once it has the numbers. Pointers distinguish "rep said $0" from "not asked".
type Extracted struct {
	EligibilityStatus   string   `json:"eligibility_status"` // active | inactive | unknown
	PlanName            string   `json:"plan_name,omitempty"`
	DeductibleAnnual    *float64 `json:"deductible_annual,omitempty"`
	DeductibleRemaining *float64 `json:"deductible_remaining,omitempty"`
	AnnualMaximum       *float64 `json:"annual_maximum,omitempty"`
	AnnualMaxRemaining  *float64 `json:"annual_max_remaining,omitempty"`
	CopayOffice         *float64 `json:"copay_office,omitempty"`
	PreventivePct       *float64 `json:"preventive_pct,omitempty"` // plan pays %
	BasicPct            *float64 `json:"basic_pct,omitempty"`
	MajorPct            *float64 `json:"major_pct,omitempty"`
	OrthoCovered        *bool    `json:"orthodontics_covered,omitempty"`
	OrthoPct            *float64 `json:"orthodontics_pct,omitempty"`
	WaitingPeriod       string   `json:"waiting_period,omitempty"`
	Limitations         []string `json:"limitations,omitempty"`
	ReferenceNumber     string   `json:"reference_number,omitempty"`
	RepName             string   `json:"rep_name,omitempty"`
	Notes               string   `json:"notes,omitempty"`
}

// Outcome is how a call ended when no facts were submitted.
type Outcome string

const (
	OutcomeNoAnswer     Outcome = "no_answer"
	OutcomeIVRDeadEnd   Outcome = "ivr_dead_end"
	OutcomeDisconnected Outcome = "disconnected"
	OutcomeRepDeclined  Outcome = "rep_declined"
	OutcomeError        Outcome = "error"
)

// Client is the outbound-calling surface. Live = Retell; mock = deterministic stand-in
// that drives the exact same completion path so the pipeline is testable end to end.
type Client interface {
	PlaceCall(ctx context.Context, req CallRequest) (callID string, err error)
	Mode() string
}

// Completer is what the platform calls back into (webhook or mock) when a call
// produces facts or ends without them. Implemented by the queue package.
type Completer interface {
	CompleteWithFacts(ctx context.Context, callID string, facts Extracted, transcript string) error
	CompleteWithFailure(ctx context.Context, callID string, outcome Outcome, detail, transcript string) error
}

// DefaultCallTimeout bounds how long a job may sit in CALL_IN_PROGRESS.
const DefaultCallTimeout = 10 * time.Minute
