package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func mockMessages(text string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-8",
			"stop_reason":"end_turn",
			"content":[{"type":"text","text":` + jsonString(text) + `}],
			"usage":{"input_tokens":5,"output_tokens":3}
		}`))
	}))
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestRunOnce_WritesSyntheticJSONLAndResult(t *testing.T) {
	srv := mockMessages("hello from claude")
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	args := spawnArgs{StageRunID: "stage-99", SystemPrompt: "be terse", UserPrompt: "hi", Model: "claude-opus-4-8"}
	var out bytes.Buffer
	if err := runOnce(args, &out); err != nil {
		t.Fatal(err)
	}

	var res spawnResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("stdout must be a single LLMSpawnResult JSON, got %q: %v", out.String(), err)
	}
	if res.SessionID != "anthropic-stage-99" {
		t.Fatalf("session id: got %q", res.SessionID)
	}
	if res.SessionFile == "" {
		t.Fatal("session file path must be set")
	}

	data, err := os.ReadFile(res.SessionFile)
	if err != nil {
		t.Fatalf("read synthetic session: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, `{"type":"assistant"`) || !strings.Contains(line, `"text":"hello from claude"`) {
		t.Fatalf("synthetic JSONL shape wrong: %s", line)
	}
}
