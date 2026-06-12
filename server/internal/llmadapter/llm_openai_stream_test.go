package llmadapter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAISpawner_SpawnStream_SSE(t *testing.T) {
	t.Setenv("OPENAI_API_KEY_FAKE", "sk-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi \"}}]}\n\n")
		f.Flush()
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"there\"}}]}\n\n")
		f.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer srv.Close()

	o := &OpenAISpawner{BaseURL: srv.URL, APIKeyEnv: "OPENAI_API_KEY_FAKE", DefaultModel: "gpt-4"}
	ch, err := o.SpawnStream(context.Background(), LLMSpawnArgs{SystemPrompt: "sys", UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("SpawnStream: %v", err)
	}
	var got []string
	for s := range ch {
		got = append(got, s)
	}
	want := []string{"Hi ", "there"}
	if len(got) != len(want) {
		t.Fatalf("chunks: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
