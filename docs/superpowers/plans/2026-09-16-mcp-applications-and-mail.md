# MCP Applications — Implementation Plan (probe + slice 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** User-scope MCP servers become registry applications that a routine can attach, whose secrets the dashboard holds, and whose tools the capability gate grants or denies per run.

**Architecture:** A new package `server/internal/mcpapps` mirrors `~/.claude.json` servers into the registry, reads each server's tool list through the MCP Go SDK client, and resolves — per task — which servers a spawn gets, with which environment, and which of their tools land in `--allowedTools` / `--disallowedTools`. The pipeline receives that resolution through a function on `StageContext`, exactly as it receives `IssueTaskAPIKey` today.

**Tech Stack:** Go 1.26, ent + SQLite, `github.com/modelcontextprotocol/go-sdk v1.7.0` (already in `server/go.mod:24`), Vue 3 + TypeScript, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-16-mcp-applications-and-mail-design.md`

**Scope of this plan:** the §8 probe and delivery slice 1 (§3.1–§3.4). Slice 2 (mail trigger, §3.5) and slice 3 (send and wait, §3.6–§3.7) get their own plans once the probe has answered §8, because their core mechanics — the cursor and "send an existing draft" — depend on those answers.

## Global Constraints

- The server binds to `127.0.0.1`, never `0.0.0.0`.
- **No dependency file changes.** `server/go.mod`, `server/go.sum`, `go.work.sum` and `pnpm-lock.yaml` must be byte-identical to `origin/main` in the final diff. The MCP client is in the already-present `go-sdk` module. A `go mod tidy` that bumps anything fails the License Attribution Freshness check.
- **ent regeneration** only via `cd server && go generate ./internal/db/ent/` (it carries `--feature sql/upsert`). Afterwards `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files. Then restore `server/go.sum` from `HEAD`, and restore `server/internal/db/ent/runtime/runtime.go` from `HEAD` if it lost its `Version`/`Sum` constants.
- **While implementing, run package-scoped tests only** (`go test ./internal/<pkg>/...`). `task test` and `go test ./...` regenerate `server/internal/db/ent/`. Run the full suites once, in the last task, and restore `ent/runtime/runtime.go` afterwards if it drifted.
- Before every commit: `gofmt -l` on touched packages prints nothing, and `go vet ./...` passes from `server/` (module-wide — a narrow test run misses sibling `_test.go` files).
- Frontend gate: `pnpm lint && pnpm typecheck && pnpm test`. Every component test that mounts calls `wrapper.unmount()`.
- Every guard on a permission path gets a test that goes red when the guard is removed; the removal is demonstrated and reverted. Restore a mutated file from a copy taken before mutating, never with `git checkout` — `HEAD` does not contain uncommitted work.
- All code, comments, commit messages and PR text in English. Conventional Commits. Commit messages describe behaviour and never reference plan task numbers or slice names.
- Comments only where they carry a non-obvious contract, timing, edge case, or case-to-effect mapping.
- `gh pr merge --squash --admin`, never `--delete-branch`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `server/internal/db/ent/schema/mcp_application.go` | Dashboard-side state per mirrored server: attach-to-all flag, required env names, tool catalogue |
| `server/internal/db/ent/schema/application_secret.go` | Encrypted secret values per application and variable name |
| `server/internal/db/ent/schema/task_schedule.go`, `task.go` | `applications` field: resource IDs a routine attaches, copied onto its tasks |
| `server/internal/db/markers.go` | One-shot markers in `applied_migrations` |
| `server/internal/db/repo/mcp_application_repo.go` | CRUD for `mcp_application` |
| `server/internal/db/repo/application_secret_repo.go` | Write-only secret storage via `secretbox` |
| `server/internal/db/repo/grant_repo.go` | `GrantViewsFromRows`, shared by the memory gate and the resolver |
| `server/internal/mcpapps/entry.go` | Parse a `~/.claude.json` server entry; merge env into its raw JSON |
| `server/internal/mcpapps/reconcile.go` | Mirror servers into the registry |
| `server/internal/mcpapps/catalogue.go` | `tools/list` through the MCP client; capability rows per tool |
| `server/internal/mcpapps/resolve.go` | Per-task resolution: servers, env, allow and deny |
| `server/internal/pipeline/{types,progress_guards,stage_handlers,spawner}.go` | Consume the resolution at spawn |
| `server/internal/channelconfig/channelconfig.go` | Export the reserved-name check; sweep orphaned temp configs |
| `server/internal/scheduler/materializer.go`, `server/serverapp/di_scheduler.go`, `server/internal/api/tasks/handler.go` | Copy `applications` from routine to task |
| `server/internal/api/schedules/{handler,view}.go` | Accept and validate `applications` on routines |
| `server/internal/api/applications/handler.go` | HTTP API for applications, secrets and catalogue refresh |
| `server/internal/mcpapps/presets/` | Grant presets per known server, produced by the probe |
| `src/features/settings/composables/useApplications.ts`, `components/ApplicationSettings.vue` | Settings → Applications |
| `src/components/ScheduleForm.vue`, `src/composables/useSchedules.ts` | Routine form: pick applications |

---

### Task 0: Probe the mail server against real accounts

Throwaway. Nothing in this task is committed except the answers and the preset data file. It needs the human for account credentials.

**Files:**
- Create (scratch, not committed): `$SCRATCH/mcpprobe/main.go`, `$SCRATCH/mcpprobe/go.mod`
- Modify: `docs/superpowers/specs/2026-09-16-mcp-applications-and-mail-design.md` (§8: add an **Answer** column)
- Create: `server/internal/mcpapps/presets/imap-mcp-server.json` (or the file named after the server finally chosen)

**Interfaces:**
- Produces: the preset file format consumed by Task 13:

```json
{
  "server": "imap-mcp-server",
  "allowForRoutine": ["<tool name>", "..."],
  "denyGlobal": ["<tool name>", "..."]
}
```

- [ ] **Step 1: Human prepares credentials**

Ask the human for, and do not store anywhere in the repo:
- an iCloud app-specific password (appleid.apple.com → Sign-In and Security → App-Specific Passwords);
- a Gmail app password (myaccount.google.com/apppasswords; requires 2-Step Verification);
- the OVH account password.

- [ ] **Step 2: Register the accounts in the server's own wizard, passwords environment-managed**

Run: `npx -p imap-mcp-server imap-setup`

For each account, tick **"Do not save to config; set later using an environment variable"** for IMAP password and SMTP password. Name the accounts `icloud`, `gmail`, `ovh`. Note the variable names the wizard prints (expected form: `IMAP_MCP_ACCOUNT_OVH_IMAP_PASSWORD`).

- [ ] **Step 3: Write the probe client**

`$SCRATCH/mcpprobe/go.mod`:

```
module mcpprobe

go 1.26

require github.com/modelcontextprotocol/go-sdk v1.7.0
```

`$SCRATCH/mcpprobe/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Usage: go run . [tool-name json-args]
// Without arguments it prints tools/list. With arguments it calls one tool.
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.Command("npx", "-y", "imap-mcp-server")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpprobe", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer session.Close()

	if len(os.Args) < 2 {
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				fmt.Fprintln(os.Stderr, "tools/list:", err)
				os.Exit(1)
			}
			hints := "-"
			if tool.Annotations != nil {
				hints = fmt.Sprintf("readOnly=%v destructive=%v", tool.Annotations.ReadOnlyHint, deref(tool.Annotations.DestructiveHint))
			}
			schema, _ := json.Marshal(tool.InputSchema)
			fmt.Printf("%s\t%s\n  %s\n  schema: %s\n", tool.Name, hints, strings.TrimSpace(tool.Description), schema)
		}
		return
	}

	var args map[string]any
	if len(os.Args) > 2 {
		if err := json.Unmarshal([]byte(os.Args[2]), &args); err != nil {
			fmt.Fprintln(os.Stderr, "args:", err)
			os.Exit(1)
		}
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: os.Args[1], Arguments: args})
	if err != nil {
		fmt.Fprintln(os.Stderr, "call:", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}

func deref(b *bool) any {
	if b == nil {
		return "unset"
	}
	return *b
}
```

- [ ] **Step 4: Answer "does `tools/list` work without credentials?"**

Run with no password variables exported: `cd $SCRATCH/mcpprobe && go run .`
Record: tool list printed → **yes**; connect/list error naming a missing variable → **no** (then Task 5's refresh must run with secrets injected, which it does anyway).

- [ ] **Step 5: Answer "multi-account credentials from env?"**

Export the three account password variables from Step 2 (IMAP and SMTP for each), run a search on each account. Use the search tool name and argument shape from Step 4's output, for example:

`go run . <search-tool> '{"accountId":"ovh","folder":"INBOX","limit":3}'`

Record: all three return messages → **yes**.

- [ ] **Step 6: Answer "UIDs and UIDVALIDITY exposed?"**

Inspect Step 5's output for per-message `uid` and a folder `uidValidity` (or equivalent). Record which fields exist.

- [ ] **Step 7: Answer "send an existing draft?"**

In `tools/list`, look for a tool that sends a draft by id. If one exists, create a draft addressed to the human's own address and send it by id. Record **yes** plus the tool name, or **no**.

- [ ] **Step 8: Answer "sent copy in `\Sent`?"**

After Step 7 (or after sending with the plain send tool), list the `Sent` folder of each account through the probe. Record per account whether the sent mail appears without the client appending it.

- [ ] **Step 9: Decision gate**

If Step 5 is **no**, switch to `codefuturist/email-mcp` with one server entry per account, repeat Steps 3–8 with `npx -y @codefuturist/email-mcp`, and use its name in the preset file.

- [ ] **Step 10: Write the preset file from the real tool list**

Classify every tool from Step 4, with the human confirming:
- search, read, list folders, create or update a draft → `allowForRoutine`;
- every tool that sends, forwards or replies → `denyGlobal`;
- every tool that adds, updates or removes accounts, or changes server settings → `denyGlobal`;
- anything else stays out of both lists and therefore asks.

Write `server/internal/mcpapps/presets/<server>.json` in the format under **Interfaces**.

- [ ] **Step 11: Record the answers**

Add an **Answer** column to the §8 table in the spec with the result of Steps 4–8.

- [ ] **Step 12: Commit**

```bash
git add docs/superpowers/specs/2026-09-16-mcp-applications-and-mail-design.md server/internal/mcpapps/presets/
git commit -m "docs: record mail MCP server probe results and its grant preset"
```

---

### Task 1: Schema for applications and their secrets

**Files:**
- Create: `server/internal/db/ent/schema/mcp_application.go`
- Create: `server/internal/db/ent/schema/application_secret.go`
- Modify: `server/internal/db/client_test.go` (append one test)
- Regenerated: `server/internal/db/ent/`

**Interfaces:**
- Produces: ent types `ent.MCPApplication` (fields `ID`, `ResourceID`, `ServerName`, `AttachAll`, `RequiredEnv []string`, `Catalogue []schema.CatalogueTool`, `CatalogueError`, `CatalogueRefreshedAt *time.Time`), `ent.ApplicationSecret` (`ID`, `ResourceID`, `EnvName`, `Ciphertext`, `Nonce`, `UpdatedAt`), and `schema.CatalogueTool`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/db/client_test.go`:

```go
func TestOpen_MCPApplicationTables(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()
	ctx := t.Context()

	app, err := bundle.Client.MCPApplication.Create().
		SetID("app-1").
		SetResourceID("res-1").
		SetServerName("mail").
		Save(ctx)
	require.NoError(t, err)
	require.False(t, app.AttachAll)
	require.Empty(t, app.RequiredEnv)
	require.Empty(t, app.Catalogue)

	_, err = bundle.Client.ApplicationSecret.Create().
		SetID("sec-1").SetResourceID("res-1").SetEnvName("PASSWORD").
		SetCiphertext("c").SetNonce("n").
		Save(ctx)
	require.NoError(t, err)

	_, err = bundle.Client.ApplicationSecret.Create().
		SetID("sec-2").SetResourceID("res-1").SetEnvName("PASSWORD").
		SetCiphertext("c").SetNonce("n").
		Save(ctx)
	require.Error(t, err, "one value per application and variable name")
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/db/ -run TestOpen_MCPApplicationTables -count=1`
Expected: FAIL — compile error, `bundle.Client.MCPApplication undefined`.

- [ ] **Step 3: Write the schemas**

`server/internal/db/ent/schema/mcp_application.go`:

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

// CatalogueTool is one entry of a server's tools/list, as last read. The hints
// are the server's own claims and are shown, never trusted.
type CatalogueTool struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
}

type MCPApplication struct{ ent.Schema }

func (MCPApplication) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("resource_id").Unique().Immutable(),
		field.String("server_name").Immutable(),
		field.Bool("attach_all").Default(false),
		field.JSON("required_env", []string{}).
			Default([]string{}).
			Annotations(entsql.Default("[]")),
		field.JSON("catalogue", []CatalogueTool{}).
			Default([]CatalogueTool{}).
			Annotations(entsql.Default("[]")),
		field.String("catalogue_error").Default(""),
		field.Time("catalogue_refreshed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
```

`server/internal/db/ent/schema/application_secret.go`:

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ApplicationSecret struct{ ent.Schema }

func (ApplicationSecret) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("resource_id").Immutable(),
		field.String("env_name").Immutable(),
		field.String("ciphertext").Sensitive(),
		field.String("nonce").Sensitive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ApplicationSecret) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_id", "env_name").Unique(),
	}
}
```

- [ ] **Step 4: Regenerate ent**

```bash
cd server && go generate ./internal/db/ent/
cd .. && grep -rl "OnConflict" server/internal/db/ent/ | head -3
git checkout HEAD -- server/go.sum
grep -q 'Version = ' server/internal/db/ent/runtime/runtime.go || git checkout HEAD -- server/internal/db/ent/runtime/runtime.go
```

Expected: the grep prints at least one file.

- [ ] **Step 5: Run the test to see it pass**

Run: `cd server && go test ./internal/db/ -run TestOpen_MCPApplicationTables -count=1`
Expected: PASS

- [ ] **Step 6: Vet, format, commit**

```bash
cd server && gofmt -l ./internal/db/ent/schema/ && go vet ./... && cd ..
git diff --name-only origin/main -- server/go.mod server/go.sum go.work.sum   # must print nothing
git add server/internal/db/ent/ server/internal/db/client_test.go
git commit -m "feat(db): store MCP application state and encrypted application secrets"
```

---

### Task 2: Repositories and one-shot markers

**Files:**
- Create: `server/internal/db/markers.go`
- Create: `server/internal/db/repo/mcp_application_repo.go`
- Create: `server/internal/db/repo/application_secret_repo.go`
- Test: `server/internal/db/markers_test.go`, `server/internal/db/repo/mcp_application_repo_test.go`, `server/internal/db/repo/application_secret_repo_test.go`

**Interfaces:**
- Consumes: ent types from Task 1; `secretbox.New(key []byte)` requires a 32-byte key; `Box.Encrypt(plaintext string) (ciphertextB64, nonceB64 string, err error)` and `Box.Decrypt(ciphertextB64, nonceB64 string) (string, error)` (`server/internal/secretbox/secretbox.go:29,46,56`).
- Produces:

```go
// package db
type MarkerStore struct{ DB *sql.DB }
func (m MarkerStore) Has(ctx context.Context, name string) (bool, error)
func (m MarkerStore) Record(ctx context.Context, name string) error

// package repo
type UpsertMCPApplicationInput struct { ResourceID, ServerName string; AttachAll bool }
type MCPApplicationRepo interface {
	Upsert(ctx context.Context, in UpsertMCPApplicationInput) (*ent.MCPApplication, error)
	GetByResourceID(ctx context.Context, resourceID string) (*ent.MCPApplication, error)
	List(ctx context.Context) ([]*ent.MCPApplication, error)
	SetAttachAll(ctx context.Context, resourceID string, attachAll bool) (*ent.MCPApplication, error)
	SetRequiredEnv(ctx context.Context, resourceID string, names []string) (*ent.MCPApplication, error)
	RecordCatalogue(ctx context.Context, resourceID string, tools []schema.CatalogueTool, catalogueErr string, at time.Time) error
}
func NewMCPApplicationRepo(client *ent.Client) MCPApplicationRepo

var ErrSecretsUnavailable error
type SecretMeta struct { EnvName string; UpdatedAt time.Time }
type ApplicationSecretRepo interface {
	Set(ctx context.Context, resourceID, envName, value string) error
	Delete(ctx context.Context, resourceID, envName string) error
	List(ctx context.Context, resourceID string) ([]SecretMeta, error)
	Values(ctx context.Context, resourceID string) (map[string]string, error)
}
func NewApplicationSecretRepo(client *ent.Client, box *secretbox.Box) ApplicationSecretRepo
```

- [ ] **Step 1: Write the failing tests**

`server/internal/db/markers_test.go`:

```go
package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func TestMarkerStore_RecordsOnce(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()
	ctx := t.Context()
	m := db.MarkerStore{DB: bundle.DB}

	has, err := m.Has(ctx, "probe")
	require.NoError(t, err)
	require.False(t, has)

	require.NoError(t, m.Record(ctx, "probe"))
	require.NoError(t, m.Record(ctx, "probe"), "recording twice is not an error")

	has, err = m.Has(ctx, "probe")
	require.NoError(t, err)
	require.True(t, has)
}
```

`server/internal/db/repo/mcp_application_repo_test.go`:

```go
package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestMCPApplicationRepo_UpsertKeepsAttachAllOfExistingRow(t *testing.T) {
	apps := repo.NewMCPApplicationRepo(openDB(t))
	ctx := context.Background()

	first, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-1", ServerName: "mail", AttachAll: true})
	require.NoError(t, err)
	require.True(t, first.AttachAll)

	again, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-1", ServerName: "mail", AttachAll: false})
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.True(t, again.AttachAll, "a later reconcile must not revoke a human's or the first reconcile's choice")
}

func TestMCPApplicationRepo_RecordCatalogueKeepsToolsOnError(t *testing.T) {
	apps := repo.NewMCPApplicationRepo(openDB(t))
	ctx := context.Background()
	_, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-1", ServerName: "mail"})
	require.NoError(t, err)

	tools := []schema.CatalogueTool{{Name: "search"}}
	require.NoError(t, apps.RecordCatalogue(ctx, "res-1", tools, "", time.Now()))
	require.NoError(t, apps.RecordCatalogue(ctx, "res-1", nil, "connect: refused", time.Now()))

	got, err := apps.GetByResourceID(ctx, "res-1")
	require.NoError(t, err)
	require.Equal(t, tools, got.Catalogue, "a failed refresh must not erase the last good catalogue")
	require.Equal(t, "connect: refused", got.CatalogueError)
}
```

`server/internal/db/repo/application_secret_repo_test.go`:

```go
package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

func newBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	return box
}

func TestApplicationSecretRepo_RoundTripAndOverwrite(t *testing.T) {
	client := openDB(t)
	secrets := repo.NewApplicationSecretRepo(client, newBox(t))
	ctx := context.Background()

	require.NoError(t, secrets.Set(ctx, "res-1", "IMAP_PASSWORD", "first"))
	require.NoError(t, secrets.Set(ctx, "res-1", "IMAP_PASSWORD", "second"))
	require.NoError(t, secrets.Set(ctx, "res-2", "IMAP_PASSWORD", "other"))

	values, err := secrets.Values(ctx, "res-1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"IMAP_PASSWORD": "second"}, values)

	rows, err := client.ApplicationSecret.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		for _, plain := range []string{"first", "second", "other"} {
			require.NotContains(t, row.Ciphertext, plain, "stored value must be encrypted")
		}
	}

	meta, err := secrets.List(ctx, "res-1")
	require.NoError(t, err)
	require.Len(t, meta, 1)
	require.Equal(t, "IMAP_PASSWORD", meta[0].EnvName)

	require.NoError(t, secrets.Delete(ctx, "res-1", "IMAP_PASSWORD"))
	values, err = secrets.Values(ctx, "res-1")
	require.NoError(t, err)
	require.Empty(t, values)
}

func TestApplicationSecretRepo_WithoutBoxRefusesToStore(t *testing.T) {
	secrets := repo.NewApplicationSecretRepo(openDB(t), nil)
	err := secrets.Set(context.Background(), "res-1", "IMAP_PASSWORD", "x")
	require.ErrorIs(t, err, repo.ErrSecretsUnavailable)
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd server && go test ./internal/db/ ./internal/db/repo/ -run 'TestMarkerStore|TestMCPApplicationRepo|TestApplicationSecretRepo' -count=1`
Expected: FAIL — compile errors, undefined `db.MarkerStore`, `repo.NewMCPApplicationRepo`, `repo.NewApplicationSecretRepo`.

- [ ] **Step 3: Implement**

`server/internal/db/markers.go`:

```go
package db

import (
	"context"
	"database/sql"
	"fmt"
)

// MarkerStore records one-shot events in applied_migrations — the table
// migrateRenameStages introduced. It creates the table itself so it does not
// depend on that migration having run first.
type MarkerStore struct{ DB *sql.DB }

func (m MarkerStore) ensure(ctx context.Context) error {
	_, err := m.DB.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS applied_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`)
	return err
}

func (m MarkerStore) Has(ctx context.Context, name string) (bool, error) {
	if err := m.ensure(ctx); err != nil {
		return false, fmt.Errorf("markers: %w", err)
	}
	var n int
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM applied_migrations WHERE name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("markers: has %q: %w", name, err)
	}
	return n > 0, nil
}

func (m MarkerStore) Record(ctx context.Context, name string) error {
	if err := m.ensure(ctx); err != nil {
		return fmt.Errorf("markers: %w", err)
	}
	if _, err := m.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO applied_migrations (name, applied_at) VALUES (?, datetime('now'))`, name); err != nil {
		return fmt.Errorf("markers: record %q: %w", name, err)
	}
	return nil
}
```

`server/internal/db/repo/mcp_application_repo.go`:

```go
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/mcpapplication"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
)

type UpsertMCPApplicationInput struct {
	ResourceID string
	ServerName string
	// AttachAll applies when the row is created; an existing row keeps its value.
	AttachAll bool
}

type MCPApplicationRepo interface {
	Upsert(ctx context.Context, in UpsertMCPApplicationInput) (*ent.MCPApplication, error)
	GetByResourceID(ctx context.Context, resourceID string) (*ent.MCPApplication, error)
	List(ctx context.Context) ([]*ent.MCPApplication, error)
	SetAttachAll(ctx context.Context, resourceID string, attachAll bool) (*ent.MCPApplication, error)
	SetRequiredEnv(ctx context.Context, resourceID string, names []string) (*ent.MCPApplication, error)
	RecordCatalogue(ctx context.Context, resourceID string, tools []schema.CatalogueTool, catalogueErr string, at time.Time) error
}

type entMCPApplicationRepo struct{ client *ent.Client }

func NewMCPApplicationRepo(client *ent.Client) MCPApplicationRepo {
	return &entMCPApplicationRepo{client: client}
}

func (r *entMCPApplicationRepo) Upsert(ctx context.Context, in UpsertMCPApplicationInput) (*ent.MCPApplication, error) {
	existing, err := r.GetByResourceID(ctx, in.ResourceID)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("mcpapplication.Upsert: %w", err)
	}
	row, err := r.client.MCPApplication.Create().
		SetID(uuid.New().String()).
		SetResourceID(in.ResourceID).
		SetServerName(in.ServerName).
		SetAttachAll(in.AttachAll).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpapplication.Upsert: %w", err)
	}
	return row, nil
}

func (r *entMCPApplicationRepo) GetByResourceID(ctx context.Context, resourceID string) (*ent.MCPApplication, error) {
	return r.client.MCPApplication.Query().Where(mcpapplication.ResourceID(resourceID)).Only(ctx)
}

func (r *entMCPApplicationRepo) List(ctx context.Context) ([]*ent.MCPApplication, error) {
	rows, err := r.client.MCPApplication.Query().Order(ent.Asc(mcpapplication.FieldServerName)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpapplication.List: %w", err)
	}
	return rows, nil
}

func (r *entMCPApplicationRepo) SetAttachAll(ctx context.Context, resourceID string, attachAll bool) (*ent.MCPApplication, error) {
	row, err := r.GetByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return row.Update().SetAttachAll(attachAll).Save(ctx)
}

func (r *entMCPApplicationRepo) SetRequiredEnv(ctx context.Context, resourceID string, names []string) (*ent.MCPApplication, error) {
	row, err := r.GetByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if names == nil {
		names = []string{}
	}
	return row.Update().SetRequiredEnv(names).Save(ctx)
}

func (r *entMCPApplicationRepo) RecordCatalogue(ctx context.Context, resourceID string, tools []schema.CatalogueTool, catalogueErr string, at time.Time) error {
	row, err := r.GetByResourceID(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("mcpapplication.RecordCatalogue: %w", err)
	}
	upd := row.Update().SetCatalogueError(catalogueErr).SetCatalogueRefreshedAt(at)
	if catalogueErr == "" {
		if tools == nil {
			tools = []schema.CatalogueTool{}
		}
		upd = upd.SetCatalogue(tools)
	}
	return upd.Exec(ctx)
}
```

`server/internal/db/repo/application_secret_repo.go`:

```go
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/applicationsecret"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

var ErrSecretsUnavailable = errors.New("application secrets: no encryption key is configured")

type SecretMeta struct {
	EnvName   string
	UpdatedAt time.Time
}

type ApplicationSecretRepo interface {
	Set(ctx context.Context, resourceID, envName, value string) error
	Delete(ctx context.Context, resourceID, envName string) error
	List(ctx context.Context, resourceID string) ([]SecretMeta, error)
	Values(ctx context.Context, resourceID string) (map[string]string, error)
}

type entApplicationSecretRepo struct {
	client *ent.Client
	box    *secretbox.Box
}

func NewApplicationSecretRepo(client *ent.Client, box *secretbox.Box) ApplicationSecretRepo {
	return &entApplicationSecretRepo{client: client, box: box}
}

func (r *entApplicationSecretRepo) Set(ctx context.Context, resourceID, envName, value string) error {
	if r.box == nil {
		return ErrSecretsUnavailable
	}
	ciphertext, nonce, err := r.box.Encrypt(value)
	if err != nil {
		return fmt.Errorf("applicationsecret.Set: encrypt: %w", err)
	}
	existing, err := r.client.ApplicationSecret.Query().
		Where(applicationsecret.ResourceID(resourceID), applicationsecret.EnvName(envName)).
		Only(ctx)
	switch {
	case err == nil:
		return existing.Update().SetCiphertext(ciphertext).SetNonce(nonce).Exec(ctx)
	case ent.IsNotFound(err):
		return r.client.ApplicationSecret.Create().
			SetID(uuid.New().String()).
			SetResourceID(resourceID).
			SetEnvName(envName).
			SetCiphertext(ciphertext).
			SetNonce(nonce).
			Exec(ctx)
	default:
		return fmt.Errorf("applicationsecret.Set: %w", err)
	}
}

func (r *entApplicationSecretRepo) Delete(ctx context.Context, resourceID, envName string) error {
	_, err := r.client.ApplicationSecret.Delete().
		Where(applicationsecret.ResourceID(resourceID), applicationsecret.EnvName(envName)).
		Exec(ctx)
	return err
}

func (r *entApplicationSecretRepo) List(ctx context.Context, resourceID string) ([]SecretMeta, error) {
	rows, err := r.client.ApplicationSecret.Query().
		Where(applicationsecret.ResourceID(resourceID)).
		Order(ent.Asc(applicationsecret.FieldEnvName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("applicationsecret.List: %w", err)
	}
	out := make([]SecretMeta, 0, len(rows))
	for _, row := range rows {
		out = append(out, SecretMeta{EnvName: row.EnvName, UpdatedAt: row.UpdatedAt})
	}
	return out, nil
}

func (r *entApplicationSecretRepo) Values(ctx context.Context, resourceID string) (map[string]string, error) {
	rows, err := r.client.ApplicationSecret.Query().Where(applicationsecret.ResourceID(resourceID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("applicationsecret.Values: %w", err)
	}
	out := make(map[string]string, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	if r.box == nil {
		return nil, ErrSecretsUnavailable
	}
	for _, row := range rows {
		plain, err := r.box.Decrypt(row.Ciphertext, row.Nonce)
		if err != nil {
			return nil, fmt.Errorf("applicationsecret.Values: decrypt %s: %w", row.EnvName, err)
		}
		out[row.EnvName] = plain
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd server && go test ./internal/db/ ./internal/db/repo/ -run 'TestMarkerStore|TestMCPApplicationRepo|TestApplicationSecretRepo' -count=1`
Expected: PASS

- [ ] **Step 5: Mutation — encryption**

Copy `application_secret_repo.go` to the scratch directory. In `Set`, replace `SetCiphertext(ciphertext)` in the **create** branch with `SetCiphertext(value)`. Run the Step 4 command; expected FAIL in `TestApplicationSecretRepo_RoundTripAndOverwrite` — the `res-2` row, which is only ever created, holds `other` in clear ("stored value must be encrypted"). Restore the file from the copy and rerun: PASS.

- [ ] **Step 6: Vet, format, commit**

```bash
cd server && gofmt -l ./internal/db/ && go vet ./... && cd ..
git add server/internal/db/markers.go server/internal/db/markers_test.go server/internal/db/repo/mcp_application_repo.go server/internal/db/repo/mcp_application_repo_test.go server/internal/db/repo/application_secret_repo.go server/internal/db/repo/application_secret_repo_test.go
git commit -m "feat(db): repositories for MCP applications and their encrypted secrets"
```

---
### Task 3: Server entries and environment merge

**Files:**
- Create: `server/internal/mcpapps/entry.go`
- Test: `server/internal/mcpapps/entry_test.go`

**Interfaces:**
- Produces:

```go
type ServerEntry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}
func ParseEntry(raw json.RawMessage) (ServerEntry, error)
func (e ServerEntry) IsStdio() bool
func WithEnv(raw json.RawMessage, env map[string]string) (json.RawMessage, error)
```

- [ ] **Step 1: Write the failing test**

```go
package mcpapps_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestParseEntry_Stdio(t *testing.T) {
	e, err := mcpapps.ParseEntry(json.RawMessage(`{"command":"npx","args":["-y","imap-mcp-server"]}`))
	require.NoError(t, err)
	require.True(t, e.IsStdio())
	require.Equal(t, []string{"-y", "imap-mcp-server"}, e.Args)

	e, err = mcpapps.ParseEntry(json.RawMessage(`{"type":"http","url":"http://127.0.0.1:1/mcp"}`))
	require.NoError(t, err)
	require.False(t, e.IsStdio())
}

func TestWithEnv_MergesAndPreservesUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{"command":"npx","env":{"KEEP":"1","OVERRIDE":"old"},"timeout":30}`)
	out, err := mcpapps.WithEnv(raw, map[string]string{"OVERRIDE": "new", "ADDED": "2"})
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, float64(30), got["timeout"], "fields the CLI adds must survive")
	require.Equal(t, map[string]any{"KEEP": "1", "OVERRIDE": "new", "ADDED": "2"}, got["env"])
}

func TestWithEnv_NoEnvReturnsInputUnchanged(t *testing.T) {
	raw := json.RawMessage(`{"command":"npx"}`)
	out, err := mcpapps.WithEnv(raw, nil)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(out))
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/mcpapps/ -count=1`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

```go
package mcpapps

import (
	"encoding/json"
	"fmt"
)

type ServerEntry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func ParseEntry(raw json.RawMessage) (ServerEntry, error) {
	var e ServerEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return ServerEntry{}, fmt.Errorf("mcpapps: parse server entry: %w", err)
	}
	return e, nil
}

func (e ServerEntry) IsStdio() bool {
	return (e.Type == "" || e.Type == "stdio") && e.Command != ""
}

// WithEnv merges env into the entry's "env" object. It works on the raw object
// rather than ServerEntry so fields the Claude CLI may add to an entry are kept.
func WithEnv(raw json.RawMessage, env map[string]string) (json.RawMessage, error) {
	if len(env) == 0 {
		return raw, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("mcpapps: merge env: %w", err)
	}
	merged, _ := obj["env"].(map[string]any)
	if merged == nil {
		merged = map[string]any{}
	}
	for k, v := range env {
		merged[k] = v
	}
	obj["env"] = merged
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("mcpapps: merge env: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `cd server && go test ./internal/mcpapps/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l ./internal/mcpapps/ && go vet ./... && cd ..
git add server/internal/mcpapps/entry.go server/internal/mcpapps/entry_test.go
git commit -m "feat(mcpapps): parse user-scope MCP server entries and merge environment"
```

---

### Task 4: Mirror user-scope servers into the registry

**Files:**
- Create: `server/internal/mcpapps/reconcile.go`
- Test: `server/internal/mcpapps/reconcile_test.go`
- Modify: `server/internal/channelconfig/channelconfig.go` (export the reserved-name check next to `reservedServerNames`, line 65)
- Modify: `server/serverapp/di.go` (call after `ReconcileScheduleResources`, line 338)

**Interfaces:**
- Consumes: `repo.MCPApplicationRepo`, `db.MarkerStore` (Task 2); `repo.ResourceRepo.Upsert`; `claudeconfig.UserMCPServers()`.
- Produces:

```go
// package channelconfig
func IsReservedServerName(name string) bool

// package mcpapps
const InitialReconcileMarker = "mcp-applications-initial-reconcile"
type Markers interface {
	Has(ctx context.Context, name string) (bool, error)
	Record(ctx context.Context, name string) error
}
func ResourceSlug(serverName string) string   // "mcp-" + serverName
func Reconcile(ctx context.Context, servers map[string]json.RawMessage, resources repo.ResourceRepo, apps repo.MCPApplicationRepo, markers Markers) (int, error)
```

- [ ] **Step 1: Write the failing test**

```go
package mcpapps_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

type reconcileFixture struct {
	resources repo.ResourceRepo
	apps      repo.MCPApplicationRepo
	markers   db.MarkerStore
}

func newReconcileFixture(t *testing.T) reconcileFixture {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return reconcileFixture{
		resources: repo.NewResourceRepo(bundle.Client),
		apps:      repo.NewMCPApplicationRepo(bundle.Client),
		markers:   db.MarkerStore{DB: bundle.DB},
	}
}

func servers(names ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, n := range names {
		out[n] = json.RawMessage(`{"command":"true"}`)
	}
	return out
}

func TestReconcile_FirstRunAttachesToAllLaterServersDoNot(t *testing.T) {
	f := newReconcileFixture(t)
	ctx := context.Background()

	n, err := mcpapps.Reconcile(ctx, servers("obsidian"), f.resources, f.apps, f.markers)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = mcpapps.Reconcile(ctx, servers("obsidian", "mail"), f.resources, f.apps, f.markers)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	rows, err := f.apps.List(ctx)
	require.NoError(t, err)
	byName := map[string]bool{}
	for _, r := range rows {
		byName[r.ServerName] = r.AttachAll
	}
	require.True(t, byName["obsidian"], "a server present before this feature keeps reaching every run")
	require.False(t, byName["mail"], "a server added later must never reach a run nobody attached it to")
}

func TestReconcile_EmptyFirstRunStillRecordsMarker(t *testing.T) {
	f := newReconcileFixture(t)
	ctx := context.Background()

	_, err := mcpapps.Reconcile(ctx, nil, f.resources, f.apps, f.markers)
	require.NoError(t, err)
	_, err = mcpapps.Reconcile(ctx, servers("mail"), f.resources, f.apps, f.markers)
	require.NoError(t, err)

	row, err := f.apps.List(ctx)
	require.NoError(t, err)
	require.Len(t, row, 1)
	require.False(t, row[0].AttachAll)
}

func TestReconcile_SkipsReservedNamesAndIsIdempotent(t *testing.T) {
	f := newReconcileFixture(t)
	ctx := context.Background()
	reserved := channelconfig.ChannelServerName
	require.True(t, channelconfig.IsReservedServerName(reserved))

	for range 2 {
		_, err := mcpapps.Reconcile(ctx, servers(reserved, "mail"), f.resources, f.apps, f.markers)
		require.NoError(t, err)
	}
	rows, err := f.apps.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "mail", rows[0].ServerName)

	res, err := f.resources.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), mcpapps.ResourceSlug("mail"))
	require.NoError(t, err)
	require.Equal(t, rows[0].ResourceID, res.ID)
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/mcpapps/ -run TestReconcile -count=1`
Expected: FAIL — `undefined: mcpapps.Reconcile`, `channelconfig.IsReservedServerName`.

- [ ] **Step 3: Export the reserved-name check**

In `server/internal/channelconfig/channelconfig.go`, directly below `var reservedServerNames = …` (line 65):

```go
func IsReservedServerName(name string) bool { return reservedServerNames[name] }
```

- [ ] **Step 4: Implement the reconciler**

`server/internal/mcpapps/reconcile.go`:

```go
package mcpapps

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// InitialReconcileMarker separates servers that existed before applications
// did — which kept reaching every run — from servers added afterwards, which
// reach a run only when attached. The data cannot tell the two apart: an empty
// table after an install with no servers looks exactly like a first run.
const InitialReconcileMarker = "mcp-applications-initial-reconcile"

type Markers interface {
	Has(ctx context.Context, name string) (bool, error)
	Record(ctx context.Context, name string) error
}

// ResourceSlug prefixes the server name so a server can never take over the
// registry row of a plugin application with the same slug.
func ResourceSlug(serverName string) string { return "mcp-" + serverName }

func Reconcile(ctx context.Context, servers map[string]json.RawMessage, resources repo.ResourceRepo, apps repo.MCPApplicationRepo, markers Markers) (int, error) {
	seen, err := markers.Has(ctx, InitialReconcileMarker)
	if err != nil {
		return 0, fmt.Errorf("mcpapps.Reconcile: %w", err)
	}
	firstRun := !seen

	names := make([]string, 0, len(servers))
	for name := range servers {
		if !channelconfig.IsReservedServerName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	mirrored := 0
	for _, name := range names {
		res, err := resources.Upsert(ctx, repo.UpsertResourceInput{
			Kind:      repo.ResourceKindApplication,
			Slug:      ResourceSlug(name),
			Name:      name,
			Scope:     repo.GlobalScope(),
			State:     repo.ResourceStateDiscovered,
			Origin:    repo.ResourceOriginLocal,
			OriginRef: name,
		})
		if err != nil {
			slog.Warn("mcpapps: server not mirrored", "server", name, "err", err)
			continue
		}
		if _, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{
			ResourceID: res.ID,
			ServerName: name,
			AttachAll:  firstRun,
		}); err != nil {
			slog.Warn("mcpapps: application state not stored", "server", name, "err", err)
			continue
		}
		mirrored++
	}

	if firstRun {
		if err := markers.Record(ctx, InitialReconcileMarker); err != nil {
			return mirrored, fmt.Errorf("mcpapps.Reconcile: %w", err)
		}
	}
	return mirrored, nil
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `cd server && go test ./internal/mcpapps/ -run TestReconcile -count=1`
Expected: PASS

- [ ] **Step 6: Mutation — later servers are opt-in**

Copy `reconcile.go` aside. Change `AttachAll: firstRun,` to `AttachAll: true,`. Run Step 5: expected FAIL in `TestReconcile_FirstRunAttachesToAllLaterServersDoNot` ("a server added later must never reach a run…"). Restore from the copy; PASS.

- [ ] **Step 7: Wire it at boot**

In `server/serverapp/di.go`, directly after the `ReconcileScheduleResources` block (lines 337–341), add. `resourceRepo` is assigned on line 332; use `bundle.DB` for the markers — `bundle` is in scope from line 174, while `rawDB` is only assigned on line 636:

```go
		mcpAppRepo := repo.NewMCPApplicationRepo(entClient)
		if servers, err := claudeconfig.UserMCPServers(); err != nil {
			// The first-run marker must not be recorded from an unreadable file,
			// or every server that exists today would be demoted to opt-in.
			slog.Warn("mcpapps: ~/.claude.json unreadable — applications not reconciled", "err", err)
		} else if n, err := mcpapps.Reconcile(ctx, servers, resourceRepo, mcpAppRepo, db.MarkerStore{DB: bundle.DB}); err != nil {
			slog.Warn("mcpapps: reconcile failed", "err", err)
		} else {
			slog.Info("mcpapps: applications reconciled", "mirrored", n)
		}
```

Add the imports `claudeconfig`, `mcpapps` and `db` if not already present.

- [ ] **Step 8: Build, vet, commit**

```bash
cd server && go build ./... && gofmt -l ./internal/mcpapps/ ./internal/channelconfig/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/mcpapps/reconcile.go server/internal/mcpapps/reconcile_test.go server/internal/channelconfig/channelconfig.go server/serverapp/di.go
git commit -m "feat(mcpapps): mirror user-scope MCP servers into the registry, opt-in for new servers"
```

---

### Task 5: Tool catalogue through the MCP client

**Files:**
- Create: `server/internal/mcpapps/catalogue.go`
- Test: `server/internal/mcpapps/catalogue_test.go`

**Interfaces:**
- Consumes: `repo.MCPApplicationRepo.RecordCatalogue`, `repo.CapabilityRepo` (`Get`, `Upsert`), `schema.CatalogueTool`, `ServerEntry` (Task 3).
- Produces:

```go
func CapabilityName(serverName, toolName string) string // "mcp__<server>__<tool>"
func ListTools(ctx context.Context, transport mcp.Transport) ([]schema.CatalogueTool, error)
func StdioTransport(entry ServerEntry, env map[string]string) (mcp.Transport, error)
type Refresher struct {
	Apps         repo.MCPApplicationRepo
	Secrets      repo.ApplicationSecretRepo
	Capabilities repo.CapabilityRepo
	ReadServers  func() (map[string]json.RawMessage, error)
	Transport    func(entry ServerEntry, env map[string]string) (mcp.Transport, error)
	Now          func() time.Time
}
func (r Refresher) Refresh(ctx context.Context, resourceID string) ([]schema.CatalogueTool, error)
```

- [ ] **Step 1: Write the failing test**

The test runs a real MCP server in memory; no process and no network.

```go
package mcpapps_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

type noArgs struct{}

// fakeMailServer returns a client-side transport to an in-memory MCP server
// exposing a read-only search tool and a send tool.
func fakeMailServer(t *testing.T) mcp.Transport {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-mail", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_emails",
		Description: "Search a mailbox",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "send_email", Description: "Send mail"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	serverT, clientT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverT) }()
	return clientT
}

func TestListTools_ReadsNamesAndHints(t *testing.T) {
	tools, err := mcpapps.ListTools(context.Background(), fakeMailServer(t))
	require.NoError(t, err)
	require.Len(t, tools, 2)
	byName := map[string]bool{}
	for _, tool := range tools {
		byName[tool.Name] = tool.ReadOnlyHint
	}
	require.True(t, byName["search_emails"])
	require.False(t, byName["send_email"])
}

func TestRefresh_SeedsCapabilitiesAsAskAndNeverDowngradesAnExistingClass(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()

	apps := repo.NewMCPApplicationRepo(bundle.Client)
	caps := repo.NewCapabilityRepo(bundle.Client)
	app, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)

	_, err = caps.Upsert(ctx, repo.UpsertCapabilityInput{
		Name: mcpapps.CapabilityName("mail", "send_email"), Class: repo.CapClassSpend,
		EnforceableBy: []string{capability.EnforcerSpawn},
	})
	require.NoError(t, err)

	transport := fakeMailServer(t)
	r := mcpapps.Refresher{
		Apps:         apps,
		Secrets:      repo.NewApplicationSecretRepo(bundle.Client, nil),
		Capabilities: caps,
		ReadServers: func() (map[string]json.RawMessage, error) {
			return map[string]json.RawMessage{"mail": json.RawMessage(`{"command":"unused"}`)}, nil
		},
		Transport: func(mcpapps.ServerEntry, map[string]string) (mcp.Transport, error) { return transport, nil },
		Now:       time.Now,
	}
	tools, err := r.Refresh(ctx, app.ResourceID)
	require.NoError(t, err)
	require.Len(t, tools, 2)

	search, err := caps.Get(ctx, "mcp__mail__search_emails")
	require.NoError(t, err)
	require.Equal(t, repo.CapClassTool, search.Class, "a new tool is never silently allowed")
	require.Equal(t, []string{capability.EnforcerSpawn}, search.EnforceableBy)

	send, err := caps.Get(ctx, "mcp__mail__send_email")
	require.NoError(t, err)
	require.Equal(t, repo.CapClassSpend, send.Class, "a refresh must not undo a stricter class")

	stored, err := apps.GetByResourceID(ctx, app.ResourceID)
	require.NoError(t, err)
	require.Len(t, stored.Catalogue, 2)
	require.Empty(t, stored.CatalogueError)
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/mcpapps/ -run 'TestListTools|TestRefresh' -count=1`
Expected: FAIL — `undefined: mcpapps.ListTools`.

- [ ] **Step 3: Implement**

`server/internal/mcpapps/catalogue.go`:

```go
package mcpapps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/version"
)

// CapabilityName is the tool name exactly as Claude Code spells it in
// --allowedTools and --disallowedTools, so a decision renders without mapping.
func CapabilityName(serverName, toolName string) string {
	return "mcp__" + serverName + "__" + toolName
}

func ListTools(ctx context.Context, transport mcp.Transport) ([]schema.CatalogueTool, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "agent-dashboard", Version: version.Version}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	var out []schema.CatalogueTool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		entry := schema.CatalogueTool{Name: tool.Name, Description: tool.Description}
		if tool.Annotations != nil {
			entry.ReadOnlyHint = tool.Annotations.ReadOnlyHint
			entry.DestructiveHint = tool.Annotations.DestructiveHint
		}
		out = append(out, entry)
	}
	return out, nil
}

func StdioTransport(entry ServerEntry, env map[string]string) (mcp.Transport, error) {
	if !entry.IsStdio() {
		return nil, fmt.Errorf("the tool catalogue supports stdio servers only, this one is %q", entry.Type)
	}
	cmd := exec.Command(entry.Command, entry.Args...)
	cmd.Env = os.Environ()
	for k, v := range entry.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return &mcp.CommandTransport{Command: cmd}, nil
}

type Refresher struct {
	Apps         repo.MCPApplicationRepo
	Secrets      repo.ApplicationSecretRepo
	Capabilities repo.CapabilityRepo
	ReadServers  func() (map[string]json.RawMessage, error)
	Transport    func(entry ServerEntry, env map[string]string) (mcp.Transport, error)
	Now          func() time.Time
}

func (r Refresher) Refresh(ctx context.Context, resourceID string) ([]schema.CatalogueTool, error) {
	app, err := r.Apps.GetByResourceID(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("mcpapps.Refresh: %w", err)
	}
	tools, listErr := r.list(ctx, app)
	msg := ""
	if listErr != nil {
		msg = listErr.Error()
	}
	if err := r.Apps.RecordCatalogue(ctx, resourceID, tools, msg, r.Now()); err != nil {
		return nil, fmt.Errorf("mcpapps.Refresh: %w", err)
	}
	if listErr != nil {
		return nil, listErr
	}
	for _, tool := range tools {
		name := CapabilityName(app.ServerName, tool.Name)
		if _, err := r.Capabilities.Get(ctx, name); err == nil {
			continue
		} else if !ent.IsNotFound(err) {
			return nil, fmt.Errorf("mcpapps.Refresh: %w", err)
		}
		if _, err := r.Capabilities.Upsert(ctx, repo.UpsertCapabilityInput{
			Name:          name,
			Class:         repo.CapClassTool,
			EnforceableBy: []string{capability.EnforcerSpawn},
			Description:   tool.Description,
		}); err != nil {
			return nil, fmt.Errorf("mcpapps.Refresh: %w", err)
		}
	}
	return tools, nil
}

func (r Refresher) list(ctx context.Context, app *ent.MCPApplication) ([]schema.CatalogueTool, error) {
	servers, err := r.ReadServers()
	if err != nil {
		return nil, err
	}
	raw, ok := servers[app.ServerName]
	if !ok {
		return nil, fmt.Errorf("server %q is no longer in ~/.claude.json", app.ServerName)
	}
	entry, err := ParseEntry(raw)
	if err != nil {
		return nil, err
	}
	env, err := r.Secrets.Values(ctx, app.ResourceID)
	if err != nil {
		return nil, err
	}
	transport, err := r.Transport(entry, env)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return ListTools(ctx, transport)
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd server && go test ./internal/mcpapps/ -run 'TestListTools|TestRefresh' -count=1`
Expected: PASS. `CapabilityRepo.Get` wraps the ent error with `%w` (`capability_repo.go:88`), so `ent.IsNotFound` sees through it.

- [ ] **Step 5: Mutation — no downgrade**

Copy `catalogue.go` aside, delete the `if _, err := r.Capabilities.Get(…); err == nil { continue }` branch so every tool is upserted. Run Step 4: expected FAIL ("a refresh must not undo a stricter class"). Restore; PASS.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l ./internal/mcpapps/ && go vet ./... && cd ..
git diff --name-only origin/main -- server/go.mod server/go.sum go.work.sum   # must print nothing
git add server/internal/mcpapps/catalogue.go server/internal/mcpapps/catalogue_test.go
git commit -m "feat(mcpapps): read MCP server tool lists and expose each tool as a capability"
```

---

### Task 6: Routines attach applications, the scheduler copies them onto tasks

**Files:**
- Modify: `server/internal/db/ent/schema/task_schedule.go` (add field after `resource_id`, line 54)
- Modify: `server/internal/db/ent/schema/task.go` (add field after `routine_id`, line 49)
- Modify: `server/internal/db/repo/task_schedule_repo.go` (`CreateTaskScheduleInput` line 30, `UpdateTaskScheduleInput` line 60, their `Create`/`Update`)
- Modify: `server/internal/db/repo/task_repo.go` (`CreateTaskInput` line 36, `Create` line 141)
- Modify: `server/internal/scheduler/materializer.go` (`NewTaskSpec` line 16, spec literal line 73)
- Modify: `server/serverapp/di_scheduler.go` (line 43), `server/internal/api/tasks/handler.go` (`CreateTaskParams` line 339 and the `taskRepo.Create` call)
- Test: `server/internal/scheduler/materializer_routine_test.go` (extend)
- Regenerated: `server/internal/db/ent/`

**Interfaces:**
- Produces: `ent.TaskSchedule.Applications []string`, `ent.Task.Applications []string`; `repo.CreateTaskScheduleInput.Applications []string`; `repo.UpdateTaskScheduleInput.Applications *[]string`; `repo.CreateTaskInput.Applications []string`; `scheduler.NewTaskSpec.Applications []string`; `tasks.CreateTaskParams.Applications []string`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/scheduler/materializer_routine_test.go` (package `scheduler`, same file as `TestMaterialize_StampsRoutineID`, which already provides `mkSchedule`):

```go
func TestMaterialize_CopiesAttachedApplications(t *testing.T) {
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })

	schedRepo := repo.NewTaskScheduleRepo(bundle.Client)
	sched := mkSchedule(t, schedRepo, repo.CreateTaskScheduleInput{Applications: []string{"res-mail"}})
	if len(sched.Applications) != 1 || sched.Applications[0] != "res-mail" {
		t.Fatalf("schedule applications = %v, want [res-mail]", sched.Applications)
	}

	var got NewTaskSpec
	create := func(_ context.Context, spec NewTaskSpec) (string, error) {
		got = spec
		return "task-1", nil
	}
	m := NewMaterializer(create, nil, nil)
	if _, err := m.Materialize(context.Background(), sched, time.Now()); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(got.Applications) != 1 || got.Applications[0] != "res-mail" {
		t.Fatalf("task applications = %v, want [res-mail] — a routine's mailbox must reach the tasks it creates", got.Applications)
	}
}
```

`mkSchedule` (`scheduler_test.go:63`) sets only the required fields and passes every other field of its input through to `Create`.

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/scheduler/ -run TestMaterialize_CopiesAttachedApplications -count=1`
Expected: FAIL — unknown field `Applications`.

- [ ] **Step 3: Schema fields**

`task_schedule.go`, after the `resource_id` field:

```go
		field.JSON("applications", []string{}).
			Default([]string{}).
			Annotations(entsql.Default("[]")),
```

`task.go`, after the `routine_id` field — same three lines. Add the `entsql` import (`entgo.io/ent/dialect/entsql`) to either file if missing.

Regenerate exactly as in Task 1 Step 4.

- [ ] **Step 4: Repository inputs**

`task_schedule_repo.go`: add `Applications []string` to `CreateTaskScheduleInput` and `Applications *[]string` to `UpdateTaskScheduleInput`. In `Create`, next to `SetMetadata` (line 147):

```go
	if in.Applications != nil {
		q = q.SetApplications(in.Applications)
	}
```

In `Update`, next to its `SetMetadata` (line 220):

```go
	if in.Applications != nil {
		q = q.SetApplications(*in.Applications)
	}
```

`task_repo.go`: add `Applications []string` to `CreateTaskInput`; in `Create` after line 141:

```go
	if in.Applications != nil {
		q = q.SetApplications(in.Applications)
	}
```

- [ ] **Step 5: Copy through the scheduler — and nowhere else**

`materializer.go`: add `Applications []string` to `NewTaskSpec`; in the spec literal add `Applications: s.Applications,` below `RoutineID: s.ID,`.

`api/tasks/handler.go`: add to `CreateTaskParams`, next to `RoutineID`:

```go
	// Applications is copied from the routine by the scheduler and is not part
	// of the HTTP create body: attaching a mailbox is an authority decision, and
	// a caller able to name applications on a task it creates could hand itself
	// any mailbox.
	Applications []string
```

and pass `Applications: p.Applications,` in the `taskRepo.Create` input. Do **not** add it to the HTTP request body struct.

`di_scheduler.go` line 43: add `Applications: spec.Applications,`.

- [ ] **Step 6: Run the test to see it pass**

Run: `cd server && go test ./internal/scheduler/ ./internal/db/repo/ ./internal/api/tasks/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l ./internal/ ./serverapp/ | grep -v '/ent/' ; go vet ./... && cd ..
git checkout HEAD -- server/go.sum
git add server/internal/db/ent/ server/internal/db/repo/task_schedule_repo.go server/internal/db/repo/task_repo.go server/internal/scheduler/ server/serverapp/di_scheduler.go server/internal/api/tasks/handler.go
git commit -m "feat(scheduler): routines attach MCP applications and pass them to the tasks they create"
```

---

### Task 7: Resolve a task's applications, secrets and tool decisions

**Files:**
- Create: `server/internal/mcpapps/resolve.go`
- Test: `server/internal/mcpapps/resolve_test.go`
- Modify: `server/internal/db/repo/grant_repo.go` (add `GrantViewsFromRows`)
- Modify: `server/internal/memory/authorize.go` (use it, lines 109–123)

**Interfaces:**
- Consumes: Tasks 2, 3, 5, 6; `capability.Decide`, `memory.RoutineContext`.
- Produces:

```go
// package repo
func GrantViewsFromRows(rows []*ent.Grant) []capability.GrantView

// package mcpapps
type RunApplications struct {
	Servers        map[string]json.RawMessage
	Allow          []string
	Deny           []string
	CatalogueTools map[string]bool
}
type MissingSecretError struct{ Server, EnvName string }
type MissingServerError struct{ Server string }
type Resolver struct {
	Apps         repo.MCPApplicationRepo
	Secrets      repo.ApplicationSecretRepo
	Grants       repo.GrantRepo
	Capabilities repo.CapabilityRepo
	ReadServers  func() (map[string]json.RawMessage, error)
}
func (r Resolver) ResolveRun(ctx context.Context, task *ent.Task) (RunApplications, error)
```

- [ ] **Step 1: Extract the grant-row conversion**

In `server/internal/db/repo/grant_repo.go` add:

```go
func GrantViewsFromRows(rows []*ent.Grant) []capability.GrantView {
	views := make([]capability.GrantView, len(rows))
	for i, gr := range rows {
		views[i] = capability.GrantView{
			ID:                 gr.ID,
			Capability:         gr.CapabilityName,
			ContextKind:        gr.ContextKind,
			ContextRef:         gr.ContextRef,
			Pattern:            gr.Pattern,
			Mode:               gr.Mode,
			LimitCount:         gr.LimitCount,
			LimitWindowSeconds: gr.LimitWindowSeconds,
			ExpiresAt:          gr.ExpiresAt,
			RevokedAt:          gr.RevokedAt,
		}
	}
	return views
}
```

In `server/internal/memory/authorize.go`, replace the loop that builds `grantViews` (lines 109–123) with `grantViews := repo.GrantViewsFromRows(grantRows)`.

Run: `cd server && go test ./internal/memory/ -count=1` — expected PASS (pure refactor).

- [ ] **Step 2: Write the failing resolver tests**

```go
package mcpapps_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

type resolveFixture struct {
	ctx      context.Context
	apps     repo.MCPApplicationRepo
	secrets  repo.ApplicationSecretRepo
	grants   repo.GrantRepo
	caps     repo.CapabilityRepo
	resolver mcpapps.Resolver
}

func newResolveFixture(t *testing.T, userServers map[string]json.RawMessage) resolveFixture {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	f := resolveFixture{
		ctx:     context.Background(),
		apps:    repo.NewMCPApplicationRepo(bundle.Client),
		secrets: repo.NewApplicationSecretRepo(bundle.Client, box),
		grants:  repo.NewGrantRepo(bundle.Client),
		caps:    repo.NewCapabilityRepo(bundle.Client),
	}
	f.resolver = mcpapps.Resolver{
		Apps: f.apps, Secrets: f.secrets, Grants: f.grants, Capabilities: f.caps,
		ReadServers: func() (map[string]json.RawMessage, error) { return userServers, nil },
	}
	return f
}

func (f resolveFixture) addApp(t *testing.T, resourceID, server string, attachAll bool, tools ...string) {
	t.Helper()
	_, err := f.apps.Upsert(f.ctx, repo.UpsertMCPApplicationInput{ResourceID: resourceID, ServerName: server, AttachAll: attachAll})
	require.NoError(t, err)
	catalogue := make([]schema.CatalogueTool, 0, len(tools))
	for _, tool := range tools {
		catalogue = append(catalogue, schema.CatalogueTool{Name: tool})
		_, err := f.caps.Upsert(f.ctx, repo.UpsertCapabilityInput{
			Name: mcpapps.CapabilityName(server, tool), Class: repo.CapClassTool,
			EnforceableBy: []string{capability.EnforcerSpawn},
		})
		require.NoError(t, err)
	}
	require.NoError(t, f.apps.RecordCatalogue(f.ctx, resourceID, catalogue, "", time.Now()))
}

func (f resolveFixture) grant(t *testing.T, capName, contextKind, contextRef, mode string) {
	t.Helper()
	_, err := f.grants.Create(f.ctx, repo.CreateGrantInput{
		CapabilityName: capName,
		Context:        repo.GrantContext{Kind: contextKind, Ref: contextRef},
		Mode:           mode,
		GrantedBy:      "test",
	})
	require.NoError(t, err)
}

var twoServers = map[string]json.RawMessage{
	"mail":    json.RawMessage(`{"command":"npx","args":["mail"]}`),
	"notes":   json.RawMessage(`{"command":"npx","args":["notes"]}`),
}

func TestResolveRun_OnlyAttachedAndAttachAllServersReachTheRun(t *testing.T) {
	f := newResolveFixture(t, twoServers)
	f.addApp(t, "res-mail", "mail", false, "search")
	f.addApp(t, "res-notes", "notes", true)

	unattached, err := f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t1", Cwd: "/repo"})
	require.NoError(t, err)
	require.Contains(t, unattached.Servers, "notes")
	require.NotContains(t, unattached.Servers, "mail", "a mailbox nobody attached must not reach a coding task")

	attached, err := f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t2", Cwd: "/repo", Applications: []string{"res-mail"}})
	require.NoError(t, err)
	require.Contains(t, attached.Servers, "mail")
}

func TestResolveRun_SecretsGoOnlyIntoTheirOwnServerEntry(t *testing.T) {
	f := newResolveFixture(t, twoServers)
	f.addApp(t, "res-mail", "mail", false)
	f.addApp(t, "res-notes", "notes", true)
	require.NoError(t, f.secrets.Set(f.ctx, "res-mail", "MAIL_PASSWORD", "hunter2"))

	out, err := f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t1", Cwd: "/repo", Applications: []string{"res-mail"}})
	require.NoError(t, err)
	require.Contains(t, string(out.Servers["mail"]), "hunter2")
	require.NotContains(t, string(out.Servers["notes"]), "hunter2")
}

func TestResolveRun_MissingRequiredSecretFailsTheRun(t *testing.T) {
	f := newResolveFixture(t, twoServers)
	f.addApp(t, "res-mail", "mail", false)
	_, err := f.apps.SetRequiredEnv(f.ctx, "res-mail", []string{"MAIL_PASSWORD"})
	require.NoError(t, err)

	_, err = f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t1", Cwd: "/repo", Applications: []string{"res-mail"}})
	var missing *mcpapps.MissingSecretError
	require.True(t, errors.As(err, &missing))
	require.Equal(t, "MAIL_PASSWORD", missing.EnvName)
}

func TestResolveRun_AttachedServerGoneFromClaudeConfigFailsTheRun(t *testing.T) {
	f := newResolveFixture(t, map[string]json.RawMessage{})
	f.addApp(t, "res-mail", "mail", false)

	_, err := f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t1", Cwd: "/repo", Applications: []string{"res-mail"}})
	var missing *mcpapps.MissingServerError
	require.True(t, errors.As(err, &missing))
}

func TestResolveRun_GrantsDecideAllowDenyAndSilenceMeansAsk(t *testing.T) {
	f := newResolveFixture(t, twoServers)
	f.addApp(t, "res-mail", "mail", false, "search", "send", "draft")
	routineID := "routine-1"
	f.grant(t, "mcp__mail__search", repo.GrantContextRoutine, routineID, "allow")
	f.grant(t, "mcp__mail__send", repo.GrantContextGlobal, "", "deny")

	out, err := f.resolver.ResolveRun(f.ctx, &ent.Task{
		ID: "t1", Cwd: "/repo", RoutineID: &routineID, Applications: []string{"res-mail"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"mcp__mail__search"}, out.Allow)
	require.Equal(t, []string{"mcp__mail__send"}, out.Deny)
	require.NotContains(t, out.Allow, "mcp__mail__draft", "no grant is not an allow")
	require.True(t, out.CatalogueTools["mcp__mail__draft"])

	other, err := f.resolver.ResolveRun(f.ctx, &ent.Task{ID: "t2", Cwd: "/repo", Applications: []string{"res-mail"}})
	require.NoError(t, err)
	require.NotContains(t, other.Allow, "mcp__mail__search", "a routine's grant does not leak into another task")
}
```

Add `"time"` to the imports. `repo.CreateGrantInput` is `{CapabilityName, Context GrantContext{Kind, Ref}, Pattern, Mode, LimitCount, LimitWindowSeconds, ExpiresAt, GrantedBy, Reason}` (`grant_repo.go:50`).

- [ ] **Step 3: Run them to see them fail**

Run: `cd server && go test ./internal/mcpapps/ -run TestResolveRun -count=1`
Expected: FAIL — `undefined: mcpapps.Resolver`.

- [ ] **Step 4: Implement**

`server/internal/mcpapps/resolve.go`:

```go
package mcpapps

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

type RunApplications struct {
	Servers        map[string]json.RawMessage
	Allow          []string
	Deny           []string
	CatalogueTools map[string]bool
}

type MissingSecretError struct{ Server, EnvName string }

func (e *MissingSecretError) Error() string {
	return fmt.Sprintf("application %q is attached but its secret %s is not set", e.Server, e.EnvName)
}

type MissingServerError struct{ Server string }

func (e *MissingServerError) Error() string {
	return fmt.Sprintf("application %q is attached but no longer registered in ~/.claude.json", e.Server)
}

type Resolver struct {
	Apps         repo.MCPApplicationRepo
	Secrets      repo.ApplicationSecretRepo
	Grants       repo.GrantRepo
	Capabilities repo.CapabilityRepo
	ReadServers  func() (map[string]json.RawMessage, error)
}

// ResolveRun decides what one run gets. It calls capability.Decide directly and
// not memory.Gate.Authorize: rendering an allow list is not a use of the tool,
// and Authorize books rate-limit usage.
func (r Resolver) ResolveRun(ctx context.Context, task *ent.Task) (RunApplications, error) {
	out := RunApplications{Servers: map[string]json.RawMessage{}, CatalogueTools: map[string]bool{}}

	servers, err := r.ReadServers()
	if err != nil {
		slog.Warn("mcpapps: ~/.claude.json unreadable — run gets no MCP applications", "task", task.ID, "err", err)
		return out, nil
	}
	apps, err := r.Apps.List(ctx)
	if err != nil {
		return RunApplications{}, fmt.Errorf("mcpapps.ResolveRun: %w", err)
	}

	attached := make(map[string]bool, len(task.Applications))
	for _, id := range task.Applications {
		attached[id] = true
	}
	contexts := runContexts(task)

	for _, app := range apps {
		explicit := attached[app.ResourceID]
		if !explicit && !app.AttachAll {
			continue
		}
		raw, ok := servers[app.ServerName]
		if !ok {
			if explicit {
				return RunApplications{}, &MissingServerError{Server: app.ServerName}
			}
			continue
		}
		values, err := r.Secrets.Values(ctx, app.ResourceID)
		if err != nil {
			return RunApplications{}, fmt.Errorf("mcpapps.ResolveRun: %s: %w", app.ServerName, err)
		}
		for _, name := range app.RequiredEnv {
			if _, ok := values[name]; !ok {
				return RunApplications{}, &MissingSecretError{Server: app.ServerName, EnvName: name}
			}
		}
		merged, err := WithEnv(raw, values)
		if err != nil {
			return RunApplications{}, err
		}
		out.Servers[app.ServerName] = merged

		for _, tool := range app.Catalogue {
			name := CapabilityName(app.ServerName, tool.Name)
			out.CatalogueTools[name] = true
			decision, err := r.decide(ctx, name, contexts)
			if err != nil {
				return RunApplications{}, err
			}
			switch decision.Effect {
			case capability.EffectAllow:
				out.Allow = append(out.Allow, name)
			case capability.EffectDeny:
				out.Deny = append(out.Deny, name)
			}
		}
	}
	return out, nil
}

func (r Resolver) decide(ctx context.Context, capName string, contexts []capability.Context) (capability.Decision, error) {
	var view capability.CapabilityView
	if row, err := r.Capabilities.Get(ctx, capName); err == nil {
		view = capability.CapabilityView{Name: row.Name, Class: row.Class, EnforceableBy: row.EnforceableBy}
	}
	rows, err := r.Grants.ListForCapability(ctx, capName)
	if err != nil {
		return capability.Decision{}, fmt.Errorf("mcpapps: grants for %s: %w", capName, err)
	}
	return capability.Decide(
		capability.Request{Capability: capName, Contexts: contexts},
		repo.GrantViewsFromRows(rows),
		view,
	), nil
}

// runContexts mirrors the chain the memory push uses: task, the routine that
// created it, the project scope — which the memory gate keys by working
// directory (stage_handlers.go) — and global.
func runContexts(task *ent.Task) []capability.Context {
	out := []capability.Context{{Kind: repo.GrantContextTask, Ref: task.ID}}
	if task.RoutineID != nil {
		out = append(out, memory.RoutineContext(*task.RoutineID)...)
	}
	if task.Cwd != "" {
		out = append(out, capability.Context{Kind: repo.GrantContextProject, Ref: task.Cwd})
	}
	return append(out, capability.Context{Kind: repo.GrantContextGlobal})
}
```

`capability.EffectAllow`, `EffectDeny`, `EffectAsk` are defined at `server/internal/capability/decide.go:12-14`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `cd server && go test ./internal/mcpapps/ ./internal/memory/ -count=1`
Expected: PASS

- [ ] **Step 6: Mutations — attachment filter and secret isolation (required)**

Copy `resolve.go` aside for each:

1. Replace `if !explicit && !app.AttachAll {` with `if false {`. Run Step 5: expected FAIL in `TestResolveRun_OnlyAttachedAndAttachAllServersReachTheRun`. Restore; PASS.
2. Move `merged, err := WithEnv(raw, values)` so every server gets the values of the **first** attached application: introduce `var firstValues map[string]string` before the loop, set it once when nil, and use `WithEnv(raw, firstValues)`. Run Step 5: expected FAIL in `TestResolveRun_SecretsGoOnlyIntoTheirOwnServerEntry`. Restore; PASS.
3. Replace `case capability.EffectAllow:` with `case capability.EffectAllow, capability.EffectAsk:`. Run Step 5: expected FAIL ("no grant is not an allow"). Restore; PASS.

Paste each red output into the PR description later.

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l ./internal/mcpapps/ ./internal/memory/ ./internal/db/repo/ && go vet ./... && cd ..
git add server/internal/mcpapps/resolve.go server/internal/mcpapps/resolve_test.go server/internal/db/repo/grant_repo.go server/internal/memory/authorize.go
git commit -m "feat(mcpapps): resolve a run's MCP applications, secrets and per-tool grant decisions"
```

---

### Task 8: The spawn uses the resolution

**Files:**
- Modify: `server/internal/pipeline/types.go` (`StageContext` line 87, `OrchestratorOptions` near `IssueTaskAPIKey` line 357)
- Modify: `server/internal/pipeline/progress_guards.go` (line 170)
- Modify: `server/internal/pipeline/stage_handlers.go` (before key issuance ~line 176; spawn options line 191; `buildAllowedToolsList` line 230)
- Modify: `server/internal/pipeline/spawner.go` (`SpawnAgentOptions` line 47; `BuildAllowList` line 169; `resolvePermissionDecisions` line 189; `writeSettingsFile` line 504; spawn lines 664–690)
- Modify: `server/serverapp/di_pipeline.go` (`provideOrchestrator` line 84 and its options literal near line 166) and its call site in `server/serverapp/di.go`
- Modify: `server/internal/pipeline/export_test.go` (`ExportedWriteSettingsFile` line 11 wraps `writeSettingsFile`)
- Test: `server/internal/pipeline/spawner_apps_test.go`, `server/internal/pipeline/stage_handlers_test.go` (append)

**Interfaces:**
- Consumes: `mcpapps.RunApplications`, `mcpapps.Resolver` (Task 7).
- Produces:

```go
// StageContext and OrchestratorOptions
ResolveApplications func(ctx context.Context, task *ent.Task) (mcpapps.RunApplications, error)

// SpawnAgentOptions
Applications mcpapps.RunApplications

// spawner.go
func BuildAllowList(autonomy string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool, catalogueTools map[string]bool) []string
func spawnToolLists(opts SpawnAgentOptions, allowGitPush bool) (allow, deny []string)
```

- [ ] **Step 1: Write the failing tests**

`server/internal/pipeline/spawner_apps_test.go`:

```go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestSpawnToolLists_RendersApplicationDecisions(t *testing.T) {
	opts := SpawnAgentOptions{
		Task: &ent.Task{Autonomy: "spec_gated"},
		Applications: mcpapps.RunApplications{
			Allow:          []string{"mcp__mail__search"},
			Deny:           []string{"mcp__mail__send"},
			CatalogueTools: map[string]bool{"mcp__mail__search": true, "mcp__mail__send": true, "mcp__mail__draft": true},
		},
	}
	allow, deny := spawnToolLists(opts, false)
	require.Contains(t, allow, "mcp__mail__search")
	require.Contains(t, deny, "mcp__mail__send", "a deny grant must reach --disallowedTools")
	require.NotContains(t, allow, "mcp__mail__draft", "allow-all autonomy does not allow application tools")
	require.NotContains(t, allow, "mcp__mail__send")
}

func TestBuildAllowList_TaskPermissionForACatalogueToolSurvives(t *testing.T) {
	granted := &ent.TaskPermission{ID: "p1", Tool: "mcp__mail__search", Granted: true}
	unknown := &ent.TaskPermission{ID: "p2", Tool: "mcp__other__thing", Granted: true}
	allow := BuildAllowList("manual", []*ent.TaskPermission{granted, unknown}, false, false,
		map[string]bool{"mcp__mail__search": true})
	require.Contains(t, allow, "mcp__mail__search")
	require.NotContains(t, allow, "mcp__other__thing", "a tool outside any catalogue stays ungrantable")
}
```

`"manual"` is a valid autonomy (`taskcontrol/autonomy.go:17`) and not allow-all — `IsAllowAll` is true only for `spec_gated` and `full` (`autonomy.go:10-12`).

Append to `server/internal/pipeline/stage_handlers_test.go` (package `pipeline_test`), mirroring `TestAgentStageHandler_IssueTaskAPIKeyErrorIsNotFatal` in the same file:

```go
func TestAgentStageHandler_ApplicationResolutionFailureFailsBeforeSpawn(t *testing.T) {
	spawned := false
	spawnFn := func(pipeline.SpawnAgentOptions) (pipeline.SpawnResult, error) {
		spawned = true
		return pipeline.SpawnResult{PID: 123}, nil
	}
	handler := pipeline.NewAgentStageHandlerForTest("implementation", spawnFn)

	minted := false
	ctx := &pipeline.StageContext{
		Ctx:               context.Background(),
		Task:              &ent.Task{Title: "Reply to mail", Cwd: "/tmp/proj-apps", StageTimeoutSeconds: 1800},
		StageRun:          &ent.StageRun{Stage: "implementation", ID: "sr-apps"},
		RecordAudit:       func(string, map[string]any) {},
		RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
		IssueTaskAPIKey: func(context.Context, string, time.Duration) (string, error) {
			minted = true
			return "key", nil
		},
		ResolveApplications: func(context.Context, *ent.Task) (mcpapps.RunApplications, error) {
			return mcpapps.RunApplications{}, &mcpapps.MissingSecretError{Server: "mail", EnvName: "MAIL_PASSWORD"}
		},
	}

	tr, err := handler.Execute(ctx)
	require.NoError(t, err)
	fail, ok := tr.(pipeline.FailTransition)
	require.True(t, ok, "got %T", tr)
	require.Contains(t, fail.Reason, "MAIL_PASSWORD")
	require.False(t, spawned, "a run must never start without the mailbox it was given")
	require.False(t, minted, "no credential is minted for a run that will not start")
}
```

Add the `mcpapps` import to that file.

- [ ] **Step 2: Run them to see them fail**

Run: `cd server && go test ./internal/pipeline/ -run 'TestSpawnToolLists|TestBuildAllowList_TaskPermissionForACatalogueTool|TestAgentStageHandler_ApplicationResolution' -count=1`
Expected: FAIL — compile errors for the new fields and functions.

- [ ] **Step 3: Carry the resolver through the orchestrator**

`types.go` — add to both `StageContext` and `OrchestratorOptions`:

```go
	// ResolveApplications decides which MCP applications a run gets, with
	// which environment, and which of their tools are allowed or denied. nil
	// means the run gets no MCP applications.
	ResolveApplications func(ctx context.Context, task *ent.Task) (mcpapps.RunApplications, error)
```

`progress_guards.go` line 170: add `ResolveApplications: o.opts.ResolveApplications,`.

- [ ] **Step 4: Fail before spawn, pass the resolution on**

`stage_handlers.go`, immediately before the `taskAPIToken := ""` block:

```go
	var apps mcpapps.RunApplications
	if ctx.ResolveApplications != nil {
		resolved, err := ctx.ResolveApplications(ctx.Ctx, ctx.Task)
		if err != nil {
			return FailTransition{Reason: "MCP applications: " + err.Error()}, nil
		}
		apps = resolved
	}
```

Add `Applications: apps,` to the `SpawnAgentOptions` literal. In `buildAllowedToolsList` (line 230) pass `nil` as the new last argument: non-Claude adapters do not receive MCP applications.

- [ ] **Step 5: Render in the spawner**

`spawner.go`:
- `SpawnAgentOptions`: add `Applications mcpapps.RunApplications`.
- `BuildAllowList`: add parameter `catalogueTools map[string]bool` and pass it to `resolvePermissionDecisions(perms, allowGitPush, catalogueTools)`.
- `resolvePermissionDecisions`: add the parameter; change rule 3 to

```go
		if !permissions.IsAllowedTool(p.Tool) && !catalogueTools[p.Tool] {
			continue // rule 3: tool not on the allow-list and not in any attached application's catalogue
		}
```

- Add:

```go
func spawnToolLists(opts SpawnAgentOptions, allowGitPush bool) (allow, deny []string) {
	allow = append(BuildAllowList(opts.Task.Autonomy, opts.Permissions, opts.EnableChannel, allowGitPush, opts.Applications.CatalogueTools), taskAPIAllow(opts)...)
	allow = append(allow, opts.Applications.Allow...)
	deny = append(BuildDenyList(opts.Task.Autonomy, allowGitPush), opts.Applications.Deny...)
	return allow, deny
}
```

- Replace lines 664–665 with `spawnAllow, spawnDeny := spawnToolLists(opts, allowGitPush)`.
- `writeSettingsFile`: change its signature to `writeSettingsFile(cwd string, allow, deny []string)` and delete its two `BuildAllowList`/`BuildDenyList` lines; pass `cwd, spawnAllow, spawnDeny` at the call on line 666. This keeps the settings file and the command-line flags from one computation. Update `ExportedWriteSettingsFile` in `export_test.go` to compute the lists with `BuildAllowList(autonomy, perms, enableChannel, allowGitPush, nil)` and `BuildDenyList(autonomy, allowGitPush)` and pass them on, so its existing callers keep their signature.
- Lines 679–684: replace the `claudeconfig.UserMCPServers()` read with `userServers := opts.Applications.Servers`, and remove the now-unused `claudeconfig` import if nothing else in the file uses it.

- [ ] **Step 6: Wire the resolver**

`di_pipeline.go`: add a last parameter `appSecrets repo.ApplicationSecretRepo` to `provideOrchestrator`, and in the options literal next to `IssueTaskAPIKey`:

```go
		ResolveApplications: mcpapps.Resolver{
			Apps:         repo.NewMCPApplicationRepo(client),
			Secrets:      appSecrets,
			Grants:       grantRepo,
			Capabilities: capabilityRepo,
			ReadServers:  claudeconfig.UserMCPServers,
		}.ResolveRun,
```

In `di.go`, find the call with `grep -n "provideOrchestrator(" server/serverapp/di.go` and pass `repo.NewApplicationSecretRepo(entClient, box)` as the new last argument (`box` is declared at line 242).

- [ ] **Step 7: Run the tests to see them pass**

Run: `cd server && go build ./... && go test ./internal/pipeline/ ./serverapp/ -count=1`
Expected: PASS. Existing tests that call `BuildAllowList` or `writeSettingsFile` need the new arguments; update each call by passing `nil` for `catalogueTools` and the lists they previously computed.

- [ ] **Step 8: Mutation — deny reaches the flags (required)**

Copy `spawner.go` aside, delete `, opts.Applications.Deny...` from `spawnToolLists`. Run Step 7: expected FAIL ("a deny grant must reach --disallowedTools"). Restore; PASS.

- [ ] **Step 9: Commit**

```bash
cd server && gofmt -l ./internal/pipeline/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/pipeline/ server/serverapp/di_pipeline.go server/serverapp/di.go
git commit -m "feat(pipeline): spawn with only a run's attached MCP applications and their tool decisions"
```

---
### Task 9: Sweep orphaned temp configs at startup

**Files:**
- Modify: `server/internal/channelconfig/channelconfig.go` (next to `WriteTempConfig`, line 147)
- Test: `server/internal/channelconfig/sweep_test.go`
- Modify: `server/serverapp/di.go` (boot, next to the reconcile block from Task 4)

**Interfaces:**
- Produces:

```go
const OrphanedConfigMaxAge = 24 * time.Hour
func SweepOrphanedConfigs(now time.Time) (int, error)
func sweepOrphanedConfigsIn(dir string, maxAge time.Duration, now time.Time) (int, error)
```

The age threshold is deliberate. Several dashboard instances of one OS user share `dashboard-<uid>` (an isolated test instance next to the production one is normal on a development machine), so a restart of one must not delete a config another instance's live agent was started with. No stage runs for 24 hours.

- [ ] **Step 1: Write the failing test**

```go
package channelconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSweepOrphanedConfigs_RemovesOnlyOldMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
		require.NoError(t, os.Chtimes(p, now.Add(-age), now.Add(-age)))
		return p
	}
	oldCfg := write("dashboard-channel-mcp-old.json", 48*time.Hour)
	freshCfg := write("dashboard-channel-mcp-fresh.json", time.Minute)
	unrelated := write("something-else.json", 48*time.Hour)

	n, err := sweepOrphanedConfigsIn(dir, OrphanedConfigMaxAge, now)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoFileExists(t, oldCfg)
	require.FileExists(t, freshCfg, "another instance's live run may still need it")
	require.FileExists(t, unrelated)
}

func TestSweepOrphanedConfigs_MissingDirIsNotAnError(t *testing.T) {
	n, err := sweepOrphanedConfigsIn(filepath.Join(t.TempDir(), "absent"), OrphanedConfigMaxAge, time.Now())
	require.NoError(t, err)
	require.Zero(t, n)
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/channelconfig/ -run TestSweepOrphanedConfigs -count=1`
Expected: FAIL — `undefined: sweepOrphanedConfigsIn`.

- [ ] **Step 3: Implement**

Add to `channelconfig.go` (imports `errors`, `io/fs`, `time` as needed), and use the same directory expression `WriteTempConfig` builds on line 157:

```go
const OrphanedConfigMaxAge = 24 * time.Hour

// SweepOrphanedConfigs removes temp MCP configs a crashed or restarted
// dashboard never cleaned up. They can hold application secrets, which, unlike
// a stage run's key, do not expire.
func SweepOrphanedConfigs(now time.Time) (int, error) {
	dir := filepath.Join(os.TempDir(), "dashboard-"+strconv.Itoa(os.Getuid()))
	return sweepOrphanedConfigsIn(dir, OrphanedConfigMaxAge, now)
}

func sweepOrphanedConfigsIn(dir string, maxAge time.Duration, now time.Time) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "dashboard-channel-mcp-*.json"))
	if err != nil {
		return 0, fmt.Errorf("channelconfig: sweep: %w", err)
	}
	removed := 0
	for _, path := range matches {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("channelconfig: sweep: %w", err)
		}
		if now.Sub(info.ModTime()) < maxAge {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("channelconfig: sweep: %w", err)
		}
		removed++
	}
	return removed, nil
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `cd server && go test ./internal/channelconfig/ -count=1`
Expected: PASS

- [ ] **Step 5: Mutation — the age guard (required)**

Copy `channelconfig.go` aside, delete the `if now.Sub(info.ModTime()) < maxAge { continue }` block. Run Step 4: expected FAIL ("another instance's live run may still need it"). Restore; PASS.

- [ ] **Step 6: Call at boot**

In `server/serverapp/di.go`, next to the Task 4 block:

```go
		if n, err := channelconfig.SweepOrphanedConfigs(time.Now()); err != nil {
			slog.Warn("channelconfig: orphaned config sweep failed", "err", err)
		} else if n > 0 {
			slog.Info("channelconfig: removed orphaned MCP configs", "count", n)
		}
```

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l ./internal/channelconfig/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/channelconfig/ server/serverapp/di.go
git commit -m "feat(channelconfig): remove day-old orphaned MCP configs at startup"
```

---

### Task 10: Routines accept applications over HTTP

**Files:**
- Modify: `server/internal/api/schedules/view.go` (`scheduleBody` line 12, `scheduleView` line 37, `toView`)
- Modify: `server/internal/api/schedules/handler.go` (`Handler` and `NewHandler` lines 37–48, `create` line 93, `update` line 158)
- Modify: `server/serverapp/di_scheduler.go` (line 68)
- Test: `server/internal/api/schedules/handler_test.go` (`newServer` line 17, plus new tests)

**Interfaces:**
- Consumes: `repo.MCPApplicationRepo.GetByResourceID` (Task 2), repo inputs from Task 6.
- Produces: `schedules.NewHandler(r repo.TaskScheduleRepo, t Translator, runner Runner, apps repo.MCPApplicationRepo, bypassAuth bool) *Handler`; JSON field `applications` on schedule create, update and view.

- [ ] **Step 1: Write the failing tests**

Change `newServer` in `handler_test.go` so the test can reach the application repo, and add the tests:

```go
func newServerWithApps(t *testing.T) (*httptest.Server, repo.MCPApplicationRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	h := schedules.NewHandler(
		repo.NewTaskScheduleRepo(bundle.Client),
		scheduler.NewNLCron(nil),
		nil,
		apps,
		true,
	)
	r := chi.NewRouter()
	h.Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, apps
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newServerWithApps(t)
	return srv
}

func scheduleWith(applications []string) map[string]any {
	return map[string]any{
		"name": "inbox", "cronExpr": "*/5 * * * *", "slugPrefix": "inbox",
		"title": "Inbox", "cwd": "/tmp", "applications": applications,
	}
}

func TestCreate_AcceptsKnownApplications(t *testing.T) {
	srv, apps := newServerWithApps(t)
	_, err := apps.Upsert(context.Background(), repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	if err != nil {
		t.Fatal(err)
	}

	resp := post(t, srv.URL+"/api/schedules", scheduleWith([]string{"res-mail"}))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Applications []string `json:"applications"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Applications) != 1 || got.Applications[0] != "res-mail" {
		t.Fatalf("applications = %v", got.Applications)
	}
}

func TestCreate_RejectsUnknownApplication(t *testing.T) {
	srv, _ := newServerWithApps(t)
	resp := post(t, srv.URL+"/api/schedules", scheduleWith([]string{"res-does-not-exist"}))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — an id that names no application must not be stored", resp.StatusCode)
	}
}
```

Add `"context"` to the test imports if missing.

- [ ] **Step 2: Run them to see them fail**

Run: `cd server && go test ./internal/api/schedules/ -count=1`
Expected: FAIL — `NewHandler` takes 4 arguments.

- [ ] **Step 3: Implement**

`view.go`: add `Applications *[]string \`json:"applications"\`` to `scheduleBody`, `Applications []string \`json:"applications"\`` to `scheduleView`, and `Applications: s.Applications,` in `toView`.

`handler.go`: add field `apps repo.MCPApplicationRepo`, the new constructor parameter in the position shown under **Interfaces**, and:

```go
func (h *Handler) validateApplications(ctx context.Context, ids *[]string) error {
	if ids == nil {
		return nil
	}
	for _, id := range *ids {
		if h.apps == nil {
			return apierr.NewAppError(http.StatusBadRequest, "applications: MCP applications are not available")
		}
		if _, err := h.apps.GetByResourceID(ctx, id); err != nil {
			return apierr.NewAppError(http.StatusBadRequest, "applications: unknown application "+id)
		}
	}
	return nil
}
```

In `create`, after the `slugPrefix` validation: `if err := h.validateApplications(r.Context(), body.Applications); err != nil { return err }`, and set `in.Applications = *body.Applications` when `body.Applications != nil`.

In `update`, after the `slugPrefix` block: the same validation, then `in.Applications = body.Applications`.

`di_scheduler.go` line 68: pass `repo.NewMCPApplicationRepo(client)` as the new fourth argument.

- [ ] **Step 4: Run them to see them pass**

Run: `cd server && go build ./... && go test ./internal/api/schedules/ ./serverapp/ -count=1`
Expected: PASS

- [ ] **Step 5: Mutation — unknown ids are rejected (required)**

Copy `handler.go` aside, make `validateApplications` return `nil` immediately. Run Step 4: expected FAIL in `TestCreate_RejectsUnknownApplication`. Restore; PASS.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l ./internal/api/schedules/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/api/schedules/ server/serverapp/di_scheduler.go
git commit -m "feat(schedules): routines name the MCP applications their tasks may use"
```

---

### Task 11: Applications HTTP API

**Files:**
- Create: `server/internal/api/applications/handler.go`
- Test: `server/internal/api/applications/handler_test.go`
- Modify: `server/internal/api/router.go` (`RouterDeps` near line 179, mount near line 433)
- Modify: `server/serverapp/di.go` (construct and pass the handler, next to `resourcesHandler` line 706)

**Interfaces:**
- Consumes: Tasks 2, 4, 5.
- Produces HTTP:

| Method and path | Body | Answer |
| --- | --- | --- |
| `GET /api/applications` | — | `[]applicationView` |
| `PATCH /api/applications/{resourceId}` | `{"attachAll"?: bool, "requiredEnv"?: string[]}` | `applicationView` |
| `PUT /api/applications/{resourceId}/secrets/{envName}` | `{"value": string}` | `204` |
| `DELETE /api/applications/{resourceId}/secrets/{envName}` | — | `204` |
| `POST /api/applications/{resourceId}/refresh` | — | `applicationView`; `502` with the error when the server could not be listed |

```go
type toolView struct {
	Capability      string `json:"capability"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
}
type secretView struct {
	EnvName   string `json:"envName"`
	UpdatedAt string `json:"updatedAt"`
}
type applicationView struct {
	ResourceID           string       `json:"resourceId"`
	ServerName           string       `json:"serverName"`
	AttachAll            bool         `json:"attachAll"`
	RequiredEnv          []string     `json:"requiredEnv"`
	Secrets              []secretView `json:"secrets"`
	Tools                []toolView   `json:"tools"`
	CatalogueError       string       `json:"catalogueError,omitempty"`
	CatalogueRefreshedAt *string      `json:"catalogueRefreshedAt,omitempty"`
}
func NewHandler(apps repo.MCPApplicationRepo, secrets repo.ApplicationSecretRepo, refresher mcpapps.Refresher) *Handler
```

A secret value never appears in any response.

- [ ] **Step 1: Write the failing tests**

```go
package applications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/applications"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

func newMux(t *testing.T) (*chi.Mux, repo.MCPApplicationRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	secrets := repo.NewApplicationSecretRepo(bundle.Client, box)
	_, err = apps.Upsert(context.Background(), repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	mux := chi.NewRouter()
	applications.NewHandler(apps, secrets, mcpapps.Refresher{Now: time.Now}).Mount(mux)
	return mux, apps
}

func do(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func TestSecrets_AreWriteOnly(t *testing.T) {
	mux, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/MAIL_PASSWORD", map[string]string{"value": "hunter2"})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "hunter2", "a secret value must never leave the server")
	require.Contains(t, rec.Body.String(), `"envName":"MAIL_PASSWORD"`)
}

func TestSecrets_RejectInvalidVariableNames(t *testing.T) {
	mux, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/not-an-env-name", map[string]string{"value": "x"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPatch_SetsAttachAllAndRequiredEnv(t *testing.T) {
	mux, apps := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"attachAll": true, "requiredEnv": []string{"MAIL_PASSWORD"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	app, err := apps.GetByResourceID(context.Background(), "res-mail")
	require.NoError(t, err)
	require.True(t, app.AttachAll)
	require.Equal(t, []string{"MAIL_PASSWORD"}, app.RequiredEnv)
}

func TestUnknownApplicationIs404(t *testing.T) {
	mux, _ := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/nope", map[string]any{"attachAll": true})
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.False(t, strings.Contains(rec.Body.String(), "panic"))
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd server && go test ./internal/api/applications/ -count=1`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

`server/internal/api/applications/handler.go`:

```go
package applications

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

var envNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type Handler struct {
	apps      repo.MCPApplicationRepo
	secrets   repo.ApplicationSecretRepo
	refresher mcpapps.Refresher
}

func NewHandler(apps repo.MCPApplicationRepo, secrets repo.ApplicationSecretRepo, refresher mcpapps.Refresher) *Handler {
	return &Handler{apps: apps, secrets: secrets, refresher: refresher}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/applications", apierr.ErrorMiddleware(h.list))
	r.Patch("/api/applications/{resourceId}", apierr.ErrorMiddleware(h.patch))
	r.Put("/api/applications/{resourceId}/secrets/{envName}", apierr.ErrorMiddleware(h.putSecret))
	r.Delete("/api/applications/{resourceId}/secrets/{envName}", apierr.ErrorMiddleware(h.deleteSecret))
	r.Post("/api/applications/{resourceId}/refresh", apierr.ErrorMiddleware(h.refresh))
}

type toolView struct {
	Capability      string `json:"capability"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
}

type secretView struct {
	EnvName   string `json:"envName"`
	UpdatedAt string `json:"updatedAt"`
}

type applicationView struct {
	ResourceID           string       `json:"resourceId"`
	ServerName           string       `json:"serverName"`
	AttachAll            bool         `json:"attachAll"`
	RequiredEnv          []string     `json:"requiredEnv"`
	Secrets              []secretView `json:"secrets"`
	Tools                []toolView   `json:"tools"`
	CatalogueError       string       `json:"catalogueError,omitempty"`
	CatalogueRefreshedAt *string      `json:"catalogueRefreshedAt,omitempty"`
}

func (h *Handler) view(r *http.Request, app *ent.MCPApplication) (applicationView, error) {
	meta, err := h.secrets.List(r.Context(), app.ResourceID)
	if err != nil {
		return applicationView{}, err
	}
	v := applicationView{
		ResourceID:     app.ResourceID,
		ServerName:     app.ServerName,
		AttachAll:      app.AttachAll,
		RequiredEnv:    app.RequiredEnv,
		Secrets:        make([]secretView, 0, len(meta)),
		Tools:          make([]toolView, 0, len(app.Catalogue)),
		CatalogueError: app.CatalogueError,
	}
	if v.RequiredEnv == nil {
		v.RequiredEnv = []string{}
	}
	for _, m := range meta {
		v.Secrets = append(v.Secrets, secretView{EnvName: m.EnvName, UpdatedAt: m.UpdatedAt.UTC().Format(time.RFC3339)})
	}
	for _, t := range app.Catalogue {
		v.Tools = append(v.Tools, toolView{
			Capability:      mcpapps.CapabilityName(app.ServerName, t.Name),
			Name:            t.Name,
			Description:     t.Description,
			ReadOnlyHint:    t.ReadOnlyHint,
			DestructiveHint: t.DestructiveHint,
		})
	}
	if app.CatalogueRefreshedAt != nil {
		s := app.CatalogueRefreshedAt.UTC().Format(time.RFC3339)
		v.CatalogueRefreshedAt = &s
	}
	return v, nil
}

func (h *Handler) load(r *http.Request) (*ent.MCPApplication, error) {
	app, err := h.apps.GetByResourceID(r.Context(), chi.URLParam(r, "resourceId"))
	if ent.IsNotFound(err) {
		return nil, apierr.ErrNotFound
	}
	return app, err
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	apps, err := h.apps.List(r.Context())
	if err != nil {
		return err
	}
	out := make([]applicationView, 0, len(apps))
	for _, app := range apps {
		v, err := h.view(r, app)
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	return writeJSON(w, http.StatusOK, out)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) error {
	app, err := h.load(r)
	if err != nil {
		return err
	}
	var body struct {
		AttachAll   *bool     `json:"attachAll"`
		RequiredEnv *[]string `json:"requiredEnv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.RequiredEnv != nil {
		for _, name := range *body.RequiredEnv {
			if !envNameRE.MatchString(name) {
				return apierr.NewAppError(http.StatusBadRequest, "requiredEnv: "+name+" is not an environment variable name")
			}
		}
		if app, err = h.apps.SetRequiredEnv(r.Context(), app.ResourceID, *body.RequiredEnv); err != nil {
			return err
		}
	}
	if body.AttachAll != nil {
		if app, err = h.apps.SetAttachAll(r.Context(), app.ResourceID, *body.AttachAll); err != nil {
			return err
		}
	}
	v, err := h.view(r, app)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, v)
}

func (h *Handler) putSecret(w http.ResponseWriter, r *http.Request) error {
	app, err := h.load(r)
	if err != nil {
		return err
	}
	name := chi.URLParam(r, "envName")
	if !envNameRE.MatchString(name) {
		return apierr.NewAppError(http.StatusBadRequest, name+" is not an environment variable name")
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Value == "" {
		return apierr.NewAppError(http.StatusBadRequest, "value is required")
	}
	if err := h.secrets.Set(r.Context(), app.ResourceID, name, body.Value); err != nil {
		if err == repo.ErrSecretsUnavailable {
			return apierr.NewAppError(http.StatusServiceUnavailable, err.Error())
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) deleteSecret(w http.ResponseWriter, r *http.Request) error {
	app, err := h.load(r)
	if err != nil {
		return err
	}
	if err := h.secrets.Delete(r.Context(), app.ResourceID, chi.URLParam(r, "envName")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	app, err := h.load(r)
	if err != nil {
		return err
	}
	if _, err := h.refresher.Refresh(r.Context(), app.ResourceID); err != nil {
		return apierr.NewAppError(http.StatusBadGateway, "tool catalogue: "+err.Error())
	}
	app, err = h.apps.GetByResourceID(r.Context(), app.ResourceID)
	if err != nil {
		return err
	}
	v, err := h.view(r, app)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, v)
}
```

Use `errors.Is(err, repo.ErrSecretsUnavailable)` instead of `==` if the linter asks for it.

`router.go`: add `ApplicationsHandler *applications.Handler` to `RouterDeps` and, next to the `ResourcesHandler` mount inside the same authenticated group:

```go
		if deps.ApplicationsHandler != nil {
			deps.ApplicationsHandler.Mount(r)
		}
```

`di.go`, next to `resourcesHandler`:

```go
		applicationsHandler = apiapplications.NewHandler(
			repo.NewMCPApplicationRepo(entClient),
			repo.NewApplicationSecretRepo(entClient, box),
			mcpapps.Refresher{
				Apps:         repo.NewMCPApplicationRepo(entClient),
				Secrets:      repo.NewApplicationSecretRepo(entClient, box),
				Capabilities: repo.NewCapabilityRepo(entClient),
				ReadServers:  claudeconfig.UserMCPServers,
				Transport:    mcpapps.StdioTransport,
				Now:          time.Now,
			},
		)
```

declare `var applicationsHandler *apiapplications.Handler` next to the `resourcesHandler` declaration, and set `ApplicationsHandler: applicationsHandler` where `RouterDeps` is filled.

- [ ] **Step 4: Run them to see them pass**

Run: `cd server && go build ./... && go test ./internal/api/applications/ ./internal/api/ -count=1`
Expected: PASS

- [ ] **Step 5: Mutation — write-only (required)**

Copy `handler.go` aside, add `Value string \`json:"value"\`` to `secretView` and fill it from `h.secrets.Values`. Run Step 4: expected FAIL ("a secret value must never leave the server"). Restore; PASS.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l ./internal/api/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/api/applications/ server/internal/api/router.go server/serverapp/di.go
git commit -m "feat(api): manage MCP applications, their secrets and tool catalogue over HTTP"
```

---

### Task 12: Settings → Applications

**Files:**
- Create: `src/features/settings/composables/useApplications.ts`
- Create: `src/features/settings/components/ApplicationSettings.vue`
- Test: `src/features/settings/components/ApplicationSettings.test.ts`
- Modify: `src/features/settings/components/ApiKeySettings.vue` (`SECTIONS` line 55, the `Section` type, the lazy imports near line 38, and the panel switch)

**Interfaces:**
- Consumes: the HTTP API of Task 11.
- Produces:

```ts
export interface ApplicationTool { capability: string, name: string, description?: string, readOnlyHint: boolean, destructiveHint?: boolean }
export interface ApplicationSecret { envName: string, updatedAt: string }
export interface ApplicationView {
  resourceId: string
  serverName: string
  attachAll: boolean
  requiredEnv: string[]
  secrets: ApplicationSecret[]
  tools: ApplicationTool[]
  catalogueError?: string
  catalogueRefreshedAt?: string
}
export function useApplications(): {
  applications: Ref<ApplicationView[]>
  loading: Ref<boolean>
  error: Ref<string | null>
  fetchApplications: () => Promise<void>
  setAttachAll: (resourceId: string, attachAll: boolean) => Promise<void>
  setRequiredEnv: (resourceId: string, names: string[]) => Promise<void>
  setSecret: (resourceId: string, envName: string, value: string) => Promise<void>
  deleteSecret: (resourceId: string, envName: string) => Promise<void>
  refreshCatalogue: (resourceId: string) => Promise<void>
}
```

- [ ] **Step 1: Write the failing test**

`src/features/settings/components/ApplicationSettings.test.ts`:

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ApplicationSettings from '@/features/settings/components/ApplicationSettings.vue'

const MAIL = {
  resourceId: 'res-mail',
  serverName: 'mail',
  attachAll: false,
  requiredEnv: ['MAIL_PASSWORD'],
  secrets: [],
  tools: [
    { capability: 'mcp__mail__search', name: 'search', readOnlyHint: true },
    { capability: 'mcp__mail__send', name: 'send', readOnlyHint: false, destructiveHint: true },
  ],
}

function stubFetch(responses: Record<string, unknown>) {
  const calls: Array<{ url: string, init?: RequestInit }> = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const body = responses[`${init?.method ?? 'GET'} ${url}`]
    return { ok: true, status: body === undefined ? 204 : 200, json: async () => body }
  }))
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('applicationSettings', () => {
  it('lists applications with their tools and marks a missing required secret', async () => {
    stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('mail')
    expect(wrapper.text()).toContain('mcp__mail__search')
    expect(wrapper.find('[data-testid="secret-missing-MAIL_PASSWORD"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the empty state when no server is registered', async () => {
    stubFetch({ 'GET /api/applications': [] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()
    expect(wrapper.find('[data-testid="applications-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('sends a secret and never renders its value afterwards', async () => {
    const calls = stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="secret-input-res-mail-MAIL_PASSWORD"]').setValue('hunter2')
    await wrapper.find('[data-testid="secret-save-res-mail-MAIL_PASSWORD"]').trigger('click')
    await flushPromises()

    const put = calls.find(c => c.init?.method === 'PUT')
    expect(put?.url).toBe('/api/applications/res-mail/secrets/MAIL_PASSWORD')
    expect(JSON.parse(String(put?.init?.body))).toEqual({ value: 'hunter2' })
    expect(wrapper.html()).not.toContain('hunter2')
    wrapper.unmount()
  })
})
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm vitest run src/features/settings/components/ApplicationSettings.test.ts`
Expected: FAIL — cannot resolve `ApplicationSettings.vue`.

- [ ] **Step 3: Composable**

`src/features/settings/composables/useApplications.ts`:

```ts
import { ref } from 'vue'

export interface ApplicationTool {
  capability: string
  name: string
  description?: string
  readOnlyHint: boolean
  destructiveHint?: boolean
}

export interface ApplicationSecret {
  envName: string
  updatedAt: string
}

export interface ApplicationView {
  resourceId: string
  serverName: string
  attachAll: boolean
  requiredEnv: string[]
  secrets: ApplicationSecret[]
  tools: ApplicationTool[]
  catalogueError?: string
  catalogueRefreshedAt?: string
}

async function readError(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => null) as { error?: string } | null
  return body?.error || fallback
}

export function useApplications() {
  const applications = ref<ApplicationView[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  function replace(updated: ApplicationView) {
    applications.value = applications.value.map(a => a.resourceId === updated.resourceId ? updated : a)
  }

  async function fetchApplications(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/applications')
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      applications.value = await res.json() as ApplicationView[]
    }
    catch (e) {
      error.value = (e as Error).message || 'Failed to load applications'
    }
    finally {
      loading.value = false
    }
  }

  async function patch(resourceId: string, body: Record<string, unknown>): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to update application'))
    replace(await res.json() as ApplicationView)
  }

  const setAttachAll = (resourceId: string, attachAll: boolean) => patch(resourceId, { attachAll })
  const setRequiredEnv = (resourceId: string, names: string[]) => patch(resourceId, { requiredEnv: names })

  async function setSecret(resourceId: string, envName: string, value: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/secrets/${encodeURIComponent(envName)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to store secret'))
    await fetchApplications()
  }

  async function deleteSecret(resourceId: string, envName: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/secrets/${encodeURIComponent(envName)}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to delete secret'))
    await fetchApplications()
  }

  async function refreshCatalogue(resourceId: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/refresh`, { method: 'POST' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to read the tool list'))
    replace(await res.json() as ApplicationView)
  }

  return { applications, loading, error, fetchApplications, setAttachAll, setRequiredEnv, setSecret, deleteSecret, refreshCatalogue }
}
```

- [ ] **Step 4: Component**

`src/features/settings/components/ApplicationSettings.vue`:

```vue
<script setup lang="ts">
import type { ApplicationView } from '@/features/settings/composables/useApplications'
import { computed, onMounted, reactive, ref } from 'vue'
import { useApplications } from '@/features/settings/composables/useApplications'
import { formatDateTime } from '@/utils/format'

const { applications, loading, error, fetchApplications, setAttachAll, setSecret, deleteSecret, refreshCatalogue } = useApplications()
const actionError = ref<string | null>(null)
const drafts = reactive<Record<string, string>>({})

onMounted(() => {
  void fetchApplications()
})

type PanelState = 'loading' | 'error' | 'empty' | 'rows'
const panelState = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (error.value)
    return 'error'
  return applications.value.length ? 'rows' : 'empty'
})

function isSet(app: ApplicationView, envName: string) {
  return app.secrets.some(s => s.envName === envName)
}

function draftKey(app: ApplicationView, envName: string) {
  return `${app.resourceId}-${envName}`
}

async function run(action: () => Promise<void>) {
  actionError.value = null
  try {
    await action()
  }
  catch (e) {
    actionError.value = (e as Error).message
  }
}

async function saveSecret(app: ApplicationView, envName: string) {
  const key = draftKey(app, envName)
  const value = drafts[key]
  if (!value)
    return
  await run(() => setSecret(app.resourceId, envName, value))
  drafts[key] = ''
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Applications
      </h3>
      <p class="text-sm text-fg-mute">
        MCP servers registered with <code>claude mcp add --scope user</code>. A routine attaches the ones its tasks may use; tools run without asking only where a grant allows them.
      </p>
    </div>

    <p v-if="actionError" class="text-sm text-red-500" role="alert">
      {{ actionError }}
    </p>

    <p v-if="panelState === 'loading'" class="text-sm text-fg-mute" aria-live="polite">
      Loading applications…
    </p>
    <p v-else-if="panelState === 'error'" class="text-sm text-red-500" role="alert">
      {{ error }}
    </p>
    <p v-else-if="panelState === 'empty'" data-testid="applications-empty" class="text-sm text-fg-mute">
      No MCP servers registered. Add one with <code>claude mcp add --scope user</code> and restart the dashboard.
    </p>

    <section
      v-for="app in applications"
      v-else
      :key="app.resourceId"
      class="rounded-lg border border-line p-4 flex flex-col gap-3"
      :data-testid="`application-${app.resourceId}`"
    >
      <div class="flex items-center justify-between gap-3">
        <h4 class="font-semibold text-fg">
          {{ app.serverName }}
        </h4>
        <label class="flex items-center gap-2 text-sm text-fg-mute">
          <input
            type="checkbox"
            :checked="app.attachAll"
            @change="run(() => setAttachAll(app.resourceId, ($event.target as HTMLInputElement).checked))"
          >
          Attach to every run
        </label>
      </div>

      <div v-if="app.requiredEnv.length" class="flex flex-col gap-2">
        <h5 class="text-sm font-medium text-fg">
          Secrets
        </h5>
        <div v-for="envName in app.requiredEnv" :key="envName" class="flex items-center gap-2 text-sm">
          <code class="min-w-48">{{ envName }}</code>
          <span v-if="isSet(app, envName)" class="text-fg-mute">set</span>
          <span v-else :data-testid="`secret-missing-${envName}`" class="text-amber-600">missing — runs attaching this application will fail</span>
          <input
            v-model="drafts[draftKey(app, envName)]"
            type="password"
            autocomplete="off"
            class="border border-line rounded px-2 py-1 bg-raised"
            :aria-label="`New value for ${envName}`"
            :data-testid="`secret-input-${app.resourceId}-${envName}`"
          >
          <button
            type="button"
            class="px-2 py-1 rounded border border-line"
            :data-testid="`secret-save-${app.resourceId}-${envName}`"
            @click="saveSecret(app, envName)"
          >
            Save
          </button>
          <button
            v-if="isSet(app, envName)"
            type="button"
            class="px-2 py-1 rounded border border-line"
            @click="run(() => deleteSecret(app.resourceId, envName))"
          >
            Remove
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <h5 class="text-sm font-medium text-fg">
            Tools
          </h5>
          <button type="button" class="text-sm px-2 py-1 rounded border border-line" @click="run(() => refreshCatalogue(app.resourceId))">
            Refresh tool list
          </button>
        </div>
        <p v-if="app.catalogueError" class="text-sm text-red-500" role="alert">
          {{ app.catalogueError }}
        </p>
        <p v-if="app.catalogueRefreshedAt" class="text-xs text-fg-mute">
          Read {{ formatDateTime(app.catalogueRefreshedAt) }}. Hints come from the server and are not trusted; grants decide.
        </p>
        <p v-if="!app.tools.length" class="text-sm text-fg-mute">
          Tool list not read yet.
        </p>
        <ul v-else class="text-sm flex flex-col gap-1">
          <li v-for="tool in app.tools" :key="tool.capability" class="flex items-center gap-2">
            <code>{{ tool.capability }}</code>
            <span v-if="tool.readOnlyHint" class="text-xs text-fg-mute">read-only (per server)</span>
            <span v-if="tool.destructiveHint" class="text-xs text-amber-600">destructive (per server)</span>
          </li>
        </ul>
      </div>
    </section>
  </div>
</template>
```

`formatDateTime` is the helper `ResourceSettings.vue` already imports from `@/utils/format`.

- [ ] **Step 5: Navigation**

In `src/features/settings/components/ApiKeySettings.vue`:
- lazy import next to `ResourceSettings` (line 38): `const ApplicationSettings = defineAsyncComponent(() => import('@/features/settings/components/ApplicationSettings.vue'))`
- add `'applications'` to the `Section` type union;
- in `SECTIONS`, directly after `registry` (line 59): `{ id: 'applications', icon: '⧉', label: 'Applications' },`
- render `<ApplicationSettings v-else-if="section === 'applications'" />` next to the branch that renders `ResourceSettings`, using the same condition variable name that branch uses.

- [ ] **Step 6: Run the tests to see them pass**

Run: `pnpm vitest run src/features/settings/components/ApplicationSettings.test.ts src/features/settings/components/ApiKeySettings.test.ts`
Expected: PASS

- [ ] **Step 7: Gate and commit**

```bash
pnpm lint && pnpm typecheck && pnpm vitest run src/features/settings/
git add src/features/settings/composables/useApplications.ts src/features/settings/components/ApplicationSettings.vue src/features/settings/components/ApplicationSettings.test.ts src/features/settings/components/ApiKeySettings.vue
git commit -m "feat(settings): manage MCP applications, their secrets and tool lists"
```

---

### Task 13: Routine form attaches applications

**Files:**
- Modify: `src/composables/useSchedules.ts` (`ScheduleView` line 4, `CreateScheduleBody` line 37)
- Modify: `src/components/ScheduleForm.vue`
- Test: `src/components/__tests__/ScheduleForm.applications.test.ts`

**Interfaces:**
- Consumes: `useApplications` (Task 12), schedule API `applications` (Task 10).
- Produces: `ScheduleView.applications: string[]`, `CreateScheduleBody.applications?: string[]`.

- [ ] **Step 1: Write the failing test**

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ScheduleForm from '@/components/ScheduleForm.vue'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('scheduleForm — applications', () => {
  it('sends the applications the user ticked', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init })
      if (url === '/api/applications') {
        return { ok: true, status: 200, json: async () => [
          { resourceId: 'res-mail', serverName: 'mail', attachAll: false, requiredEnv: [], secrets: [], tools: [] },
          { resourceId: 'res-notes', serverName: 'notes', attachAll: true, requiredEnv: [], secrets: [], tools: [] },
        ] }
      }
      return { ok: true, status: 200, json: async () => ({ id: 's1', applications: ['res-mail'] }) }
    }))

    const wrapper = mount(ScheduleForm)
    await flushPromises()

    expect(wrapper.find('[data-testid="schedule-application-res-notes"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-testid="schedule-application-res-mail"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const post = calls.find(c => c.url === '/api/schedules' && c.init?.method === 'POST')
    expect(JSON.parse(String(post?.init?.body)).applications).toEqual(['res-mail'])
    wrapper.unmount()
  })
})
```

`ScheduleForm.vue` submits through `<form @submit.prevent="onSubmit">` (line 104).

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm vitest run src/components/__tests__/ScheduleForm.applications.test.ts`
Expected: FAIL — `schedule-application-res-mail` not found.

- [ ] **Step 3: Types**

`useSchedules.ts`: add `applications: string[]` to `ScheduleView` and `applications?: string[]` to `CreateScheduleBody`.

- [ ] **Step 4: Form**

In `ScheduleForm.vue` `<script setup>`:

```ts
import { onMounted } from 'vue'
import { useApplications } from '@/features/settings/composables/useApplications'

const { applications, fetchApplications } = useApplications()
const selectedApplications = ref<string[]>([...(props.schedule?.applications ?? [])])
onMounted(() => {
  void fetchApplications()
})
```

(merge `onMounted` into the existing `vue` import instead of adding a second import line), and add `applications: selectedApplications.value,` to the `body` literal in `onSubmit`.

In the template, before the submit controls:

```vue
    <fieldset v-if="applications.length" class="flex flex-col gap-1">
      <legend class="text-sm font-medium text-fg">
        Applications
      </legend>
      <p class="text-xs text-fg-mute">
        MCP servers this routine's tasks may use. Tools still run only where a grant allows them.
      </p>
      <label v-for="app in applications" :key="app.resourceId" class="flex items-center gap-2 text-sm">
        <input
          v-model="selectedApplications"
          type="checkbox"
          :value="app.resourceId"
          :disabled="app.attachAll"
          :data-testid="`schedule-application-${app.resourceId}`"
        >
        {{ app.serverName }}
        <span v-if="app.attachAll" class="text-xs text-fg-mute">attached to every run</span>
      </label>
    </fieldset>
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `pnpm vitest run src/components/`
Expected: PASS

- [ ] **Step 6: Gate and commit**

```bash
pnpm lint && pnpm typecheck
git add src/composables/useSchedules.ts src/components/ScheduleForm.vue src/components/__tests__/ScheduleForm.applications.test.ts
git commit -m "feat(schedules): pick the MCP applications a routine attaches"
```

---

### Task 14: Apply a grant preset

**Files:**
- Create: `server/internal/mcpapps/preset.go`
- Test: `server/internal/mcpapps/preset_test.go`
- Modify: `server/internal/api/applications/handler.go` (one route), `handler_test.go` (one test)

**Interfaces:**
- Consumes: the preset file from Task 0, `repo.GrantRepo` (`Create`, `ListForCapability`), the application catalogue.
- Produces:

```go
type Preset struct {
	Server          string   `json:"server"`
	AllowForRoutine []string `json:"allowForRoutine"`
	DenyGlobal      []string `json:"denyGlobal"`
}
func LoadPreset(name string) (Preset, error)  // reads the embedded presets/<name>.json
type PresetResult struct { Created, Existing, Skipped []string }
func ApplyPreset(ctx context.Context, grants repo.GrantRepo, app *ent.MCPApplication, p Preset, routineID, grantedBy string) (PresetResult, error)
```

HTTP: `POST /api/applications/{resourceId}/presets/{preset}` with `{"routineId": "…"}` → `PresetResult` as JSON (`created`, `existing`, `skipped`).

- [ ] **Step 1: Write the failing test**

```go
package mcpapps_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestApplyPreset_GrantsKnownToolsOnceAndSkipsUnknown(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	ctx := context.Background()
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)

	app, err := apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	require.NoError(t, apps.RecordCatalogue(ctx, "res-mail",
		[]schema.CatalogueTool{{Name: "search"}, {Name: "send"}}, "", time.Now()))
	app, err = apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)

	p := mcpapps.Preset{AllowForRoutine: []string{"search", "not_in_catalogue"}, DenyGlobal: []string{"send"}}

	first, err := mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"mcp__mail__search", "mcp__mail__send"}, first.Created)
	require.Equal(t, []string{"not_in_catalogue"}, first.Skipped)

	second, err := mcpapps.ApplyPreset(ctx, grants, app, p, "routine-1", "test")
	require.NoError(t, err)
	require.Empty(t, second.Created, "applying twice must not duplicate grants")
	require.ElementsMatch(t, []string{"mcp__mail__search", "mcp__mail__send"}, second.Existing)

	rows, err := grants.ListForCapability(ctx, "mcp__mail__send")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "deny", rows[0].Mode)
	require.Equal(t, repo.GrantContextGlobal, rows[0].ContextKind)
}

func TestLoadPreset_ProbeOutputIsEmbedded(t *testing.T) {
	p, err := mcpapps.LoadPreset("imap-mcp-server")
	require.NoError(t, err)
	require.NotEmpty(t, p.DenyGlobal, "the preset must deny at least the send tools")
}
```

If Task 0 chose a different server, use its preset file name in the second test.

- [ ] **Step 2: Run it to see it fail**

Run: `cd server && go test ./internal/mcpapps/ -run 'TestApplyPreset|TestLoadPreset' -count=1`
Expected: FAIL — `undefined: mcpapps.Preset`.

- [ ] **Step 3: Implement**

`server/internal/mcpapps/preset.go`:

```go
package mcpapps

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

//go:embed presets/*.json
var presetFiles embed.FS

type Preset struct {
	Server          string   `json:"server"`
	AllowForRoutine []string `json:"allowForRoutine"`
	DenyGlobal      []string `json:"denyGlobal"`
}

type PresetResult struct {
	Created  []string `json:"created"`
	Existing []string `json:"existing"`
	Skipped  []string `json:"skipped"`
}

func LoadPreset(name string) (Preset, error) {
	data, err := presetFiles.ReadFile("presets/" + name + ".json")
	if err != nil {
		return Preset{}, fmt.Errorf("mcpapps: unknown preset %q", name)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, fmt.Errorf("mcpapps: preset %q: %w", name, err)
	}
	return p, nil
}

func ApplyPreset(ctx context.Context, grants repo.GrantRepo, app *ent.MCPApplication, p Preset, routineID, grantedBy string) (PresetResult, error) {
	if routineID == "" && len(p.AllowForRoutine) > 0 {
		return PresetResult{}, fmt.Errorf("mcpapps: a routine is required to allow tools")
	}
	known := make(map[string]bool, len(app.Catalogue))
	for _, t := range app.Catalogue {
		known[t.Name] = true
	}
	res := PresetResult{Created: []string{}, Existing: []string{}, Skipped: []string{}}
	apply := func(tool, mode string, grantCtx repo.GrantContext) error {
		if !known[tool] {
			res.Skipped = append(res.Skipped, tool)
			return nil
		}
		name := CapabilityName(app.ServerName, tool)
		rows, err := grants.ListForCapability(ctx, name)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.RevokedAt == nil && row.Mode == mode && row.ContextKind == grantCtx.Kind && row.ContextRef == grantCtx.Ref {
				res.Existing = append(res.Existing, name)
				return nil
			}
		}
		if _, err := grants.Create(ctx, repo.CreateGrantInput{
			CapabilityName: name,
			Context:        grantCtx,
			Mode:           mode,
			GrantedBy:      grantedBy,
			Reason:         "preset " + p.Server,
		}); err != nil {
			return err
		}
		res.Created = append(res.Created, name)
		return nil
	}
	for _, tool := range p.AllowForRoutine {
		if err := apply(tool, "allow", repo.GrantContext{Kind: repo.GrantContextRoutine, Ref: routineID}); err != nil {
			return PresetResult{}, fmt.Errorf("mcpapps.ApplyPreset: %w", err)
		}
	}
	for _, tool := range p.DenyGlobal {
		if err := apply(tool, "deny", repo.GrantContext{Kind: repo.GrantContextGlobal}); err != nil {
			return PresetResult{}, fmt.Errorf("mcpapps.ApplyPreset: %w", err)
		}
	}
	return res, nil
}
```

Route in `server/internal/api/applications/handler.go` — add `grants repo.GrantRepo` to `Handler` and as the last `NewHandler` parameter (pass `repo.NewGrantRepo(entClient)` in `di.go` and in the test's `newMux`), mount `r.Post("/api/applications/{resourceId}/presets/{preset}", apierr.ErrorMiddleware(h.applyPreset))`, and:

```go
func (h *Handler) applyPreset(w http.ResponseWriter, r *http.Request) error {
	app, err := h.load(r)
	if err != nil {
		return err
	}
	preset, err := mcpapps.LoadPreset(chi.URLParam(r, "preset"))
	if err != nil {
		return apierr.NewAppError(http.StatusNotFound, err.Error())
	}
	var body struct {
		RoutineID string `json:"routineId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	grantedBy := "dashboard"
	if payload, ok := auth.PayloadFromContext(r.Context()); ok && payload.Sub != "" {
		grantedBy = payload.Sub
	}
	res, err := mcpapps.ApplyPreset(r.Context(), h.grants, app, preset, body.RoutineID, grantedBy)
	if err != nil {
		return apierr.NewAppError(http.StatusBadRequest, err.Error())
	}
	return writeJSON(w, http.StatusOK, res)
}
```

Add a handler test that posts to `/api/applications/res-mail/presets/imap-mcp-server` without `routineId` and expects `400`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd server && go test ./internal/mcpapps/ ./internal/api/applications/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l ./internal/mcpapps/ ./internal/api/applications/ ./serverapp/ && go vet ./... && cd ..
git add server/internal/mcpapps/preset.go server/internal/mcpapps/preset_test.go server/internal/api/applications/ server/serverapp/di.go
git commit -m "feat(mcpapps): apply a server's grant preset to a routine"
```

---

### Task 15: Documentation, full gates, acceptance on an isolated instance

**Files:**
- Modify: `CHANGELOG.md` (`### Added` under `## [Unreleased]`)
- Modify: `docs/guides/mcp.md` (new section "MCP applications")
- Modify: `docs/guides/security.md` (new section "Application secrets")

- [ ] **Step 1: CHANGELOG**

Add one entry at the top of `### Added`, in the file's existing style — what was wrong, what changed, and why:
- user-scope MCP servers are mirrored as applications;
- existing servers keep reaching every run, new ones only when a routine attaches them;
- the dashboard stores their secrets encrypted and write-only, injecting them only into the attached server's entry;
- every server tool is a capability, and allow-all autonomy does not allow them;
- a run whose attached application lacks a required secret, or whose server was removed, fails before it starts.

- [ ] **Step 2: `docs/guides/mcp.md`**

Document: `claude mcp add --scope user`; Settings → Applications (attach to every run, required secrets, refresh tool list); attaching on a routine; capability names `mcp__<server>__<tool>` and granting them with `agent-dashboard grants add`; the grant preset endpoint; that tool hints are not trusted.

- [ ] **Step 3: `docs/guides/security.md`**

Document, without softening:
- secrets are AES-256-GCM encrypted at rest with the dashboard's `secretbox` key and never returned by the API;
- at spawn they are written in clear into a per-run `0600` temp file for the attached server only, removed at run cleanup, and — if a crash prevented that — removed at the next start once a day old;
- **an agent with Bash runs as the same OS user and can read that file and its MCP server's environment.** Use app-specific passwords, revocable one by one, never account passwords;
- under `auth.mode=none` every local process can call the schedules and applications API.

- [ ] **Step 4: Full gates — paste raw output**

```bash
cd server && go vet ./... && cd ../sdk && go vet ./... && cd ..
task test
git checkout HEAD -- server/internal/db/ent/runtime/runtime.go 2>/dev/null; git status --short server/internal/db/ent/
task lint
pnpm lint && pnpm typecheck && pnpm test
git diff --name-only origin/main -- server/go.mod server/go.sum go.work.sum pnpm-lock.yaml   # must print nothing
```

- [ ] **Step 5: Acceptance on an isolated instance, never the production database**

1. Build and start: `task build:all`, then `git checkout HEAD -- server/frontend/dist/.gitkeep`; start `bin/agent-dashboard serve` with its own `DASHBOARD_PORT`, `DASHBOARD_DB_PATH` and `DASHBOARD_WORKTREE_ROOT`. Print `shasum -a 256 bin/agent-dashboard` and confirm the running PID uses that binary.
2. Register the mail server: `claude mcp add --scope user mail -- npx -y imap-mcp-server`. Restart the instance. Settings → Applications lists `mail`, **not** attached to every run.
3. Set required env to the variable names from Task 0, enter the OVH password, refresh the tool list.
4. Create a routine that attaches `mail` and apply the preset for it.
5. Run the routine now with the task prompt "Search the OVH inbox for the three most recent messages and create a draft reply to the newest one saying 'received'".
6. Verify in the OVH mailbox that the draft exists in `Drafts`.
7. Verify isolation: create a second, unattached task; in its spawn's temp config (path in the agent's process arguments) there is no `mail` entry.
8. Verify fail-before-spawn: delete the OVH secret, run the routine again; the stage run fails with a reason naming the variable, and no agent process started.

Record each result with its evidence in the PR description.

- [ ] **Step 6: Commit, push, PR**

```bash
git add CHANGELOG.md docs/guides/mcp.md docs/guides/security.md
git commit -m "docs: MCP applications, attachment and application secrets"
git push -u origin feat/mcp-applications
```

Open the PR with the raw gate output and every mutation's red output from Tasks 2, 4, 5, 7, 8, 9, 10 and 11.

---

## Deliberately not in this plan

- **Mail trigger** (spec §3.5) and **send with approval, parked wait** (§3.6–§3.7): separate plans once Task 0 has answered spec §8.
- **MCP applications for non-Claude adapters**: `buildAllowedToolsList` receives `nil` for the catalogue (Task 8).
- **http and sse servers in the tool catalogue**: `StdioTransport` refuses them with a clear error (Task 5); they are still attachable and grantable by name through task permissions.
