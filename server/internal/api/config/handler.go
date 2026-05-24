// Package config provides HTTP handlers for read-only enumeration of the
// user's Claude configuration: installed skills, slash commands, and memory
// (CLAUDE.md / AGENTS.md) files.
//
// All endpoints are read-only and enumerate from a fixed set of filesystem
// prefixes. Path query parameters are never accepted from clients — path
// traversal is impossible by construction.
package config

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxCommandBodyBytes caps the on-disk command file size read into the
// response. Larger files are truncated and a trailing marker is appended.
const maxCommandBodyBytes = 1 * 1024 * 1024 // 1 MB

// homeDir returns the user's home directory. It is a var so tests can override.
var homeDir = func() string {
	h, _ := os.UserHomeDir()
	return h
}

// claudeConfigDirs lists the Claude config root candidates that this package
// enumerates. Currently only ~/.claude is enumerated — DASHBOARD_CLAUDE_CONFIG_DIRS
// is used by the parser/session layer for JSONL discovery, but the config
// explorer intentionally limits itself to the canonical config root.
func claudeConfigDirs() []string {
	h := homeDir()
	if h == "" {
		return nil
	}
	return []string{filepath.Join(h, ".claude")}
}

// SkillEntry describes a single installed skill (user or plugin).
type SkillEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "user" | "plugin:<plugin-id>"
	Description string `json:"description"`
}

// CommandEntry describes a single slash command (user or plugin).
type CommandEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "user" | "plugin:<plugin-id>"
	Description string `json:"description"`
	Body        string `json:"body"`
}

// MemoryEntry describes a single memory file (CLAUDE.md / AGENTS.md).
type MemoryEntry struct {
	Path  string `json:"path"`
	Scope string `json:"scope"` // "user" | "project"
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix seconds
}

// Skills handles GET /api/config/skills.
func Skills(w http.ResponseWriter, r *http.Request) {
	entries := enumerateSkills()
	writeJSON(w, entries)
}

// Commands handles GET /api/config/commands.
func Commands(w http.ResponseWriter, r *http.Request) {
	entries := enumerateCommands()
	writeJSON(w, entries)
}

// Memory handles GET /api/config/memory.
func Memory(w http.ResponseWriter, r *http.Request) {
	entries := enumerateMemoryFiles()
	writeJSON(w, entries)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// enumerateSkills walks the fixed user + plugin skill prefixes and returns
// every SKILL.md discovered. Symlinks that fail to resolve are silently
// skipped.
func enumerateSkills() []SkillEntry {
	out := []SkillEntry{}
	seen := make(map[string]bool) // dedupe by name+source

	for _, root := range claudeConfigDirs() {
		// User skills: ~/.claude/skills/<name>/SKILL.md
		userSkillsDir := filepath.Join(root, "skills")
		for _, entry := range listSkillDirs(userSkillsDir) {
			skillPath := filepath.Join(userSkillsDir, entry, "SKILL.md")
			name, desc := parseSkillFrontmatter(skillPath)
			if name == "" {
				name = entry
			}
			key := "user:" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SkillEntry{Name: name, Source: "user", Description: desc})
		}

		// Plugin skills: ~/.claude/plugins/cache/<plugin-id>/.../skills/**/SKILL.md
		pluginsCache := filepath.Join(root, "plugins", "cache")
		for _, pluginEntry := range listPluginRoots(pluginsCache) {
			pluginID := filepath.Base(pluginEntry)
			for _, skillFile := range findSkillFiles(pluginEntry) {
				name, desc := parseSkillFrontmatter(skillFile)
				if name == "" {
					continue
				}
				source := "plugin:" + pluginID
				key := source + ":" + name
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, SkillEntry{Name: name, Source: source, Description: desc})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// enumerateCommands walks the fixed user + plugin command prefixes.
func enumerateCommands() []CommandEntry {
	out := []CommandEntry{}
	seen := make(map[string]bool)

	for _, root := range claudeConfigDirs() {
		// User commands: ~/.claude/commands/<name>.md
		userCmdsDir := filepath.Join(root, "commands")
		for _, file := range listCommandFiles(userCmdsDir) {
			name, desc, body := readCommandFile(file)
			if name == "" {
				continue
			}
			key := "user:" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, CommandEntry{Name: name, Source: "user", Description: desc, Body: body})
		}

		// Plugin commands: ~/.claude/plugins/cache/<plugin-id>/<plugin-name>/<version>/commands/<name>.md
		pluginsCache := filepath.Join(root, "plugins", "cache")
		for _, pluginEntry := range listPluginRoots(pluginsCache) {
			pluginID := filepath.Base(pluginEntry)
			for _, cmdFile := range findCommandFiles(pluginEntry) {
				name, desc, body := readCommandFile(cmdFile)
				if name == "" {
					continue
				}
				source := "plugin:" + pluginID
				key := source + ":" + name
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, CommandEntry{Name: name, Source: source, Description: desc, Body: body})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// enumerateMemoryFiles lists known memory file scopes:
//   - user:    ~/.claude/CLAUDE.md, ~/.claude/AGENTS.md
//   - project: <cwd>/CLAUDE.md, <cwd>/AGENTS.md, <cwd>/.claude/CLAUDE.md
func enumerateMemoryFiles() []MemoryEntry {
	out := []MemoryEntry{}

	candidates := []struct {
		scope string
		path  string
	}{}

	// User-scope: ~/.claude/CLAUDE.md, ~/.claude/AGENTS.md
	h := homeDir()
	if h != "" {
		candidates = append(candidates,
			struct {
				scope string
				path  string
			}{"user", filepath.Join(h, ".claude", "CLAUDE.md")},
			struct {
				scope string
				path  string
			}{"user", filepath.Join(h, ".claude", "AGENTS.md")},
		)
	}

	// Project-scope: server cwd + .claude/CLAUDE.md
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			struct {
				scope string
				path  string
			}{"project", filepath.Join(cwd, "CLAUDE.md")},
			struct {
				scope string
				path  string
			}{"project", filepath.Join(cwd, "AGENTS.md")},
			struct {
				scope string
				path  string
			}{"project", filepath.Join(cwd, ".claude", "CLAUDE.md")},
		)
	}

	for _, c := range candidates {
		info, err := os.Stat(c.path)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, MemoryEntry{
			Path:  c.path,
			Scope: c.scope,
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// listSkillDirs returns the names of immediate children of dir that are
// directories (or symlinks resolving to directories with a SKILL.md inside).
// Dangling symlinks are silently skipped.
func listSkillDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		// Skip files like SKILL.md at the root (none expected) and hidden entries.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Resolve symlinks: only include entries that have a SKILL.md.
		skillFile := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// listCommandFiles returns absolute paths of *.md files directly inside dir.
func listCommandFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out
}

// listPluginRoots returns the per-plugin top-level directories under the
// plugin cache, e.g. ~/.claude/plugins/cache/<plugin-id>.
func listPluginRoots(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, filepath.Join(cacheDir, e.Name()))
	}
	sort.Strings(out)
	return out
}

// findSkillFiles walks root recursively looking for SKILL.md files.
// node_modules and hidden dirs are pruned. The walk is bounded to keep the
// endpoint fast on plugin caches with deep trees.
func findSkillFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "SKILL.md" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// findCommandFiles walks root looking for *.md files inside any directory
// named "commands". Hidden dirs and node_modules are pruned.
func findCommandFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		// Only include if the file's immediate parent dir is "commands".
		if filepath.Base(filepath.Dir(path)) == "commands" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// parseSkillFrontmatter reads the YAML frontmatter of a SKILL.md and returns
// (name, description). Mirrors parser/skills.go but does not prepend a slash.
func parseSkillFrontmatter(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNum := 0
	inFrontmatter := false
	var name, description string
	readingDesc := false

	for sc.Scan() {
		lineNum++
		if lineNum > 80 {
			break
		}
		line := sc.Text()

		if lineNum == 1 {
			if line == "---" {
				inFrontmatter = true
				continue
			}
			return "", ""
		}
		if !inFrontmatter {
			break
		}
		if line == "---" {
			break
		}

		if readingDesc && description == "" && strings.HasPrefix(line, " ") {
			description = strings.TrimSpace(line)
			readingDesc = false
			continue
		}
		readingDesc = false

		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), `"'`)
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			switch desc {
			case ">-", ">", "|", "|-":
				readingDesc = true
			default:
				description = strings.Trim(desc, `"'`)
			}
		}
	}
	return name, description
}

// readCommandFile returns (name, description, body) for a slash command markdown
// file. Name defaults to the filename (without .md). Description is read from
// optional YAML frontmatter (`description:` field). Body is the entire file
// content, capped at maxCommandBodyBytes.
func readCommandFile(path string) (string, string, string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", ""
	}
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	if name == "" {
		return "", "", ""
	}

	f, err := os.Open(path)
	if err != nil {
		return name, "", ""
	}
	defer f.Close()

	// Cap the read size; if file is larger, mark it as truncated.
	limit := int64(maxCommandBodyBytes)
	truncated := info.Size() > limit
	readSize := info.Size()
	if truncated {
		readSize = limit
	}
	buf := make([]byte, readSize)
	n, _ := f.Read(buf)
	body := string(buf[:n])
	if truncated {
		body += "\n\n<!-- truncated: file exceeds 1 MB limit -->\n"
	}

	desc := parseCommandDescription(body)
	return name, desc, body
}

// parseCommandDescription extracts the `description:` field from a markdown
// file's YAML frontmatter if present. Returns "" if no frontmatter exists.
func parseCommandDescription(body string) string {
	if !strings.HasPrefix(body, "---") {
		return ""
	}
	rest := strings.TrimPrefix(body, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	frontmatter := rest[:end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "description:") {
			d := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			return strings.Trim(d, `"'`)
		}
	}
	return ""
}
