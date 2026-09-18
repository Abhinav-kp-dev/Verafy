package llm

import (
	"context"
	"strings"
	"testing"

	"coveragecheck/internal/normalize"
	"coveragecheck/internal/stedi"
)

func factsFor(t *testing.T, member string) *normalize.Facts {
	t.Helper()
	m := &stedi.MockClient{}
	resp, _, err := m.Check(context.Background(), stedi.Request{PayerID: "x", Subscriber: stedi.Subscriber{MemberID: member, Name: stedi.Name{Person: &stedi.PersonName{FirstName: "A", LastName: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return normalize.Extract(resp)
}

func TestExtractAmeritas(t *testing.T) {
	f := factsFor(t, "007007007")
	if f.EligibilityStatus != "active" {
		t.Fatalf("status = %s", f.EligibilityStatus)
	}
	if f.DeductibleRemain == nil || *f.DeductibleRemain != 0 || f.DeductibleAnnual == nil || *f.DeductibleAnnual != 50 {
		t.Fatalf("deductible wrong: %+v / %+v", f.DeductibleRemain, f.DeductibleAnnual)
	}
	if f.AnnualMaximum == nil || *f.AnnualMaximum != 1500 {
		t.Fatalf("annual max wrong")
	}
	if f.HasGap {
		t.Fatalf("Ameritas mock should have no gap")
	}
	var prev *normalize.Category
	for i := range f.Categories {
		if f.Categories[i].STC == "41" {
			prev = &f.Categories[i]
		}
	}
	if prev == nil || *prev.PlanPaysPct != 100 || *prev.PatientPct != 0 {
		t.Fatalf("preventive mapping wrong: %+v", prev)
	}
}

func TestExtractGapAndInactive(t *testing.T) {
	f := factsFor(t, "AFK987654321") // BCBSCA: orthodontics non-covered
	if !f.HasGap || !contains(f.Flags, "orthodontics_not_covered") {
		t.Fatalf("expected ortho gap, flags=%v", f.Flags)
	}
	f = factsFor(t, "INACTIVE001")
	if f.EligibilityStatus != "inactive" || !f.HasGap || !contains(f.Flags, "coverage_inactive") {
		t.Fatalf("inactive not detected: %s %v", f.EligibilityStatus, f.Flags)
	}
}

func TestTemplateBriefOnlyUsesSourceNumbers(t *testing.T) {
	f := factsFor(t, "007007007")
	b := Template(f)
	if reason := validate(b, f); reason != "" {
		t.Fatalf("template brief failed its own validation: %s", reason)
	}
	if !strings.Contains(b.Brief, "$1500 annual maximum") {
		t.Fatalf("brief missing annual max: %s", b.Brief)
	}
}

func TestValidateRejectsHallucinatedNumbers(t *testing.T) {
	f := factsFor(t, "007007007")
	good := Template(f)

	bad := *good
	bad.DeductibleRemaining = ptr(75) // not in source
	if validate(&bad, f) == "" {
		t.Fatal("expected rejection for invented deductible")
	}

	bad2 := *good
	bad2.Brief = "Active coverage. Preventive covered 95%." // 95 not in source
	if validate(&bad2, f) == "" {
		t.Fatal("expected rejection for invented percentage in prose")
	}

	bad3 := *good
	bad3.Status = "inactive"
	if validate(&bad3, f) == "" {
		t.Fatal("expected rejection for wrong status")
	}

	bad4 := *good
	bad4.CoveragePercentByCategory = map[string]*float64{"restorative": ptr(85)}
	if validate(&bad4, f) == "" {
		t.Fatal("expected rejection for invented category percentage")
	}
}

func TestGeneratorFallsBackWithoutKey(t *testing.T) {
	g := New("", "any")
	b := g.Generate(context.Background(), factsFor(t, "88877788"))
	if b.Source != "template" {
		t.Fatalf("source = %s", b.Source)
	}
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
