package voiceagent

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"coveragecheck/internal/normalize"
)

// Validate is the completeness/plausibility gate for facts collected by phone. It is
// deliberately separate from the brief validator in internal/llm (which checks prose
// against facts) — this checks the facts themselves before they are trusted.
func Validate(e Extracted) error {
	status := strings.ToLower(strings.TrimSpace(e.EligibilityStatus))
	switch status {
	case "active", "inactive":
	case "unknown", "":
		return errors.New("eligibility status was not confirmed by the payer")
	default:
		return fmt.Errorf("unrecognized eligibility status %q", e.EligibilityStatus)
	}
	money := map[string]*float64{
		"deductible_annual": e.DeductibleAnnual, "deductible_remaining": e.DeductibleRemaining,
		"annual_maximum": e.AnnualMaximum, "annual_max_remaining": e.AnnualMaxRemaining, "copay_office": e.CopayOffice,
	}
	for k, v := range money {
		if v == nil {
			continue
		}
		if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 100000 {
			return fmt.Errorf("%s=%v is not a plausible dollar amount", k, *v)
		}
	}
	pcts := map[string]*float64{"preventive_pct": e.PreventivePct, "basic_pct": e.BasicPct, "major_pct": e.MajorPct, "orthodontics_pct": e.OrthoPct}
	for k, v := range pcts {
		if v != nil && (*v < 0 || *v > 100) {
			return fmt.Errorf("%s=%v is not a percentage", k, *v)
		}
	}
	if e.DeductibleAnnual != nil && e.DeductibleRemaining != nil && *e.DeductibleRemaining > *e.DeductibleAnnual+0.01 {
		return errors.New("deductible remaining exceeds the annual deductible")
	}
	if e.AnnualMaximum != nil && e.AnnualMaxRemaining != nil && *e.AnnualMaxRemaining > *e.AnnualMaximum+0.01 {
		return errors.New("annual max remaining exceeds the annual maximum")
	}
	if status == "active" && e.PreventivePct == nil && e.BasicPct == nil && e.MajorPct == nil && e.AnnualMaximum == nil && e.DeductibleAnnual == nil {
		return errors.New("active coverage reported but no benefit detail was collected")
	}
	return nil
}

// ToFacts maps phone-collected data into the same Facts the 271 normalizer produces,
// so the brief writer, PDF, estimates and dashboard treat both paths identically.
// Every number is inventoried in Facts.Numbers so the LLM brief validator still applies.
func ToFacts(e Extracted, payerName, sourceTag string) *normalize.Facts {
	if sourceTag == "" {
		sourceTag = "manual_channel"
	}
	f := &normalize.Facts{
		EligibilityStatus: strings.ToLower(strings.TrimSpace(e.EligibilityStatus)),
		PlanName:          strings.TrimSpace(e.PlanName),
		PayerName:         payerName,
		DeductibleAnnual:  e.DeductibleAnnual,
		DeductibleRemain:  e.DeductibleRemaining,
		AnnualMaximum:     e.AnnualMaximum,
		CopayOffice:       e.CopayOffice,
		Categories:        []normalize.Category{},
		NonCovered:        []string{},
		Limitations:       append([]string{}, e.Limitations...),
		Flags:             []string{"source_" + sourceTag},
		Messages:          []string{},
	}
	if f.EligibilityStatus == "inactive" {
		f.Flags = append(f.Flags, "coverage_inactive")
		f.HasGap = true
	}
	if f.DeductibleRemain == nil && f.DeductibleAnnual != nil {
		f.DeductibleRemain = f.DeductibleAnnual
		f.Flags = append(f.Flags, "deductible_remaining_assumed_full")
	}
	if f.DeductibleRemain != nil && *f.DeductibleRemain > 0 {
		f.Flags = append(f.Flags, "deductible_not_met")
	}
	if e.AnnualMaxRemaining != nil {
		f.Limitations = append(f.Limitations, fmt.Sprintf("$%s of annual maximum remaining", trimMoney(*e.AnnualMaxRemaining)))
	}
	if e.WaitingPeriod != "" {
		f.Limitations = append(f.Limitations, "Waiting period: "+e.WaitingPeriod)
	}

	cat := func(stc, label string, pct *float64) {
		if pct == nil {
			return
		}
		plan := math.Round(*pct)
		patient := 100 - plan
		f.Categories = append(f.Categories, normalize.Category{STC: stc, Label: label, PlanPaysPct: &plan, PatientPct: &patient, Covered: true, Network: "in-network", Note: "confirmed by phone"})
	}
	cat("41", "Routine (Preventive) Dental", e.PreventivePct)
	cat("25", "Restorative", e.BasicPct)
	cat("36", "Dental Crowns", e.MajorPct)
	switch {
	case e.OrthoCovered != nil && !*e.OrthoCovered:
		f.NonCovered = append(f.NonCovered, "Orthodontics")
		f.Categories = append(f.Categories, normalize.Category{STC: "38", Label: "Orthodontics", Covered: false, Note: "Not covered under this plan"})
		if f.EligibilityStatus == "active" {
			f.Flags = append(f.Flags, "orthodontics_not_covered")
			f.HasGap = true
		}
	case e.OrthoPct != nil:
		cat("38", "Orthodontics", e.OrthoPct)
	default:
		if f.EligibilityStatus == "active" {
			f.Flags = append(f.Flags, "orthodontics_not_reported")
		}
	}
	if f.EligibilityStatus == "active" && len(f.Categories) == 0 {
		f.Flags = append(f.Flags, "no_benefit_detail")
	}
	if e.ReferenceNumber != "" {
		f.Messages = append(f.Messages, "Payer call reference "+e.ReferenceNumber)
	}
	if e.RepName != "" {
		f.Messages = append(f.Messages, "Spoke with "+e.RepName)
	}
	if e.Notes != "" {
		f.Messages = append(f.Messages, e.Notes)
	}

	for _, p := range []*float64{f.DeductibleAnnual, f.DeductibleRemain, f.AnnualMaximum, f.CopayOffice, e.AnnualMaxRemaining} {
		if p != nil {
			f.Numbers = append(f.Numbers, *p)
		}
	}
	for _, c := range f.Categories {
		if c.PlanPaysPct != nil {
			f.Numbers = append(f.Numbers, *c.PlanPaysPct, *c.PatientPct)
		}
	}
	// Numbers the rep stated inside free text (lifetime maximums, age limits, frequencies)
	// are source numbers too; without them the brief validator rejects a correct brief.
	for _, txt := range append(append([]string{}, f.Limitations...), f.Messages...) {
		for _, tok := range numToken.FindAllString(txt, -1) {
			if v, err := strconv.ParseFloat(strings.NewReplacer("$", "", ",", "", "%", "").Replace(tok), 64); err == nil {
				f.Numbers = append(f.Numbers, v)
			}
		}
	}
	return f
}

var numToken = regexp.MustCompile(`\$?\d+(?:,\d{3})*(?:\.\d+)?%?`)

func trimMoney(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}
