package stedi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// MockClient reproduces Stedi's documented mock-request scenarios locally so the full
// pipeline can be exercised without a test API key. Responses use the exact 271 JSON
// shape the live endpoint returns. Member IDs match Stedi's published test data.
type MockClient struct {
	LatencyMin, LatencyMax time.Duration
	FailRate               float64 // applies only to synthetic-load calls
}

func (m *MockClient) Mode() string { return "mock" }

type ctxKey int

const syntheticKey ctxKey = 1

// WithSynthetic marks a call as part of a synthetic load run (random transient failures allowed).
func WithSynthetic(ctx context.Context) context.Context { return context.WithValue(ctx, syntheticKey, true) }

func isSynthetic(ctx context.Context) bool { v, _ := ctx.Value(syntheticKey).(bool); return v }

func (m *MockClient) Check(ctx context.Context, req Request) (*Response, []byte, error) {
	// simulate payer latency
	if m.LatencyMax > 0 {
		d := m.LatencyMin
		if m.LatencyMax > m.LatencyMin {
			d += time.Duration(rand.Int64N(int64(m.LatencyMax - m.LatencyMin)))
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, nil, &CallError{Kind: ErrKindTransient, Code: "timeout", Message: ctx.Err().Error()}
		}
	}
	if isSynthetic(ctx) && m.FailRate > 0 && rand.Float64() < m.FailRate {
		return nil, nil, &CallError{Kind: ErrKindTransient, Code: "http_503", Message: "simulated payer outage (synthetic load)"}
	}

	member := strings.ToUpper(strings.TrimSpace(req.Subscriber.MemberID))
	first := req.Subscriber.Name.Person
	fn, ln := "", ""
	if first != nil {
		fn, ln = first.FirstName, first.LastName
	}

	var resp *Response
	switch member {
	// ---- Stedi documented AAA error mocks (payer 87726, STC 30) ----
	case "UHCAAA42":
		resp = aaa(req, "42", "Unable to Respond at Current Time", "Please Resubmit Original Transaction", "PAYER")
	case "UHCAAA43":
		resp = aaa(req, "43", "Invalid/Missing Provider Identification", "Please Correct and Resubmit", "PROVIDER")
	case "UHCAAA72":
		resp = aaa(req, "72", "Invalid/Missing Subscriber/Insured ID", "Please Correct and Resubmit", "SUBSCRIBER")
	case "UHCAAA73":
		resp = aaa(req, "73", "Invalid/Missing Subscriber/Insured Name", "Please Correct and Resubmit", "SUBSCRIBER")
	case "UHCAAA75":
		resp = aaa(req, "75", "Subscriber/Insured Not Found", "Please Correct and Resubmit", "SUBSCRIBER")
	case "UHCAAA79":
		resp = aaa(req, "79", "Invalid Participant Identification", "Please Resubmit Original Transaction", "PAYER")

	// ---- Stedi documented dental mocks (STC 35) ----
	case "007007007": // Ameritas — Falcon Dent
		resp = dental(req, "Ameritas", "Ameritas Dental PPO", "PREFERRED_PROVIDER_ORGANIZATION_PPO", 50, 0, 1500, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.20, "26": 0.20, "24": 0.20, "40": 0.20, "36": 0.50, "39": 0.50, "38": 0.50,
		}, nil)
	case "AFK987654321": // Anthem BCBS CA — Aardvark Dent
		resp = dental(req, "Anthem Blue Cross Blue Shield of CA", "Dental Blue PPO", "PREFERRED_PROVIDER_ORGANIZATION_PPO", 50, 25, 2000, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.20, "26": 0.20, "24": 0.20, "40": 0.20, "36": 0.50, "39": 0.50,
		}, []string{"38"}) // orthodontics not covered
	case "U3141592653": // Cigna — Jaguar Dent
		resp = dental(req, "Cigna", "Cigna Dental Care DHMO", "HEALTH_MAINTENANCE_ORGANIZATION_HMO", 0, 0, 1000, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.30, "26": 0.30, "24": 0.30, "40": 0.30, "36": 0.60, "39": 0.60,
		}, []string{"38"})
	case "U9876543210": // Cigna with procedure code — James Doe
		resp = dental(req, "Cigna", "Cigna Dental PPO", "PREFERRED_PROVIDER_ORGANIZATION_PPO", 50, 50, 1500, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.20, "26": 0.20, "24": 0.20, "40": 0.20, "36": 0.50, "39": 0.50, "38": 0.50,
		}, nil)
	case "88877788": // MetLife — Elephant Dent
		resp = dental(req, "Metlife", "MetLife Dental PPO", "PREFERRED_PROVIDER_ORGANIZATION_PPO", 50, 50, 1500, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.20, "26": 0.20, "24": 0.20, "40": 0.20, "36": 0.50, "39": 0.50,
		}, []string{"38"})
	case "404404404": // UnitedHealthcare — Beaver Dent
		resp = dental(req, "UnitedHealthcare", "UHC Dental PPO", "PREFERRED_PROVIDER_ORGANIZATION_PPO", 75, 75, 1500, dentalCover{
			"41": 0.00, "23": 0.00, "25": 0.20, "26": 0.20, "24": 0.20, "40": 0.20, "36": 0.50, "39": 0.50, "38": 0.50,
		}, nil)
	case "INACTIVE001": // local-only scenario: coverage terminated
		resp = inactive(req, "UnitedHealthcare", "UHC Dental PPO")
	default:
		// Mirrors real sandbox behaviour: unknown test data is rejected by the payer.
		resp = aaa(req, "75", "Subscriber/Insured Not Found", "Please Correct and Resubmit", "SUBSCRIBER")
	}
	_ = fn
	_ = ln
	raw, _ := json.Marshal(resp)
	return resp, raw, nil
}

type dentalCover map[string]float64 // STC -> patient coinsurance share (0.20 = plan pays 80%)

func aaa(req Request, code, desc, followup, location string) *Response {
	return &Response{
		ID: "ec_mock_" + fmt.Sprintf("%08x", rand.Uint32()), PayerID: req.PayerID,
		Payer:      &PayerInfo{Identification: req.PayerID, Name: Name{Organization: "UnitedHealthcare"}},
		Subscriber: ResponseSubscriber{Name: req.Subscriber.Name, MemberID: req.Subscriber.MemberID, DateOfBirth: req.Subscriber.DateOfBirth},
		Errors: []AAAError{{
			Code: code, Description: desc, FollowupAction: followup, Location: location,
			PossibleResolutions: "Verify the submitted subscriber and provider details match the payer's records, then resubmit.",
		}},
	}
}

func inactive(req Request, payerName, planName string) *Response {
	return &Response{
		ID: "ec_mock_" + fmt.Sprintf("%08x", rand.Uint32()), PayerID: req.PayerID,
		Payer:      &PayerInfo{Identification: req.PayerID, Name: Name{Organization: payerName}},
		Subscriber: ResponseSubscriber{Name: req.Subscriber.Name, MemberID: req.Subscriber.MemberID, DateOfBirth: req.Subscriber.DateOfBirth},
		Plans: []Plan{{Name: planName, Benefits: Benefits{
			Statuses: []Benefit{{
				Status: "INACTIVE", CoverageLevel: "INDIVIDUAL",
				Service: &ServiceRef{Value: "35", System: "STC", Definition: "Dental Care"},
				Dates:   &Dates{Plan: &DateRange{Start: "2025-01-01", End: "2026-06-30"}},
				Messages: []string{"COVERAGE TERMINATED 06/30/2026"},
			}},
		}}},
	}
}

func dental(req Request, payerName, planName, insType string, dedIndividual, dedRemaining, annualMax float64, cover dentalCover, nonCovered []string) *Response {
	b := Benefits{}
	b.Statuses = append(b.Statuses, Benefit{
		Status: "ACTIVE_COVERAGE", CoverageLevel: "INDIVIDUAL", InsuranceType: insType,
		Service: &ServiceRef{Value: "35", System: "STC", Definition: "Dental Care"},
		Dates:   &Dates{Plan: &DateRange{Start: "2026-01-01", End: "2026-12-31"}, Eligibility: &DateRange{Start: "2026-01-01"}},
	})
	b.Deductible = append(b.Deductible,
		Benefit{Amount: money(dedIndividual), CoverageLevel: "INDIVIDUAL", InsuranceType: insType, TimePeriod: "CALENDAR_YEAR",
			Network: &Network{Indicator: "IN_NETWORK"}, Service: &ServiceRef{Value: "35", System: "STC", Definition: "Dental Care"}},
		Benefit{Amount: money(dedRemaining), CoverageLevel: "INDIVIDUAL", InsuranceType: insType, TimePeriod: "REMAINING",
			Network: &Network{Indicator: "IN_NETWORK"}, Service: &ServiceRef{Value: "35", System: "STC", Definition: "Dental Care"}},
	)
	for _, stc := range []string{"41", "23", "25", "26", "24", "40", "36", "39", "38"} {
		share, ok := cover[stc]
		if !ok {
			continue
		}
		b.CoInsurance = append(b.CoInsurance, Benefit{
			Percent: fmt.Sprintf("%.2f", share), CoverageLevel: "INDIVIDUAL", InsuranceType: insType,
			Network: &Network{Indicator: "IN_NETWORK"},
			Service: &ServiceRef{Value: stc, System: "STC", Definition: STCLabels[stc]},
		})
	}
	for _, stc := range nonCovered {
		b.NonCovered = append(b.NonCovered, Benefit{
			CoverageLevel: "INDIVIDUAL", InsuranceType: insType,
			Network:  &Network{Indicator: "IN_AND_OUT_OF_NETWORK"},
			Service:  &ServiceRef{Value: stc, System: "STC", Definition: STCLabels[stc]},
			Messages: []string{"SERVICE NOT COVERED UNDER THIS PLAN"},
		})
	}
	b.Limitations = append(b.Limitations, Benefit{
		Amount: money(annualMax), CoverageLevel: "INDIVIDUAL", InsuranceType: insType, TimePeriod: "CALENDAR_YEAR",
		Network: &Network{Indicator: "IN_NETWORK"}, Service: &ServiceRef{Value: "35", System: "STC", Definition: "Dental Care"},
		Messages: []string{"ANNUAL MAXIMUM"},
	})
	b.Limitations = append(b.Limitations, Benefit{
		CoverageLevel: "INDIVIDUAL", Quantity: &Quantity{Qualifier: "VISITS", Value: "2"}, TimePeriod: "CALENDAR_YEAR",
		Service:  &ServiceRef{Value: "41", System: "STC", Definition: STCLabels["41"]},
		Messages: []string{"2 CLEANINGS PER CALENDAR YEAR"},
	})
	b.BenefitDisclaimer = append(b.BenefitDisclaimer, Benefit{
		CoverageLevel: "INDIVIDUAL", Network: &Network{Indicator: "IN_AND_OUT_OF_NETWORK"},
		Messages: []string{"THIS IS ONLY AN ESTIMATION OF BENEFITS. ALL PAYMENTS ARE SUBJECT TO POLICY GUIDELINES AND MEMBER ELIGIBILITY AT THE TIME SERVICES ARE PERFORMED."},
	})
	return &Response{
		ID: "ec_mock_" + fmt.Sprintf("%08x", rand.Uint32()), EligibilitySearchID: fmt.Sprintf("%08x-mock", rand.Uint32()), PayerID: req.PayerID,
		Payer:      &PayerInfo{Identification: req.PayerID, Name: Name{Organization: payerName}},
		Subscriber: ResponseSubscriber{Name: req.Subscriber.Name, MemberID: req.Subscriber.MemberID, DateOfBirth: req.Subscriber.DateOfBirth},
		Plans:      []Plan{{Name: planName, Benefits: b}},
	}
}

func money(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}
