package sdk_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// src/sdk.generated.ts is marked `-diff` in .gitattributes, so a review of a
// change to sdk/types.go never sees the TS side at all. It is also not
// reproducible from `tygo generate` -- the committed file has been hand-kept in
// tygo's order with the generator's `/* int */` comments removed -- so a
// regenerate-and-compare gate would be red on an unchanged tree.
//
// What actually breaks is narrower than formatting: a Go field with no TS
// counterpart is silently `undefined` at every read site. That is how
// PatternDisplay would have gone missing while toolUseLabel() quietly fell back
// to the bare tool name, with no test failing, because every TS fixture in the
// suite is hand-built.
func TestEveryWireFieldHasATypeScriptCounterpart(t *testing.T) {
	goTypes := parseGoStructs(t, "types.go")
	tsTypes := parseTSInterfaces(t, "../src/sdk.generated.ts")

	checked := 0
	for name, fields := range goTypes {
		props, ok := tsTypes[name]
		if !ok {
			// Not every type crosses the wire; tygo.yaml decides. Absence is a
			// configuration choice, not a defect.
			continue
		}
		checked++
		for _, f := range fields {
			if !props[f] {
				t.Errorf("%s.%s has no counterpart in src/sdk.generated.ts — every read of it in the client is undefined", name, f)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no type was compared; the parser stopped matching one of the two files")
	}
}

// parseGoStructs returns JSON field names per struct type, skipping fields tagged "-".
func parseGoStructs(t *testing.T, path string) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		var names []string
		for _, f := range st.Fields.List {
			if f.Tag == nil || len(f.Names) == 0 || !f.Names[0].IsExported() {
				continue
			}
			tag, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				continue
			}
			name, _, _ := strings.Cut(reflectTag(tag, "json"), ",")
			if name != "" && name != "-" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			out[spec.Name.Name] = names
		}
		return true
	})
	return out
}

func reflectTag(tag, key string) string {
	for _, part := range strings.Fields(tag) {
		k, rest, ok := strings.Cut(part, ":")
		if !ok || k != key {
			continue
		}
		v, err := strconv.Unquote(rest)
		if err != nil {
			return ""
		}
		return v
	}
	return ""
}

var (
	tsInterfaceRE = regexp.MustCompile(`^export interface (\w+)`)
	tsPropRE      = regexp.MustCompile(`^\s{2}(\w+)\??:`)
)

// parseTSInterfaces returns the property names of each exported interface.
func parseTSInterfaces(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	out := map[string]map[string]bool{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := tsInterfaceRE.FindStringSubmatch(line); m != nil {
			current = m[1]
			out[current] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, "}") {
			current = ""
			continue
		}
		if m := tsPropRE.FindStringSubmatch(line); current != "" && m != nil {
			out[current][m[1]] = true
		}
	}
	return out
}
