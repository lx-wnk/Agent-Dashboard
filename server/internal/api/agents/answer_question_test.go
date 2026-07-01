package agents

import (
	"context"
	"errors"
	"net/http/httptest"
	"strconv"
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
	// German is option index 1 → number "2"; multi-select both → digit,digit,Tab,Enter.
	require.Equal(t, [][]AnswerKey{
		{{Char: "2"}},
		{{Char: "1"}, {Char: "2"}, {Named: "Tab"}, {Named: "Enter"}},
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
	got, err := singleSelectKeys(0)
	require.NoError(t, err)
	require.Equal(t, []AnswerKey{{Char: "1"}}, got)

	got, err = singleSelectKeys(2)
	require.NoError(t, err)
	require.Equal(t, []AnswerKey{{Char: "3"}}, got)

	// 10th option (index 9) has no number hotkey — out of scope, must error.
	_, err = singleSelectKeys(9)
	require.Error(t, err)
}

func TestMultiSelectKeys(t *testing.T) {
	// select positions 0 and 2: digit "1", digit "3", Tab, Enter.
	got, err := multiSelectKeys([]int{0, 2})
	require.NoError(t, err)
	require.Equal(t, []AnswerKey{
		{Char: "1"}, {Char: "3"}, {Named: "Tab"}, {Named: "Enter"},
	}, got)

	_, err = multiSelectKeys([]int{0, 9})
	require.Error(t, err)
}

func TestCustomInputKeys(t *testing.T) {
	q := sdk.QuestionSpec{Header: "Fruit", Options: []sdk.QuestionOption{{Label: "Apple"}, {Label: "Banana"}}}
	got, err := customInputKeys(q, "Cherry")
	require.NoError(t, err)
	require.Equal(t, []AnswerKey{{Char: "3"}, {Text: "Cherry"}, {Named: "Enter"}}, got)
}

func TestChatKeys(t *testing.T) {
	q := sdk.QuestionSpec{Header: "Fruit", Options: []sdk.QuestionOption{{Label: "Apple"}, {Label: "Banana"}}}
	got, err := chatKeys(q, "why?")
	require.NoError(t, err)
	require.Equal(t, []AnswerKey{{Char: "4"}, {Text: "why?"}, {Named: "Enter"}}, got)
}

func TestBuildAnswerKeys_TableDriven(t *testing.T) {
	single := sdk.QuestionSpec{Header: "Name-Style", Options: []sdk.QuestionOption{{Label: "English"}, {Label: "German"}, {Label: "French"}}}
	fruit := sdk.QuestionSpec{Header: "Fruit", Options: []sdk.QuestionOption{{Label: "Apple"}, {Label: "Banana"}}}

	t.Run("single-select idx 0", func(t *testing.T) {
		got, err := buildAnswerKeys([]sdk.QuestionSpec{single}, []answerInput{{Header: "Name-Style", Selected: []string{"English"}}}, "")
		require.NoError(t, err)
		require.Equal(t, [][]AnswerKey{{{Char: "1"}}}, got)
	})

	t.Run("single-select idx 2", func(t *testing.T) {
		got, err := buildAnswerKeys([]sdk.QuestionSpec{single}, []answerInput{{Header: "Name-Style", Selected: []string{"French"}}}, "")
		require.NoError(t, err)
		require.Equal(t, [][]AnswerKey{{{Char: "3"}}}, got)
	})

	t.Run("multi-select idxs 0,2", func(t *testing.T) {
		threeOpt := sdk.QuestionSpec{Header: "Constraints", MultiSelect: true, Options: []sdk.QuestionOption{{Label: "Trademark"}, {Label: "Domain"}, {Label: "Slogan"}}}
		got, err := buildAnswerKeys([]sdk.QuestionSpec{threeOpt}, []answerInput{{Header: "Constraints", Selected: []string{"Trademark", "Slogan"}}}, "")
		require.NoError(t, err)
		require.Equal(t, [][]AnswerKey{{{Char: "1"}, {Char: "3"}, {Named: "Tab"}, {Named: "Enter"}}}, got)
	})

	t.Run("custom input", func(t *testing.T) {
		got, err := buildAnswerKeys([]sdk.QuestionSpec{fruit}, []answerInput{{Header: "Fruit", CustomText: "Cherry"}}, "")
		require.NoError(t, err)
		require.Equal(t, [][]AnswerKey{{{Char: "3"}, {Text: "Cherry"}, {Named: "Enter"}}}, got)
	})

	t.Run("chat", func(t *testing.T) {
		got, err := buildAnswerKeys([]sdk.QuestionSpec{fruit}, nil, "why?")
		require.NoError(t, err)
		require.Equal(t, [][]AnswerKey{{{Char: "4"}, {Text: "why?"}, {Named: "Enter"}}}, got)
	})

	t.Run("idx >= 9 errors", func(t *testing.T) {
		tenOpts := make([]sdk.QuestionOption, 10)
		for i := range tenOpts {
			tenOpts[i] = sdk.QuestionOption{Label: strconv.Itoa(i)}
		}
		q := sdk.QuestionSpec{Header: "Big", Options: tenOpts}
		_, err := buildAnswerKeys([]sdk.QuestionSpec{q}, []answerInput{{Header: "Big", Selected: []string{"9"}}}, "")
		require.Error(t, err)
	})
}
