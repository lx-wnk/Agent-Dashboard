package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SlashCommand is a slash command entry returned by GET /api/slash-commands.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "builtin" | "skill"
}

var builtinCommands = []SlashCommand{
	{Name: "/clear", Description: "Clear conversation history", Source: "builtin"},
	{Name: "/compact", Description: "Compact context window", Source: "builtin"},
	{Name: "/help", Description: "Show available commands", Source: "builtin"},
	{Name: "/memory", Description: "Show memory files", Source: "builtin"},
	{Name: "/model", Description: "Switch model", Source: "builtin"},
	{Name: "/status", Description: "Show session status", Source: "builtin"},
	{Name: "/vim", Description: "Toggle vim keybindings", Source: "builtin"},
}

// GetSlashCommands returns all available slash commands: built-ins + installed skills.
func GetSlashCommands() []SlashCommand {
	seen := make(map[string]bool)
	cmds := []SlashCommand{}

	for _, c := range builtinCommands {
		seen[c.Name] = true
		cmds = append(cmds, c)
	}

	for _, configDir := range allClaudeConfigDirs() {
		cacheDir := filepath.Join(configDir, "plugins", "cache")
		for _, path := range findSkillFiles(cacheDir) {
			cmd := parseSkillFile(path)
			if cmd == nil || seen[cmd.Name] {
				continue
			}
			seen[cmd.Name] = true
			cmds = append(cmds, *cmd)
		}
	}

	sort.Slice(cmds, func(i, j int) bool {
		if cmds[i].Source != cmds[j].Source {
			return cmds[i].Source == "builtin"
		}
		return cmds[i].Name < cmds[j].Name
	})
	return cmds
}

func findSkillFiles(cacheDir string) []string {
	var files []string
	_ = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// parseSkillFile reads the YAML frontmatter of a SKILL.md and extracts name + description.
// Expected format (first line must be "---"):
//
//	---
//	name: my-skill
//	description: Short description
//	---
//
// For multi-line "description: >-" blocks, the first indented continuation line is used.
func parseSkillFile(path string) *SlashCommand {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNum := 0
	inFrontmatter := false
	var name, description string
	readingDesc := false

	for sc.Scan() {
		lineNum++
		if lineNum > 60 {
			break
		}
		line := sc.Text()

		if lineNum == 1 {
			if line == "---" {
				inFrontmatter = true
				continue
			}
			return nil
		}
		if !inFrontmatter {
			break
		}
		if line == "---" {
			break
		}

		// Continuation line for multi-line description
		if readingDesc && description == "" && strings.HasPrefix(line, " ") {
			description = strings.TrimSpace(line)
			readingDesc = false
			continue
		}
		readingDesc = false

		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			switch desc {
			case ">-", ">", "|", "|-":
				readingDesc = true
			default:
				description = desc
			}
		}
	}

	if name == "" {
		return nil
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return &SlashCommand{Name: name, Description: description, Source: "skill"}
}
