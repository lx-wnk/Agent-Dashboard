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
// real keystrokes, per the empirically verified interaction model: every row is
// numbered (options 1..N, then "Type something" at N+1, "Chat about this" at
// N+2); a digit selects-and-submits a single-select question in one keystroke;
// a multi-select question is answered by pressing the digit for each selected
// option, then Tab (opens the review), then Enter (confirms); custom input and
// chat both select their row's digit, type the text, then Enter.
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
	// ChatText, when non-empty, dismisses the whole modal via "Chat about this"
	// instead of answering the pending question(s) — per-question Answers are
	// ignored in that case.
	ChatText string `json:"chatText"`
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
	if body.ToolUseID == "" || (len(body.Answers) == 0 && body.ChatText == "") {
		return apierr.NewAppError(http.StatusBadRequest, "toolUseId and (answers or chatText) are required")
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

	batches, err := buildAnswerKeys(agent.PendingQuestion.Questions, body.Answers, body.ChatText)
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
	// CustomText, when non-empty, answers this question via the modal's
	// auto-appended "Type something" row instead of Selected.
	CustomText string `json:"customText"`
}

// buildAnswerKeys turns the user's selections into one keystroke batch per
// question, in the on-screen order. chatText, when set, short-circuits to a
// single batch that dismisses the whole modal via the first question's "Chat
// about this" row; per-question answers are ignored in that case.
func buildAnswerKeys(questions []sdk.QuestionSpec, answers []answerInput, chatText string) ([][]AnswerKey, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("no pending question")
	}

	if chatText != "" {
		keys, err := chatKeys(questions[0], chatText)
		if err != nil {
			return nil, err
		}
		return [][]AnswerKey{keys}, nil
	}

	batches := make([][]AnswerKey, 0, len(questions))
	for qi, q := range questions {
		a := resolveAnswer(q, answers, qi)
		switch {
		case a.CustomText != "":
			keys, err := customInputKeys(q, a.CustomText)
			if err != nil {
				return nil, err
			}
			batches = append(batches, keys)
		case len(a.Selected) > 0:
			idxs, err := indicesForLabels(q, a.Selected)
			if err != nil {
				return nil, err
			}
			var keys []AnswerKey
			if q.MultiSelect {
				keys, err = multiSelectKeys(idxs)
			} else {
				keys, err = singleSelectKeys(idxs[0])
			}
			if err != nil {
				return nil, err
			}
			batches = append(batches, keys)
		default:
			return nil, fmt.Errorf("no option selected for question %q", q.Header)
		}
	}
	return batches, nil
}

// resolveAnswer picks the answerInput for question q: matched by header, or
// falling back to the answer at the same index when headers are absent.
func resolveAnswer(q sdk.QuestionSpec, answers []answerInput, qi int) answerInput {
	for _, a := range answers {
		if a.Header == q.Header {
			return a
		}
	}
	if qi < len(answers) {
		return answers[qi]
	}
	return answerInput{}
}

// indicesForLabels resolves the chosen option labels to their ascending
// on-screen positions.
func indicesForLabels(q sdk.QuestionSpec, labels []string) ([]int, error) {
	pos := make(map[string]int, len(q.Options))
	for i, o := range q.Options {
		pos[o.Label] = i
	}
	idxs := make([]int, 0, len(labels))
	for _, label := range labels {
		i, ok := pos[label]
		if !ok {
			return nil, fmt.Errorf("option %q is not valid for question %q", label, q.Header)
		}
		idxs = append(idxs, i)
	}
	sortInts(idxs)
	return idxs, nil
}

// digitKey returns the keystroke for on-screen row pos (1-based). Claude's
// selector only exposes number hotkeys for rows 1-9; a 10th-or-later row has no
// plain-byte hotkey, so arrow-key navigation would be required — out of scope.
func digitKey(pos int) (AnswerKey, error) {
	if pos > 9 {
		return AnswerKey{}, fmt.Errorf("too many options to answer from dashboard")
	}
	return AnswerKey{Char: strconv.Itoa(pos)}, nil
}

// singleSelectKeys returns the keystrokes to pick option i (0-based): its
// number hotkey selects and submits in one press.
func singleSelectKeys(i int) ([]AnswerKey, error) {
	key, err := digitKey(i + 1)
	if err != nil {
		return nil, err
	}
	return []AnswerKey{key}, nil
}

// multiSelectKeys returns the keystrokes to answer a multi-select question:
// the digit for each selected 0-based position (toggles its checkbox), then Tab
// (opens the "Submit answers" review), then Enter (confirms). A bare Enter
// without Tab only toggles the focused row instead of submitting.
func multiSelectKeys(idxs []int) ([]AnswerKey, error) {
	keys := make([]AnswerKey, 0, len(idxs)+2)
	for _, i := range idxs {
		key, err := digitKey(i + 1)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	keys = append(keys, AnswerKey{Named: "Tab"}, AnswerKey{Named: "Enter"})
	return keys, nil
}

// customInputKeys returns the keystrokes to answer via the modal's
// auto-appended "Type something" row (position len(options)+1): its digit
// selects it and makes the row editable, then the text is typed, then Enter
// submits.
func customInputKeys(q sdk.QuestionSpec, text string) ([]AnswerKey, error) {
	key, err := digitKey(len(q.Options) + 1)
	if err != nil {
		return nil, err
	}
	return []AnswerKey{key, {Text: text}, {Named: "Enter"}}, nil
}

// chatKeys returns the keystrokes to dismiss the modal via "Chat about this"
// (position len(options)+2): its digit dismisses the question back to a normal
// prompt, then the text is typed, then Enter submits it.
func chatKeys(q sdk.QuestionSpec, text string) ([]AnswerKey, error) {
	key, err := digitKey(len(q.Options) + 2)
	if err != nil {
		return nil, err
	}
	return []AnswerKey{key, {Text: text}, {Named: "Enter"}}, nil
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
