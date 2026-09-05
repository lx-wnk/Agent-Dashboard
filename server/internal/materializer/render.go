package materializer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MarkerKey is the frontmatter key every materialized skill file carries.
//
// It is not by itself proof of ownership — a file copied out of a config dir
// carries it too, and cmd/serve/hooks.go:252-263 learned that the hard way for
// settings.json: treating a marker match as ownership made uninstall delete a
// script it never wrote. The materialization record's path is what decides;
// this key only says which resource produced the bytes.
const MarkerKey = "x-dashboard-resource"

// Skill is what one skill resource contributes to a rendered file: the
// identity half from the resource row, the content half from the skill row.
type Skill struct {
	ResourceID  string
	Slug        string
	Description string
	Body        string
}

// RenderClaudeSkill produces the SKILL.md a Claude runtime reads.
//
// The frontmatter name is always the slug, which is also the directory name
// SkillPath builds — written consistently so cmdscope's directory-name
// fallback (enumerate.go:286-289) never has to fire for a file this component
// produced. The output is byte-stable for a given Skill: an unstable render
// would classify as "repaired" on every run and rewrite the file forever.
func RenderClaudeSkill(s Skill) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlString(s.Slug))
	fmt.Fprintf(&b, "description: %s\n", yamlString(s.Description))
	fmt.Fprintf(&b, "%s: %s\n", MarkerKey, yamlString(s.ResourceID))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(s.Body, "\n"))
	b.WriteString("\n")
	return b.Bytes()
}

// yamlString renders v as a YAML double-quoted scalar. A JSON string literal
// is a valid YAML 1.2 double-quoted scalar, so the stdlib does the escaping —
// which is what stops a description containing a newline and "---" from
// closing the frontmatter block and rewriting the keys above it.
func yamlString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

// HashBytes is the content hash recorded against a materialized file. It is
// what decides whether a file on disk is still the one this node wrote.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
