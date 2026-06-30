package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// askUserQuestionEntry builds a JSONL assistant entry with an AskUserQuestion tool_use block.
func askUserQuestionEntry(t *testing.T, toolUseID string) string {
	t.Helper()
	input := map[string]any{
		"questions": []map[string]any{
			{
				"question":    "Which deployment strategy?",
				"header":      "Choose strategy",
				"multiSelect": false,
				"options": []map[string]any{
					{"label": "Blue/Green", "description": "Zero-downtime swap", "preview": "recommended"},
					{"label": "Rolling", "description": "Gradual rollout"},
				},
			},
		},
	}
	inputBytes, err := json.Marshal(input)
	require.NoError(t, err)
	return buildAssistantEntry(t, "claude-opus-4", 1, 1, []map[string]any{
		{
			"type":  "tool_use",
			"id":    toolUseID,
			"name":  "AskUserQuestion",
			"input": json.RawMessage(inputBytes),
		},
	})
}

// TestParseSessionFile_PendingQuestion_Unresolved verifies that an unresolved
// AskUserQuestion tool_use sets PendingQuestion (not PendingToolUse) and forces TurnOpen.
func TestParseSessionFile_PendingQuestion_Unresolved(t *testing.T) {
	const id = "toolu_ask_01"
	path := writeSessionLines(t, []string{askUserQuestionEntry(t, id)})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)

	require.NotNil(t, data.PendingQuestion, "PendingQuestion must be set for unresolved AskUserQuestion")
	require.Equal(t, id, data.PendingQuestion.ToolUseID)
	require.Nil(t, data.PendingToolUse, "PendingToolUse must be nil — question takes precedence")
	require.True(t, data.TurnOpen, "TurnOpen must be true while a question awaits an answer")

	require.Len(t, data.PendingQuestion.Questions, 1)
	q := data.PendingQuestion.Questions[0]
	require.Equal(t, "Choose strategy", q.Header)
	require.Equal(t, "Which deployment strategy?", q.Question)
	require.False(t, q.MultiSelect)
	require.Len(t, q.Options, 2)
	require.Equal(t, "Blue/Green", q.Options[0].Label)
	require.Equal(t, "Zero-downtime swap", q.Options[0].Description)
	require.Equal(t, "Rolling", q.Options[1].Label)
}

// TestParseSessionFile_PendingQuestion_Resolved verifies that a matching
// tool_result clears PendingQuestion entirely.
func TestParseSessionFile_PendingQuestion_Resolved(t *testing.T) {
	const id = "toolu_ask_01"
	path := writeSessionLines(t, []string{
		askUserQuestionEntry(t, id),
		buildUserToolResult(t, id),
	})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)

	require.Nil(t, data.PendingQuestion, "PendingQuestion must be nil once the tool_result arrives")
	require.Nil(t, data.PendingToolUse)
}
