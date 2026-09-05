// Package materializer produces, on this node, the files agent runtimes read
// from skill resources the database owns.
//
// It is the only component in this system that writes into the user's own
// directories, so almost all of it is about refusing to. A file it did not
// write is never touched; a file it wrote that a human has since edited stops
// the run for that resource; and without the node lease it writes nothing at
// all, whatever the caller asked for.
package materializer

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// Layers a target can sit in. They are cmdscope's editable sources
// (cmdscope/enumerate.go:96-98); builtin and plugin are never targeted.
const (
	LayerUser    = "user"
	LayerProject = "project"
)

// Format adapters. AdapterNone is a runtime with no skill concept: its target
// is a recorded no-op, never a silent gap, and never a fabricated format.
const (
	AdapterClaude = "claude"
	AdapterNone   = "none"
)

// Target is one place on this node a skill may be materialized into. The
// target set is node × config dir × provider, not "the filesystem": a
// materializer that writes to one config dir writes to the wrong one about
// half the time on a machine that runs ~/.claude-personal.
type Target struct {
	// NodeID is the node the target lives on, repo.DefaultNodeID until the
	// node registry lands.
	NodeID string
	// Provider is "claude" or a provider-registry id ("codex", "gemini", …).
	Provider string
	// Layer is LayerUser or LayerProject.
	Layer string
	// Root is what the path template is anchored at: a config dir for the user
	// layer, a project working directory for the project layer.
	Root string
	// Adapter is AdapterClaude or AdapterNone.
	Adapter string
}

// Key identifies the target in a materialization record. It must stay stable
// across runs: a changed key orphans the record, and the next run would then
// report the file this node wrote itself as foreign.
func (t Target) Key() string { return t.Provider + "|" + t.Layer + "|" + t.Root }

// Resolver turns this node's config directories into the target list.
//
// Both directory sources are injected rather than called directly. Production
// passes parser.AllClaudeConfigDirs and Registry.ConfigDirs; a test passes its
// own temp dirs, which is the only reason the suite of the one component that
// can overwrite a person's skill file never reads a real config directory.
type Resolver struct {
	NodeID string
	// ClaudeConfigDirs returns every Claude config root on this node. Claude is
	// the always-on built-in and deliberately not a provider descriptor
	// (provider/registry.go:131-133), so its dirs come from the parser's own
	// four-tier search set (parser/parser.go:133-162) — the tier that also
	// finds ~/.claude-personal.
	ClaudeConfigDirs func() []string
	// ProviderConfigDirs returns the enabled non-Claude providers' config dirs.
	// Registry.ConfigDirs already drops the ones that do not exist
	// (provider/registry.go:199).
	ProviderConfigDirs func() []parser.ProviderConfigDir
}

// Targets returns every target a resource in scope materializes into, sorted
// by key so a report and a golden test read the same both times.
func (r Resolver) Targets(scope repo.Scope) []Target {
	out := []Target{}

	switch scope.Normalize().Kind {
	case repo.ScopeGlobal:
		for _, dir := range r.ClaudeConfigDirs() {
			if isDir(dir) {
				out = append(out, Target{
					NodeID: r.NodeID, Provider: string(sdk.ProviderClaude),
					Layer: LayerUser, Root: filepath.Clean(dir), Adapter: AdapterClaude,
				})
			}
		}
	case repo.ScopeProject:
		if isDir(scope.Ref) {
			out = append(out, Target{
				NodeID: r.NodeID, Provider: string(sdk.ProviderClaude),
				Layer: LayerProject, Root: filepath.Clean(scope.Ref), Adapter: AdapterClaude,
			})
		}
	}
	// repo.ScopeApplication falls through with no Claude target on purpose:
	// spec §3 lists path templates for the user and project layers only, and
	// guessing a third is how a file lands somewhere nothing reads it.

	// Every enabled non-Claude provider gets one recorded no-op, whatever the
	// scope. None of the four ships a SKILL.md equivalent, and a user who
	// authored a skill and sees nothing at all for Codex has been misled.
	for _, d := range r.ProviderConfigDirs() {
		out = append(out, Target{
			NodeID: r.NodeID, Provider: string(d.Provider),
			Layer: LayerUser, Root: filepath.Clean(d.Path), Adapter: AdapterNone,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// isDir reports whether path exists and is a directory. A config dir that does
// not exist is skipped, never created.
func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
