package mcp

import (
	"context"
	"strings"
)

// NamespaceSeparator keeps a module's tool from colliding with a core tool or
// with another module's: an agent sees `obsidian__search`, never a bare
// `search` whose owner it cannot tell.
const NamespaceSeparator = "__"

// ModuleTool is one tool a module contributes.
type ModuleTool struct {
	ModuleID    string
	Name        string
	Description string
	InputSchema map[string]any
}

// QualifiedName is what the agent sees.
func (t ModuleTool) QualifiedName() string {
	return t.ModuleID + NamespaceSeparator + t.Name
}

// SplitQualifiedName reports the module and tool a qualified name refers to.
func SplitQualifiedName(qualified string) (moduleID, tool string, ok bool) {
	moduleID, tool, ok = strings.Cut(qualified, NamespaceSeparator)
	if !ok || moduleID == "" || tool == "" {
		return "", "", false
	}
	return moduleID, tool, true
}

// ModuleToolScope is the scope a caller needs for one module tool. Naming each
// tool separately is what makes "may search the vault" and "may write to it"
// different permissions, and no key carries such a scope until someone grants
// it — so a module tool is denied until it is allowed.
func ModuleToolScope(moduleID, tool string) string {
	return "module:" + moduleID + ":" + tool
}

// ModuleTools supplies the tools modules contribute. It is consulted per
// request rather than built once, because a module can start, stop or become
// unhealthy while the server runs: a tool list that outlives the module behind
// it turns a missing module into a failed call instead of an absent tool.
type ModuleTools interface {
	List(ctx context.Context) []ModuleTool
	Call(ctx context.Context, moduleID, tool string, args map[string]any) (string, error)
}
