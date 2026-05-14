package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFile_ValidFrontmatter(t *testing.T) {
	content := "---\nname: my-skill\ndescription: Does something useful\n---\n\n# Body\n"
	path := writeTempSkill(t, content)

	cmd := parseSkillFile(path)
	if cmd == nil {
		t.Fatal("expected non-nil SlashCommand")
	}
	if cmd.Name != "/my-skill" {
		t.Errorf("name = %q, want %q", cmd.Name, "/my-skill")
	}
	if cmd.Description != "Does something useful" {
		t.Errorf("description = %q, want %q", cmd.Description, "Does something useful")
	}
}

func TestParseSkillFile_MultiLineDescription(t *testing.T) {
	content := "---\nname: multi\ndescription: >-\n  First continuation line\n---\n"
	path := writeTempSkill(t, content)

	cmd := parseSkillFile(path)
	if cmd == nil {
		t.Fatal("expected non-nil SlashCommand")
	}
	if cmd.Description != "First continuation line" {
		t.Errorf("description = %q, want %q", cmd.Description, "First continuation line")
	}
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	content := "# Just a markdown file\nNo YAML here.\n"
	path := writeTempSkill(t, content)

	cmd := parseSkillFile(path)
	if cmd != nil {
		t.Errorf("expected nil for file without frontmatter, got %+v", cmd)
	}
}

func TestParseSkillFile_EmptyFile(t *testing.T) {
	path := writeTempSkill(t, "")
	if cmd := parseSkillFile(path); cmd != nil {
		t.Errorf("expected nil for empty file, got %+v", cmd)
	}
}

func TestParseSkillFile_MissingName(t *testing.T) {
	content := "---\ndescription: no name here\n---\n"
	path := writeTempSkill(t, content)

	cmd := parseSkillFile(path)
	// A skill with no name is unusable — expect nil or empty name.
	// parseSkillFile returns nil when name is empty
	if cmd != nil {
		t.Errorf("expected nil for skill without name field, got %+v", cmd)
	}
}

func TestParseSkillFile_NotExist(t *testing.T) {
	cmd := parseSkillFile("/does/not/exist/skill.md")
	if cmd != nil {
		t.Errorf("expected nil for missing file, got %+v", cmd)
	}
}

func writeTempSkill(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp skill: %v", err)
	}
	return path
}
