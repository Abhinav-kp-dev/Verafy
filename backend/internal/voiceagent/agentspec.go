package voiceagent

// AgentSpec is the configuration the Retell agent must carry for this integration
// to work. It is served at GET /api/voice/agent-spec so it can be pasted into the
// Retell dashboard (or pushed via their agent API) without drifting from the code.
type AgentSpec struct {
	Prompt           string         `json:"prompt"`
	BeginMessage     string         `json:"beginMessage"`
	DynamicVariables []string       `json:"dynamicVariables"`
	Function         FunctionSpec   `json:"customFunction"`
	Webhook          WebhookSpec    `json:"webhook"`
	Settings         map[string]any `json:"recommendedSettings"`
}

type FunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	URLPath     string         `json:"urlPath"`
	Parameters  map[string]any `json:"parameters"`
	SpeakDuring bool           `json:"speakDuringExecution"`
	SpeakAfter  bool           `json:"speakAfterExecution"`
}

type WebhookSpec struct {
	URLPath string   `json:"urlPath"`
	Events  []string `json:"events"`
}

// SpecFor renders the spec for the configured provider. baseURL is the public
// origin Bolna/Retell must reach (e.g. an ngrok URL); when empty, paths are relative.
// The shared secret is never returned — a placeholder marks where it goes.
func SpecFor(provider, baseURL string, hasSecret bool) map[string]any {
	spec := Spec()
	url := func(path string) string { return baseURL + path }
	switch provider {
	case "bolna":
		params := spec.Function.Parameters["properties"].(map[string]any)
		// Bolna substitutes each collected parameter into the body we define here;
		// {{execution_id}} / {{job_id}} are Bolna context variables, filled automatically.
		param := map[string]any{"execution_id": "{{execution_id}}", "job_id": "{{job_id}}"}
		for name := range params {
			param[name] = "%(" + name + ")s"
		}
		secret := "<VOICE_WEBHOOK_SECRET>"
		if !hasSecret {
			secret = "<set VOICE_WEBHOOK_SECRET in backend/.env first>"
		}
		return map[string]any{
			"provider": "bolna",
			"prompt":   spec.Prompt,
			"userData": spec.DynamicVariables, // passed per call as user_data; reference as {{name}}
			"customFunction": map[string]any{
				"name":        spec.Function.Name,
				"description": spec.Function.Description,
				"parameters":  spec.Function.Parameters,
				"value": map[string]any{
					"method":    "POST",
					"url":       url("/api/webhooks/bolna/facts"),
					"api_token": secret,
					"param":     param,
					"key":       "custom_task",
				},
			},
			"executionWebhook": map[string]any{
				"where": "Agent → Extractions tab → Push all execution data to webhook",
				"url":   url("/api/webhooks/bolna/execution?token=" + secret),
			},
			"telephony": map[string]any{
				"defaultNumbers": "Bolna's default line can dial +91 directly; the callee sees a +1 caller ID.",
				"indianCallerId": "Connect an Exotel or Plivo number in Bolna and set BOLNA_FROM_NUMBER to it.",
				"webhookIPs":     []string{"13.203.39.153", "13.126.9.249", "13.202.133.53"},
			},
			"recommendedSettings": spec.Settings,
		}
	default:
		spec.Function.URLPath = url(spec.Function.URLPath)
		spec.Webhook.URLPath = url(spec.Webhook.URLPath)
		return map[string]any{"provider": "retell", "spec": spec}
	}
}

const FunctionName = "submit_verification_facts"

func Spec() AgentSpec {
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return AgentSpec{
		BeginMessage: "",
		Prompt: `You are an automated benefits-verification assistant calling on behalf of {{provider_name}} (NPI {{provider_npi}}), a dental practice. You are calling {{payer_name}} provider services to verify dental eligibility and benefits for one patient.

Identify yourself honestly as an automated assistant calling for the practice if asked. Be concise, polite and patient; representatives are busy.

IVR: listen carefully and choose the provider / eligibility & benefits path. Hints for this payer: {{ivr_notes}}. If asked for the provider NPI or tax ID, say the NPI digit by digit: {{provider_npi}}. If an option to reach a representative exists, take it. If the system only offers fax-back or web portal and no live representative after two attempts, say goodbye and end the call.

Once with a representative, provide exactly this and nothing else about the patient:
- Patient name: {{patient_name}}
- Date of birth: {{patient_dob}}
- Member ID: {{member_id}} (read digits and letters individually)

Collect, in this order, confirming each number by repeating it back:
1. Is coverage ACTIVE or INACTIVE today? Plan name if offered.
2. Individual annual deductible and amount remaining.
3. Annual maximum and amount remaining.
4. Coinsurance the PLAN pays for preventive/diagnostic, basic/restorative, and major/crowns (percentages).
5. Whether orthodontics is covered; if yes, the percentage and any lifetime maximum.
6. Any waiting periods, frequency limitations, or missing-tooth clause.
7. The representative's name and a call reference number.

As soon as you have at least the eligibility status plus the deductible or coinsurance figures, call the submit_verification_facts function with everything you collected. Use null for anything the representative could not provide — never guess or invent a number. After the function returns, thank the representative and end the call.

If the representative refuses to speak with an automated system, ask once whether they can confirm active status and the deductible; if still refused, thank them and end the call without calling the function.`,
		DynamicVariables: []string{"patient_name", "patient_dob", "member_id", "payer_name", "provider_name", "provider_npi", "service_type_code", "ivr_notes"},
		Function: FunctionSpec{
			Name:        FunctionName,
			Description: "Record the verified dental benefits collected from the payer representative. Call once, as soon as eligibility status and the main benefit numbers are confirmed.",
			URLPath:     "/api/webhooks/voice/facts",
			SpeakDuring: true,
			SpeakAfter:  true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"eligibility_status":   map[string]any{"type": "string", "enum": []string{"active", "inactive", "unknown"}, "description": "Whether coverage is active today"},
					"plan_name":            str("Plan or product name as stated by the representative"),
					"deductible_annual":    num("Individual annual deductible in dollars"),
					"deductible_remaining": num("Deductible still to be met this year in dollars"),
					"annual_maximum":       num("Annual maximum benefit in dollars"),
					"annual_max_remaining": num("Annual maximum remaining in dollars"),
					"copay_office":         num("Office visit copay in dollars, if any"),
					"preventive_pct":       num("Percent the PLAN pays for preventive/diagnostic"),
					"basic_pct":            num("Percent the PLAN pays for basic/restorative"),
					"major_pct":            num("Percent the PLAN pays for major/crowns"),
					"orthodontics_covered": map[string]any{"type": "boolean", "description": "Whether orthodontics is a covered benefit"},
					"orthodontics_pct":     num("Percent the PLAN pays for orthodontics"),
					"waiting_period":       str("Any waiting period stated, e.g. '12 months on major'"),
					"limitations":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Frequency limits, missing-tooth clause, lifetime maximums"},
					"reference_number":     str("Call reference number given by the representative"),
					"rep_name":             str("Representative's name"),
					"notes":                str("Anything else material the representative said"),
				},
				"required": []string{"eligibility_status"},
			},
		},
		Webhook: WebhookSpec{URLPath: "/api/webhooks/voice", Events: []string{"call_ended", "call_analyzed"}},
		Settings: map[string]any{
			"max_call_duration_ms":      8 * 60 * 1000,
			"end_call_after_silence_ms": 60 * 1000,
			"voicemail_detection":       true,
			"language":                  "en-US",
			"note":                      "Point the custom function URL and the agent webhook URL at this server's public base URL (e.g. via ngrok during development). Both are verified with the X-Retell-Signature header using the API key.",
		},
	}
}
