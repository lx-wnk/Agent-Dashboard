package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a simple REST client for the dashboard API.
type Client struct {
	host  string
	token string
	http  *http.Client
}

func newClient(cfg CLIConfig) *Client {
	return &Client{
		host:  strings.TrimRight(cfg.Host, "/"),
		token: cfg.Token,
		http:  &http.Client{},
	}
}

// get performs GET {path} and decodes the JSON response into out.
func (c *Client) get(path string, out any) error {
	return c.do(http.MethodGet, path, nil, out)
}

// post performs POST {path} with jsonBody and decodes the JSON response into out.
func (c *Client) post(path string, body any, out any) error {
	return c.do(http.MethodPost, path, body, out)
}

// delete performs DELETE {path}.
func (c *Client) delete(path string) error {
	return c.do(http.MethodDelete, path, nil, nil)
}

func (c *Client) do(method, path string, reqBody, out any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.host+path, bodyReader)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// Provide a helpful message for connection refused.
		if strings.Contains(err.Error(), "connection refused") {
			return fmt.Errorf("connection refused — is the dashboard server running at %s?", c.host)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// stream opens an SSE connection and calls onEvent for each data line.
// Returns when ctx is cancelled or the server closes the stream.
func (c *Client) stream(path string, onEvent func(data []byte)) error {
	req, err := http.NewRequest(http.MethodGet, c.host+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			lines := strings.Split(string(buf[:n]), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					onEvent([]byte(strings.TrimPrefix(line, "data: ")))
				}
			}
		}
		if err != nil {
			return nil
		}
	}
}
