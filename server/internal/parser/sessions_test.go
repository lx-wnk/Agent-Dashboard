package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBuildSessionInfo_CapturesCwdWithoutModel guards the decouple fix:
// cwd is written on every JSONL line, model only on assistant turns. A
// session that never surfaces a model in the head window must still pick
// up its real project path from cwd (not fall back to the encoded dir).
func TestBuildSessionInfo_CapturesCwdWithoutModel(t *testing.T) {
	dir := t.TempDir()
	sessID := "11111111-1111-1111-1111-111111111111"
	path := filepath.Join(dir, sessID+".jsonl")
	line := `{"type":"user","cwd":"/Users/me/projects/myproject","message":{"role":"user","content":"hi"}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o644))

	info := buildSessionInfo(jsonlFileEntry{
		sessionID:         sessID,
		filePath:          path,
		projectDirEncoded: "-Users-me-projects-myproject",
		mtime:             time.Unix(0, 0),
	}, nil)

	require.Equal(t, "/Users/me/projects/myproject", info.ProjectPath)
	require.Equal(t, "myproject", info.ProjectName)
}

// TestBuildSessionInfo_RecoversModelFromTail guards the tail fallback: when
// the first assistant turn (carrying the model) sits past the 8KB head
// window, the model is recovered from the tail so cost + color still work.
func TestBuildSessionInfo_RecoversModelFromTail(t *testing.T) {
	dir := t.TempDir()
	sessID := "22222222-2222-2222-2222-222222222222"
	path := filepath.Join(dir, sessID+".jsonl")

	var b strings.Builder
	// Small leading line so cwd is found in the head window.
	b.WriteString(`{"type":"user","cwd":"/Users/me/projects/deepmodel","message":{"role":"user","content":"hi"}}` + "\n")
	// Padding line pushes the assistant/model line past the 8KB head read.
	b.WriteString(`{"type":"x","pad":"` + strings.Repeat("x", 9000) + `"}` + "\n")
	b.WriteString(`{"type":"assistant","cwd":"/Users/me/projects/deepmodel","message":{"role":"assistant","model":"claude-opus-4-8","content":[]}}` + "\n")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))

	info := buildSessionInfo(jsonlFileEntry{
		sessionID:         sessID,
		filePath:          path,
		projectDirEncoded: "-enc",
		mtime:             time.Unix(0, 0),
	}, nil)

	require.NotNil(t, info.Model, "model should be recovered from tail")
	require.Equal(t, "claude-opus-4-8", *info.Model)
	require.Equal(t, "deepmodel", info.ProjectName, "cwd captured from head despite deep model")
}

// buildAssistantLine constructs a minimal JSONL line with an assistant role.
func buildAssistantLine(t *testing.T, blocks []map[string]any) string {
	t.Helper()
	content, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": json.RawMessage(content),
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal msg: %v", err)
	}
	entry := map[string]any{
		"type":      "assistant",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"message":   json.RawMessage(msgBytes),
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	return string(b)
}

// TestParseOutputMessages_SimpleAssistantMessage verifies that a plain text
// assistant block is parsed into a single "assistant" OutputMessage.
func TestParseOutputMessages_SimpleAssistantMessage(t *testing.T) {
	line := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "Hello, world!"},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 1)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "Hello, world!", msgs[0].Content)
}

// TestParseOutputMessages_ToolUseBlock verifies that a tool_use block is parsed
// into a "tool_call" OutputMessage with the correct tool name.
func TestParseOutputMessages_ToolUseBlock(t *testing.T) {
	inputJSON, _ := json.Marshal(map[string]any{"file_path": "/foo/bar.go"})
	line := buildAssistantLine(t, []map[string]any{
		{
			"type":  "tool_use",
			"id":    "tu_abc123",
			"name":  "Read",
			"input": json.RawMessage(inputJSON),
		},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 1)
	require.Equal(t, "tool_call", msgs[0].Role)
	require.Equal(t, "Read", msgs[0].Content)
	require.NotNil(t, msgs[0].ToolName)
	require.Equal(t, "Read", *msgs[0].ToolName)
	require.NotNil(t, msgs[0].FilePath)
	require.Equal(t, "/foo/bar.go", *msgs[0].FilePath)
}

// TestParseOutputMessages_EmptyInput verifies that parseOutputMessages does not
// panic and returns an empty (non-nil) slice for empty input.
func TestParseOutputMessages_EmptyInput(t *testing.T) {
	require.NotPanics(t, func() {
		msgs := parseOutputMessages("", false)
		require.Empty(t, msgs)
	})
}

// TestParseOutputMessages_MultipleBlocks verifies that multiple content blocks
// in a single assistant message are all returned.
func TestParseOutputMessages_MultipleBlocks(t *testing.T) {
	inputJSON, _ := json.Marshal(map[string]any{"path": "/tmp/x"})
	line := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "I will read the file."},
		{
			"type":  "tool_use",
			"id":    "tu_xyz",
			"name":  "Read",
			"input": json.RawMessage(inputJSON),
		},
		{"type": "text", "text": "Done."},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 3)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "tool_call", msgs[1].Role)
	require.Equal(t, "assistant", msgs[2].Role)
}

// TestParseOutputMessages_LastOnly verifies that when lastOnly==true, only the
// final assistant message is returned.
func TestParseOutputMessages_LastOnly(t *testing.T) {
	line1 := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "first"},
	})
	line2 := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "second"},
	})
	raw := line1 + "\n" + line2

	msgs := parseOutputMessages(raw, true)

	require.Len(t, msgs, 1)
	require.Equal(t, "second", msgs[0].Content)
}

// TestParseOutputMessages_QueuedCommandAttachment verifies that an attachment
// entry with type queued_command produces a human message with Queued=true.
func TestParseOutputMessages_QueuedCommandAttachment(t *testing.T) {
	entry := map[string]any{
		"type":      "attachment",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"attachment": map[string]any{
			"type":   "queued_command",
			"prompt": "run the build",
		},
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)

	msgs := parseOutputMessages(string(b), false)

	require.Len(t, msgs, 1)
	require.Equal(t, "human", msgs[0].Role)
	require.True(t, msgs[0].Queued)
	require.Equal(t, "run the build", msgs[0].Content)
}

// TestParseOutputMessages_LegacyResultTruncation verifies that a standalone
// result entry with content longer than 1000 chars is truncated to 1000 chars.
func TestParseOutputMessages_LegacyResultTruncation(t *testing.T) {
	longResult := strings.Repeat("x", 1500)
	entry := map[string]any{
		"type":      "result",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"result":    longResult,
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)

	msgs := parseOutputMessages(string(b), false)

	require.Len(t, msgs, 1)
	require.Equal(t, "tool_result", msgs[0].Role)
	require.Equal(t, 1000, len(msgs[0].Content))
}

// TestParseOutputMessages_TaskCreateRealIDResolution verifies that when a
// TaskCreate tool_use is followed by a tool_result carrying the real task ID,
// the task message's TaskID is updated from the placeholder to the real ID.
func TestParseOutputMessages_TaskCreateRealIDResolution(t *testing.T) {
	inputJSON, _ := json.Marshal(map[string]any{"subject": "Build feature"})
	assistantLine := buildAssistantLine(t, []map[string]any{
		{
			"type":  "tool_use",
			"id":    "tu_task1",
			"name":  "TaskCreate",
			"input": json.RawMessage(inputJSON),
		},
	})

	userMsg := map[string]any{
		"role": "user",
		"content": []map[string]any{
			{
				"type":        "tool_result",
				"tool_use_id": "tu_task1",
				"content":     "real-task-id-42",
			},
		},
	}
	userMsgBytes, _ := json.Marshal(userMsg)
	userEntry := map[string]any{
		"type":      "user",
		"timestamp": "2025-01-15T10:30:01.000Z",
		"message":   json.RawMessage(userMsgBytes),
	}
	userEntryBytes, _ := json.Marshal(userEntry)

	raw := assistantLine + "\n" + string(userEntryBytes)
	msgs := parseOutputMessages(raw, false)

	var taskMsg *OutputMessage
	for i := range msgs {
		if msgs[i].Role == "task" {
			taskMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, taskMsg, "expected a task message")
	require.NotNil(t, taskMsg.TaskID)
	require.Equal(t, "real-task-id-42", *taskMsg.TaskID)
}

// A subagent transcript is a session in its own right, but it sits one level
// deeper: <projects>/<encoded>/<parent>/subagents/<id>.jsonl. Resolving only the
// top level left the dashboard unable to open a subagent at all.
func TestLocateTranscript_FindsMainAndSubagentSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", root)

	const (
		parentID = "22222222-2222-2222-2222-222222222222"
		subID    = "33333333-3333-3333-3333-333333333333"
	)
	projectDir := filepath.Join(root, "projects", "-Users-someone-repo")
	subDir := filepath.Join(projectDir, parentID, "subagents")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	parentPath := filepath.Join(projectDir, parentID+".jsonl")
	subPath := filepath.Join(subDir, subID+".jsonl")
	require.NoError(t, os.WriteFile(parentPath, []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(subPath, []byte("{}\n"), 0o644))

	require.Equal(t, parentPath, locateTranscript(parentID))
	require.Equal(t, subPath, locateTranscript(subID))
	require.Empty(t, locateTranscript("44444444-4444-4444-4444-444444444444"))
}

// A directory named like a session file must not be mistaken for one.
func TestLocateTranscript_IgnoresDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", root)

	const sessionID = "55555555-5555-5555-5555-555555555555"
	decoy := filepath.Join(root, "projects", "-Users-someone-repo", sessionID+".jsonl")
	require.NoError(t, os.MkdirAll(decoy, 0o755))

	require.Empty(t, locateTranscript(sessionID))
}
