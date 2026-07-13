// Package ollamaclient is the shared HTTP transport for talking to a local
// Ollama server. It owns base-URL resolution and the /api/chat and /api/tags
// wire round-trips only — no caching, no JSONL writing, no cost/locality
// policy. Those concerns stay with the caller (llmadapter.OllamaSpawner and
// provider.OllamaClassifier respectively).
package ollamaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultHost is used when New is given an empty host string.
const DefaultHost = "http://localhost:11434"

// Client is a minimal Ollama HTTP transport.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client for host, trimming a trailing slash and defaulting to
// DefaultHost when host is empty. timeout is applied to every request.
func New(host string, timeout time.Duration) *Client {
	base := strings.TrimRight(strings.TrimSpace(host), "/")
	if base == "" {
		base = DefaultHost
	}
	return &Client{
		baseURL: base,
		http:    &http.Client{Timeout: timeout},
	}
}

// ChatMessage is one message in a /api/chat request or response.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the /api/chat request body.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatResponse is one /api/chat response frame (the whole body for a
// non-streaming call, or a single NDJSON line for a streaming one).
type ChatResponse struct {
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
}

// Chat performs a non-streaming POST /api/chat round-trip.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Stream = false
	resp, err := c.doChat(ctx, req)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	var out ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatResponse{}, fmt.Errorf("ollamaclient: decode chat response: %w", err)
	}
	return out, nil
}

// ChatStream performs a streaming POST /api/chat round-trip and returns the
// response body for the caller to decode as successive NDJSON ChatResponse
// frames. The caller owns closing the returned body.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
	req.Stream = true
	resp, err := c.doChat(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) doChat(ctx context.Context, req ChatRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ollamaclient: marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollamaclient: build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollamaclient: POST /api/chat: %w", err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollamaclient: HTTP %d: %s", resp.StatusCode, b)
	}
	return resp, nil
}

// modelTag is one entry of the /api/tags response.
type modelTag struct {
	Name string `json:"name"`
}

// Tags performs a GET /api/tags round-trip and returns the installed model
// names. A transport-level failure (unreachable host, non-2xx, bad body) is
// returned as an error — callers that want an empty-set fallback handle that
// themselves.
func (c *Client) Tags(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollamaclient: build tags request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollamaclient: GET /api/tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollamaclient: HTTP %d: %s", resp.StatusCode, b)
	}

	var body struct {
		Models []modelTag `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollamaclient: decode tags response: %w", err)
	}

	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
