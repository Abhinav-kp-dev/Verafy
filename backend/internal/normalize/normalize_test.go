package normalize

import (
	"context"
	"testing"

	"coveragecheck/internal/stedi"
)

// Live payers (e.g. UnitedHealthcare) report benefits per CDT code rather than per STC.
func TestCDTLevelBenefits(t *testing.T) {
	pct := func(s string) string { return s }
	r := &stedi.Response{Plans: []stedi.Plan{{Name: "UHC Dental PPO", Benefits: stedi.Benefits{
		Statuses: []stedi.Benefit{{Status: "ACTIVE_COVERAGE", Service: &stedi.ServiceRef{System: "STC", Value: "35"}}},
		CoInsurance: []stedi.Benefit{
			{Percent: pct("0"), CoverageLevel: "INDIVIDUAL", Network: &stedi.Network{Indicator: "IN_NETWORK"}, Service: &stedi.ServiceRef{System: "CDT", Value: "D1110"}},
			{Percent: pct("0.2"), CoverageLevel: "INDIVIDUAL", Network: &stedi.Network{Indicator: "OUT_OF_NETWORK"}, Service: &stedi.ServiceRef{System: "CDT", Value: "D1110"}},
			{Percent: pct("0.5"), CoverageLevel: "INDIVIDUAL", Network: &stedi.Network{Indicator: "IN_NETWORK"}, Service: &stedi.ServiceRef{System: "CDT", Value: "D2740"}},
		},
		NonCovered: []stedi.Benefit{
			{Service: &stedi.ServiceRef{System: "CDT", Value: "D2950"}},
			{Service: &stedi.ServiceRef{System: "STC", Value: "38", Definition: "Orthodontics"}},
		},
	}}}}
	f := Extract(r)
	if f.EligibilityStatus != "active" {
		t.Fatalf("status %s", f.EligibilityStatus)
	}
	if c, ok := f.ByProcedure["D1110"]; !ok || !c.Covered || *c.PlanPaysPct != 100 {
		t.Fatalf("D1110 procedure-level coverage wrong: %+v", c)
	}
	if c, ok := f.ByProcedure["D2740"]; !ok || *c.PlanPaysPct != 50 {
		t.Fatalf("D2740 wrong: %+v", c)
	}
	if c, ok := f.ByProcedure["D2950"]; !ok || c.Covered {
		t.Fatalf("D2950 should be non-covered at procedure level: %+v", c)
	}
	// category 36 (crowns) inherits the D2740 rate and is NOT blanked by the D2950 exclusion
	var crowns *Category
	for i := range f.Categories {
		if f.Categories[i].STC == "36" {
			crowns = &f.Categories[i]
		}
	}
	if crowns == nil || !crowns.Covered || *crowns.PlanPaysPct != 50 {
		t.Fatalf("crown category should be covered at 50%%: %+v", crowns)
	}
	// no garbage "STC Dxxxx" categories
	for _, c := range f.Categories {
		if len(c.STC) > 2 {
			t.Fatalf("CDT code leaked into categories: %+v", c)
		}
	}
	// STC-level ortho exclusion still flags a gap
	if !f.HasGap || !contains(f.Flags, "orthodontics_not_covered") {
		t.Fatalf("ortho gap missing: %v", f.Flags)
	}
	for _, fl := range f.Flags {
		if len(fl) > len("category_not_covered:") && fl[:len("category_not_covered:")] == "category_not_covered:" && fl[len("category_not_covered:")] == 'D' {
			t.Fatalf("CDT-level exclusion leaked into category flags: %s", fl)
		}
	}
}

// Mock (STC-level) responses must behave exactly as before.
func TestSTCLevelUnchanged(t *testing.T) {
	resp, _, _ := (&stedi.MockClient{}).Check(context.Background(), stedi.Request{PayerID: "x", Subscriber: stedi.Subscriber{MemberID: "AFK987654321", Name: stedi.Name{Person: &stedi.PersonName{FirstName: "A", LastName: "B"}}}})
	f := Extract(resp)
	if len(f.ByProcedure) != 0 {
		t.Fatalf("mock has no CDT entries; got %v", f.ByProcedure)
	}
	if !f.HasGap || len(f.Categories) != 9 {
		t.Fatalf("categories=%d gap=%v", len(f.Categories), f.HasGap)
	}
}
