package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// QuestionDeliverFn delivers an answer message to the live session for pid and
// returns the transport used ("tmux"/"pty"/"bridge"). It is the seam over
// SpawnManager.SendMessageToChannel so the handler is testable without a real
// session.
type QuestionDeliverFn func(ctx context.Context, pid int, message string) (transport string, err error)

// AnswerQuestionHandler handles POST /api/agents/{pid}/answer-question. It
// answers an outstanding AskUserQuestion by delivering the chosen option labels
// to the running interactive session as a normal message — we deliberately send
// free text (which Claude accepts as an answer) rather than emulating the
// terminal selector's keystrokes.
type AnswerQuestionHandler struct {
	getAgents GetAgentsFn
	deliver   QuestionDeliverFn
}

// NewAnswerQuestionHandler creates an AnswerQuestionHandler.
func NewAnswerQuestionHandler(getAgents GetAgentsFn, deliver QuestionDeliverFn) *AnswerQuestionHandler {
	return &AnswerQuestionHandler{getAgents: getAgents, deliver: deliver}
}

type answerQuestionRequest struct {
	ToolUseID string `json:"toolUseId"`
	Answers   []struct {
		Header   string   `json:"header"`
		Selected []string `json:"selected"`
	} `json:"answers"`
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

	message := buildAnswerMessage(body.Answers)
	if message == "" {
		return apierr.NewAppError(http.StatusBadRequest, "no options selected")
	}

	transport, err := h.deliver(r.Context(), pid, message)
	if err != nil {
		return apierr.NewAppError(http.StatusBadGateway, err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"ok": true, "transport": transport})
}

// buildAnswerMessage renders the selected option labels into a free-text answer
// the running session can interpret, e.g. "Name-Stil: Englisch; Markt: DACH".
func buildAnswerMessage(answers []struct {
	Header   string   `json:"header"`
	Selected []string `json:"selected"`
}) string {
	parts := make([]string, 0, len(answers))
	for _, a := range answers {
		if len(a.Selected) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", a.Header, strings.Join(a.Selected, ", ")))
	}
	return strings.Join(parts, "; ")
}
