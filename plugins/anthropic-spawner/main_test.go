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

func mockStream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		write := func(s string) { _, _ = w.Write([]byte(s)); if fl != nil { fl.Flush() } }
		write("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-8\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		write("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"alpha\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"beta\"}}\n\n")
		write("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n")
		write("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
}

func TestRunStream_EmitsTextDeltaLines(t *testing.T) {
	srv := mockStream()
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	var out bytes.Buffer
	if err := runStream(spawnArgs{StageRunID: "s", UserPrompt: "hi", Model: "claude-opus-4-8"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(strings.TrimSpace(out.String()))
	if len(lines) != 2 || lines[0] != "alpha" || lines[1] != "beta" {
		t.Fatalf("want [alpha beta] as separate lines, got %q", out.String())
	}
}

func TestRun_DefaultModelApplied(t *testing.T) {
	srv := mockMessages("ok")
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	in := bytes.NewBufferString(`{"StageRunID":"d","UserPrompt":"hi","Stream":false}`)
	var out bytes.Buffer
	if err := run(in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"SessionID":"anthropic-d"`) {
		t.Fatalf("default-model run failed: %s", out.String())
	}
}

func TestRunOnce_RefusalIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-opus-4-8","stop_reason":"refusal","content":[],"usage":{"input_tokens":1,"output_tokens":0}}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	if err := runOnce(spawnArgs{StageRunID: "r", UserPrompt: "x", Model: "claude-opus-4-8"}, &bytes.Buffer{}); err == nil {
		t.Fatal("refusal must return an error")
	}
}
