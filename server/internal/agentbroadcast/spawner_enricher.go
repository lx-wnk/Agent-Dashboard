// server/internal/agentbroadcast/spawner_enricher.go
package agentbroadcast

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/pathutil"
	"github.com/lx-wnk/agent-dashboard/server/internal/procenv"
)

// ClaudeConfigDirEnv is the variable a Claude profile wrapper sets to point a
// session at its own config directory; it is what separates one configured
// spawner from another when both run the same command.
const ClaudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// envLookupFn is the seam procenv.Lookup fills; tests substitute a map.
type envLookupFn func(pids []int, key string) map[int]string

// Only the two reads this needs, so the repos can be stubbed without standing
// up their full interfaces.
type spawnerLister interface {
	List(ctx context.Context) ([]*ent.Spawner, error)
}

type taskLister interface {
	ListByIDs(ctx context.Context, ids []string) ([]*ent.Task, error)
}

// NewSpawnerEnricher annotates each agent with the spawner it belongs to.
//
// Two sources, most authoritative first:
//
//  1. task — the agent runs a pipeline stage whose task names a spawner.
//  2. env — the live process carries CLAUDE_CONFIG_DIR, which identifies the
//     profile it was started with. This is read from the running process, not
//     guessed from session files: two config dirs that symlink to the same
//     store are indistinguishable on disk but not here.
//
// A dashboard-started session needs no third source: spawning applies the
// spawner's own env to the child, so the env source recognises it exactly. Two
// spawners that differ in nothing but command or args are indistinguishable
// this way — attribution then falls to the default one.
//
// Agents that match none keep an empty SpawnerID. The source travels with the
// annotation so the UI can mark a derived attribution as derived.
//
// Best-effort throughout: a missing repo, an unreadable process, or a query
// error leaves the annotation empty instead of failing the scan.
// A nil spawner repo returns a nil enricher, which merger.ChainEnrichers skips
// — matching NewHookEventEnricher, so the no-database path keeps composing away
// without the composition root carrying a per-enricher guard.
func NewSpawnerEnricher(spawners spawnerLister, tasks taskLister) merger.Enricher {
	return newSpawnerEnricher(spawners, tasks, procenv.Lookup)
}

func newSpawnerEnricher(spawners spawnerLister, tasks taskLister, lookupEnv envLookupFn) merger.Enricher {
	if spawners == nil {
		return nil
	}
	return func(ctx context.Context, agents []sdk.Agent) {
		if len(agents) == 0 {
			return
		}
		rows, err := spawners.List(ctx)
		if err != nil {
			slog.Debug("spawner enricher: spawner list failed", "err", err)
			return
		}
		if len(rows) == 0 {
			return
		}

		byID := make(map[string]*ent.Spawner, len(rows))
		for _, s := range rows {
			byID[s.ID] = s
		}
		byConfigDir, defaultSpawner := indexByConfigDir(rows)

		taskSpawner := taskSpawnerIDs(ctx, tasks, agents)

		pids := make([]int, 0, len(agents))
		for i := range agents {
			if agents[i].PID > 0 {
				pids = append(pids, agents[i].PID)
			}
		}
		configDirs := lookupEnv(pids, ClaudeConfigDirEnv)

		for i := range agents {
			id, source := attribute(&agents[i], taskSpawner, configDirs, byConfigDir, defaultSpawner)
			if id == "" {
				continue
			}
			row, ok := byID[id]
			if !ok {
				continue
			}
			agents[i].SpawnerID = id
			agents[i].SpawnerName = row.Name
			agents[i].SpawnerSource = source
		}
	}
}

func attribute(
	agent *sdk.Agent,
	taskSpawner map[string]string,
	configDirs map[int]string,
	byConfigDir map[string]*ent.Spawner,
	defaultSpawner *ent.Spawner,
) (id, source string) {
	if agent.PipelineTaskID != "" {
		if fromTask := taskSpawner[agent.PipelineTaskID]; fromTask != "" {
			return fromTask, sdk.SpawnerSourceTask
		}
	}
	dir, ok := configDirs[agent.PID]
	if !ok {
		// No variable set means the session runs on the default config dir,
		// which is exactly what a spawner without one targets.
		if defaultSpawner != nil {
			return defaultSpawner.ID, sdk.SpawnerSourceEnv
		}
		return "", ""
	}
	if match, ok := byConfigDir[canonicalDir(dir)]; ok {
		return match.ID, sdk.SpawnerSourceEnv
	}
	return "", ""
}

// indexByConfigDir maps each spawner's config dir to it, and returns the
// spawner that owns the default dir. Two spawners can name the same directory
// (a symlink to the same store); the default one wins so the attribution is
// stable rather than map-order dependent.
func indexByConfigDir(rows []*ent.Spawner) (map[string]*ent.Spawner, *ent.Spawner) {
	byDir := make(map[string]*ent.Spawner, len(rows))
	var fallback *ent.Spawner
	for _, s := range rows {
		dir := strings.TrimSpace(s.Env[ClaudeConfigDirEnv])
		if dir == "" {
			if fallback == nil || s.IsDefault {
				fallback = s
			}
			continue
		}
		key := canonicalDir(dir)
		if prev, ok := byDir[key]; ok && prev.IsDefault {
			continue
		}
		byDir[key] = s
	}
	if fallback != nil {
		if key := canonicalDir(defaultConfigDir()); key != "" {
			if prev, ok := byDir[key]; !ok || !prev.IsDefault {
				byDir[key] = fallback
			}
		}
	}
	return byDir, fallback
}

func defaultConfigDir() string {
	if v := strings.TrimSpace(os.Getenv(ClaudeConfigDirEnv)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// canonicalDir resolves ~, relative segments, and symlinks so that a spawner
// pointing at ~/.claude and a process reporting the store it links to compare
// equal. Tilde handling goes through pathutil so attribution expands exactly
// what BuildSpawnEnv exported to the process in the first place.
func canonicalDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	dir = pathutil.ExpandLeadingTilde(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(dir)
}

func taskSpawnerIDs(ctx context.Context, tasks taskLister, agents []sdk.Agent) map[string]string {
	if tasks == nil {
		return nil
	}
	ids := make([]string, 0, len(agents))
	seen := make(map[string]struct{}, len(agents))
	for i := range agents {
		id := agents[i].PipelineTaskID
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tasks.ListByIDs(ctx, ids)
	if err != nil {
		slog.Debug("spawner enricher: task batch lookup failed", "err", err)
		return nil
	}
	out := make(map[string]string, len(rows))
	for _, t := range rows {
		if t.SpawnerID != nil && *t.SpawnerID != "" {
			out[t.ID] = *t.SpawnerID
		}
	}
	return out
}
