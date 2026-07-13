package ollamaclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew_HostNormalization(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"trailing slash trimmed", "http://localhost:11434/", "http://localhost:11434"},
		{"empty defaults", "", DefaultHost},
		{"whitespace-only defaults", "   ", DefaultHost},
		{"passthrough", "http://127.0.0.1:1", "http://127.0.0.1:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.host, time.Second)
			if c.baseURL != tc.want {
				t.Errorf("baseURL = %q, want %q", c.baseURL, tc.want)
			}
		})
	}
}

func TestClient_Chat_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi there"},"done":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model: "llama3",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "hi there" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "hi there")
	}
	if !resp.Done {
		t.Errorf("done = false, want true")
	}
}

func TestClient_Chat_ErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	_, err := c.Chat(context.Background(), ChatRequest{Model: "llama3"})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want it to mention HTTP 500 and body", err.Error())
	}
}

func TestClient_Chat_UnreachableHost(t *testing.T) {
	c := New("http://127.0.0.1:1", 200*time.Millisecond)
	_, err := c.Chat(context.Background(), ChatRequest{Model: "llama3"})
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "POST /api/chat") {
		t.Errorf("error = %q, want it to mention POST /api/chat", err.Error())
	}
}

func TestClient_Tags_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %q, want /api/tags", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:latest"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	names, err := c.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	want := []string{"qwen2.5-coder:7b", "llama3.2:latest"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestClient_Tags_UnreachableHost(t *testing.T) {
	c := New("http://127.0.0.1:1", 200*time.Millisecond)
	_, err := c.Tags(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "GET /api/tags") {
		t.Errorf("error = %q, want it to mention GET /api/tags", err.Error())
	}
}

func TestClient_ChatStream_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"content":"hello "},"done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"message":{"content":"world"},"done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"done":true}` + "\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	body, err := c.ChatStream(context.Background(), ChatRequest{Model: "llama3"})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer body.Close()

	dec := json.NewDecoder(body)
	var chunks []string
	for dec.More() {
		var chunk ChatResponse
		if err := dec.Decode(&chunk); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if chunk.Done {
			break
		}
		chunks = append(chunks, chunk.Message.Content)
	}
	want := []string{"hello ", "world"}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %v, want %v", chunks, want)
	}
	for i := range want {
		if chunks[i] != want[i] {
			t.Errorf("chunk[%d] = %q, want %q", i, chunks[i], want[i])
		}
	}
}
