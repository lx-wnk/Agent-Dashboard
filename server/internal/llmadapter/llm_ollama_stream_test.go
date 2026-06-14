package llmadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaSpawner_SpawnStream_NDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]any{"message": map[string]string{"content": "hello "}})
		_ = enc.Encode(map[string]any{"message": map[string]string{"content": "world"}})
		_ = enc.Encode(map[string]any{"done": true})
	}))
	defer srv.Close()

	o := &OllamaSpawner{Host: srv.URL, DefaultModel: "llama3"}
	ch, err := o.SpawnStream(context.Background(), LLMSpawnArgs{SystemPrompt: "sys", UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"hello ", "world"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
