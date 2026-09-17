package mcpapps

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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

// IsEmptyEntry reports whether raw is the schema default for
// MCPApplication.Entry — unset or "{}" — meaning nobody has stored a server
// definition for this application yet.
func IsEmptyEntry(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "{}"
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

// entryKeys are the object keys ServerEntry owns, derived from its own tags so
// the list cannot drift from the struct.
var entryKeys = entryJSONKeys()

func entryJSONKeys() []string {
	t := reflect.TypeOf(ServerEntry{})
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		keys = append(keys, name)
	}
	return keys
}

// MergeEntry writes the fields the app can edit over a stored entry and keeps
// every other key, so editing a server that came from Claude's config does not
// drop what the CLI wrote there. A field the caller leaves empty is removed,
// which is what an edit form clearing it means.
func MergeEntry(stored json.RawMessage, entry ServerEntry) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if !IsEmptyEntry(stored) {
		if err := json.Unmarshal(stored, &obj); err != nil {
			return nil, fmt.Errorf("mcpapps: parse stored entry: %w", err)
		}
	}
	next, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(next, &fields); err != nil {
		return nil, err
	}
	for _, key := range entryKeys {
		if v, ok := fields[key]; ok {
			obj[key] = v
			continue
		}
		delete(obj, key)
	}
	return json.Marshal(obj)
}
