package provider

import (
	"fmt"
	"strings"
)

// TokenMode selects how token fields aggregate across matching JSONL lines.
type TokenMode string

const (
	// TokenCumulative: the last matching value is the session total (e.g. Codex).
	TokenCumulative TokenMode = "cumulative"
	// TokenPerMessage: sum the value across every matching line (e.g. Gemini, Junie).
	TokenPerMessage TokenMode = "perMessage"
)

// CostRule selects how a session's cost is derived.
type CostRule string

const (
	CostByModel CostRule = "byModel" // pricing-table lookup
	CostInFile  CostRule = "inFile"  // provider supplies cost in the session file
	CostNone    CostRule = "unknown" // always CostUnknown
)

// Descriptor declares one provider. source "jsonl" is fully declarative;
// source "custom:<id>" routes to a registered Adapter (none built in this plan).
type Descriptor struct {
	ID            string        `yaml:"id"`
	DisplayName   string        `yaml:"displayName"`
	Enabled       bool          `yaml:"enabled"`
	ExeNames      []string      `yaml:"exeNames"`
	ConfigDir     ConfigDirSpec `yaml:"configDir"`
	SessionGlob   string        `yaml:"sessionGlob"`
	SessionIDFrom string        `yaml:"sessionIdFrom"`
	Source        string        `yaml:"source"`
	Parse         ParseSpec     `yaml:"parse"`
	Cost          CostSpec      `yaml:"cost"`
}

type ConfigDirSpec struct {
	Env     string `yaml:"env"`
	Default string `yaml:"default"`
}

type ParseSpec struct {
	EventFilter *EventFilter `yaml:"eventFilter"`
	Tokens      TokenSpec    `yaml:"tokens"`
	Model       []string     `yaml:"model"`
	Provider    []string     `yaml:"provider"`
	Timestamp   []string     `yaml:"timestamp"` // JSON paths to per-line timestamp field
}

// EventFilter restricts token extraction to JSONL lines where Path == Equals.
type EventFilter struct {
	Path   string `yaml:"path"`
	Equals string `yaml:"equals"`
}

type TokenSpec struct {
	Mode        TokenMode `yaml:"mode"`
	Input       []string  `yaml:"input"`
	Output      []string  `yaml:"output"`
	CacheRead   []string  `yaml:"cacheRead"`
	CacheCreate []string  `yaml:"cacheCreate"`
}

type CostSpec struct {
	Rule       CostRule `yaml:"rule"`
	InFilePath []string `yaml:"inFilePath"`
	LocalIf    *LocalIf `yaml:"localIf"`
}

// LocalIf marks a session as local ($0) when the provider matches or the model
// is a currently-installed Ollama tag.
type LocalIf struct {
	ProviderEquals      string `yaml:"providerEquals"`
	OrModelInOllamaTags bool   `yaml:"orModelInOllamaTags"`
}

// IsCustom reports whether the descriptor routes to an Adapter.
func (d Descriptor) IsCustom() bool { return strings.HasPrefix(d.Source, "custom:") }

// AdapterID returns the adapter key for a custom source ("custom:cursor" -> "cursor").
func (d Descriptor) AdapterID() string { return strings.TrimPrefix(d.Source, "custom:") }

// Validate checks structural invariants. A failing descriptor is dropped at
// load time (logged) so a bad file never crashes the scan.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("descriptor: id is required")
	}
	if len(d.ExeNames) == 0 {
		return fmt.Errorf("descriptor %q: exeNames is required", d.ID)
	}
	if d.Source == "jsonl" {
		if d.SessionGlob == "" {
			return fmt.Errorf("descriptor %q: sessionGlob required for source jsonl", d.ID)
		}
		switch d.SessionIDFrom {
		case "", "filename", "parentDir":
		default:
			return fmt.Errorf("descriptor %q: unknown sessionIdFrom %q", d.ID, d.SessionIDFrom)
		}
		switch d.Parse.Tokens.Mode {
		case "", TokenCumulative, TokenPerMessage:
		default:
			return fmt.Errorf("descriptor %q: unknown token mode %q", d.ID, d.Parse.Tokens.Mode)
		}
		return nil
	}
	if d.IsCustom() {
		return nil
	}
	return fmt.Errorf("descriptor %q: unknown source %q", d.ID, d.Source)
}
