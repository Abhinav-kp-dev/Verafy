package voiceagent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }
func b(v bool) *bool       { return &v }

func TestValidateRejectsUnconfirmedOrImplausible(t *testing.T) {
	cases := map[string]Extracted{
		"unknown status":             {EligibilityStatus: "unknown", DeductibleAnnual: f(50)},
		"empty status":               {DeductibleAnnual: f(50)},
		"garbage status":             {EligibilityStatus: "maybe", DeductibleAnnual: f(50)},
		"negative money":             {EligibilityStatus: "active", DeductibleAnnual: f(-5)},
		"absurd money":               {EligibilityStatus: "active", AnnualMaximum: f(5_000_000)},
		"pct over 100":               {EligibilityStatus: "active", BasicPct: f(180)},
		"remaining > annual":         {EligibilityStatus: "active", DeductibleAnnual: f(50), DeductibleRemaining: f(75)},
		"active with no detail":      {EligibilityStatus: "active", RepName: "Dana"},
		"annual max remaining > max": {EligibilityStatus: "active", AnnualMaximum: f(1000), AnnualMaxRemaining: f(1200)},
	}
	for name, ex := range cases {
		if err := Validate(ex); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestValidateAcceptsRealisticCalls(t *testing.T) {
	ok := []Extracted{
		{EligibilityStatus: "active", DeductibleAnnual: f(50), DeductibleRemaining: f(0), AnnualMaximum: f(1500), PreventivePct: f(100), BasicPct: f(80), MajorPct: f(50)},
		{EligibilityStatus: "inactive"}, // inactive needs no benefit detail
		{EligibilityStatus: "Active ", PreventivePct: f(100)},
	}
	for i, ex := range ok {
		if err := Validate(ex); err != nil {
			t.Errorf("case %d: unexpected rejection: %v", i, err)
		}
	}
}

func TestToFactsMirrorsEDIShape(t *testing.T) {
	ex := Extracted{EligibilityStatus: "active", PlanName: "Delta PPO", DeductibleAnnual: f(50), DeductibleRemaining: f(25), AnnualMaximum: f(1500),
		PreventivePct: f(100), BasicPct: f(80), MajorPct: f(50), OrthoCovered: b(false), ReferenceNumber: "REF-1", RepName: "Dana"}
	facts := ToFacts(ex, "Delta Dental", "ai_voice_call")
	if facts.EligibilityStatus != "active" || facts.PayerName != "Delta Dental" || facts.PlanName != "Delta PPO" {
		t.Fatalf("header fields wrong: %+v", facts)
	}
	if !facts.HasGap {
		t.Error("ortho not covered on an active plan must flag a gap, exactly like the 271 path")
	}
	if len(facts.Categories) != 4 || facts.Categories[0].STC != "41" || facts.Categories[3].STC != "38" || facts.Categories[3].Covered {
		t.Errorf("categories: %+v", facts.Categories)
	}
	if *facts.Categories[1].PlanPaysPct != 80 || *facts.Categories[1].PatientPct != 20 {
		t.Errorf("basic coinsurance split wrong: %+v", facts.Categories[1])
	}
	want := map[float64]bool{50: true, 25: true, 1500: true, 100: true, 0: true, 80: true, 20: true}
	for _, n := range facts.Numbers {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("Numbers inventory missing %v (brief validator would reject the LLM output)", want)
	}
	if !contains(facts.Flags, "source_ai_voice_call") || !contains(facts.Flags, "deductible_not_met") || !contains(facts.Flags, "orthodontics_not_covered") {
		t.Errorf("flags: %v", facts.Flags)
	}
}

func TestToFactsInventoriesNumbersFromFreeText(t *testing.T) {
	ex := Extracted{EligibilityStatus: "active", PreventivePct: f(100), OrthoCovered: b(true), OrthoPct: f(50),
		Limitations: []string{"Orthodontic lifetime maximum $1,500", "Dependents to age 19"}, WaitingPeriod: "12 months on major"}
	facts := ToFacts(ex, "P", "ai_voice_call")
	for _, want := range []float64{1500, 19, 12} {
		found := false
		for _, n := range facts.Numbers {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%v stated by the rep is missing from Numbers; the brief validator would wrongly reject it", want)
		}
	}
}

func TestToFactsInactiveIsAGap(t *testing.T) {
	facts := ToFacts(Extracted{EligibilityStatus: "inactive"}, "X", "ai_voice_call")
	if !facts.HasGap || !contains(facts.Flags, "coverage_inactive") {
		t.Errorf("inactive must be a gap: %+v", facts)
	}
}

func TestVerifySignature(t *testing.T) {
	key := "key_test_123"
	body := []byte(`{"event":"call_ended","call":{"call_id":"abc"}}`)
	now := time.Now()
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	mac.Write([]byte(ts))
	good := "v=" + ts + ",d=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(body, key, good, now) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature(body, "other_key", good, now) {
		t.Error("wrong key accepted")
	}
	if VerifySignature([]byte(`{"event":"call_ended"}`), key, good, now) {
		t.Error("tampered body accepted")
	}
	if VerifySignature(body, key, good, now.Add(6*time.Minute)) {
		t.Error("stale timestamp accepted (replay)")
	}
	if VerifySignature(body, key, "", now) || VerifySignature(body, "", good, now) {
		t.Error("empty header/key accepted")
	}
}

func TestMockScenarioIsDeterministic(t *testing.T) {
	if mockScenario("ABC120") != "no_answer" || mockScenario("ABC129") != "ivr_dead_end" || mockScenario("ABC125") != "inactive" || mockScenario("ABC121") != "active" {
		t.Error("mock scenario suffix rules changed; update docs")
	}
	for _, id := range []string{"ABC121", "X5551", "DD00007"} {
		if err := Validate(mockFacts(CallRequest{MemberID: id, PayerName: "P"}, "active")); err != nil {
			t.Errorf("mock facts for %s fail our own validator: %v", id, err)
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

func TestParseBolnaFunctionCallToleratesStringSubstitution(t *testing.T) {
	raw := []byte(`{"execution_id":"exec-1","job_id":"job-1","eligibility_status":"Active","plan_name":"null",
	  "deductible_annual":"50","deductible_remaining":"$25","annual_maximum":"1,500","preventive_pct":"100%","basic_pct":80,
	  "orthodontics_covered":"no","limitations":"Lifetime max $1,500; Dependents to age 19","reference_number":"REF-9","notes":"N/A"}`)
	fc, err := ParseBolnaFunctionCall(raw)
	if err != nil {
		t.Fatal(err)
	}
	e := fc.Extracted
	if fc.ExecutionID != "exec-1" || fc.JobID != "job-1" || e.EligibilityStatus != "Active" || e.PlanName != "" || e.Notes != "" {
		t.Errorf("identifiers/strings: %+v", fc)
	}
	if *e.DeductibleAnnual != 50 || *e.DeductibleRemaining != 25 || *e.AnnualMaximum != 1500 || *e.PreventivePct != 100 || *e.BasicPct != 80 {
		t.Errorf("numbers: %+v", e)
	}
	if e.OrthoCovered == nil || *e.OrthoCovered || e.MajorPct != nil {
		t.Errorf("bool/nil handling: %+v", e)
	}
	if len(e.Limitations) != 2 || e.Limitations[0] != "Lifetime max $1,500" {
		t.Errorf("limitations: %v", e.Limitations)
	}
	if err := Validate(e); err != nil {
		t.Errorf("parsed call should validate: %v", err)
	}
}

func TestBolnaTerminalAndOutcome(t *testing.T) {
	for _, st := range []string{"queued", "ringing", "in-progress", "call-disconnected"} {
		if BolnaTerminal(st) {
			t.Errorf("%s must not be terminal", st)
		}
	}
	for _, st := range []string{"completed", "no-answer", "busy", "failed", "error"} {
		if !BolnaTerminal(st) {
			t.Errorf("%s must be terminal", st)
		}
	}
	if o, _ := OutcomeFromBolna(BolnaExecution{Status: "no-answer"}); o != OutcomeNoAnswer {
		t.Error("no-answer outcome")
	}
	if o, _ := OutcomeFromBolna(BolnaExecution{Status: "completed", Voicemail: true}); o != OutcomeNoAnswer {
		t.Error("voicemail outcome")
	}
	if o, _ := OutcomeFromBolna(BolnaExecution{Status: "completed"}); o != OutcomeDisconnected {
		t.Error("completed without facts must be a disconnect")
	}
	if !CheckSecret("abc", "abc") || CheckSecret("abc", "abd") || CheckSecret("", "") {
		t.Error("CheckSecret")
	}
}
