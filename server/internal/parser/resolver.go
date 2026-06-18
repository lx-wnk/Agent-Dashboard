package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// ProviderSessionResolver parses a single session-log file into SessionData.
type ProviderSessionResolver func(path string) (*SessionData, error)

// resolverFor returns the session-file parser for a provider. Codex and Gemini
// share the Codex best-effort stub until their real JSONL schema is known; any
// other provider (including Claude) uses the full Claude parser.
func resolverFor(p sdk.Provider) ProviderSessionResolver {
	switch p {
	case sdk.ProviderCodex:
		return ParseCodexSession
	case sdk.ProviderGemini:
		return ParseCodexSession
	default:
		return ParseSessionFile
	}
}

// ResolveNonClaudeSession resolves the newest session-log file for a non-Claude
// process by scanning that provider's config directories for
// <configDir>/projects/<encoded-cwd>/*.jsonl and parsing the first match with
// the provider's resolver.
//
// It is intentionally separate from ResolveSessionForProcess, which depends on
// Claude-specific pid-session JSON. Missing or unreadable provider directories
// are silently skipped (no error, no log). claimed, when non-nil, excludes
// session IDs already bound to other processes and records the resolved ID, so
// two same-folder agents stay on distinct files. An error is returned only when
// no session file exists under any matching config dir — callers skip such
// processes silently.
func ResolveNonClaudeSession(provider sdk.Provider, cwd string, claimed map[string]bool) (*SessionData, error) {
	encoded := EncodePath(cwd)
	resolve := resolverFor(provider)

	for _, dir := range providerConfigDirs(provider) {
		projectDir := filepath.Join(dir, "projects", encoded)
		for _, c := range listJSONLByMtime(projectDir) {
			id := strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
			if claimed != nil && claimed[id] {
				continue
			}
			data, err := resolve(c.path)
			if err != nil {
				continue
			}
			data.SessionID = id
			data.ProjectPath = cwd
			if claimed != nil {
				claimed[id] = true
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("no %s session for %s", provider, cwd)
}

// providerConfigDirs returns the config-dir paths registered for one provider
// in AllAgentConfigDirs (which already skips dirs absent from disk).
func providerConfigDirs(provider sdk.Provider) []string {
	var out []string
	for _, pcd := range AllAgentConfigDirs() {
		if pcd.Provider == provider {
			out = append(out, pcd.Path)
		}
	}
	return out
}

// listJSONLByMtime lists *.jsonl files in dir, newest first. A missing or
// unreadable directory yields nil with no error, so absent provider project
// directories are silently skipped. Unlike statSessionFiles it does not require
// UUID-shaped names — foreign CLIs may name session files differently.
func listJSONLByMtime(dir string) []sessionFileCandidate {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sessionFileCandidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, sessionFileCandidate{
			path:  filepath.Join(dir, e.Name()),
			mtime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].mtime.After(out[j].mtime)
	})
	return out
}
