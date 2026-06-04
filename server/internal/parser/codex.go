// Package parser — Codex (OpenAI) JSONL session parser.
//
// # Codex Session Schema (research findings, 2026-05-24)
//
// The openai/codex CLI (https://github.com/openai/codex) stores conversation
// state in ~/.codex/. As of the 0.1.x series the CLI does NOT write JSONL
// session logs that mirror Claude Code's format. Persistence is limited to:
//   - ~/.codex/config.toml  — user config (model, approval policy)
//   - ~/.codex/history      — a plain-text readline history file
//
// There is no per-project JSONL directory structure equivalent to
// ~/.claude/projects/{encoded}/{sessionId}.jsonl.
//
// This parser is a forward-compatible stub: if a future Codex release adds
// JSONL-based session logs, ParseCodexSession should be updated to decode
// that schema. Until then, ParseCodexSession accepts any file path and
// returns a minimal SessionData with Provider="codex", marking costs as
// unknown (no token-count fields available to estimate against).
//
// The detection path in AllAgentConfigDirs activates as soon as ~/.codex
// (or $CODEX_HOME) exists on disk, so new Codex sessions discovered via
// future scanner support will flow through here automatically.
package parser

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// ParseCodexSession parses a Codex session file at path.
// It returns a minimal SessionData with Provider=codex and CostUnknown=true
// because the Codex CLI schema does not expose token counts.
//
// If the file does not exist or cannot be read, an error is returned.
// Token counts and model fields are left at zero-value — callers must set
// CostUnknown=true on the resulting Agent (handled by merger.go).
func ParseCodexSession(path string) (*SessionData, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Derive a session ID from the file name (strip .jsonl extension).
	base := filepath.Base(path)
	sessionID := strings.TrimSuffix(base, ".jsonl")

	data := &SessionData{
		SessionID:    sessionID,
		ToolCounts:   make(map[string]int),
		Entrypoint:   sdk.EntrypointCLI,
		LastActivity: info.ModTime(),
		// Model is unknown for Codex sessions without a parsed schema.
		// Token usage remains zero; CostUnknown will be set by the merger.
	}

	// Attempt a best-effort parse: if the file looks like JSONL with
	// Claude-compatible entries, reuse the standard parser so we get
	// real timestamps and turn counts. Otherwise, fall through to the
	// stub above (mtime-only session data).
	if strings.HasSuffix(path, ".jsonl") {
		// NOTE: this calls ParseSessionFile directly, bypassing the session cache —
		// the whole-file token scan therefore runs on every call. When real Codex
		// JSONL lands, route through FindSessionForProject (cached) or accept the
		// per-call full scan deliberately for these (currently small/absent) files.
		if parsed, err2 := ParseSessionFile(path); err2 == nil {
			parsed.SessionID = sessionID
			// Override last activity with at least file mtime when parse
			// returns the default 24h-ago sentinel.
			if time.Since(parsed.LastActivity) > 12*time.Hour {
				parsed.LastActivity = info.ModTime()
			}
			return parsed, nil
		}
	}

	return data, nil
}
