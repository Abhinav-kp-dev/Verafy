package chatbot

import (
	"context"
	"log/slog"

	"coveragecheck/internal/db"
)

const systemPrompt = `You are the in-app assistant inside Verafy, a dental insurance verification tool used by front-desk staff at a dental practice.

What Verafy does: it automates checking whether a patient's dental insurance is active and what it covers, using a real X12 270/271 clearinghouse integration (Stedi) instead of a 20-30 minute manual phone call. Every check is queued and processed asynchronously; it can be VERIFIED (active, fully covered), COVERAGE_GAP_FLAGGED (active, but some services aren't covered), RETRYING (a transient payer error, retried automatically with backoff), or NEEDS_MANUAL_REVIEW (the system could not resolve it automatically and a staff member must call the payer). The app also computes a pre-visit cost estimate (deductible, then coinsurance, then annual maximum) against the practice's fee schedule, and emails the patient a cost notice when their appointment is booked plus a reminder the day before.

The app's pages: Dashboard (overview + recent activity), Verify Insurance (check one patient now), Batch Upload (verify many patients from a CSV or the patient list, plus a synthetic load test), Patients (manage patient records, contact info, delete), Verification History (every check ever run, filterable), Manual Review (cases needing a human, with the exact reason and a resolve action), Pre-Visit Notices (appointments, cost estimates, reminders, notice log), Reports (analytics), Settings (clinic profile, payer directory, engine configuration).

How to answer:
- For questions about a specific patient, their verification status, coverage, appointments, or notices: use the tools to look up real data. Never guess or invent patient information — if a tool returns nothing, say so plainly.
- For "how do I..." or "what does X mean" questions about the app itself: answer directly from what you know above, no tool needed.
- Keep answers short and concrete — this is a chat widget, not a document. Prefer 2-4 sentences plus a short list if useful.
- If asked to do something you cannot do (send an email, delete a record, change data), say so and point to the right page/button in the UI — you are read-only.`

type ChatMessage struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

type Reply struct {
	Text      string   `json:"text"`
	ToolsUsed []string `json:"toolsUsed,omitempty"`
}

type Assistant struct {
	client   *Client
	store    *db.Store
	log      *slog.Logger
	dispatch func(ctx context.Context, store *db.Store, name string, args map[string]any) map[string]any // swappable in tests
}

func NewAssistant(client *Client, store *db.Store, log *slog.Logger) *Assistant {
	return &Assistant{client: client, store: store, log: log, dispatch: dispatch}
}

func (a *Assistant) Configured() bool { return a.client.Configured() }

const maxHistory = 16
const maxToolRounds = 4

// Ask runs the function-calling loop: send the conversation, execute any tool
// calls the model asks for, feed results back, repeat until it answers in text
// or the round budget runs out.
func (a *Assistant) Ask(ctx context.Context, history []ChatMessage) (*Reply, error) {
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	contents := make([]content, 0, len(history))
	for _, m := range history {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: m.Text}}})
	}

	tools := []tool{{FunctionDeclarations: toolDeclarations()}}
	sys := &content{Parts: []part{{Text: systemPrompt}}}
	var toolsUsed []string

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.client.call(ctx, generateRequest{
			Contents: contents, Tools: tools, SystemInstruction: sys,
			GenerationConfig: genConfig{Temperature: 0.3, MaxOutputTokens: 500},
		})
		if err != nil {
			return nil, err
		}
		if len(resp.Candidates) == 0 {
			return &Reply{Text: "I didn't get a response — try rephrasing that?"}, nil
		}
		modelContent := resp.Candidates[0].Content

		var calls []functionCall
		var text string
		for _, p := range modelContent.Parts {
			if p.FunctionCall != nil {
				calls = append(calls, *p.FunctionCall)
			}
			if p.Text != "" {
				text += p.Text
			}
		}

		if len(calls) == 0 {
			return &Reply{Text: text, ToolsUsed: toolsUsed}, nil
		}

		// Record the model's call turn, then execute each call and append results.
		contents = append(contents, modelContent)
		resultParts := make([]part, 0, len(calls))
		for _, fc := range calls {
			a.log.Info("chatbot tool call", "name", fc.Name, "args", fc.Args)
			toolsUsed = append(toolsUsed, fc.Name)
			result := a.dispatch(ctx, a.store, fc.Name, fc.Args)
			resultParts = append(resultParts, part{FunctionResponse: &functionResponse{Name: fc.Name, Response: result}})
		}
		contents = append(contents, content{Role: "function", Parts: resultParts})
	}

	return &Reply{Text: "That took more lookups than I'm allowed in one go — could you narrow the question down?", ToolsUsed: toolsUsed}, nil
}
