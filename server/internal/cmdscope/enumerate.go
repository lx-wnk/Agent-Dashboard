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

// CommandDetail is a slash command with its on-disk body, for the Config
// explorer. Built-in commands carry an empty Body.
type CommandDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Body        string `json:"body"`
}

// SkillEntry is one installed skill available within a Scope.
type SkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "user" | "project" | "plugin:<plugin-id>"
}

// maxCommandBodyBytes caps the on-disk command file body read into CommandDetail.
const maxCommandBodyBytes = 1 * 1024 * 1024 // 1 MB

// builtinCommands are the Claude Code commands baked into the CLI binary. The
// CLI exposes no machine-readable listing, so they are curated here against
// CuratedBuiltinsVersion; the version probe surfaces drift (see version.go).
var builtinCommands = []SlashCommand{
	{Name: "/add-dir", Description: "Add a working directory", Source: "builtin"},
	{Name: "/agents", Description: "Manage agents", Source: "builtin"},
	{Name: "/bug", Description: "Report a bug", Source: "builtin"},
	{Name: "/clear", Description: "Clear conversation history", Source: "builtin"},
	{Name: "/compact", Description: "Compact context window", Source: "builtin"},
	{Name: "/config", Description: "Open settings", Source: "builtin"},
	{Name: "/cost", Description: "Show token cost of the session", Source: "builtin"},
	{Name: "/doctor", Description: "Diagnose Claude Code health", Source: "builtin"},
	{Name: "/export", Description: "Export the conversation", Source: "builtin"},
	{Name: "/help", Description: "Show available commands", Source: "builtin"},
	{Name: "/init", Description: "Initialize a CLAUDE.md", Source: "builtin"},
	{Name: "/login", Description: "Log in to an account", Source: "builtin"},
	{Name: "/logout", Description: "Log out", Source: "builtin"},
	{Name: "/mcp", Description: "Manage MCP servers", Source: "builtin"},
	{Name: "/memory", Description: "Edit memory files", Source: "builtin"},
	{Name: "/model", Description: "Switch model", Source: "builtin"},
	{Name: "/pr-comments", Description: "Show pull request comments", Source: "builtin"},
	{Name: "/release-notes", Description: "Show release notes", Source: "builtin"},
	{Name: "/resume", Description: "Resume a previous conversation", Source: "builtin"},
	{Name: "/review", Description: "Review a pull request", Source: "builtin"},
	{Name: "/status", Description: "Show session status", Source: "builtin"},
	{Name: "/terminal-setup", Description: "Configure terminal key bindings", Source: "builtin"},
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

// Commands returns the slash commands visible in the scope (without bodies):
// built-ins plus user, project, and plugin commands, deduped by name
// (builtin > project > user > plugin) and sorted builtins-first then by source
// then name. A non-Claude scope returns an empty slice.
func (s Scope) Commands() []SlashCommand {
	details := s.commandDetails(false)
	out := make([]SlashCommand, len(details))
	for i, d := range details {
		out[i] = SlashCommand{Name: d.Name, Description: d.Description, Source: d.Source}
	}
	return out
}

// CommandDetails returns the same commands as Commands, each with its on-disk
// body (capped at maxCommandBodyBytes). Built-in commands carry an empty Body.
func (s Scope) CommandDetails() []CommandDetail {
	return s.commandDetails(true)
}

func (s Scope) commandDetails(withBody bool) []CommandDetail {
	if !s.Supported {
		return []CommandDetail{}
	}

	out := make([]CommandDetail, 0, len(builtinCommands))
	for _, b := range builtinCommands {
		out = append(out, CommandDetail{Name: b.Name, Description: b.Description, Source: b.Source})
	}

	collect := func(dir, source string) {
		for _, file := range listCommandFiles(dir) {
			if d, ok := readCommand(file, source, "", withBody); ok {
				out = append(out, d)
			}
		}
	}

	// User commands: <ConfigDir>/commands/*.md
	collect(filepath.Join(s.ConfigDir, "commands"), "user")

	// Project commands: <ProjectCwd>/.claude/commands/*.md
	if s.ProjectCwd != "" {
		collect(filepath.Join(s.ProjectCwd, ".claude", "commands"), "project")
	}

	// Plugin commands: <ConfigDir>/plugins/cache/<marketplace>/<plugin>/**/commands/*.md
	// Namespaced as /<plugin>:<name>, matching Claude's invocation syntax.
	for _, pluginDir := range pluginDirs(filepath.Join(s.ConfigDir, "plugins", "cache")) {
		plugin := filepath.Base(pluginDir)
		source := "plugin:" + plugin
		for _, file := range findCommandFiles(pluginDir) {
			if d, ok := readCommand(file, source, plugin, withBody); ok {
				out = append(out, d)
			}
		}
	}

	return dedupAndSortCommands(out)
}

// SlashCommands returns everything typeable as a leading-slash command in the
// scope: built-in + file commands AND skills (Claude exposes every skill as a
// /<name> — or /<plugin>:<name> — command). Deduped by name; a real command
// shadows a skill of the same name. A non-Claude scope returns an empty slice.
func (s Scope) SlashCommands() []SlashCommand {
	if !s.Supported {
		return []SlashCommand{}
	}
	details := s.commandDetails(false)
	for _, sk := range s.Skills() {
		details = append(details, CommandDetail{Name: "/" + sk.Name, Description: sk.Description, Source: sk.Source})
	}
	details = dedupAndSortCommands(details)
	out := make([]SlashCommand, len(details))
	for i, d := range details {
		out[i] = SlashCommand{Name: d.Name, Description: d.Description, Source: d.Source}
	}
	return out
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

	// Plugin skills: <ConfigDir>/plugins/cache/<marketplace>/<plugin>/**/SKILL.md
	// Namespaced as <plugin>:<skill>, matching Claude's skill identifiers.
	for _, pluginDir := range pluginDirs(filepath.Join(s.ConfigDir, "plugins", "cache")) {
		plugin := filepath.Base(pluginDir)
		source := "plugin:" + plugin
		for _, file := range findSkillFiles(pluginDir) {
			name, desc := parseSkillFrontmatter(file)
			if name == "" {
				continue
			}
			out = append(out, SkillEntry{Name: plugin + ":" + name, Description: desc, Source: source})
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

func dedupAndSortCommands(in []CommandDetail) []CommandDetail {
	sort.SliceStable(in, func(i, j int) bool {
		return sourceRank(in[i].Source) < sourceRank(in[j].Source)
	})
	seen := make(map[string]bool, len(in))
	deduped := make([]CommandDetail, 0, len(in))
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

// immediateDirs returns the non-hidden immediate child directories of dir.
func immediateDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out
}

// pluginDirs returns the per-plugin directories under the plugin cache. The
// cache is laid out as <cache>/<marketplace>/<plugin>/<version>/..., so the
// plugin level is two deep; filepath.Base of each result is the plugin name
// used as the /<plugin>:<name> command/skill namespace.
func pluginDirs(cacheDir string) []string {
	var out []string
	for _, marketplace := range immediateDirs(cacheDir) {
		out = append(out, immediateDirs(marketplace)...)
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

// readCommand builds a CommandDetail for a slash-command markdown file. Name is
// the filename without .md, prefixed "/" and — for plugin commands — namespaced
// as /<namespace>:<base> to match Claude's invocation syntax (namespace is "" for
// user/project commands). Description comes from optional YAML frontmatter, and
// (when withBody) body is the file content capped at maxCommandBodyBytes. ok is
// false only when the path has no usable name.
func readCommand(path, source, namespace string, withBody bool) (CommandDetail, bool) {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	if base == "" {
		return CommandDetail{}, false
	}
	name := "/" + base
	if namespace != "" {
		name = "/" + namespace + ":" + base
	}
	d := CommandDetail{Name: name, Source: source}

	if withBody {
		body, desc := readCommandBody(path)
		d.Body = body
		d.Description = desc
		return d, true
	}

	f, err := os.Open(path)
	if err != nil {
		return d, true
	}
	defer f.Close()
	d.Description = parseFrontmatterDescription(f)
	return d, true
}

// readCommandBody reads the (capped) file content and its frontmatter
// description in one pass.
func readCommandBody(path string) (body, desc string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", ""
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	limit := int64(maxCommandBodyBytes)
	truncated := info.Size() > limit
	readSize := info.Size()
	if truncated {
		readSize = limit
	}
	buf := make([]byte, readSize)
	n, _ := io.ReadFull(f, buf)
	body = string(buf[:n])
	if truncated {
		body += "\n\n<!-- truncated: file exceeds 1 MB limit -->\n"
	}
	desc = parseFrontmatterDescription(strings.NewReader(body))
	return body, desc
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
