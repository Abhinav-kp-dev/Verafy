// Package normalize deterministically extracts coverage facts from a 271 response.
// No model is involved here: every number in Facts is traceable to a specific field
// in the raw Stedi payload.
package normalize

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"coveragecheck/internal/stedi"
)

type Category struct {
	STC         string   `json:"stc"`
	Label       string   `json:"label"`
	PlanPaysPct *float64 `json:"planPaysPct"`         // 100 - patient coinsurance
	PatientPct  *float64 `json:"patientCoinsurancePct"` // raw EB08 as percent
	Network     string   `json:"network,omitempty"`
	Covered     bool     `json:"covered"`
	Note        string   `json:"note,omitempty"`
}

// cdtRanges maps ADA CDT code ranges to the X12 service type code of their category.
var cdtRanges = []struct {
	lo, hi int
	stc    string
}{
	{100, 999, "23"}, {1000, 1999, "41"}, {2000, 2699, "25"}, {2700, 2999, "36"}, {3000, 3999, "26"},
	{4000, 4999, "24"}, {5000, 6999, "39"}, {7000, 7999, "40"}, {8000, 8999, "38"}, {9000, 9999, "28"},
}

// STCForCDT maps a CDT procedure code (e.g. "D2740") to its benefit category STC.
func STCForCDT(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	if len(c) != 5 || c[0] != 'D' {
		return "35"
	}
	n := 0
	for _, ch := range c[1:] {
		if ch < '0' || ch > '9' {
			return "35"
		}
		n = n*10 + int(ch-'0')
	}
	for _, r := range cdtRanges {
		if n >= r.lo && n <= r.hi {
			return r.stc
		}
	}
	return "35"
}

type Facts struct {
	EligibilityStatus string      `json:"eligibilityStatus"` // active | inactive | unknown
	StatusCode        string      `json:"statusCode,omitempty"`
	PlanName          string      `json:"planName,omitempty"`
	PayerName         string      `json:"payerName,omitempty"`
	InsuranceType     string      `json:"insuranceType,omitempty"`
	PlanStart         string      `json:"planStart,omitempty"`
	PlanEnd           string      `json:"planEnd,omitempty"`
	DeductibleAnnual  *float64    `json:"deductibleAnnual"`
	DeductibleRemain  *float64    `json:"deductibleRemaining"`
	AnnualMaximum     *float64    `json:"annualMaximum"`
	CopayOffice       *float64    `json:"copayOffice"`
	Categories        []Category  `json:"categories"`
	ByProcedure       map[string]Category `json:"coverageByProcedure,omitempty"` // CDT-level entries when the payer reports them
	NonCovered        []string    `json:"nonCoveredLabels"`
	Limitations       []string    `json:"limitations"`
	Flags             []string    `json:"flags"`
	Messages          []string    `json:"payerMessages"`
	HasGap            bool        `json:"hasGap"`
	Numbers           []float64   `json:"-"` // every numeric value present; used to validate LLM output
}

// Categories that matter to a front desk, in display order.
var coreDental = []string{"41", "23", "25", "26", "24", "40", "36", "39", "38"}

func Extract(r *stedi.Response) *Facts {
	f := &Facts{Flags: []string{}, NonCovered: []string{}, Limitations: []string{}, Messages: []string{}, Categories: []Category{}, ByProcedure: map[string]Category{}}
	if r.Payer != nil {
		f.PayerName = r.Payer.Name.Organization
	}
	if len(r.Plans) == 0 {
		f.EligibilityStatus = "unknown"
		f.Flags = append(f.Flags, "no_plan_information")
		return f
	}
	plan := r.Plans[0]
	f.PlanName = plan.Name
	b := plan.Benefits

	// --- status: prefer the dental (35) or plan-level (30) status entry ---
	f.EligibilityStatus = "unknown"
	for _, s := range pickPreferred(b.Statuses, []string{"35", "30", ""}) {
		st := strings.ToUpper(s.Status)
		switch {
		case strings.HasPrefix(st, "ACTIVE"):
			f.EligibilityStatus = "active"
		case strings.HasPrefix(st, "INACTIVE"):
			f.EligibilityStatus = "inactive"
		}
		f.StatusCode = s.Status
		if s.InsuranceType != "" {
			f.InsuranceType = s.InsuranceType
		}
		if s.Dates != nil && s.Dates.Plan != nil {
			f.PlanStart, f.PlanEnd = s.Dates.Plan.Start, s.Dates.Plan.End
		}
		f.Messages = append(f.Messages, s.Messages...)
		break
	}
	if f.EligibilityStatus == "inactive" {
		f.Flags = append(f.Flags, "coverage_inactive")
		f.HasGap = true
	}
	if f.EligibilityStatus == "unknown" {
		f.Flags = append(f.Flags, "status_not_reported")
	}

	// --- deductible ---
	for _, d := range b.Deductible {
		if !individualInNetwork(d) {
			continue
		}
		v, ok := num(d.Amount)
		if !ok {
			continue
		}
		switch d.TimePeriod {
		case "REMAINING":
			f.DeductibleRemain = ptr(v)
		case "CALENDAR_YEAR", "SERVICE_YEAR", "YEAR_TO_DATE", "CONTRACT", "":
			if f.DeductibleAnnual == nil {
				f.DeductibleAnnual = ptr(v)
			}
		}
	}
	if f.DeductibleRemain == nil && f.DeductibleAnnual != nil {
		f.DeductibleRemain = f.DeductibleAnnual
		f.Flags = append(f.Flags, "deductible_remaining_assumed_full")
	}
	if f.DeductibleRemain != nil && *f.DeductibleRemain > 0 {
		f.Flags = append(f.Flags, "deductible_not_met")
	}

	// --- copay (office visit / general) ---
	for _, c := range b.CoPayment {
		if v, ok := num(c.Amount); ok && individualInNetwork(c) {
			f.CopayOffice = ptr(v)
			break
		}
	}

	// --- coinsurance by category ---
	byStc := map[string]Category{}
	for _, c := range b.CoInsurance {
		if c.Service == nil || (c.Service.System != "STC" && c.Service.System != "CDT") {
			continue
		}
		if c.Network != nil && c.Network.Indicator == "OUT_OF_NETWORK" {
			continue
		}
		share, ok := num(c.Percent)
		if !ok {
			continue
		}
		if share <= 1 { // decimal form (0.20) -> percent
			share = share * 100
		}
		share = math.Round(share)
		plan := 100 - share
		if c.Service.System == "CDT" {
			code := strings.ToUpper(c.Service.Value)
			if _, exists := f.ByProcedure[code]; exists {
				continue
			}
			stc := STCForCDT(code)
			f.ByProcedure[code] = Category{STC: stc, Label: label(stc, ""), PlanPaysPct: ptr(plan), PatientPct: ptr(share), Covered: true, Network: netLabel(c.Network), Note: code}
			// The category inherits the first in-network CDT rate seen when no STC-level entry exists.
			if _, exists := byStc[stc]; !exists {
				byStc[stc] = Category{STC: stc, Label: label(stc, ""), PlanPaysPct: ptr(plan), PatientPct: ptr(share), Covered: true, Network: netLabel(c.Network), Note: "from procedure-level benefits"}
			}
			continue
		}
		stc := c.Service.Value
		if prev, exists := byStc[stc]; exists && prev.Note != "from procedure-level benefits" {
			continue
		}
		byStc[stc] = Category{STC: stc, Label: label(stc, c.Service.Definition), PlanPaysPct: ptr(plan), PatientPct: ptr(share), Covered: true, Network: netLabel(c.Network)}
	}
	nonCov := map[string]bool{}
	for _, n := range b.NonCovered {
		if n.Service == nil {
			continue
		}
		if n.Service.System == "CDT" {
			// Procedure-level exclusion: record it, but don't blank out the whole category.
			code := strings.ToUpper(n.Service.Value)
			f.ByProcedure[code] = Category{STC: STCForCDT(code), Label: label(STCForCDT(code), ""), Covered: false, Note: "Not covered under this plan"}
			continue
		}
		nonCov[n.Service.Value] = true
		lbl := label(n.Service.Value, n.Service.Definition)
		f.NonCovered = append(f.NonCovered, lbl)
		byStc[n.Service.Value] = Category{STC: n.Service.Value, Label: lbl, Covered: false, Note: "Not covered under this plan"}
	}
	for _, stc := range coreDental {
		if c, ok := byStc[stc]; ok {
			f.Categories = append(f.Categories, c)
		}
	}
	// any extra STCs the payer reported, after the core ones
	extra := []string{}
	for stc := range byStc {
		if !contains(coreDental, stc) {
			extra = append(extra, stc)
		}
	}
	sort.Strings(extra)
	for _, stc := range extra {
		f.Categories = append(f.Categories, byStc[stc])
	}
	if f.EligibilityStatus == "active" {
		if nonCov["38"] {
			f.Flags = append(f.Flags, "orthodontics_not_covered")
			f.HasGap = true
		} else if _, ok := byStc["38"]; !ok {
			f.Flags = append(f.Flags, "orthodontics_not_reported")
		}
		for stc := range nonCov {
			if stc != "38" {
				f.Flags = append(f.Flags, "category_not_covered:"+stc)
				f.HasGap = true
			}
		}
		if len(byStc) == 0 {
			f.Flags = append(f.Flags, "no_benefit_detail")
		}
	}

	// --- limitations / annual max ---
	for _, l := range b.Limitations {
		if v, ok := num(l.Amount); ok && l.Service != nil && (l.Service.Value == "35" || l.Service.Value == "30") && f.AnnualMaximum == nil {
			f.AnnualMaximum = ptr(v)
		}
		if len(l.Messages) > 0 {
			f.Limitations = append(f.Limitations, strings.Join(l.Messages, "; "))
		} else if l.Quantity != nil && l.Service != nil {
			f.Limitations = append(f.Limitations, l.Quantity.Value+" "+strings.ToLower(l.Quantity.Qualifier)+" — "+label(l.Service.Value, l.Service.Definition))
		}
	}

	// --- numeric inventory for LLM validation ---
	for _, p := range []*float64{f.DeductibleAnnual, f.DeductibleRemain, f.AnnualMaximum, f.CopayOffice} {
		if p != nil {
			f.Numbers = append(f.Numbers, *p)
		}
	}
	for _, c := range f.Categories {
		if c.PlanPaysPct != nil {
			f.Numbers = append(f.Numbers, *c.PlanPaysPct, *c.PatientPct)
		}
	}
	return f
}

// pickPreferred returns status entries ordered by STC preference.
func pickPreferred(list []stedi.Benefit, prefs []string) []stedi.Benefit {
	out := []stedi.Benefit{}
	for _, p := range prefs {
		for _, s := range list {
			v := ""
			if s.Service != nil {
				v = s.Service.Value
			}
			if v == p || (p == "" && v != "") {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return list
}

func individualInNetwork(b stedi.Benefit) bool {
	if b.CoverageLevel != "" && b.CoverageLevel != "INDIVIDUAL" && b.CoverageLevel != "EMPLOYEE_ONLY" {
		return false
	}
	if b.Network != nil && b.Network.Indicator == "OUT_OF_NETWORK" {
		return false
	}
	return true
}

func netLabel(n *stedi.Network) string {
	if n == nil {
		return ""
	}
	switch n.Indicator {
	case "IN_NETWORK":
		return "in-network"
	case "OUT_OF_NETWORK":
		return "out-of-network"
	case "IN_AND_OUT_OF_NETWORK":
		return "in & out of network"
	}
	return ""
}

func label(stc, def string) string {
	if l, ok := stedi.STCLabels[stc]; ok {
		return l
	}
	if def != "" {
		return def
	}
	return "STC " + stc
}

func num(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func ptr(v float64) *float64 { return &v }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
