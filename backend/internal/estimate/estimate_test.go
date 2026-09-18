package estimate

import (
	"context"
	"testing"

	"coveragecheck/internal/normalize"
	"coveragecheck/internal/stedi"
)

func facts(t *testing.T, member string) *normalize.Facts {
	t.Helper()
	resp, _, err := (&stedi.MockClient{}).Check(context.Background(), stedi.Request{PayerID: "x", Subscriber: stedi.Subscriber{MemberID: member, Name: stedi.Name{Person: &stedi.PersonName{FirstName: "A", LastName: "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return normalize.Extract(resp)
}

func TestSTCMapping(t *testing.T) {
	cases := map[string]string{"D1110": "41", "D0120": "23", "D2140": "25", "D2740": "36", "D3310": "26", "D4341": "24", "D6010": "39", "D7140": "40", "D8080": "38", "bogus": "35"}
	for code, want := range cases {
		if got := STCForCDT(code); got != want {
			t.Errorf("%s -> %s, want %s", code, got, want)
		}
	}
}

// Ameritas mock: active, $0 deductible remaining, $1500 annual max, preventive 100%, restorative 80%, crowns 50%.
func TestDeductibleMetSimpleCoinsurance(t *testing.T) {
	r := Compute(facts(t, "007007007"), []ProcedureInput{{Code: "D1110", Description: "Cleaning", FeeCents: 12000}, {Code: "D2140", Description: "Filling", FeeCents: 22000}})
	if r.PatientPaysCents != 0+4400 || r.InsurancePaysCents != 12000+17600 {
		t.Fatalf("patient=%d insurance=%d", r.PatientPaysCents, r.InsurancePaysCents)
	}
	if r.Lines[0].DeductibleApplied != 0 || r.Lines[1].CoinsuranceCents != 4400 {
		t.Fatalf("line detail wrong: %+v", r.Lines)
	}
}

// MetLife mock: $50 deductible remaining. Deductible applies to the first covered charge, then coinsurance.
func TestDeductibleAppliedFirst(t *testing.T) {
	r := Compute(facts(t, "88877788"), []ProcedureInput{{Code: "D2140", Description: "Filling", FeeCents: 22000}})
	li := r.Lines[0]
	// fee 220: deductible 50 -> 170 remaining; plan 80% of 170 = 136; patient = 50 + 34 = 84
	if li.DeductibleApplied != 5000 || li.CoinsuranceCents != 3400 || li.PatientPays != 8400 || li.InsurancePays != 13600 {
		t.Fatalf("got %+v", li)
	}
	if r.DeductibleUsedCents != 5000 {
		t.Fatalf("deductible used = %d", r.DeductibleUsedCents)
	}
}

// Deductible is consumed once across lines, not once per line.
func TestDeductibleConsumedAcrossLines(t *testing.T) {
	r := Compute(facts(t, "88877788"), []ProcedureInput{{Code: "D2140", FeeCents: 3000}, {Code: "D2150", FeeCents: 25000}})
	if r.Lines[0].DeductibleApplied != 3000 || r.Lines[1].DeductibleApplied != 2000 {
		t.Fatalf("deductible split wrong: %d / %d", r.Lines[0].DeductibleApplied, r.Lines[1].DeductibleApplied)
	}
}

// Cigna DHMO mock: annual max $1000. A $1200 crown at 40% plan-pays is under max, but stack crowns to exceed it.
func TestAnnualMaximumCap(t *testing.T) {
	f := facts(t, "U3141592653") // $0 deductible, crowns 40%, max $1000
	procs := []ProcedureInput{}
	for i := 0; i < 3; i++ {
		procs = append(procs, ProcedureInput{Code: "D2740", Description: "Crown", FeeCents: 120000})
	}
	r := Compute(f, procs)
	// plan would pay 480 per crown = 1440 total; capped at 1000
	if r.InsurancePaysCents != 100000 {
		t.Fatalf("insurance should be capped at annual max: %d", r.InsurancePaysCents)
	}
	if r.PatientPaysCents != 360000-100000 {
		t.Fatalf("patient = %d", r.PatientPaysCents)
	}
	if r.Lines[2].OverAnnualMax == 0 || !contains(r.Flags, "annual_maximum_exceeded") {
		t.Fatalf("expected annual max flag; lines=%+v flags=%v", r.Lines, r.Flags)
	}
}

// BCBSCA mock: orthodontics non-covered -> patient pays 100% of ortho line.
func TestNotCoveredCategory(t *testing.T) {
	r := Compute(facts(t, "AFK987654321"), []ProcedureInput{{Code: "D8080", Description: "Braces", FeeCents: 450000}, {Code: "D1110", FeeCents: 12000}})
	if r.Lines[0].Covered || r.Lines[0].PatientPays != 450000 {
		t.Fatalf("ortho should be fully patient: %+v", r.Lines[0])
	}
	// BCBSCA has $25 deductible remaining: cleaning (100% plan-pays) still absorbs the deductible first.
	if !r.Lines[1].Covered || r.Lines[1].DeductibleApplied != 2500 || r.Lines[1].CoinsuranceCents != 0 || r.Lines[1].PatientPays != 2500 {
		t.Fatalf("cleaning should be covered with only the deductible owed: %+v", r.Lines[1])
	}
}

func TestInactiveAndNilFacts(t *testing.T) {
	r := Compute(facts(t, "INACTIVE001"), []ProcedureInput{{Code: "D1110", FeeCents: 12000}})
	if r.PatientPaysCents != 12000 || r.InsurancePaysCents != 0 {
		t.Fatalf("inactive: %+v", r)
	}
	r = Compute(nil, []ProcedureInput{{Code: "D1110", FeeCents: 12000}})
	if r.PatientPaysCents != 12000 || !contains(r.Flags, "no_eligibility_data") {
		t.Fatalf("nil facts: %+v", r)
	}
}

func TestTotalsAlwaysReconcile(t *testing.T) {
	for _, m := range []string{"007007007", "88877788", "U3141592653", "AFK987654321", "INACTIVE001"} {
		r := Compute(facts(t, m), []ProcedureInput{{Code: "D0120", FeeCents: 6000}, {Code: "D2740", FeeCents: 120000}, {Code: "D8080", FeeCents: 450000}})
		if r.InsurancePaysCents+r.PatientPaysCents != r.TotalFeeCents {
			t.Fatalf("%s: %d + %d != %d", m, r.InsurancePaysCents, r.PatientPaysCents, r.TotalFeeCents)
		}
		for _, li := range r.Lines {
			if li.PatientPays < 0 || li.PatientPays > li.FeeCents || li.InsurancePays < 0 {
				t.Fatalf("%s: out-of-range line %+v", m, li)
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
