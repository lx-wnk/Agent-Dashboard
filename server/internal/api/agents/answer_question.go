package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// QuestionDeliverFn drives the interactive selector in the live session for pid
// by injecting one key batch per question, returning the transport used. It is
// the seam over SpawnManager.SendAnswerKeys so the handler is testable without a
// real session.
type QuestionDeliverFn func(ctx context.Context, pid int, batches [][]AnswerKey) (transport string, err error)

// AnswerQuestionHandler handles POST /api/agents/{pid}/answer-question. It
// answers an outstanding AskUserQuestion by driving the terminal selector with
// real keystrokes: a digit selects-and-submits a single-select question, while a
// multi-select question is toggled with Space (navigating with Down) and
// confirmed with Enter. Claude's selector does not accept free text, so the
// chosen option labels are mapped back to their on-screen positions.
type AnswerQuestionHandler struct {
	getAgents GetAgentsFn
	deliver   QuestionDeliverFn
}

// NewAnswerQuestionHandler creates an AnswerQuestionHandler.
func NewAnswerQuestionHandler(getAgents GetAgentsFn, deliver QuestionDeliverFn) *AnswerQuestionHandler {
	return &AnswerQuestionHandler{getAgents: getAgents, deliver: deliver}
}

type answerQuestionRequest struct {
	ToolUseID string        `json:"toolUseId"`
	Answers   []answerInput `json:"answers"`
}

// AnswerQuestion handles POST /api/agents/{pid}/answer-question.
func (h *AnswerQuestionHandler) AnswerQuestion(w http.ResponseWriter, r *http.Request) error {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || pid <= 0 {
		return apierr.NewAppError(http.StatusBadRequest, "invalid pid")
	}

	var body answerQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.ToolUseID == "" || len(body.Answers) == 0 {
		return apierr.NewAppError(http.StatusBadRequest, "toolUseId and answers are required")
	}

	agents, err := h.getAgents(r.Context())
	if err != nil {
		return fmt.Errorf("answer-question: get agents: %w", err)
	}
	var agent *sdk.Agent
	for i := range agents {
		if agents[i].PID == pid {
			agent = &agents[i]
			break
		}
	}
	if agent == nil {
		return apierr.NewAppError(http.StatusNotFound, "agent not found")
	}

	// Anti-stale: only answer the question the session is currently blocked on.
	if agent.PendingQuestion == nil || agent.PendingQuestion.ToolUseID != body.ToolUseID {
		return apierr.NewAppError(http.StatusConflict, "no matching pending question (already answered?)")
	}
	if !agent.LiveInjectable {
		return apierr.NewAppError(http.StatusConflict, "session is not live-injectable; answer in your terminal")
	}

	batches, err := buildAnswerKeys(agent.PendingQuestion.Questions, body.Answers)
	if err != nil {
		return apierr.NewAppError(http.StatusBadRequest, err.Error())
	}

	transport, err := h.deliver(r.Context(), pid, batches)
	if err != nil {
		return apierr.NewAppError(http.StatusBadGateway, err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"ok": true, "transport": transport})
}

type answerInput struct {
	Header   string   `json:"header"`
	Selected []string `json:"selected"`
}

// buildAnswerKeys turns the user's selections into one keystroke batch per
// question, in the on-screen order. A single-select question is answered with
// the option's 1-based number (which selects and submits in Claude's selector);
// a multi-select question walks Down from the top, presses Space on each chosen
// option, and confirms with Enter.
func buildAnswerKeys(questions []sdk.QuestionSpec, answers []answerInput) ([][]AnswerKey, error) {
	batches := make([][]AnswerKey, 0, len(questions))
	for qi, q := range questions {
		idxs, err := selectedIndices(q, answers, qi)
		if err != nil {
			return nil, err
		}
		if len(idxs) == 0 {
			return nil, fmt.Errorf("no option selected for question %q", q.Header)
		}
		if q.MultiSelect {
			batches = append(batches, multiSelectKeys(idxs))
		} else {
			batches = append(batches, singleSelectKeys(idxs[0]))
		}
	}
	if len(batches) == 0 {
		return nil, fmt.Errorf("no answers provided")
	}
	return batches, nil
}

// selectedIndices resolves the chosen option labels for question qi to their
// ascending option positions. It matches the answer by header, falling back to
// the answer at the same index when headers are absent.
func selectedIndices(q sdk.QuestionSpec, answers []answerInput, qi int) ([]int, error) {
	var selected []string
	for _, a := range answers {
		if a.Header == q.Header {
			selected = a.Selected
			break
		}
	}
	if selected == nil && qi < len(answers) {
		selected = answers[qi].Selected
	}
	pos := make(map[string]int, len(q.Options))
	for i, o := range q.Options {
		pos[o.Label] = i
	}
	idxs := make([]int, 0, len(selected))
	for _, label := range selected {
		i, ok := pos[label]
		if !ok {
			return nil, fmt.Errorf("option %q is not valid for question %q", label, q.Header)
		}
		idxs = append(idxs, i)
	}
	sortInts(idxs)
	return idxs, nil
}

// singleSelectKeys returns the keystrokes to pick option i in a single-select
// question. Options 1-9 use the number hotkey (selects and submits in one
// press); a 10th-or-later option falls back to Down-navigation plus Enter.
func singleSelectKeys(i int) []AnswerKey {
	if i < 9 {
		return []AnswerKey{{Char: strconv.Itoa(i + 1)}}
	}
	keys := make([]AnswerKey, 0, i+1)
	for j := 0; j < i; j++ {
		keys = append(keys, AnswerKey{Named: "Down"})
	}
	return append(keys, AnswerKey{Named: "Enter"})
}

// multiSelectKeys returns the keystrokes to toggle the given ascending option
// positions in a multi-select question: walk Down from the top, Space on each
// selected position, then Enter to confirm.
func multiSelectKeys(idxs []int) []AnswerKey {
	last := idxs[len(idxs)-1]
	sel := make(map[int]bool, len(idxs))
	for _, x := range idxs {
		sel[x] = true
	}
	keys := make([]AnswerKey, 0, last+len(idxs)+1)
	for pos := 0; pos <= last; pos++ {
		if sel[pos] {
			keys = append(keys, AnswerKey{Named: "Space"})
		}
		if pos < last {
			keys = append(keys, AnswerKey{Named: "Down"})
		}
	}
	return append(keys, AnswerKey{Named: "Enter"})
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
