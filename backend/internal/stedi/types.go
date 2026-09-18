// Package stedi implements the X12 270/271 eligibility check against Stedi's
// clearinghouse JSON API (https://healthcare.us.stedi.com/2026-06-01/eligibility-check).
package stedi

import "fmt"

// ---- request (270) ----

type PersonName struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
}

type Name struct {
	Person       *PersonName `json:"person,omitempty"`
	Organization string      `json:"organization,omitempty"`
}

type Provider struct {
	Name Name   `json:"name"`
	NPI  string `json:"npi"`
}

type Subscriber struct {
	Name        Name   `json:"name"`
	MemberID    string `json:"memberId"`
	DateOfBirth string `json:"dateOfBirth"` // YYYY-MM-DD
}

type Service struct {
	Value  string `json:"value"`
	System string `json:"system"` // STC | CDT
}

type Encounter struct {
	Services []Service `json:"services"`
}

type Request struct {
	PayerID    string     `json:"payerId"`
	Provider   Provider   `json:"provider"`
	Subscriber Subscriber `json:"subscriber"`
	Encounter  Encounter  `json:"encounter"`
}

// ---- response (271) ----

type DateRange struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type Dates struct {
	Plan        *DateRange `json:"plan,omitempty"`
	Eligibility *DateRange `json:"eligibility,omitempty"`
	Service     *DateRange `json:"service,omitempty"`
	Benefit     *DateRange `json:"benefit,omitempty"`
}

type Network struct {
	Indicator   string `json:"indicator,omitempty"` // IN_NETWORK | OUT_OF_NETWORK | IN_AND_OUT_OF_NETWORK
	Description string `json:"description,omitempty"`
}

type ServiceRef struct {
	Value      string `json:"value,omitempty"`
	System     string `json:"system,omitempty"`
	Definition string `json:"definition,omitempty"`
}

type Quantity struct {
	Qualifier string `json:"qualifier,omitempty"`
	Value     string `json:"value,omitempty"`
}

// Benefit is the common shape of every entry under plans[].benefits.*
type Benefit struct {
	Status             string      `json:"status,omitempty"` // statuses[] only
	CoverageLevel      string      `json:"coverageLevel,omitempty"`
	InsuranceType      string      `json:"insuranceType,omitempty"`
	Amount             string      `json:"amount,omitempty"`  // deductible / coPayment / outOfPocket / limitations
	Percent            string      `json:"percent,omitempty"` // coInsurance (decimal, e.g. "0.20")
	TimePeriod         string      `json:"timePeriod,omitempty"`
	Network            *Network    `json:"network,omitempty"`
	Service            *ServiceRef `json:"service,omitempty"`
	Messages           []string    `json:"messages,omitempty"`
	Dates              *Dates      `json:"dates,omitempty"`
	Quantity           *Quantity   `json:"quantity,omitempty"`
	PriorAuthIndicator string      `json:"priorAuthIndicator,omitempty"`
}

type Benefits struct {
	Statuses          []Benefit `json:"statuses,omitempty"`
	Deductible        []Benefit `json:"deductible,omitempty"`
	CoInsurance       []Benefit `json:"coInsurance,omitempty"`
	CoPayment         []Benefit `json:"coPayment,omitempty"`
	OutOfPocket       []Benefit `json:"outOfPocket,omitempty"`
	Limitations       []Benefit `json:"limitations,omitempty"`
	NonCovered        []Benefit `json:"nonCovered,omitempty"`
	Exclusions        []Benefit `json:"exclusions,omitempty"`
	BenefitDisclaimer []Benefit `json:"benefitDisclaimer,omitempty"`
}

type Plan struct {
	Name     string   `json:"name,omitempty"`
	Benefits Benefits `json:"benefits"`
}

type PayerInfo struct {
	Identification string `json:"identification,omitempty"`
	Name           Name   `json:"name"`
}

type ResponseSubscriber struct {
	Name        Name   `json:"name"`
	MemberID    string `json:"memberId,omitempty"`
	DateOfBirth string `json:"dateOfBirth,omitempty"`
	Dates       *Dates `json:"dates,omitempty"`
}

// AAAError is a payer rejection (AAA segment) returned inside a 200 response.
type AAAError struct {
	Code                string `json:"code"`
	Description         string `json:"description"`
	FollowupAction      string `json:"followupAction,omitempty"`
	Location            string `json:"location,omitempty"`
	PossibleResolutions string `json:"possibleResolutions,omitempty"`
}

type Response struct {
	ID                  string             `json:"id,omitempty"`
	EligibilitySearchID string             `json:"eligibilitySearchId,omitempty"`
	PayerID             string             `json:"payerId,omitempty"`
	Payer               *PayerInfo         `json:"payer,omitempty"`
	Subscriber          ResponseSubscriber `json:"subscriber"`
	Plans               []Plan             `json:"plans,omitempty"`
	Errors              []AAAError         `json:"errors,omitempty"`
	Warnings            []AAAError         `json:"warnings,omitempty"`
}

// ---- error classification ----

type ErrorKind int

const (
	ErrKindNone      ErrorKind = iota
	ErrKindTransient           // retry: timeout, 5xx, 429, AAA 42/79
	ErrKindRejected            // payer rejected the request: AAA 43/72/73/75 etc.
	ErrKindMalformed           // could not parse the response
)

type CallError struct {
	Kind    ErrorKind
	Code    string
	Message string
	Raw     []byte
}

func (e *CallError) Error() string { return fmt.Sprintf("stedi %s: %s", e.Code, e.Message) }

// Transient AAA codes: payer unable to respond / connectivity problems.
var transientAAA = map[string]bool{"42": true, "79": true, "80": true}

// ClassifyAAA maps an AAA reject code to an error kind.
func ClassifyAAA(code string) ErrorKind {
	if transientAAA[code] {
		return ErrKindTransient
	}
	return ErrKindRejected
}

// Standard X12 dental service type codes and their labels.
var STCLabels = map[string]string{
	"35": "Dental Care",
	"23": "Diagnostic Dental",
	"24": "Periodontics",
	"25": "Restorative",
	"26": "Endodontics",
	"27": "Maxillofacial Prosthetics",
	"28": "Adjunctive Dental Services",
	"36": "Dental Crowns",
	"37": "Dental Accident",
	"38": "Orthodontics",
	"39": "Prosthodontics",
	"40": "Oral Surgery",
	"41": "Routine (Preventive) Dental",
	"30": "Health Benefit Plan Coverage",
}
