// Package estimate computes a patient's expected out-of-pocket cost for scheduled
// procedures from (a) the practice fee schedule and (b) the coverage facts already
// extracted from the payer's 271 response. Pure functions, no I/O.
//
// Order of operations per procedure (standard dental benefit adjudication):
//  1. Not covered / inactive       -> patient owes 100%
//  2. Deductible (remaining) first -> patient pays fee up to the remaining deductible
//  3. Coinsurance on the remainder -> patient pays their coinsurance share
//  4. Annual maximum               -> anything the plan would pay beyond the remaining
//                                     annual maximum falls back to the patient
//
// Every number is an estimate derived from the eligibility response. It is not a
// guarantee of payment — the same disclaimer real dental estimators carry.
package estimate

import (
	"math"
	"strings"

	"coveragecheck/internal/normalize"
)

// STCForCDT maps a CDT code to its benefit category STC (delegates to normalize).
func STCForCDT(code string) string { return normalize.STCForCDT(code) }

type ProcedureInput struct {
	Code        string
	Description string
	FeeCents    int64
}

type LineItem struct {
	Code              string  `json:"code"`
	Description       string  `json:"description"`
	Category          string  `json:"category"`
	FeeCents          int64   `json:"feeCents"`
	Covered           bool    `json:"covered"`
	PlanPaysPct       *float64 `json:"planPaysPct"`
	DeductibleApplied int64   `json:"deductibleAppliedCents"`
	CoinsuranceCents  int64   `json:"coinsuranceCents"`
	OverAnnualMax     int64   `json:"overAnnualMaxCents"`
	InsurancePays     int64   `json:"insurancePaysCents"`
	PatientPays       int64   `json:"patientPaysCents"`
	Note              string  `json:"note,omitempty"`
}

type Result struct {
	EligibilityStatus     string     `json:"eligibilityStatus"`
	PlanName              string     `json:"planName,omitempty"`
	TotalFeeCents         int64      `json:"totalFeeCents"`
	InsurancePaysCents    int64      `json:"insurancePaysCents"`
	PatientPaysCents      int64      `json:"patientPaysCents"`
	DeductibleStartCents  *int64     `json:"deductibleRemainingStartCents"`
	DeductibleUsedCents   int64      `json:"deductibleUsedCents"`
	AnnualMaxCents        *int64     `json:"annualMaximumCents"`
	Lines                 []LineItem `json:"lines"`
	Flags                 []string   `json:"flags"`
	Disclaimer            string     `json:"disclaimer"`
}

const Disclaimer = "This is an estimate based on the eligibility response from your insurance company and the practice's standard fees. It is not a guarantee of payment; final amounts depend on the claim your insurer processes after treatment."

func dollarsToCents(d *float64) *int64 {
	if d == nil {
		return nil
	}
	c := int64(math.Round(*d * 100))
	return &c
}

// Compute produces the estimate. Facts may be nil (unknown coverage) — every line
// then falls to the patient and the result is flagged.
func Compute(f *normalize.Facts, procs []ProcedureInput) Result {
	res := Result{Flags: []string{}, Disclaimer: Disclaimer, Lines: []LineItem{}}
	if f == nil {
		res.EligibilityStatus = "unknown"
		res.Flags = append(res.Flags, "no_eligibility_data")
	} else {
		res.EligibilityStatus = f.EligibilityStatus
		res.PlanName = f.PlanName
		res.DeductibleStartCents = dollarsToCents(f.DeductibleRemain)
		res.AnnualMaxCents = dollarsToCents(f.AnnualMaximum)
		res.Flags = append(res.Flags, f.Flags...)
	}

	active := f != nil && f.EligibilityStatus == "active"
	var dedLeft int64
	if res.DeductibleStartCents != nil {
		dedLeft = *res.DeductibleStartCents
	}
	var maxLeft int64 = math.MaxInt64
	if res.AnnualMaxCents != nil {
		maxLeft = *res.AnnualMaxCents
	}

	byStc := map[string]normalize.Category{}
	if f != nil {
		for _, c := range f.Categories {
			byStc[c.STC] = c
		}
	}

	for _, p := range procs {
		li := LineItem{Code: strings.ToUpper(strings.TrimSpace(p.Code)), Description: p.Description, FeeCents: p.FeeCents}
		stc := STCForCDT(li.Code)
		cat, known := byStc[stc]
		// Payer-reported procedure-level benefit wins over the category rate.
		if f != nil {
			if pc, ok := f.ByProcedure[li.Code]; ok {
				cat, known = pc, true
			}
		}
		if known {
			li.Category = cat.Label
		}
		res.TotalFeeCents += p.FeeCents

		switch {
		case !active:
			li.PatientPays = p.FeeCents
			li.Note = "Coverage not active — full fee is patient responsibility"
		case !known:
			li.PatientPays = p.FeeCents
			li.Note = "Payer did not report coverage for this category — treated as not covered"
			res.Flags = appendUnique(res.Flags, "category_not_reported:"+stc)
		case !cat.Covered || cat.PlanPaysPct == nil:
			li.PatientPays = p.FeeCents
			li.Note = "Not covered under this plan"
		default:
			li.Covered = true
			li.PlanPaysPct = cat.PlanPaysPct
			remaining := p.FeeCents
			// 1. deductible
			if dedLeft > 0 {
				d := min(dedLeft, remaining)
				li.DeductibleApplied = d
				dedLeft -= d
				remaining -= d
			}
			// 2. coinsurance
			planShare := int64(math.Round(float64(remaining) * (*cat.PlanPaysPct / 100)))
			li.CoinsuranceCents = remaining - planShare
			// 3. annual maximum
			if planShare > maxLeft {
				li.OverAnnualMax = planShare - maxLeft
				planShare = maxLeft
			}
			maxLeft -= planShare
			li.InsurancePays = planShare
			li.PatientPays = p.FeeCents - planShare
			if li.OverAnnualMax > 0 {
				li.Note = "Exceeds remaining annual maximum — excess is patient responsibility"
				res.Flags = appendUnique(res.Flags, "annual_maximum_exceeded")
			}
		}
		res.InsurancePaysCents += li.InsurancePays
		res.PatientPaysCents += li.PatientPays
		res.Lines = append(res.Lines, li)
	}
	if res.DeductibleStartCents != nil {
		res.DeductibleUsedCents = *res.DeductibleStartCents - dedLeft
	}
	return res
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
