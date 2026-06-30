package agents

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/stretchr/testify/require"
)

func agentWithPendingQuestion(pid int, injectable bool, toolUseID string) sdk.Agent {
	return sdk.Agent{
		PID:            pid,
		LiveInjectable: injectable,
		PendingQuestion: &sdk.PendingQuestion{
			ToolUseID: toolUseID,
			Questions: []sdk.QuestionSpec{{Header: "Name-Style", Question: "Which?"}},
		},
	}
}

func newAnswerHandler(agent sdk.Agent, deliver QuestionDeliverFn) *AnswerQuestionHandler {
	getAgents := func(context.Context) ([]sdk.Agent, error) { return []sdk.Agent{agent}, nil }
	return NewAnswerQuestionHandler(getAgents, deliver)
}

func postAnswer(t *testing.T, h *AnswerQuestionHandler, pid, body string) error {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/agents/"+pid+"/answer-question", strings.NewReader(body))
	req.SetPathValue("pid", pid)
	return h.AnswerQuestion(httptest.NewRecorder(), req)
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	var appErr *apierr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, status, appErr.Status)
}

func TestAnswerQuestion_HappyPath_DeliversBuiltMessage(t *testing.T) {
	var gotPID int
	var gotMsg string
	deliver := func(_ context.Context, pid int, msg string) (string, error) {
		gotPID, gotMsg = pid, msg
		return "tmux", nil
	}
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), deliver)

	body := `{"toolUseId":"tu_q","answers":[{"header":"Name-Style","selected":["English"]},{"header":"Constraints","selected":["Trademark","Domain"]}]}`
	require.NoError(t, postAnswer(t, h, "100", body))

	require.Equal(t, 100, gotPID)
	require.Equal(t, "Name-Style: English; Constraints: Trademark, Domain", gotMsg)
}

func TestAnswerQuestion_StaleToolUseID_Conflict(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_current"), func(context.Context, int, string) (string, error) {
		t.Fatal("deliver must not be called for a stale answer")
		return "", nil
	})
	err := postAnswer(t, h, "100", `{"toolUseId":"tu_old","answers":[{"header":"H","selected":["x"]}]}`)
	wantStatus(t, err, 409)
}

func TestAnswerQuestion_NotInjectable_Conflict(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, false, "tu_q"), func(context.Context, int, string) (string, error) {
		t.Fatal("deliver must not be called for a non-injectable session")
		return "", nil
	})
	err := postAnswer(t, h, "100", `{"toolUseId":"tu_q","answers":[{"header":"H","selected":["x"]}]}`)
	wantStatus(t, err, 409)
}

func TestAnswerQuestion_AgentNotFound(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), func(context.Context, int, string) (string, error) {
		return "", nil
	})
	err := postAnswer(t, h, "999", `{"toolUseId":"tu_q","answers":[{"header":"H","selected":["x"]}]}`)
	wantStatus(t, err, 404)
}

func TestAnswerQuestion_DeliveryError_BadGateway(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), func(context.Context, int, string) (string, error) {
		return "", errors.New("channel not available")
	})
	err := postAnswer(t, h, "100", `{"toolUseId":"tu_q","answers":[{"header":"H","selected":["x"]}]}`)
	wantStatus(t, err, 502)
}
