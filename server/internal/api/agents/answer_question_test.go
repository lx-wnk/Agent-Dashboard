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

func singleSelectQuestion() sdk.QuestionSpec {
	return sdk.QuestionSpec{
		Header: "Name-Style", Question: "Which?", MultiSelect: false,
		Options: []sdk.QuestionOption{{Label: "English"}, {Label: "German"}},
	}
}

func agentWithQuestions(pid int, injectable bool, toolUseID string, qs []sdk.QuestionSpec) sdk.Agent {
	return sdk.Agent{
		PID:             pid,
		LiveInjectable:  injectable,
		PendingQuestion: &sdk.PendingQuestion{ToolUseID: toolUseID, Questions: qs},
	}
}

func agentWithPendingQuestion(pid int, injectable bool, toolUseID string) sdk.Agent {
	return agentWithQuestions(pid, injectable, toolUseID, []sdk.QuestionSpec{singleSelectQuestion()})
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

// validBody answers the default single-select question with its first option.
const validBody = `{"toolUseId":"tu_q","answers":[{"header":"Name-Style","selected":["English"]}]}`

func TestAnswerQuestion_HappyPath_DeliversKeyBatches(t *testing.T) {
	var gotPID int
	var gotBatches [][]AnswerKey
	deliver := func(_ context.Context, pid int, batches [][]AnswerKey) (string, error) {
		gotPID, gotBatches = pid, batches
		return "tmux", nil
	}
	qs := []sdk.QuestionSpec{
		singleSelectQuestion(),
		{Header: "Constraints", MultiSelect: true, Options: []sdk.QuestionOption{{Label: "Trademark"}, {Label: "Domain"}}},
	}
	h := newAnswerHandler(agentWithQuestions(100, true, "tu_q", qs), deliver)

	body := `{"toolUseId":"tu_q","answers":[{"header":"Name-Style","selected":["German"]},{"header":"Constraints","selected":["Trademark","Domain"]}]}`
	require.NoError(t, postAnswer(t, h, "100", body))

	require.Equal(t, 100, gotPID)
	// German is option index 1 → number "2"; multi-select both → Space,Down,Space,Enter.
	require.Equal(t, [][]AnswerKey{
		{{Char: "2"}},
		{{Named: "Space"}, {Named: "Down"}, {Named: "Space"}, {Named: "Enter"}},
	}, gotBatches)
}

func TestAnswerQuestion_UnknownOption_BadRequest(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), func(context.Context, int, [][]AnswerKey) (string, error) {
		t.Fatal("deliver must not be called when an option is invalid")
		return "", nil
	})
	err := postAnswer(t, h, "100", `{"toolUseId":"tu_q","answers":[{"header":"Name-Style","selected":["Klingon"]}]}`)
	wantStatus(t, err, 400)
}

func TestAnswerQuestion_StaleToolUseID_Conflict(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_current"), func(context.Context, int, [][]AnswerKey) (string, error) {
		t.Fatal("deliver must not be called for a stale answer")
		return "", nil
	})
	err := postAnswer(t, h, "100", `{"toolUseId":"tu_old","answers":[{"header":"Name-Style","selected":["English"]}]}`)
	wantStatus(t, err, 409)
}

func TestAnswerQuestion_NotInjectable_Conflict(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, false, "tu_q"), func(context.Context, int, [][]AnswerKey) (string, error) {
		t.Fatal("deliver must not be called for a non-injectable session")
		return "", nil
	})
	err := postAnswer(t, h, "100", validBody)
	wantStatus(t, err, 409)
}

func TestAnswerQuestion_AgentNotFound(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), func(context.Context, int, [][]AnswerKey) (string, error) {
		return "", nil
	})
	err := postAnswer(t, h, "999", validBody)
	wantStatus(t, err, 404)
}

func TestAnswerQuestion_DeliveryError_BadGateway(t *testing.T) {
	h := newAnswerHandler(agentWithPendingQuestion(100, true, "tu_q"), func(context.Context, int, [][]AnswerKey) (string, error) {
		return "", errors.New("channel not available")
	})
	err := postAnswer(t, h, "100", validBody)
	wantStatus(t, err, 502)
}

func TestSingleSelectKeys(t *testing.T) {
	require.Equal(t, []AnswerKey{{Char: "1"}}, singleSelectKeys(0))
	require.Equal(t, []AnswerKey{{Char: "9"}}, singleSelectKeys(8))
	// 10th option (index 9) has no number hotkey → Down×9 + Enter.
	got := singleSelectKeys(9)
	require.Len(t, got, 10)
	require.Equal(t, AnswerKey{Named: "Down"}, got[0])
	require.Equal(t, AnswerKey{Named: "Enter"}, got[9])
}

func TestMultiSelectKeys(t *testing.T) {
	// select positions 0 and 2 of a list: Space, Down, Down, Space, Enter.
	require.Equal(t, []AnswerKey{
		{Named: "Space"}, {Named: "Down"}, {Named: "Down"}, {Named: "Space"}, {Named: "Enter"},
	}, multiSelectKeys([]int{0, 2}))
}
