package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	StatusQueued         JobStatus = "QUEUED"
	StatusProcessing     JobStatus = "PROCESSING"
	StatusVerified       JobStatus = "VERIFIED"
	StatusGapFlagged     JobStatus = "COVERAGE_GAP_FLAGGED"
	StatusRetrying       JobStatus = "RETRYING"
	StatusNeedsReview    JobStatus = "NEEDS_MANUAL_REVIEW"
	StatusManualResolve  JobStatus = "MANUAL_RESOLVED"
	StatusCallInProgress JobStatus = "CALL_IN_PROGRESS" // AI voice agent is on the phone with the payer
	StatusEmailPending   JobStatus = "EMAIL_PENDING"    // verification form emailed to the payer, awaiting their response
)

const (
	SourceStedi     = "stedi_270_271"
	SourceVoiceCall = "ai_voice_call"
	SourceEmailForm = "email_form"
)

// batchColumn maps a status to its denormalized counter column on batches.
// This is a fixed whitelist; it is the only thing ever interpolated into SQL.
var batchColumn = map[JobStatus]string{
	StatusQueued:        "queued",
	StatusProcessing:    "processing",
	StatusVerified:      "verified",
	StatusGapFlagged:    "gap_flagged",
	StatusRetrying:      "retrying",
	StatusNeedsReview:   "needs_review",
	StatusManualResolve: "manual_resolved",
	// A call or an outstanding email are both still "processing" from the batch's point of
	// view; sharing the column keeps counters exact without a schema change.
	StatusCallInProgress: "processing",
	StatusEmailPending:   "processing",
}

type ReviewReason string

const (
	ReasonUnsupportedPayer  ReviewReason = "unsupported_payer"
	ReasonAmbiguousMatch    ReviewReason = "ambiguous_match"
	ReasonRetryExhausted    ReviewReason = "retry_exhausted"
	ReasonMalformedResponse ReviewReason = "malformed_response"
	ReasonPayerRejected     ReviewReason = "payer_rejected"
	ReasonVoiceCallFailed   ReviewReason = "voice_call_failed"
	ReasonCallTimeout       ReviewReason = "call_timeout"
	ReasonEmailNotAnswered  ReviewReason = "email_not_answered"
	ReasonEmailInvalid      ReviewReason = "email_response_invalid"
)

type Practice struct {
	ID   uuid.UUID `db:"id" json:"id"`
	Name string    `db:"name" json:"name"`
}

type Payer struct {
	ID               uuid.UUID `db:"id" json:"id"`
	Name             string    `db:"name" json:"name"`
	StediPayerID     string    `db:"stedi_payer_id" json:"stediPayerId"`
	SupportsRealtime bool      `db:"supports_realtime" json:"supportsRealtime"`
	ServiceTypeCode  string    `db:"service_type_code" json:"serviceTypeCode"`
	PlanType         string    `db:"plan_type" json:"planType"`
	// Manual-verification channels for payers without real-time EDI. Both are optional
	// and independent; a payer may have a phone, an email, both, or neither (plain manual review).
	ProviderServicesPhone *string `db:"provider_services_phone" json:"providerServicesPhone,omitempty"`
	IVRNotes              *string `db:"ivr_notes" json:"ivrNotes,omitempty"`
	ProviderServicesEmail *string `db:"provider_services_email" json:"providerServicesEmail,omitempty"`
}

// VoiceEnabled reports whether this payer can be verified by an AI phone call.
func (p *Payer) VoiceEnabled() bool {
	return p.ProviderServicesPhone != nil && *p.ProviderServicesPhone != ""
}

// EmailEnabled reports whether this payer can be verified via the emailed form.
func (p *Payer) EmailEnabled() bool {
	return p.ProviderServicesEmail != nil && *p.ProviderServicesEmail != ""
}

type Patient struct {
	ID         uuid.UUID `db:"id" json:"id"`
	PracticeID uuid.UUID `db:"practice_id" json:"practiceId"`
	Name       string    `db:"name" json:"name"`
	DOB        time.Time `db:"dob" json:"dob"`
	MemberID   string    `db:"member_id" json:"memberId"`
	PayerID    uuid.UUID `db:"payer_id" json:"payerId"`
	CreatedAt  time.Time `db:"created_at" json:"createdAt"`
	Email      *string   `db:"email" json:"email,omitempty"`
	Phone      *string   `db:"phone" json:"phone,omitempty"`
	// joined
	PayerName    string     `db:"payer_name" json:"payerName"`
	StediPayerID string     `db:"stedi_payer_id" json:"stediPayerId"`
	LastVerified *time.Time `db:"last_verified" json:"lastVerified,omitempty"`
	LastStatus   *string    `db:"last_status" json:"lastStatus,omitempty"`
}

type Batch struct {
	ID             uuid.UUID `db:"id" json:"id"`
	PracticeID     uuid.UUID `db:"practice_id" json:"practiceId"`
	Label          string    `db:"label" json:"label"`
	Kind           string    `db:"kind" json:"kind"`
	TotalJobs      int       `db:"total_jobs" json:"totalJobs"`
	Queued         int       `db:"queued" json:"queued"`
	Processing     int       `db:"processing" json:"processing"`
	Verified       int       `db:"verified" json:"verified"`
	GapFlagged     int       `db:"gap_flagged" json:"gapFlagged"`
	Retrying       int       `db:"retrying" json:"retrying"`
	NeedsReview    int       `db:"needs_review" json:"needsReview"`
	ManualResolved int       `db:"manual_resolved" json:"manualResolved"`
	CreatedAt      time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
}

type Job struct {
	ID              uuid.UUID       `db:"id" json:"id"`
	BatchID         *uuid.UUID      `db:"batch_id" json:"batchId,omitempty"`
	PatientID       uuid.UUID       `db:"patient_id" json:"patientId"`
	PayerID         uuid.UUID       `db:"payer_id" json:"payerId"`
	Status          JobStatus       `db:"status" json:"status"`
	AttemptCount    int             `db:"attempt_count" json:"attemptCount"`
	NextAttemptAt   *time.Time      `db:"next_attempt_at" json:"nextAttemptAt,omitempty"`
	RawResponse     json.RawMessage `db:"raw_response" json:"rawResponse,omitempty"`
	NormalizedBrief json.RawMessage `db:"normalized_brief" json:"normalizedBrief,omitempty"`
	ReviewReason    *ReviewReason   `db:"review_reason" json:"reviewReason,omitempty"`
	ErrorCode       *string         `db:"error_code" json:"errorCode,omitempty"`
	ErrorMessage    *string         `db:"error_message" json:"errorMessage,omitempty"`
	ResolvedBy      *string         `db:"resolved_by" json:"resolvedBy,omitempty"`
	ResolutionNote  *string         `db:"resolution_note" json:"resolutionNote,omitempty"`
	StartedAt       *time.Time      `db:"started_at" json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `db:"completed_at" json:"completedAt,omitempty"`
	ResolvedAt      *time.Time      `db:"resolved_at" json:"resolvedAt,omitempty"`
	CreatedAt       time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time       `db:"updated_at" json:"updatedAt"`
	// AI voice verification
	VerificationSource string     `db:"verification_source" json:"verificationSource"`
	CallID             *string    `db:"call_id" json:"callId,omitempty"`
	CallTranscript     *string    `db:"call_transcript" json:"callTranscript,omitempty"`
	CallStartedAt      *time.Time `db:"call_started_at" json:"callStartedAt,omitempty"`
	CallCompletedAt    *time.Time `db:"call_completed_at" json:"callCompletedAt,omitempty"`
	// email-form verification
	EmailToken  *string    `db:"email_token" json:"emailToken,omitempty"`
	EmailSentAt *time.Time `db:"email_sent_at" json:"emailSentAt,omitempty"`
	// joined
	PatientName  string    `db:"patient_name" json:"patientName"`
	PatientDOB   time.Time `db:"patient_dob" json:"patientDob"`
	MemberID     string    `db:"member_id" json:"memberId"`
	PayerName    string    `db:"payer_name" json:"payerName"`
	StediPayerID string    `db:"stedi_payer_id" json:"stediPayerId"`
	PayerPhone   *string   `db:"payer_phone" json:"payerPhone,omitempty"`
	PayerEmail   *string   `db:"payer_email" json:"payerEmail,omitempty"`
}

type JobAttempt struct {
	ID            uuid.UUID `db:"id" json:"id"`
	JobID         uuid.UUID `db:"job_id" json:"jobId"`
	AttemptNumber int       `db:"attempt_number" json:"attemptNumber"`
	Status        string    `db:"status" json:"status"`
	ErrorCode     *string   `db:"error_code" json:"errorCode,omitempty"`
	ErrorMessage  *string   `db:"error_message" json:"errorMessage,omitempty"`
	AttemptedAt   time.Time `db:"attempted_at" json:"attemptedAt"`
}
