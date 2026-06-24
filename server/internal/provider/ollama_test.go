package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllama_ModelInTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:latest"}]}`))
	}))
	defer srv.Close()
	oc := NewOllamaClassifier(srv.URL)

	if !oc.IsLocal("ollama", "qwen2.5-coder:7b") {
		t.Fatal("expected qwen2.5-coder:7b to be local")
	}
	if !oc.IsLocal("", "ollama_chat/llama3.2:latest") {
		t.Fatal("expected prefixed model to be local")
	}
	if oc.IsLocal("", "gpt-5-codex") {
		t.Fatal("expected cloud model not local")
	}
}

func TestOllama_ProviderEqualsAlwaysLocal(t *testing.T) {
	oc := NewOllamaClassifier("http://127.0.0.1:1")
	if !oc.IsLocal("ollama", "anything") {
		t.Fatal("provider==ollama must classify local without tags")
	}
}

func TestOllama_UnreachableFallsBackToCloud(t *testing.T) {
	oc := NewOllamaClassifier("http://127.0.0.1:1")
	if oc.IsLocal("", "qwen2.5-coder:7b") {
		t.Fatal("unreachable ollama must not classify by-name as local")
	}
}
