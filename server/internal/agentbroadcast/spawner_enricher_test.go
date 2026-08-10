package agentbroadcast

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSpawnerRepo struct {
	rows []*ent.Spawner
	err  error
}

func (s stubSpawnerRepo) List(context.Context) ([]*ent.Spawner, error) { return s.rows, s.err }

type stubTaskRepo struct {
	rows []*ent.Task
}

func (s stubTaskRepo) ListByIDs(context.Context, []string) ([]*ent.Task, error) {
	return s.rows, nil
}

func spawner(id, name, configDir string, isDefault bool) *ent.Spawner {
	env := map[string]string{}
	if configDir != "" {
		env[ClaudeConfigDirEnv] = configDir
	}
	return &ent.Spawner{ID: id, Name: name, Env: env, IsDefault: isDefault}
}

func enrich(t *testing.T, rows []*ent.Spawner, tasks taskLister, agents []sdk.Agent) []sdk.Agent {
	t.Helper()
	NewSpawnerEnricher(stubSpawnerRepo{rows: rows}, tasks)(context.Background(), agents)
	return agents
}

func TestPlacesASessionByTheConfigDirItsProcessCarries(t *testing.T) {
	work := t.TempDir()
	rows := []*ent.Spawner{
		spawner("default-id", "Claude (default)", "", true),
		spawner("work-id", "Claude Work", work, false),
	}
	agents := []sdk.Agent{{PID: 4597, ClaudeConfigDir: work}, {PID: 9920}}

	got := enrich(t, rows, nil, agents)

	assert.Equal(t, "work-id", got[0].SpawnerID)
	assert.Equal(t, "Claude Work", got[0].SpawnerName)
	assert.Equal(t, sdk.SpawnerSourceEnv, got[0].SpawnerSource)
	// No variable set is not "unknown" — it means the default config dir.
	assert.Equal(t, "default-id", got[1].SpawnerID)
}

func TestResolvesSymlinkedConfigDirsToTheSameSpawner(t *testing.T) {
	store := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(store, link))

	rows := []*ent.Spawner{spawner("work-id", "Claude Work", link, false)}

	// The spawner names the symlink, the process reports the store it points at.
	got := enrich(t, rows, nil, []sdk.Agent{{PID: 1, ClaudeConfigDir: store}})

	assert.Equal(t, "work-id", got[0].SpawnerID)
}

func TestPipelineTaskWinsOverTheProcessEnvironment(t *testing.T) {
	work := t.TempDir()
	rows := []*ent.Spawner{
		spawner("default-id", "Claude (default)", "", true),
		spawner("work-id", "Claude Work", work, false),
	}
	taskSpawner := "work-id"
	tasks := stubTaskRepo{rows: []*ent.Task{{ID: "task-1", SpawnerID: &taskSpawner}}}

	// The process reports no config dir, which would place it on the default.
	got := enrich(t, rows, tasks, []sdk.Agent{{PID: 1, PipelineTaskID: "task-1"}})

	assert.Equal(t, "work-id", got[0].SpawnerID)
	assert.Equal(t, sdk.SpawnerSourceTask, got[0].SpawnerSource)
}

func TestLeavesTheAgentUnattributedWhenNoSpawnerOwnsItsConfigDir(t *testing.T) {
	rows := []*ent.Spawner{spawner("work-id", "Claude Work", t.TempDir(), false)}

	// Not the work dir, and no spawner claims the default one.
	got := enrich(t, rows, nil, []sdk.Agent{{PID: 1, ClaudeConfigDir: t.TempDir()}})

	assert.Empty(t, got[0].SpawnerID)
	assert.Empty(t, got[0].SpawnerSource)
}

// Two spawners can point at one store (a symlink), which no signal can tell
// apart — the default one has to win, or attribution flips between scans.
func TestPrefersTheDefaultSpawnerWhenTwoClaimTheSameConfigDir(t *testing.T) {
	shared := t.TempDir()
	rows := []*ent.Spawner{
		spawner("custom-id", "Custom (imported)", shared, false),
		spawner("default-id", "Claude (default)", shared, true),
	}

	for range 5 {
		got := enrich(t, rows, nil, []sdk.Agent{{PID: 1, ClaudeConfigDir: shared}})
		assert.Equal(t, "default-id", got[0].SpawnerID)
	}
}

// merger.ChainEnrichers skips nil elements, so the no-database path composes
// away here rather than in the composition root.
func TestReturnsANilEnricherWithoutASpawnerRepo(t *testing.T) {
	assert.Nil(t, NewSpawnerEnricher(nil, nil))
}

func TestAnnotatesNothingWithoutSpawners(t *testing.T) {
	got := enrich(t, nil, nil, []sdk.Agent{{PID: 1, ClaudeConfigDir: "/tmp"}})

	assert.Empty(t, got[0].SpawnerID)
}

// A finished agent's process is gone, so nothing can be read back out of it —
// but the config dir it ran under was recorded on the agent while it was live.
// Attribution has to use that, or every finished card lands on the default
// spawner regardless of the profile it actually ran under.
func TestAttributesAFinishedAgentFromTheConfigDirRecordedWhileItWasLive(t *testing.T) {
	work := t.TempDir()
	rows := []*ent.Spawner{
		spawner("default-id", "Claude (default)", "", true),
		spawner("work-id", "Claude Work", work, false),
	}
	agents := []sdk.Agent{{PID: 4597, ClaudeConfigDir: work, Status: sdk.AgentStatusFinished}}

	got := enrich(t, rows, nil, agents)

	assert.Equal(t, "work-id", got[0].SpawnerID)
	assert.Equal(t, sdk.SpawnerSourceEnv, got[0].SpawnerSource)
}
