package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func writeTempJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "session.jsonl")
	var content string
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func assistantTodoWriteLine(todos []map[string]string) string {
	input, _ := json.Marshal(map[string]any{"todos": todos})
	block, _ := json.Marshal(map[string]any{
		"type":  "tool_use",
		"name":  "TodoWrite",
		"input": json.RawMessage(input),
	})
	content, _ := json.Marshal([]json.RawMessage{block})
	msg, _ := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": json.RawMessage(content),
		"model":   "claude-opus-4-5",
		"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
	})
	line, _ := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"message":   json.RawMessage(msg),
	})
	return string(line)
}

func TestParseSessionFile_TaskExtraction(t *testing.T) {
	todos := []map[string]string{
		{"id": "1", "content": "Implement feature X", "status": "in_progress"},
		{"id": "2", "content": "Write tests", "status": "pending"},
	}
	path := writeTempJSONL(t, []string{assistantTodoWriteLine(todos)})

	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(data.Tasks))
	}
	if data.Tasks[0].ID != "1" || data.Tasks[0].Subject != "Implement feature X" || data.Tasks[0].Status != "in_progress" {
		t.Errorf("task 0 mismatch: %+v", data.Tasks[0])
	}
	if data.Tasks[1].ID != "2" || data.Tasks[1].Status != "pending" {
		t.Errorf("task 1 mismatch: %+v", data.Tasks[1])
	}
}

func TestParseSessionFile_LastTodoWriteWins(t *testing.T) {
	first := assistantTodoWriteLine([]map[string]string{
		{"id": "1", "content": "Old task", "status": "pending"},
	})
	second := assistantTodoWriteLine([]map[string]string{
		{"id": "1", "content": "Old task", "status": "done"},
		{"id": "2", "content": "New task", "status": "in_progress"},
	})
	path := writeTempJSONL(t, []string{first, second})

	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 2 {
		t.Fatalf("want 2 tasks from last TodoWrite, got %d", len(data.Tasks))
	}
	if data.Tasks[0].Status != "done" {
		t.Errorf("want task 0 status 'done', got %q", data.Tasks[0].Status)
	}
}

func TestParseSessionFile_NoTasks(t *testing.T) {
	todos := []map[string]string{}
	path := writeTempJSONL(t, []string{assistantTodoWriteLine(todos)})
	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 0 {
		t.Errorf("want 0 tasks, got %d", len(data.Tasks))
	}
}
