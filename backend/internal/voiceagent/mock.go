package voiceagent

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Mock stands in for the telephony platform when VOICE_MODE=mock. It never pretends
// to be a real call in the data it produces (the transcript says so), but it drives
// the exact same Completer path a Retell webhook would, so state transitions, SSE,
// the brief pipeline and the UI are exercised for real.
type Mock struct {
	Delay     time.Duration
	Completer Completer // set after the queue exists
	Log       *slog.Logger
}

func (m *Mock) Mode() string { return "mock" }

func (m *Mock) PlaceCall(ctx context.Context, req CallRequest) (string, error) {
	if m.Completer == nil {
		return "", fmt.Errorf("mock voice client has no completer wired")
	}
	callID := "mock_" + uuid.NewString()
	delay := m.Delay
	if delay <= 0 {
		delay = 8 * time.Second
	}
	scenario := mockScenario(req.MemberID)
	go func() {
		// Detached from the worker's ctx on purpose: the River job returns as soon as
		// the call is dispatched, exactly like live mode.
		bg := context.Background()
		time.Sleep(delay)
		switch scenario {
		case "no_answer":
			_ = m.Completer.CompleteWithFailure(bg, callID, OutcomeNoAnswer, "Payer line did not answer (dial_no_answer)", mockTranscript(req, scenario, nil))
		case "ivr_dead_end":
			_ = m.Completer.CompleteWithFailure(bg, callID, OutcomeIVRDeadEnd, "IVR required a fax-back request; no live representative reached", mockTranscript(req, scenario, nil))
		default:
			facts := mockFacts(req, scenario)
			_ = m.Completer.CompleteWithFacts(bg, callID, facts, mockTranscript(req, scenario, &facts))
		}
	}()
	return callID, nil
}

// mockScenario is deterministic per member id so demos are repeatable.
// Suffix rules: ...0 -> no answer, ...9 -> IVR dead end, ...5 -> inactive, else active.
func mockScenario(memberID string) string {
	if memberID == "" {
		return "active"
	}
	switch memberID[len(memberID)-1] {
	case '0':
		return "no_answer"
	case '9':
		return "ivr_dead_end"
	case '5':
		return "inactive"
	}
	return "active"
}

func mockFacts(req CallRequest, scenario string) Extracted {
	h := fnv.New32a()
	h.Write([]byte(req.MemberID))
	seed := h.Sum32()
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }
	ref := fmt.Sprintf("REF-%06d", seed%1000000)
	if scenario == "inactive" {
		return Extracted{EligibilityStatus: "inactive", PlanName: req.PayerName + " PPO", ReferenceNumber: ref, RepName: "Marcus", Notes: "Coverage terminated at end of last month per representative."}
	}
	deductible := []float64{50, 50, 75, 100}[seed%4]
	remaining := []float64{0, 25, 50, deductible}[(seed/4)%4]
	if remaining > deductible {
		remaining = deductible
	}
	annualMax := []float64{1000, 1500, 2000}[(seed/16)%3]
	orthoCovered := seed%3 != 0
	e := Extracted{
		EligibilityStatus:   "active",
		PlanName:            req.PayerName + " PPO",
		DeductibleAnnual:    f(deductible),
		DeductibleRemaining: f(remaining),
		AnnualMaximum:       f(annualMax),
		AnnualMaxRemaining:  f(annualMax - float64((seed/64)%5)*100),
		PreventivePct:       f(100),
		BasicPct:            f(80),
		MajorPct:            f(50),
		OrthoCovered:        b(orthoCovered),
		ReferenceNumber:     ref,
		RepName:             []string{"Dana", "Priya", "Luis", "Kim"}[(seed/8)%4],
	}
	if orthoCovered {
		e.OrthoPct = f(50)
		e.Limitations = []string{"Orthodontic lifetime maximum $1,500", "Dependents to age 19"}
	}
	return e
}

func mockTranscript(req CallRequest, scenario string, e *Extracted) string {
	var sb strings.Builder
	w := func(who, line string) { fmt.Fprintf(&sb, "%s: %s\n", who, line) }
	w("System", "[VOICE_MODE=mock — this transcript is generated locally; no call was placed]")
	w("IVR", fmt.Sprintf("Thank you for calling %s provider services.", req.PayerName))
	switch scenario {
	case "no_answer":
		w("System", "Ring timeout after 45 seconds. No answer.")
		return sb.String()
	case "ivr_dead_end":
		w("IVR", "For eligibility and benefits, please submit a fax request to the number on the back of the member's card. Goodbye.")
		w("Agent", "Attempted to reach a representative by pressing 0 — menu repeated. No live agent available.")
		return sb.String()
	}
	w("Agent", fmt.Sprintf("Hi, this is the automated assistant calling from %s, NPI %s, to verify dental benefits for a patient.", req.ProviderName, req.ProviderNPI))
	w("Rep", fmt.Sprintf("Sure, this is %s. Member ID and date of birth?", e.RepName))
	w("Agent", fmt.Sprintf("Member ID %s, date of birth %s, patient %s.", req.MemberID, req.PatientDOB, req.PatientName))
	if e.EligibilityStatus == "inactive" {
		w("Rep", "I'm showing that coverage terminated. The plan is no longer active.")
		w("Agent", "Understood. Could I get a reference number for this call?")
		w("Rep", "Reference "+e.ReferenceNumber+".")
		return sb.String()
	}
	w("Rep", fmt.Sprintf("Coverage is active under the %s. Individual deductible is $%.0f with $%.0f remaining. Annual max is $%.0f.", e.PlanName, *e.DeductibleAnnual, *e.DeductibleRemaining, *e.AnnualMaximum))
	w("Agent", "And the coinsurance for preventive, basic and major?")
	w("Rep", "Preventive at 100 percent, basic at 80, major at 50.")
	w("Agent", "Is orthodontics covered?")
	if e.OrthoCovered != nil && *e.OrthoCovered {
		w("Rep", "Yes, ortho at 50 percent, lifetime max fifteen hundred, dependents to age nineteen.")
	} else {
		w("Rep", "No, there is no orthodontic benefit on this plan.")
	}
	w("Agent", "Thank you. May I have a reference number?")
	w("Rep", "Reference "+e.ReferenceNumber+". Anything else?")
	w("Agent", "That's everything. Thank you for your help.")
	return sb.String()
}
