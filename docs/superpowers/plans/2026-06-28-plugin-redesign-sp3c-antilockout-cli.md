# Plugin Redesign SP3c — Anti-Lockout Plugin CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `dashboard plugins` direct-DB CLI (`list`/`disable <id>`/`enable <id>`) so a bad `auth_provider` plugin that bricks boot can be disabled offline, bypassing the HTTP auth gate.

**Architecture:** A new cobra command group in `cmd/cli`, mirroring the existing `dashboard settings` lockout hatch (`cmd_settings.go` + `dbstore.go`): open the ent client against the resolved DB path, mutate `plugin.active` via `repo.PluginRepo`, close. No HTTP, no server.

**Tech Stack:** Go, cobra, ent, testify. Reuses `openDBStore`/`resolveDBPath`/`withStore` (cmd/cli package `main`).

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `server/cmd/cli/cmd_plugins.go` | `plugins` cobra group: list/disable/enable over `repo.PluginRepo` | Create |
| `server/cmd/cli/cmd_plugins_test.go` | CLI tests against a temp ent DB | Create |
| `server/cmd/cli/main.go` | register `newPluginsCmd()` on the root | Modify |

**Commands:** Test `cd server && go test ./cmd/cli/ -v`. Build `cd server && go build ./...`. Lint `golangci-lint run ./cmd/...`. Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```
Do NOT run `go test ./...` (regenerates ent). Scope to `./cmd/cli/`.

---

### Task 1: `plugins list` + `disable` + `enable`

**Files:**
- Create: `server/cmd/cli/cmd_plugins.go`
- Create: `server/cmd/cli/cmd_plugins_test.go`
- Modify: `server/cmd/cli/main.go` (register command)

- [ ] **Step 1: Verify the PluginRepo surface**

Run: `cd server && grep -nE "func NewPluginRepo|func .*PluginRepo.* (List|Get|SetActive)\(|IsNotFound" internal/db/repo/plugin*.go internal/db/repo/*.go | head`
Read the exact signatures of `repo.NewPluginRepo`, `PluginRepo.List`, `PluginRepo.Get`, `PluginRepo.SetActive`, and `repo.IsNotFound`. The repo returns plugin rows with at least `ID string`, `Active bool`, `InstalledAt *time.Time`. Adjust the field accessors in the code below to match the actual row type names.

- [ ] **Step 2: Write the failing test**

Create `server/cmd/cli/cmd_plugins_test.go` (model DB setup on `cmd_settings_test.go` — it uses `openDBStore(tmp)` to create+migrate a DB):

```go
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func seedPlugin(t *testing.T, dbPath, id string, active bool) {
	t.Helper()
	store, err := openDBStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	pr := repo.NewPluginRepo(store.client)
	_, err = pr.Upsert(context.Background(), repo.UpsertPluginInput{ID: id})
	require.NoError(t, err)
	require.NoError(t, pr.SetActive(context.Background(), id, active))
}

func pluginActive(t *testing.T, dbPath, id string) bool {
	t.Helper()
	store, err := openDBStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	row, err := repo.NewPluginRepo(store.client).Get(context.Background(), id)
	require.NoError(t, err)
	return row.Active
}

func TestPluginsDisableSetsInactive(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	seedPlugin(t, dbPath, "github-oauth", true)

	cmd := newPluginsCmd()
	cmd.SetArgs([]string{"disable", "github-oauth", "--db", dbPath})
	require.NoError(t, cmd.Execute())

	require.False(t, pluginActive(t, dbPath, "github-oauth"))
}

func TestPluginsEnableSetsActive(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	seedPlugin(t, dbPath, "github-oauth", false)

	cmd := newPluginsCmd()
	cmd.SetArgs([]string{"enable", "github-oauth", "--db", dbPath})
	require.NoError(t, cmd.Execute())

	require.True(t, pluginActive(t, dbPath, "github-oauth"))
}

func TestPluginsDisableUnknownErrors(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	seedPlugin(t, dbPath, "other", true) // ensures DB exists

	cmd := newPluginsCmd()
	cmd.SetArgs([]string{"disable", "nope", "--db", dbPath})
	require.Error(t, cmd.Execute())
}
```

> If `repo.UpsertPluginInput` / `repo.NewPluginRepo` signatures differ from Step 1's findings, adjust `seedPlugin`/`pluginActive` accordingly. `store.client` is the `*ent.Client` (cmd/cli package `main`, same package — accessible).

- [ ] **Step 3: Run test to verify it fails**

Run: `cd server && go test ./cmd/cli/ -run TestPlugins -v`
Expected: FAIL — `newPluginsCmd undefined`.

- [ ] **Step 4: Write minimal implementation**

Create `server/cmd/cli/cmd_plugins.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// newPluginsCmd is the direct-DB plugin control group — the offline lockout
// hatch. It mutates plugin.active without HTTP, so a broken auth_provider plugin
// that prevents boot can be disabled while the server is down. Lifecycle hooks
// are NOT run (they need a live server); the change applies on next boot.
func newPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugins", Short: "Enable/disable plugins directly in the DB (offline lockout hatch)"}
	cmd.PersistentFlags().String("db", "", "Path to the dashboard SQLite DB (default: $DASHBOARD_DB_PATH or ~/.claude/dashboard-tasks.db)")

	list := &cobra.Command{Use: "list", Short: "List plugins with their active state", RunE: func(cmd *cobra.Command, _ []string) error {
		return withPluginRepo(cmd, func(ctx context.Context, pr repo.PluginRepo) error {
			rows, err := pr.List(ctx)
			if err != nil {
				return err
			}
			for _, p := range rows {
				installed := p.InstalledAt != nil
				fmt.Printf("%-24s active=%-5v installed=%v\n", p.ID, p.Active, installed)
			}
			return nil
		})
	}}

	disable := &cobra.Command{Use: "disable <id>", Short: "Disable a plugin (sets active=false)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return setPluginActive(cmd, args[0], false)
	}}

	enable := &cobra.Command{Use: "enable <id>", Short: "Enable a plugin (sets active=true; hooks skipped, applies on next boot)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return setPluginActive(cmd, args[0], true)
	}}

	cmd.AddCommand(list, disable, enable)
	return cmd
}

// setPluginActive flips active for an existing plugin row; an unknown id errors
// (enabling a never-discovered plugin is almost always a typo, and the row would
// lack path/manifest — run discovery via the server instead).
func setPluginActive(cmd *cobra.Command, id string, active bool) error {
	return withPluginRepo(cmd, func(ctx context.Context, pr repo.PluginRepo) error {
		if _, err := pr.Get(ctx, id); err != nil {
			if repo.IsNotFound(err) {
				return fmt.Errorf("unknown plugin %q", id)
			}
			return err
		}
		if err := pr.SetActive(ctx, id, active); err != nil {
			return err
		}
		verb := "disabled"
		if active {
			verb = "enabled"
		}
		fmt.Printf("%s %s — restart the server to apply\n", verb, id)
		return nil
	})
}

// withPluginRepo opens the DB (reusing the settings hatch's opener), builds a
// PluginRepo, runs fn, and closes.
func withPluginRepo(cmd *cobra.Command, fn func(ctx context.Context, pr repo.PluginRepo) error) error {
	path, err := resolveDBPath(cmd)
	if err != nil {
		return err
	}
	store, err := openDBStore(path)
	if err != nil {
		return fmt.Errorf("open db %s: %w", path, err)
	}
	defer func() { _ = store.Close() }()
	return fn(cmd.Context(), repo.NewPluginRepo(store.client))
}
```

> Adjust to the real `PluginRepo` interface from Step 1 (e.g. if `Get` returns a value not pointer, or `List` returns a different row type). Keep the structure.

- [ ] **Step 5: Register on the root**

In `server/cmd/cli/main.go`, find where the root command adds subcommands (alongside `newSettingsCmd()` / `cmd_pipeline`/`cmd_tasks`). Add:

```go
	root.AddCommand(newPluginsCmd())
```

(Match the existing registration style in that file.)

- [ ] **Step 6: Run tests + build**

Run: `cd server && go test ./cmd/cli/ -run TestPlugins -v && go build ./...`
Expected: PASS, build clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/alexanderwink/code/_privat/projects/<worktree>
git add server/cmd/cli/cmd_plugins.go server/cmd/cli/cmd_plugins_test.go server/cmd/cli/main.go
git commit --no-gpg-sign -m "feat: add offline plugins enable/disable CLI

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: Docs

**Files:**
- Modify: `README.md` (or the docs section covering the `dashboard settings` lockout hatch), `CHANGELOG.md`

- [ ] **Step 1: Document the command**

Add a short subsection next to the existing `dashboard settings set auth.mode none` lockout-hatch docs: `dashboard plugins disable <id>` / `enable <id>` / `list` operate directly on the DB while the server is down; use `disable` to recover from a broken `auth_provider` plugin that prevents boot; the change applies on next start. Add a `CHANGELOG.md` Unreleased `### Added` bullet.

- [ ] **Step 2: Verify + commit**

Run: `cd server && go build ./...` (sanity). Then:
```bash
git add README.md CHANGELOG.md
git commit --no-gpg-sign -m "docs: document offline plugins CLI lockout hatch

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** list/disable/enable (Task 1) ✓; direct-DB via existing opener ✓; unknown-id errors ✓; enable skips hooks + applies on boot (message + docs) ✓; docs (Task 2) ✓. No ent change ✓.
**Placeholder scan:** the one verification point (Step 1, exact PluginRepo signatures) is explicit and bounded, not a vague TODO. Code is complete; the worker adjusts field/return shapes to the real repo.
**Type consistency:** `newPluginsCmd`, `withPluginRepo`, `setPluginActive`, `repo.NewPluginRepo`, `repo.IsNotFound`, `repo.UpsertPluginInput` used consistently; `store.client` is the ent client from `dbstore.go`.
