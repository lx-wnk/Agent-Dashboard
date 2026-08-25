// Package claudesettings reads the parts of Claude Code's own settings files
// that the dashboard must respect.
//
// Today that is the deny list. A PreToolUse hook answering "allow"
// short-circuits Claude Code's permission evaluation entirely, including rules
// the user wrote by hand — so without this the dashboard would let one click
// override a `"deny": ["Bash(rm:*)"]` the user reasonably believes is absolute.
// The bridge offers no Allow for a call the user's own rules forbid.
package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DenyRule is one entry of permissions.deny, parsed into the parts the matcher
// needs. Raw is kept so the UI can name the rule that blocked a call.
type DenyRule struct {
	Raw     string
	Tool    string
	Pattern string // the text inside the parentheses; empty when the rule is bare
}

// Matches reports whether this rule forbids a call of tool with argument arg.
//
// The supported forms are the ones Claude Code documents:
//
//	Tool                 every call of that tool
//	Tool(prefix:*)       an argument starting with prefix
//	Tool(exact)          exactly that argument
//	Tool(domain:host)    WebFetch against that host
//
// Anything else is treated as matching whenever the tool matches. That is
// deliberate and it is the safe direction: an unrecognised rule suppresses the
// Allow button rather than overriding a restriction nobody has parsed. The user
// can still answer in their terminal, which is where the rule is evaluated for
// real — this matcher never grants, it only declines to offer.
func (r DenyRule) Matches(tool, arg string) bool {
	if !strings.EqualFold(r.Tool, tool) {
		return false
	}
	if r.Pattern == "" {
		return true
	}
	if prefix, ok := strings.CutSuffix(r.Pattern, ":*"); ok {
		return strings.HasPrefix(arg, prefix)
	}
	if host, ok := strings.CutPrefix(r.Pattern, "domain:"); ok {
		return strings.Contains(arg, host)
	}
	if r.Pattern == arg {
		return true
	}
	// Unrecognised shape — a glob, a regex, something newer. Decline to offer.
	return !isPlainArgument(r.Pattern)
}

// isPlainArgument reports whether a rule pattern is a literal this matcher
// understands completely, so a non-match on it can be trusted.
func isPlainArgument(pattern string) bool {
	return !strings.ContainsAny(pattern, "*?[]")
}

// FirstMatch returns the first rule forbidding this call, or nil.
func FirstMatch(rules []DenyRule, tool, arg string) *DenyRule {
	for i := range rules {
		if rules[i].Matches(tool, arg) {
			return &rules[i]
		}
	}
	return nil
}

// ParseDenyRules turns raw permissions.deny entries into rules.
func ParseDenyRules(raw []string) []DenyRule {
	out := make([]DenyRule, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		rule := DenyRule{Raw: entry, Tool: entry}
		if open := strings.Index(entry, "("); open > 0 && strings.HasSuffix(entry, ")") {
			rule.Tool = entry[:open]
			rule.Pattern = entry[open+1 : len(entry)-1]
		}
		out = append(out, rule)
	}
	return out
}

// maxCachedFiles bounds the per-path cache. The working directory that keys the
// project half of a lookup arrives in a hook payload, so the map is grown by an
// outside caller; past the cap it is dropped whole rather than pruned, which
// costs one re-read of a handful of small files.
const maxCachedFiles = 64

type cachedFile struct {
	rules   []DenyRule
	modTime time.Time
	size    int64
	missing bool
}

// Reader loads deny rules from Claude Code's settings files, re-reading a file
// only when it changes. The bridge consults it while holding a hook call, so it
// must not parse on every call — but it must also not serve a rule the user
// deleted five minutes ago.
type Reader struct {
	configDirs []string

	mu    sync.Mutex
	cache map[string]cachedFile
}

// NewReader builds a Reader over the user-level config directories — every dir
// the dashboard already knows about, because a session can run under a custom
// CLAUDE_CONFIG_DIR and its rules live there.
func NewReader(configDirs ...string) *Reader {
	return &Reader{configDirs: configDirs, cache: map[string]cachedFile{}}
}

// DenyRules returns every deny rule that applies to a session working in cwd:
// the user-level rules plus the project's own. Claude Code unions deny across
// all scopes — a deny is never relaxed by a narrower file — so the union is the
// correct set here too. cwd may be empty, which yields the user-level rules
// alone.
//
// A missing or malformed settings file yields no rules from that file: it is
// Claude Code's own, and failing to parse it is not a reason to start blocking
// things the user did not ask to block.
func (r *Reader) DenyRules(cwd string) []DenyRule {
	if r == nil {
		return nil
	}
	paths := make([]string, 0, len(r.configDirs)+2)
	for _, dir := range r.configDirs {
		paths = append(paths, filepath.Join(dir, "settings.json"))
	}
	if cwd != "" && filepath.IsAbs(cwd) {
		paths = append(paths,
			filepath.Join(cwd, ".claude", "settings.json"),
			filepath.Join(cwd, ".claude", "settings.local.json"),
		)
	}

	var out []DenyRule
	for _, p := range paths {
		out = append(out, r.rulesIn(p)...)
	}
	return out
}

func (r *Reader) rulesIn(path string) []DenyRule {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		r.store(path, cachedFile{missing: true})
		return nil
	}
	if c, ok := r.cache[path]; ok && !c.missing && c.modTime.Equal(info.ModTime()) && c.size == info.Size() {
		return c.rules
	}

	entry := cachedFile{modTime: info.ModTime(), size: info.Size()}
	defer func() { r.store(path, entry) }()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return nil
	}
	entry.rules = ParseDenyRules(parsed.Permissions.Deny)
	return entry.rules
}

func (r *Reader) store(path string, entry cachedFile) {
	if len(r.cache) >= maxCachedFiles {
		r.cache = map[string]cachedFile{}
	}
	r.cache[path] = entry
}
