package mcpapps

import (
	"encoding/json"
	"fmt"
)

type ServerEntry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func ParseEntry(raw json.RawMessage) (ServerEntry, error) {
	var e ServerEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return ServerEntry{}, fmt.Errorf("mcpapps: parse server entry: %w", err)
	}
	return e, nil
}

func (e ServerEntry) IsStdio() bool {
	return (e.Type == "" || e.Type == "stdio") && e.Command != ""
}

// WithEnv merges env into the entry's "env" object. It works on the raw object
// rather than ServerEntry so fields the Claude CLI may add to an entry are kept.
func WithEnv(raw json.RawMessage, env map[string]string) (json.RawMessage, error) {
	if len(env) == 0 {
		return raw, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("mcpapps: merge env: %w", err)
	}
	merged, _ := obj["env"].(map[string]any)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range env {
		merged[k] = v
	}
	obj["env"] = merged
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("mcpapps: merge env: %w", err)
	}
	return out, nil
}
