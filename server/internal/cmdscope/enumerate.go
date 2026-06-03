package cmdscope

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SlashCommand is one slash command available within a Scope.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "builtin" | "user" | "project" | "plugin:<plugin-id>"
}

// SkillEntry is one installed skill available within a Scope.
type SkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "user" | "project" | "plugin:<plugin-id>"
}

// builtinCommands are the Claude Code commands baked into the CLI binary. The
// CLI exposes no machine-readable listing, so they are curated here against
// CuratedBuiltinsVersion; the version probe surfaces drift (see version.go).
var builtinCommands = []SlashCommand{
	{Name: "/clear", Description: "Clear conversation history", Source: "builtin"},
	{Name: "/compact", Description: "Compact context window", Source: "builtin"},
	{Name: "/help", Description: "Show available commands", Source: "builtin"},
	{Name: "/memory", Description: "Show memory files", Source: "builtin"},
	{Name: "/model", Description: "Switch model", Source: "builtin"},
	{Name: "/status", Description: "Show session status", Source: "builtin"},
	{Name: "/vim", Description: "Toggle vim keybindings", Source: "builtin"},
}

// sourceRank orders sources for dedup precedence (lower wins) and for the final
// sort. builtin > project > user > plugin: a more specific layer shadows a less
// specific one of the same name, and builtins can never be overridden.
func sourceRank(source string) int {
	switch source {
	case "builtin":
		return 0
	case "project":
		return 1
	case "user":
		return 2
	default: // plugin:<id>
		return 3
	}
}

// Commands returns the slash commands visible in the scope: built-ins plus user,
// project, and plugin commands, deduped by name (builtin > project > user >
// plugin) and sorted builtins-first then by source then name. A non-Claude
// scope returns an empty slice.
func (s Scope) Commands() []SlashCommand {
	if !s.Supported {
		return []SlashCommand{}
	}

	out := append([]SlashCommand{}, builtinCommands...)

	// User commands: <ConfigDir>/commands/*.md
	for _, file := range listCommandFiles(filepath.Join(s.ConfigDir, "commands")) {
		if name, desc, ok := readCommandMeta(file); ok {
			out = append(out, SlashCommand{Name: name, Description: desc, Source: "user"})
		}
	}

	// Project commands: <ProjectCwd>/.claude/commands/*.md
	if s.ProjectCwd != "" {
		for _, file := range listCommandFiles(filepath.Join(s.ProjectCwd, ".claude", "commands")) {
			if name, desc, ok := readCommandMeta(file); ok {
				out = append(out, SlashCommand{Name: name, Description: desc, Source: "project"})
			}
		}
	}

	// Plugin commands: <ConfigDir>/plugins/cache/<plugin-id>/**/commands/*.md
	pluginsCache := filepath.Join(s.ConfigDir, "plugins", "cache")
	for _, pluginRoot := range listPluginRoots(pluginsCache) {
		source := "plugin:" + filepath.Base(pluginRoot)
		for _, file := range findCommandFiles(pluginRoot) {
			if name, desc, ok := readCommandMeta(file); ok {
				out = append(out, SlashCommand{Name: name, Description: desc, Source: source})
			}
		}
	}

	return dedupAndSortCommands(out)
}

// Skills returns the skills visible in the scope: user, project, and plugin
// skills, deduped by name (project > user > plugin). A non-Claude scope returns
// an empty slice.
func (s Scope) Skills() []SkillEntry {
	if !s.Supported {
		return []SkillEntry{}
	}

	out := []SkillEntry{}

	// User skills: <ConfigDir>/skills/<name>/SKILL.md
	out = append(out, skillsInDir(filepath.Join(s.ConfigDir, "skills"), "user")...)

	// Project skills: <ProjectCwd>/.claude/skills/<name>/SKILL.md
	if s.ProjectCwd != "" {
		out = append(out, skillsInDir(filepath.Join(s.ProjectCwd, ".claude", "skills"), "project")...)
	}

	// Plugin skills: <ConfigDir>/plugins/cache/<plugin-id>/**/SKILL.md
	pluginsCache := filepath.Join(s.ConfigDir, "plugins", "cache")
	for _, pluginRoot := range listPluginRoots(pluginsCache) {
		source := "plugin:" + filepath.Base(pluginRoot)
		for _, file := range findSkillFiles(pluginRoot) {
			name, desc := parseSkillFrontmatter(file)
			if name == "" {
				continue
			}
			out = append(out, SkillEntry{Name: name, Description: desc, Source: source})
		}
	}

	return dedupAndSortSkills(out)
}

// skillsInDir enumerates <dir>/<name>/SKILL.md immediate-child skills.
func skillsInDir(dir, source string) []SkillEntry {
	var out []SkillEntry
	for _, entry := range listSkillDirs(dir) {
		skillPath := filepath.Join(dir, entry, "SKILL.md")
		name, desc := parseSkillFrontmatter(skillPath)
		if name == "" {
			name = entry
		}
		out = append(out, SkillEntry{Name: name, Description: desc, Source: source})
	}
	return out
}

func dedupAndSortCommands(in []SlashCommand) []SlashCommand {
	sort.SliceStable(in, func(i, j int) bool {
		return sourceRank(in[i].Source) < sourceRank(in[j].Source)
	})
	seen := make(map[string]bool, len(in))
	deduped := make([]SlashCommand, 0, len(in))
	for _, c := range in {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		deduped = append(deduped, c)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		if ri, rj := sourceRank(deduped[i].Source), sourceRank(deduped[j].Source); ri != rj {
			return ri < rj
		}
		if deduped[i].Source != deduped[j].Source {
			return deduped[i].Source < deduped[j].Source
		}
		return deduped[i].Name < deduped[j].Name
	})
	return deduped
}

func dedupAndSortSkills(in []SkillEntry) []SkillEntry {
	sort.SliceStable(in, func(i, j int) bool {
		return sourceRank(in[i].Source) < sourceRank(in[j].Source)
	})
	seen := make(map[string]bool, len(in))
	deduped := make([]SkillEntry, 0, len(in))
	for _, s := range in {
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		deduped = append(deduped, s)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		if deduped[i].Source != deduped[j].Source {
			return deduped[i].Source < deduped[j].Source
		}
		return deduped[i].Name < deduped[j].Name
	})
	return deduped
}

// ---- filesystem helpers (single canonical copy) ----

// listCommandFiles returns absolute paths of *.md files directly inside dir.
// Symlinks and hidden entries are rejected so a malicious entry cannot redirect
// a read outside the configured root.
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
		if e.Type()&os.ModeSymlink != 0 {
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

// listSkillDirs returns immediate child directory names of dir that contain a
// regular SKILL.md. Symlinked and hidden entries are skipped.
func listSkillDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		skillFile := filepath.Join(dir, e.Name(), "SKILL.md")
		info, err := os.Lstat(skillFile)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// listPluginRoots returns the per-plugin top-level directories under cacheDir.
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

// findSkillFiles walks root for SKILL.md files, pruning node_modules + hidden dirs.
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

// findCommandFiles walks root for *.md files whose immediate parent dir is
// "commands", pruning node_modules + hidden dirs.
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
		if filepath.Base(filepath.Dir(path)) == "commands" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// readCommandMeta returns the slash-command name (filename without .md, prefixed
// "/"), its frontmatter description, and ok=false when the file is unreadable.
func readCommandMeta(path string) (name, desc string, ok bool) {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	if base == "" {
		return "", "", false
	}
	name = "/" + base

	f, err := os.Open(path)
	if err != nil {
		return name, "", true
	}
	defer f.Close()
	desc = parseFrontmatterDescription(f)
	return name, desc, true
}

// parseSkillFrontmatter reads the YAML frontmatter of a SKILL.md and returns
// (name, description). name is unquoted; no leading slash is added.
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
		if !inFrontmatter || line == "---" {
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

// parseFrontmatterDescription extracts the `description:` field from the leading
// YAML frontmatter of a command markdown file. Returns "" when none is present.
func parseFrontmatterDescription(r io.Reader) string {
	sc := bufio.NewScanner(r)
	lineNum := 0
	inFrontmatter := false
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
			return ""
		}
		if !inFrontmatter || line == "---" {
			break
		}
		if readingDesc && strings.HasPrefix(line, " ") {
			return strings.TrimSpace(line)
		}
		readingDesc = false
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			switch desc {
			case ">-", ">", "|", "|-":
				readingDesc = true
			default:
				return strings.Trim(desc, `"'`)
			}
		}
	}
	return ""
}
