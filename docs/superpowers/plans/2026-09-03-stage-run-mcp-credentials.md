# Stage-Run MCP Credentials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Issue a short-lived, revocable MCP credential per stage run so the server can attribute an MCP call to the task and routine that made it, making `task:` and `routine:` capability grants effective for the MCP tools.

**Architecture:** `api_key` gains `kind`, `stage_run_id` and `expires_at`. The pipeline orchestrator mints a `stage_run` key before each spawn and hands it to the spawner, which adds a second entry to the `--mcp-config` it already writes per run. The MCP auth middleware carries the stage-run id forward on `MCPAuthInfo`; a resolver turns it into `[{task, …}, {routine, …}]` capability contexts, which the tools pass to `Gate.Authorize` through the variadic parameter PR #421 added. Revocation happens on the stage run's terminal status write, with `expires_at` as an independent second net.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite, cobra), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest)

**Spec:** `docs/superpowers/specs/2026-09-03-stage-run-mcp-credentials-design.md`

## Global Constraints

- Server MUST bind to `127.0.0.1`. Never `0.0.0.0`.
- Never run `go test ./...` or `task test` while implementing — both regenerate `server/internal/db/ent/`. Use package-scoped test paths. Task 1 regenerates ent deliberately; that is the only task where a changed `server/internal/db/ent/` belongs in the commit.
- ent regeneration MUST use the project's own path: `cd server && go generate ./internal/db/ent/` (it carries `--feature sql/upsert`). Verify after regen: `grep -rl "OnConflict" server/internal/db/ent/ | head` must print files. Then `git checkout -- server/go.sum` — `go generate` pulls codegen-only dependencies into it that `go build` does not need.
- After regen, `git checkout -- server/internal/db/ent/runtime/runtime.go` if it lost its `Version`/`Sum` constants. A local ent version differing from the committed one strips them; that diff does not belong in the commit.
- `gofmt -l <pkg>` is mandatory before every commit. CI runs `golangci-lint fmt --diff`, which fails on struct-literal alignment that `go build`, `go vet` and `go test` all pass. Adding a field to a literal is exactly when this bites.
- Run `go vet ./...` module-wide (from `server/`) before every commit — a package-scoped `go test` misses `_test.go` files in sibling packages that reference a changed exported type.
- ent auto-migrate is non-destructive and this project deliberately does not enable `WithDropColumn`. Added columns must be additive-safe with defaults.
- A stage-run token MUST never be logged, and MUST never be returned by any read endpoint or MCP tool.
- All code, comments, commit messages, PR titles and bodies in English. Conventional Commits.

---

## File Structure

| File | Responsibility |
|---|---|
| `server/internal/db/ent/schema/api_key.go` | The three new columns and their indexes |
| `server/internal/db/repo/api_key_repo.go` | Named create input, expiry filtering, revoke-by-stage-run, delete-expired, kind-filtered list |
| `server/internal/mcp/stagekey.go` | Issuer: mints and revokes `stage_run` keys. Owns the scope set and the TTL buffer |
| `server/internal/mcp/caller.go` | `CallerResolver`: stage-run id → capability contexts |
| `server/internal/mcp/auth.go` | `MCPAuthInfo.StageRunID`; unchanged refusal behaviour |
| `server/internal/channelconfig/channelconfig.go` | Optional second `mcpServers` entry for the task API |
| `server/internal/pipeline/spawner.go` | Passes the token into the config it already writes |
| `server/internal/pipeline/types.go`, `stage_handlers.go` | `IssueTaskAPIKey` seam, wired like `AuthorizeMemory` |
| `server/internal/pipeline/stage_run_service.go` | Revocation on the terminal status write |
| `server/serverapp/di_pipeline.go`, `di_mcp.go`, `di.go` | Composition root wiring, and the expiry sweep |

---

### Task 1: Storage — the three columns

**Files:**
- Modify: `server/internal/db/ent/schema/api_key.go`
- Modify: `server/internal/db/repo/api_key_repo.go`
- Modify: `server/internal/mcp/tools/keys.go:125` (the one `Create` caller)
- Regenerate: `server/internal/db/ent/` (deliberate, belongs in this commit)
- Test: `server/internal/db/repo/api_key_repo_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `repo.ApiKeyKindUser = "user"`, `repo.ApiKeyKindStageRun = "stage_run"`
  - `repo.CreateApiKeyInput{Name, Hash string; Scopes []string; Kind string; StageRunID string; ExpiresAt *time.Time}`
  - `ApiKeyRepo.Create(ctx context.Context, in CreateApiKeyInput) (*ent.ApiKey, error)`
  - `ApiKeyRepo.RevokeForStageRun(ctx context.Context, stageRunID string) (int, error)`
  - `ApiKeyRepo.DeleteExpired(ctx context.Context, before time.Time) (int, error)`

**Design note this task settles:** the expiry check lives in `GetByHash`, next to the existing `active = true` filter, not in the middleware. The spec (§4.2) says the middleware refuses an expired key — it still does, because `GetByHash` returns an error and the middleware already answers that with 401. Putting the rule in the repo means no future caller can forget it, and keeps one place deciding what "a usable key" means.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/db/repo/api_key_repo_test.go` (create the file with `package repo_test` and the imports below if absent):

```go
func TestApiKey_ExpiredKeyIsNotFoundByHash(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Minute)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "expired", Hash: "hash-expired", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)

	_, err = r.GetByHash(ctx, "hash-expired")
	require.Error(t, err, "an expired key must not resolve, the same way a revoked one does not")
}

func TestApiKey_UnexpiredStageRunKeyResolves(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	future := time.Now().Add(time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "live", Hash: "hash-live", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
	})
	require.NoError(t, err)

	got, err := r.GetByHash(ctx, "hash-live")
	require.NoError(t, err)
	require.Equal(t, "sr-1", got.StageRunID)
	require.Equal(t, repo.ApiKeyKindStageRun, got.Kind)
}

func TestApiKey_UserKeyWithoutExpiryStillResolves(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	// Every row that existed before this change looks like this one.
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "human", Hash: "hash-human", Scopes: []string{"tasks:read"},
	})
	require.NoError(t, err)

	got, err := r.GetByHash(ctx, "hash-human")
	require.NoError(t, err)
	require.Equal(t, repo.ApiKeyKindUser, got.Kind, "the default kind must be user")
	require.Nil(t, got.ExpiresAt)
}

func TestApiKey_ListShowsOnlyUserKeys(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	_, err = r.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "h1", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)
	future := time.Now().Add(time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "sr", Hash: "h2", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
	})
	require.NoError(t, err)

	keys, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1, "an ephemeral key must not appear in the human-facing list")
	require.Equal(t, "human", keys[0].Name)
}

func TestApiKey_RevokeForStageRun(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	future := time.Now().Add(time.Hour)
	for _, h := range []string{"a", "b"} {
		_, err = r.Create(ctx, repo.CreateApiKeyInput{
			Name: h, Hash: h, Scopes: []string{"memory:read"},
			Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &future,
		})
		require.NoError(t, err)
	}
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "other", Hash: "c", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-2", ExpiresAt: &future,
	})
	require.NoError(t, err)

	n, err := r.RevokeForStageRun(ctx, "sr-1")
	require.NoError(t, err)
	require.Equal(t, 2, n)

	_, err = r.GetByHash(ctx, "a")
	require.Error(t, err)
	_, err = r.GetByHash(ctx, "c")
	require.NoError(t, err, "another stage run's key must survive")
}

func TestApiKey_DeleteExpiredRemovesOnlyEphemeralRows(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	r := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{
		Name: "old", Hash: "old", Scopes: []string{"memory:read"},
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = r.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "human", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)

	n, err := r.DeleteExpired(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	keys, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, keys, 1, "a user key must never be swept, whatever its expiry")
}
```

Imports for the file:

```go
import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/db/repo/ -run 'TestApiKey_' -v`
Expected: compile failure — `repo.CreateApiKeyInput` and `repo.ApiKeyKindUser` are undefined.

- [ ] **Step 3: Add the schema fields**

In `server/internal/db/ent/schema/api_key.go`, replace the `Fields` and `Indexes` methods:

```go
// Fields of the ApiKey.
func (ApiKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id"),
		field.String("name"),
		field.String("key_hash").Unique().Sensitive(),
		field.JSON("scopes", []string{}).Default([]string{}),
		field.Bool("active").Default(true),
		// kind separates the keys a person created from the ephemeral ones the
		// pipeline mints per stage run. The default keeps every existing row a
		// user key without a backfill.
		field.String("kind").Default("user"),
		// stage_run_id is the attribution a capability context is resolved from.
		// Empty for a user key.
		field.String("stage_run_id").Default(""),
		// expires_at is a hard stop independent of active: the orchestrator
		// revoking on a terminal transition and this timestamp are two nets, and
		// a server that dies between spawn and transition only trips the second.
		field.Time("expires_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

// Indexes of the ApiKey.
func (ApiKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("active"),
		index.Fields("stage_run_id"),
		index.Fields("expires_at"),
	}
}
```

- [ ] **Step 4: Regenerate ent**

```bash
cd server && go generate ./internal/db/ent/
grep -rl "OnConflict" internal/db/ent/ | head
git checkout -- go.sum
```

Expected: the `grep` prints files. If `internal/db/ent/runtime/runtime.go` lost its `Version`/`Sum` constants, run `git checkout -- internal/db/ent/runtime/runtime.go`.

- [ ] **Step 5: Rework the repo**

In `server/internal/db/repo/api_key_repo.go`, add above the interface:

```go
// API key kinds. A user key is one a person created and manages; a stage_run
// key is minted by the pipeline for one agent process and swept when it expires.
const (
	ApiKeyKindUser     = "user"
	ApiKeyKindStageRun = "stage_run"
)

// CreateApiKeyInput is the named input for Create. Named rather than
// positional because the call now has more than four parameters, which is
// where this codebase's convention switches. A zero Kind means ApiKeyKindUser.
type CreateApiKeyInput struct {
	Name       string
	Hash       string
	Scopes     []string
	Kind       string
	StageRunID string
	ExpiresAt  *time.Time
}
```

Change the interface's first three lines to:

```go
	Create(ctx context.Context, in CreateApiKeyInput) (*ent.ApiKey, error)
	GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error)
	GetByID(ctx context.Context, id string) (*ent.ApiKey, error)
```

and add to it:

```go
	// RevokeForStageRun deactivates every key issued for stageRunID and
	// returns how many rows it touched.
	RevokeForStageRun(ctx context.Context, stageRunID string) (int, error)
	// DeleteExpired hard-deletes stage_run keys whose expires_at is before
	// the given instant. User keys are never deleted here: they are soft-
	// deleted through Delete so their hash stays available for audit.
	DeleteExpired(ctx context.Context, before time.Time) (int, error)
```

Replace `Create`, `GetByHash` and `List`:

```go
func (r *entApiKeyRepo) Create(ctx context.Context, in CreateApiKeyInput) (*ent.ApiKey, error) {
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	kind := in.Kind
	if kind == "" {
		kind = ApiKeyKindUser
	}
	q := r.client.ApiKey.Create().
		SetID(uuid.New().String()).
		SetName(in.Name).
		SetKeyHash(in.Hash).
		SetScopes(scopes).
		SetKind(kind).
		SetStageRunID(in.StageRunID).
		SetNillableExpiresAt(in.ExpiresAt)
	k, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.Create: %w", err)
	}
	return k, nil
}

// GetByHash resolves a usable key: active, and either without an expiry or
// not yet expired. The expiry rule lives here rather than at the call site so
// no future caller can forget it — "usable" is decided in one place.
func (r *entApiKeyRepo) GetByHash(ctx context.Context, hash string) (*ent.ApiKey, error) {
	k, err := r.client.ApiKey.Query().
		Where(
			apikey.KeyHash(hash),
			apikey.Active(true),
			apikey.Or(
				apikey.ExpiresAtIsNil(),
				apikey.ExpiresAtGT(time.Now()),
			),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.GetByHash: %w", err)
	}
	return k, nil
}

// List returns the keys a person manages. Ephemeral stage_run keys are
// excluded: one row per stage run per retry would turn this list into a log.
func (r *entApiKeyRepo) List(ctx context.Context) ([]*ent.ApiKey, error) {
	keys, err := r.client.ApiKey.Query().
		Where(apikey.Active(true), apikey.KindEQ(ApiKeyKindUser)).
		Order(apikey.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("apikey.List: %w", err)
	}
	return keys, nil
}

func (r *entApiKeyRepo) RevokeForStageRun(ctx context.Context, stageRunID string) (int, error) {
	n, err := r.client.ApiKey.Update().
		Where(apikey.StageRunIDEQ(stageRunID), apikey.Active(true)).
		SetActive(false).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("apikey.RevokeForStageRun: %w", err)
	}
	return n, nil
}

func (r *entApiKeyRepo) DeleteExpired(ctx context.Context, before time.Time) (int, error) {
	n, err := r.client.ApiKey.Delete().
		Where(
			apikey.KindEQ(ApiKeyKindStageRun),
			apikey.ExpiresAtNotNil(),
			apikey.ExpiresAtLT(before),
		).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("apikey.DeleteExpired: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 6: Update the one existing caller**

In `server/internal/mcp/tools/keys.go:125`, replace:

```go
			key, err := d.ApiKeyRepo.Create(ctx, name, hash, scopes)
```

with:

```go
			key, err := d.ApiKeyRepo.Create(ctx, repo.CreateApiKeyInput{Name: name, Hash: hash, Scopes: scopes})
```

Add `"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"` to that file's imports if it is not already there.

- [ ] **Step 7: Run the tests and the module-wide vet**

```bash
cd server && go test ./internal/db/repo/ -run 'TestApiKey_' -v && go vet ./... && gofmt -l internal
```

Expected: all six tests PASS, `go vet` silent, `gofmt -l` prints nothing under the files you touched.

- [ ] **Step 8: Commit**

```bash
git add server/internal/db/ent server/internal/db/repo/api_key_repo.go server/internal/db/repo/api_key_repo_test.go server/internal/mcp/tools/keys.go
git commit -m "feat(api-key): add kind, stage-run attribution and expiry"
```

---

### Task 2: The issuer

**Files:**
- Create: `server/internal/mcp/stagekey.go`
- Test: `server/internal/mcp/stagekey_test.go`

**Interfaces:**
- Consumes: `repo.CreateApiKeyInput`, `repo.ApiKeyKindStageRun`, `ApiKeyRepo.Create`, `ApiKeyRepo.RevokeForStageRun` (Task 1).
- Produces:
  - `mcp.StageRunScopes []string`
  - `mcp.StageKeyTTLBuffer time.Duration`
  - `mcp.StageKeyIssuer{Keys repo.ApiKeyRepo}`
  - `(StageKeyIssuer) Issue(ctx context.Context, stageRunID string, stageTimeout time.Duration) (token string, err error)`
  - `(StageKeyIssuer) Revoke(ctx context.Context, stageRunID string) error`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/mcp/stagekey_test.go`:

```go
package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func newIssuer(t *testing.T) (mcp.StageKeyIssuer, repo.ApiKeyRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	keys := repo.NewApiKeyRepo(bundle.Client)
	return mcp.StageKeyIssuer{Keys: keys}, keys, context.Background()
}

func TestStageKeyIssuer_IssuedKeyResolvesAndCarriesAttribution(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", 30*time.Minute)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(token, "mcp_"), "the token must be an ordinary MCP bearer")

	row, err := keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)
	require.Equal(t, "sr-1", row.StageRunID)
	require.Equal(t, repo.ApiKeyKindStageRun, row.Kind)
	require.NotNil(t, row.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(30*time.Minute+mcp.StageKeyTTLBuffer), *row.ExpiresAt, time.Minute)
}

// The scope set is fixed by design (spec D3). keys:manage is excluded on
// purpose: an agent that can mint keys can mint one with no stage run and
// escape its own attribution.
func TestStageKeyIssuer_NeverGrantsKeysManage(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	row, err := keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)

	require.NotContains(t, row.Scopes, "keys:manage")
	require.Contains(t, row.Scopes, "memory:read")
	require.Contains(t, row.Scopes, "obsidian:write")
}

func TestStageKeyIssuer_RevokeStopsTheKey(t *testing.T) {
	issuer, keys, ctx := newIssuer(t)

	token, err := issuer.Issue(ctx, "sr-1", time.Hour)
	require.NoError(t, err)
	_, err = keys.GetByHash(ctx, mcp.HashToken(token))
	require.NoError(t, err)

	require.NoError(t, issuer.Revoke(ctx, "sr-1"))

	_, err = keys.GetByHash(ctx, mcp.HashToken(token))
	require.Error(t, err)
}

// Revoking a stage run that never had a key is not an error: the orchestrator
// calls Revoke on every terminal transition, including runs whose spawn never
// got a credential.
func TestStageKeyIssuer_RevokeUnknownStageRunIsNotAnError(t *testing.T) {
	issuer, _, ctx := newIssuer(t)
	require.NoError(t, issuer.Revoke(ctx, "sr-never-existed"))
}

func TestStageKeyIssuer_TwoIssuesGiveDifferentTokens(t *testing.T) {
	issuer, _, ctx := newIssuer(t)

	a, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	b, err := issuer.Issue(ctx, "sr-1", time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "a retry must not reuse the previous run's token")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/mcp/ -run 'TestStageKeyIssuer_' -v`
Expected: compile failure — `mcp.StageKeyIssuer` is undefined.

- [ ] **Step 3: Write the issuer**

Create `server/internal/mcp/stagekey.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageRunScopes is the fixed transport scope set every stage-run key gets.
//
// It deliberately omits keys:manage — an agent able to mint keys could mint
// one with no stage run and escape its own attribution. Everything else is
// granted because narrowing is the capability gate's job, per capability and
// per value; a second narrowing through scopes would be two places holding
// one decision, and they would drift.
var StageRunScopes = []string{
	"tasks:read", "tasks:write", "pipeline:control", "agent:coord",
	"memory:read", "memory:write", "obsidian:read", "obsidian:write",
}

// StageKeyTTLBuffer is added to a stage's timeout when setting expires_at. It
// covers the window between an agent hitting its timeout and the orchestrator
// recording the transition, so the key does not die under a run that is still
// being wound down.
const StageKeyTTLBuffer = 5 * time.Minute

// StageKeyIssuer mints and revokes the ephemeral MCP credentials a pipeline
// stage run presents to /api/mcp. It is the only writer of
// repo.ApiKeyKindStageRun rows.
type StageKeyIssuer struct {
	Keys repo.ApiKeyRepo
}

// Issue mints a bearer token for stageRunID and returns it. The token is
// returned once and never stored in clear — only its hash reaches the row.
func (i StageKeyIssuer) Issue(ctx context.Context, stageRunID string, stageTimeout time.Duration) (string, error) {
	if i.Keys == nil {
		return "", fmt.Errorf("mcp: StageKeyIssuer has no key repo")
	}
	if stageRunID == "" {
		return "", fmt.Errorf("mcp: refusing to issue a key with no stage run — it would be unattributable and unrevocable")
	}
	token := GenerateAPIToken()
	expires := time.Now().Add(stageTimeout + StageKeyTTLBuffer)
	if _, err := i.Keys.Create(ctx, repo.CreateApiKeyInput{
		Name:       "stage-run " + stageRunID,
		Hash:       HashToken(token),
		Scopes:     StageRunScopes,
		Kind:       repo.ApiKeyKindStageRun,
		StageRunID: stageRunID,
		ExpiresAt:  &expires,
	}); err != nil {
		return "", fmt.Errorf("mcp: issue stage-run key: %w", err)
	}
	return token, nil
}

// Revoke deactivates every key issued for stageRunID. A stage run that never
// received a key is not an error — the orchestrator calls this on every
// terminal transition, including spawns that ran without a credential.
func (i StageKeyIssuer) Revoke(ctx context.Context, stageRunID string) error {
	if i.Keys == nil || stageRunID == "" {
		return nil
	}
	if _, err := i.Keys.RevokeForStageRun(ctx, stageRunID); err != nil {
		return fmt.Errorf("mcp: revoke stage-run keys: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `cd server && go test ./internal/mcp/ -run 'TestStageKeyIssuer_' -v`
Expected: all five PASS.

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l internal/mcp && go vet ./...
git add server/internal/mcp/stagekey.go server/internal/mcp/stagekey_test.go
git commit -m "feat(mcp): mint and revoke per-stage-run credentials"
```

---

### Task 3: Auth carries the attribution, and it becomes contexts

**Files:**
- Modify: `server/internal/mcp/auth.go` (the `MCPAuthInfo` struct at `:99-104`, and `McpAuthMiddleware` at `:145`)
- Create: `server/internal/mcp/caller.go`
- Test: `server/internal/mcp/caller_test.go`

**Interfaces:**
- Consumes: `MCPAuthInfo` (existing), `AuthFromContext` (existing), `capability.Context`.
- Produces:
  - `MCPAuthInfo.StageRunID string`
  - `mcp.StageRunLookup` interface: `GetByID(ctx context.Context, id string) (*ent.StageRun, error)`
  - `mcp.TaskLookup` interface: `GetByID(ctx context.Context, id string) (*ent.Task, error)`
  - `mcp.CallerResolver{StageRuns StageRunLookup; Tasks TaskLookup}`
  - `(CallerResolver) Contexts(ctx context.Context) []capability.Context`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/mcp/caller_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

// seedRun creates a task (optionally bound to a routine) plus one stage run on
// it, and returns the stage run's id.
func seedRun(t *testing.T, client *db.Bundle, slug, routineID string) (string, string) {
	t.Helper()
	ctx := context.Background()
	tasks := repo.NewTaskRepo(client.Client)
	runs := repo.NewStageRunRepo(client.Client)

	in := repo.CreateTaskInput{
		Slug: slug, Title: slug, Cwd: "/tmp",
		CurrentStage: "implementation", Priority: "medium",
		MaxIterations: 5, StageTimeoutSeconds: 60,
	}
	if routineID != "" {
		in.RoutineID = &routineID
	}
	task, err := tasks.Create(ctx, in)
	require.NoError(t, err)

	run, err := runs.Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: "implementation"})
	require.NoError(t, err)
	return run.ID, task.ID
}

func newResolver(t *testing.T) (mcp.CallerResolver, *db.Bundle) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return mcp.CallerResolver{
		StageRuns: repo.NewStageRunRepo(bundle.Client),
		Tasks:     repo.NewTaskRepo(bundle.Client),
	}, bundle
}

func TestCallerResolver_NoAuthYieldsNoContexts(t *testing.T) {
	resolver, _ := newResolver(t)
	require.Empty(t, resolver.Contexts(context.Background()))
}

// A machine-wide key must behave exactly as it did before this existed.
func TestCallerResolver_UserKeyYieldsNoContexts(t *testing.T) {
	resolver, _ := newResolver(t)
	ctx := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "k1"})
	require.Empty(t, resolver.Contexts(ctx))
}

func TestCallerResolver_StageRunKeyYieldsTaskAndRoutine(t *testing.T) {
	resolver, bundle := newResolver(t)
	runID, taskID := seedRun(t, bundle, "from-routine", "sched-1")

	ctx := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "k1", StageRunID: runID})
	got := resolver.Contexts(ctx)

	require.Equal(t, []capability.Context{
		{Kind: "task", Ref: taskID},
		{Kind: "routine", Ref: "sched-1"},
	}, got)
}

// A hand-created task has no routine. A routine context with an empty ref
// would match every grant stored with an empty ContextRef, so it must be
// omitted rather than emitted blank — the same rule memory.RoutineContext applies.
func TestCallerResolver_TaskWithoutRoutineOmitsTheRoutineContext(t *testing.T) {
	resolver, bundle := newResolver(t)
	runID, taskID := seedRun(t, bundle, "hand-made", "")

	ctx := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "k1", StageRunID: runID})
	got := resolver.Contexts(ctx)

	require.Equal(t, []capability.Context{{Kind: "task", Ref: taskID}}, got)
}

// A key naming a stage run that no longer exists resolves to nothing rather
// than to a partial chain: failing open to "no context" is the same outcome as
// a user key, which is the safe direction.
func TestCallerResolver_UnknownStageRunYieldsNoContexts(t *testing.T) {
	resolver, _ := newResolver(t)
	ctx := mcp.ContextWithAuth(context.Background(), &mcp.MCPAuthInfo{KeyID: "k1", StageRunID: "gone"})
	require.Empty(t, resolver.Contexts(ctx))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/mcp/ -run 'TestCallerResolver_' -v`
Expected: compile failure — `mcp.CallerResolver` and `MCPAuthInfo.StageRunID` are undefined.

- [ ] **Step 3: Extend `MCPAuthInfo` and the middleware**

In `server/internal/mcp/auth.go`, replace the struct:

```go
// MCPAuthInfo carries resolved auth info attached to the request context.
type MCPAuthInfo struct {
	KeyID  string
	Scopes map[string]bool
	// StageRunID names the pipeline stage run this key was issued for, empty
	// for a key a person created. CallerResolver turns it into the capability
	// contexts the request resolves against.
	StageRunID string
}
```

and, inside `McpAuthMiddleware`, the `info` literal:

```go
			info := &MCPAuthInfo{
				KeyID:      key.ID,
				Scopes:     ResolveScopes(key.Scopes),
				StageRunID: key.StageRunID,
			}
```

No expiry check is added here: `GetByHash` (Task 1) already refuses an expired key, and this middleware already answers its error with 401.

- [ ] **Step 4: Write the resolver**

Create `server/internal/mcp/caller.go`:

```go
package mcp

import (
	"context"
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageRunLookup and TaskLookup are the two reads CallerResolver needs. They
// are declared here, narrow, rather than taking the full repo interfaces: the
// resolver depends on the methods it calls and nothing else.
type StageRunLookup interface {
	GetByID(ctx context.Context, id string) (*ent.StageRun, error)
}

type TaskLookup interface {
	GetByID(ctx context.Context, id string) (*ent.Task, error)
}

// CallerResolver turns the stage run on a request's MCP credential into the
// capability contexts that request resolves against.
//
// A user key resolves to nothing, which is why an unattributed call behaves
// exactly as it did before this existed — capability.Decide drops every grant
// whose context the request does not name, so "no contexts" means the scope
// chain alone decides, as it always did.
type CallerResolver struct {
	StageRuns StageRunLookup
	Tasks     TaskLookup
}

// Contexts resolves the caller's chain: the task the stage run belongs to, and
// the routine that materialized that task when there is one.
//
// Every failure resolves to no contexts rather than a partial chain: the safe
// direction is the one a machine-wide key already takes.
func (r CallerResolver) Contexts(ctx context.Context) []capability.Context {
	info := AuthFromContext(ctx)
	if info == nil || info.StageRunID == "" || r.StageRuns == nil || r.Tasks == nil {
		return nil
	}
	run, err := r.StageRuns.GetByID(ctx, info.StageRunID)
	if err != nil {
		slog.Debug("mcp: stage run for credential not found", "stageRun", info.StageRunID, "err", err)
		return nil
	}
	task, err := r.Tasks.GetByID(ctx, run.TaskID)
	if err != nil {
		slog.Debug("mcp: task for stage run not found", "stageRun", run.ID, "task", run.TaskID, "err", err)
		return nil
	}
	out := []capability.Context{{Kind: repo.GrantContextTask, Ref: task.ID}}
	if task.RoutineID != nil && *task.RoutineID != "" {
		out = append(out, capability.Context{Kind: repo.GrantContextRoutine, Ref: *task.RoutineID})
	}
	return out
}
```

- [ ] **Step 5: Run the tests**

Run: `cd server && go test ./internal/mcp/ -run 'TestCallerResolver_' -v`
Expected: all five PASS.

- [ ] **Step 6: Prove the tests are coupled to the change**

Temporarily make `Contexts` return `nil` as its first statement, re-run, and confirm `TestCallerResolver_StageRunKeyYieldsTaskAndRoutine` and `TestCallerResolver_TaskWithoutRoutineOmitsTheRoutineContext` FAIL. Restore the body and confirm they pass again. Paste both outputs. A test that passes either way proves nothing.

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l internal/mcp && go vet ./...
git add server/internal/mcp/auth.go server/internal/mcp/caller.go server/internal/mcp/caller_test.go
git commit -m "feat(mcp): resolve caller contexts from a stage-run credential"
```

---

### Task 4: The tools pass the contexts to the gate

**Files:**
- Modify: `server/internal/mcp/tools/obsidian.go` (all four `d.Gate.Authorize` calls)
- Modify: `server/internal/mcp/tools/memory.go` (every `Gate.Authorize` call)
- Modify: `server/serverapp/di_mcp.go` (wire the resolver into both deps structs)
- Test: `server/internal/mcp/tools/obsidian_test.go`

**Interfaces:**
- Consumes: `mcp.CallerResolver` (Task 3), `memory.Gate.Authorize(ctx, cap, value string, scope repo.Scope, extra ...capability.Context)` (existing, from PR #421).
- Produces: `ObsidianDeps.Caller mcp.CallerResolver` and `MemoryDeps.Caller mcp.CallerResolver`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/mcp/tools/obsidian_test.go` (follow the file's existing fixture helpers; the assertions that matter are these):

```go
// TestObsidianWrite_RoutineGrantAppliesOnlyToThatRoutine is the whole point of
// this change, in one test: one grant, two callers, one difference.
func TestObsidianWrite_RoutineGrantAppliesOnlyToThatRoutine(t *testing.T) {
	env := newObsidianToolEnv(t) // seeds capabilities, a vault stub, and the registry
	ctx := context.Background()

	_, err := env.Grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: obsidian.CapabilityWrite,
		Context:        repo.GrantContextFor(repo.GrantContextRoutine, "sched-1"),
		Pattern:        "*",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)

	runFromRoutine, _ := env.SeedRun(t, "from-routine", "sched-1")
	runFromHuman, _ := env.SeedRun(t, "hand-made", "")

	// The routine's own agent may write.
	res, err := env.Call(mcp.ContextWithAuth(ctx, &mcp.MCPAuthInfo{StageRunID: runFromRoutine}),
		"obsidian_write", map[string]any{"path": "notes/a.md", "content": "x"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// A task the same routine did not start may not.
	_, err = env.Call(mcp.ContextWithAuth(ctx, &mcp.MCPAuthInfo{StageRunID: runFromHuman}),
		"obsidian_write", map[string]any{"path": "notes/a.md", "content": "x"})
	require.Error(t, err)

	// Neither may the machine-wide key.
	_, err = env.Call(mcp.ContextWithAuth(ctx, &mcp.MCPAuthInfo{KeyID: "user-key"}),
		"obsidian_write", map[string]any{"path": "notes/a.md", "content": "x"})
	require.Error(t, err)
}
```

If `newObsidianToolEnv`, `env.SeedRun` and `env.Call` do not exist in that file, add them alongside the existing fixtures: `SeedRun` is the `seedRun` helper from Task 3's test, and `Call` looks the tool up in the registry and invokes its handler with the given context and args.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/mcp/tools/ -run 'TestObsidianWrite_RoutineGrant' -v`
Expected: FAIL — the first call is denied, because nothing passes the caller's contexts to the gate yet.

- [ ] **Step 3: Thread the resolver through the tools**

In `server/internal/mcp/tools/obsidian.go`, add to `ObsidianDeps`:

```go
	// Caller resolves the stage run on the request's credential into the task
	// and routine capability contexts the grant chain is ranked against. The
	// zero value resolves to nothing, which is exactly how a machine-wide key
	// behaves.
	Caller mcp.CallerResolver
```

and change every one of the four `d.Gate.Authorize(...)` calls from

```go
			if err := d.Gate.Authorize(ctx, obsidian.CapabilityRead, notePath, obsidianScope()); err != nil {
```

to

```go
			if err := d.Gate.Authorize(ctx, obsidian.CapabilityRead, notePath, obsidianScope(), d.Caller.Contexts(ctx)...); err != nil {
```

(substituting the capability constant and value each call already uses). Do the same for every `Gate.Authorize` call in `server/internal/mcp/tools/memory.go`, adding the identical `Caller` field to `MemoryDeps`.

- [ ] **Step 4: Wire it at the composition root**

In `server/serverapp/di_mcp.go`, build the resolver once next to the repos already constructed there (`taskRepo` and `srRepo` exist at `:38-39`):

```go
	caller := mcp.CallerResolver{StageRuns: srRepo, Tasks: taskRepo}
```

and add `Caller: caller,` to both the `mcptools.MemoryDeps` literal (`:120`) and the `mcptools.ObsidianDeps` literal (`:137`).

- [ ] **Step 5: Run the test**

Run: `cd server && go test ./internal/mcp/tools/ -v`
Expected: the new test PASSES and every existing test in the package still passes — a tool called without auth must behave exactly as before.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l internal/mcp serverapp && go vet ./...
git add server/internal/mcp/tools server/serverapp/di_mcp.go
git commit -m "feat(mcp): rank tool calls against the caller's task and routine"
```

---

### Task 5: The config carries the credential

**Files:**
- Modify: `server/internal/channelconfig/channelconfig.go`
- Test: `server/internal/channelconfig/channelconfig_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `channelconfig.TaskAPI{URL, Token string}`
  - `channelconfig.ConfigJSON(binaryPath string, taskAPI *TaskAPI) (string, error)`
  - `channelconfig.WriteTempConfig(binaryPath string, taskAPI *TaskAPI) (path string, err error)`

Both existing functions gain the second parameter; `nil` reproduces today's output byte for byte.

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/channelconfig/channelconfig_test.go`:

```go
func TestConfigJSON_NilTaskAPIIsUnchanged(t *testing.T) {
	got, err := channelconfig.ConfigJSON("/bin/agent-dashboard", nil)
	require.NoError(t, err)
	require.Equal(t,
		`{"mcpServers":{"dashboard-channel":{"command":"/bin/agent-dashboard","args":["channel"]}}}`,
		got)
}

func TestConfigJSON_TaskAPIAddsASecondServer(t *testing.T) {
	got, err := channelconfig.ConfigJSON("/bin/agent-dashboard", &channelconfig.TaskAPI{
		URL: "http://127.0.0.1:13120/api/mcp", Token: "mcp_deadbeef",
	})
	require.NoError(t, err)

	var parsed struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &parsed))
	require.Len(t, parsed.MCPServers, 2)

	tasks := parsed.MCPServers["dashboard-tasks"]
	require.Equal(t, "http", tasks.Type)
	require.Equal(t, "http://127.0.0.1:13120/api/mcp", tasks.URL)
	require.Equal(t, "Bearer mcp_deadbeef", tasks.Headers["Authorization"])
	require.Equal(t, "/bin/agent-dashboard", parsed.MCPServers["dashboard-channel"].Command)
}

// A half-filled TaskAPI would write a server entry the agent cannot use and,
// worse, one that looks configured. Refuse it instead.
func TestConfigJSON_TaskAPIWithoutTokenIsRefused(t *testing.T) {
	_, err := channelconfig.ConfigJSON("/bin/agent-dashboard", &channelconfig.TaskAPI{URL: "http://127.0.0.1:13120/api/mcp"})
	require.Error(t, err)
}

func TestWriteTempConfig_FileIsNotWorldReadable(t *testing.T) {
	path, err := channelconfig.WriteTempConfig("/bin/agent-dashboard", &channelconfig.TaskAPI{
		URL: "http://127.0.0.1:13120/api/mcp", Token: "mcp_deadbeef",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o077, "a file holding a bearer token must not be group- or world-readable")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/channelconfig/ -v`
Expected: compile failure — `ConfigJSON` takes one argument.

- [ ] **Step 3: Extend the config builder**

In `server/internal/channelconfig/channelconfig.go`, replace the entry type, `buildConfig`, `ConfigJSON` and `WriteTempConfig`:

```go
// mcpServerEntry mirrors the claude CLI's mcpServers JSON shape. A stdio
// server sets Command/Args; an HTTP server sets Type/URL/Headers. omitempty on
// every field keeps the stdio entry byte-identical to what this package wrote
// before the HTTP form existed.
type mcpServerEntry struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// TaskAPI describes the dashboard's own MCP endpoint and the credential the
// spawned agent presents to it. Both fields are required together: an entry
// with a URL and no token would look configured and fail every call 401.
type TaskAPI struct {
	URL   string
	Token string
}

func buildConfig(binaryPath string, taskAPI *TaskAPI) (mcpConfig, error) {
	servers := map[string]mcpServerEntry{
		"dashboard-channel": {
			Command: binaryPath,
			Args:    []string{SubcommandChannel},
		},
	}
	if taskAPI != nil {
		if taskAPI.URL == "" || taskAPI.Token == "" {
			return mcpConfig{}, fmt.Errorf("channelconfig: TaskAPI needs both a URL and a token")
		}
		servers["dashboard-tasks"] = mcpServerEntry{
			Type:    "http",
			URL:     taskAPI.URL,
			Headers: map[string]string{"Authorization": "Bearer " + taskAPI.Token},
		}
	}
	return mcpConfig{MCPServers: servers}, nil
}
```

`ConfigJSON` and `WriteTempConfig` each gain the `taskAPI *TaskAPI` parameter, pass it to `buildConfig`, and return its error. In `WriteTempConfig`, create the file with explicit `0600` rather than relying on the umask, since it now holds a bearer token:

```go
	f, err := os.CreateTemp(dir, "dashboard-channel-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("channelconfig: create temp file: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("channelconfig: chmod temp file: %w", err)
	}
```

- [ ] **Step 4: Fix the other callers**

`cd server && go build ./...` names them. Pass `nil` at each existing call site except the pipeline spawner, which Task 6 changes.

- [ ] **Step 5: Run the tests**

Run: `cd server && go test ./internal/channelconfig/ -v && go vet ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l internal/channelconfig
git add server/internal/channelconfig server/cmd server/internal/api/agents
git commit -m "feat(channelconfig): optionally register the task API with a bearer"
```

---

### Task 6: Issue on spawn, revoke on the terminal write

**Files:**
- Modify: `server/internal/pipeline/spawner.go` (`SpawnAgentOptions` at `:44`, `SpawnStageAgent` at `:606-617`)
- Modify: `server/internal/pipeline/types.go` (`StageContext` and `OrchestratorOptions`), `server/internal/pipeline/stage_handlers.go:175`
- Modify: `server/internal/pipeline/stage_run_service.go`, `server/serverapp/di_pipeline.go`
- Test: `server/internal/pipeline/stage_key_test.go`

**Interfaces:**
- Consumes: `mcp.StageKeyIssuer.Issue/Revoke` (Task 2), `channelconfig.TaskAPI` and the two-argument `WriteTempConfig` (Task 5).
- Produces:
  - `SpawnAgentOptions.TaskAPIToken string`
  - `StageContext.IssueTaskAPIKey func(ctx context.Context, stageRunID string, stageTimeout time.Duration) (string, error)`
  - `OrchestratorOptions.IssueTaskAPIKey` and `OrchestratorOptions.RevokeTaskAPIKeys func(ctx context.Context, stageRunID string) error`, forwarded onto every `StageContext` the orchestrator builds — the same shape `AuthorizeMemory` already uses.

**Design note this task settles:** revocation hangs on `stageRunService.Update`, the single seam every stage-run status write already goes through. It fires for `done`, `failed` and `cancelled` only. `awaiting_user` is deliberately excluded: a run in that state can be resumed and its agent may still be alive, and `expires_at` still caps it.

- [ ] **Step 1: Write the failing tests**

Create `server/internal/pipeline/stage_key_test.go`:

```go
package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// A spawn whose credential could not be minted must still run: the agent keeps
// the channel bridge and loses only the task API, which is exactly today's
// behaviour. A spawn that died because a credential failed would be worse.
func TestSpawnStageAgent_IssuanceFailureIsNotFatal(t *testing.T) {
	called := false
	opts := SpawnAgentOptions{
		Task:          &ent.Task{ID: "t1", Slug: "t1", Cwd: t.TempDir(), Autonomy: "full"},
		StageRun:      &ent.StageRun{ID: "sr1", Stage: "implementation"},
		EnableChannel: true,
	}
	_ = called
	_ = opts
	t.Skip("replace this skip with the package's existing fake-spawn harness; see fakespawn")
}

func TestStageRunService_RevokesOnTerminalStatus(t *testing.T) {
	var revoked []string
	svc := &stageRunService{
		repo:   newFakeStageRunRepo(),
		revoke: func(_ context.Context, id string) error { revoked = append(revoked, id); return nil },
	}
	ctx := context.Background()

	_, _ = svc.Update(ctx, "sr-1", repoUpdateStatus("running"))
	if len(revoked) != 0 {
		t.Fatalf("a running run must keep its credential, got %v", revoked)
	}

	_, _ = svc.Update(ctx, "sr-1", repoUpdateStatus("awaiting_user"))
	if len(revoked) != 0 {
		t.Fatalf("awaiting_user is resumable and must keep its credential, got %v", revoked)
	}

	for _, status := range []string{"done", "failed", "cancelled"} {
		revoked = nil
		_, _ = svc.Update(ctx, "sr-1", repoUpdateStatus(status))
		if len(revoked) != 1 || revoked[0] != "sr-1" {
			t.Fatalf("status %q must revoke the run's credentials, got %v", status, revoked)
		}
	}
}

func TestStageRunService_RevokeFailureDoesNotFailTheWrite(t *testing.T) {
	svc := &stageRunService{
		repo:   newFakeStageRunRepo(),
		revoke: func(context.Context, string) error { return errRevokeBoom },
	}
	if _, err := svc.Update(context.Background(), "sr-1", repoUpdateStatus("done")); err != nil {
		t.Fatalf("a failed revoke must not roll back the status write: %v", err)
	}
}

var _ = time.Second
```

Write `newFakeStageRunRepo`, `repoUpdateStatus` and `errRevokeBoom` in the same file as small local helpers: the fake records the last input and returns a bare `*ent.StageRun`; `repoUpdateStatus(s)` returns `repo.UpdateStageRunInput{Status: strPtr(s)}`.

For the spawner half, use the package's existing fake-spawn harness (`server/internal/testsupport/fakespawn`) rather than launching a process — replace the `t.Skip` above with an assertion that `SpawnStageAgent` returns no error when `TaskAPIToken` is empty and that the written config then holds one server entry.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/pipeline/ -run 'TestStageRunService_Revoke|TestSpawnStageAgent_Issuance' -v`
Expected: compile failure — `stageRunService` has no `revoke` field.

- [ ] **Step 3: Revocation in the stage-run seam**

In `server/internal/pipeline/stage_run_service.go`:

```go
// terminalStageRunStatuses are the statuses after which a stage run's agent can
// make no further calls. awaiting_user is deliberately absent: such a run is
// resumable and its agent may still be alive; expires_at caps it instead.
var terminalStageRunStatuses = map[string]bool{
	"done": true, "failed": true, "cancelled": true,
}

type stageRunService struct {
	repo repo.StageRunRepo
	// revoke invalidates the MCP credentials issued for a stage run. Nil in
	// tests and in any composition that wires no issuer, which disables
	// revocation without changing any call site.
	revoke func(ctx context.Context, stageRunID string) error
}

func newStageRunService(r repo.StageRunRepo, revoke func(context.Context, string) error) *stageRunService {
	return &stageRunService{repo: r, revoke: revoke}
}

func (s *stageRunService) Update(ctx context.Context, id string, input repo.UpdateStageRunInput) (*ent.StageRun, error) {
	run, err := s.repo.Update(ctx, id, input)
	if err != nil {
		return nil, err
	}
	// Revoke after the write, and never let its failure surface: the status
	// change has already happened, so returning an error here would report
	// that nothing happened when something did. expires_at is the second net.
	if s.revoke != nil && input.Status != nil && terminalStageRunStatuses[*input.Status] {
		if rerr := s.revoke(ctx, id); rerr != nil {
			slog.Warn("pipeline: revoking stage-run credentials failed", "stageRun", id, "err", rerr)
		}
	}
	return run, nil
}
```

Update `newStageRunService`'s call site to pass the new argument (`cd server && go build ./...` names it).

- [ ] **Step 4: Issuance and delivery in the spawn path**

Add to `SpawnAgentOptions` in `server/internal/pipeline/spawner.go`:

```go
	// TaskAPIToken is the stage-run credential the agent presents to the
	// dashboard's own MCP endpoint. Empty means the config gets no such entry
	// and the agent reaches only the channel bridge.
	TaskAPIToken string
```

In `SpawnStageAgent`, replace the `WriteTempConfig` call:

```go
			var taskAPI *channelconfig.TaskAPI
			if opts.TaskAPIToken != "" && opts.MCPUrl != "" {
				taskAPI = &channelconfig.TaskAPI{URL: opts.MCPUrl + mcp.EndpointPath, Token: opts.TaskAPIToken}
			}
			if cfgPath, cfgErr := channelconfig.WriteTempConfig(selfBin, taskAPI); cfgErr == nil {
```

In `server/internal/pipeline/stage_handlers.go`, mint the token immediately before the `h.spawnFn(...)` literal at `:175` and add it to that literal:

```go
	// A credential that cannot be minted is not a reason to refuse the spawn:
	// the agent then runs with the channel bridge alone, which is what every
	// spawn did before this existed.
	taskAPIToken := ""
	if ctx.IssueTaskAPIKey != nil {
		timeout := time.Duration(ctx.Task.StageTimeoutSeconds) * time.Second
		if tok, err := ctx.IssueTaskAPIKey(ctx.Ctx, ctx.StageRun.ID, timeout); err != nil {
			slog.Warn("pipeline: issuing the stage-run MCP credential failed — agent runs without task API access",
				"stageRun", ctx.StageRun.ID, "err", err)
		} else {
			taskAPIToken = tok
		}
	}
```

then add `TaskAPIToken: taskAPIToken,` to the `SpawnAgentOptions` literal.

Add the matching field to `StageContext` and `OrchestratorOptions` in `types.go`, forwarded the same way `AuthorizeMemory` is (see `progress_guards.go:169` for the forwarding site).

- [ ] **Step 5: Wire the composition root**

In `server/serverapp/di_pipeline.go`, next to the `AuthorizeMemory` closure:

```go
		IssueTaskAPIKey: func(ctx context.Context, stageRunID string, stageTimeout time.Duration) (string, error) {
			return mcp.StageKeyIssuer{Keys: apiKeyRepo}.Issue(ctx, stageRunID, stageTimeout)
		},
		RevokeTaskAPIKeys: func(ctx context.Context, stageRunID string) error {
			return mcp.StageKeyIssuer{Keys: apiKeyRepo}.Revoke(ctx, stageRunID)
		},
```

Construct `apiKeyRepo := repo.NewApiKeyRepo(client)` there if it is not already in scope.

- [ ] **Step 6: Run the tests**

Run: `cd server && go test ./internal/pipeline/ -count=1 && go vet ./...`
Expected: PASS, including every existing pipeline test.

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l internal/pipeline serverapp
git add server/internal/pipeline server/serverapp/di_pipeline.go
git commit -m "feat(pipeline): issue a stage-run credential and revoke it on the terminal write"
```

---

### Task 7: The sweep, and the documentation

**Files:**
- Create: `server/internal/mcp/sweep.go`
- Modify: `server/serverapp/di.go` (start the sweeper alongside the other scanners)
- Modify: `README.md`, `CHANGELOG.md`, `docs/guides/security.md`, `docs/guides/mcp.md`
- Test: `server/internal/mcp/sweep_test.go`

**Interfaces:**
- Consumes: `ApiKeyRepo.DeleteExpired` (Task 1).
- Produces: `mcp.SweepExpiredKeys(ctx context.Context, keys repo.ApiKeyRepo, interval time.Duration)`

- [ ] **Step 1: Write the failing test**

Create `server/internal/mcp/sweep_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func TestSweepExpiredKeys_BootSweepRemovesExpiredEphemeralKeys(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	keys := repo.NewApiKeyRepo(bundle.Client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, err = keys.Create(ctx, repo.CreateApiKeyInput{
		Name: "old", Hash: "old", Scopes: mcp.StageRunScopes,
		Kind: repo.ApiKeyKindStageRun, StageRunID: "sr-1", ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = keys.Create(ctx, repo.CreateApiKeyInput{Name: "human", Hash: "human", Scopes: []string{"tasks:read"}})
	require.NoError(t, err)

	// interval <= 0 runs the boot sweep only and returns.
	mcp.SweepExpiredKeys(ctx, keys, 0)

	remaining, err := keys.List(ctx)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, "human", remaining[0].Name)
}

func TestSweepExpiredKeys_StopsWhenTheContextIsCancelled(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		mcp.SweepExpiredKeys(ctx, repo.NewApiKeyRepo(bundle.Client), time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SweepExpiredKeys did not return on a cancelled context")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/mcp/ -run 'TestSweepExpiredKeys' -v`
Expected: compile failure — `mcp.SweepExpiredKeys` is undefined.

- [ ] **Step 3: Write the sweeper**

Create `server/internal/mcp/sweep.go`:

```go
package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// SweepExpiredKeys deletes expired stage-run credentials once at boot and then
// on every tick until ctx is cancelled. interval <= 0 runs the boot sweep only.
//
// These rows are deleted rather than deactivated: they carry no audit value —
// the stage run they name has its own record — and one row per stage run per
// retry, kept forever, turns the key table into a log. User keys are untouched;
// they are soft-deleted through ApiKeyRepo.Delete so their hash survives.
func SweepExpiredKeys(ctx context.Context, keys repo.ApiKeyRepo, interval time.Duration) {
	sweep := func() {
		n, err := keys.DeleteExpired(ctx, time.Now())
		if err != nil {
			slog.Warn("mcp.sweep: deleting expired stage-run keys failed", "err", err)
			return
		}
		if n > 0 {
			slog.Info("mcp.sweep: removed expired stage-run keys", "count", n)
		}
	}

	slog.Info("mcp.sweep: starting expired-credential sweeper", "interval", interval)
	sweep()
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
```

- [ ] **Step 4: Start it**

In `server/serverapp/di.go`, where the other periodic scanners are started, add:

```go
	// One hour: these rows are already unusable the moment they expire
	// (GetByHash refuses them), so the sweep is housekeeping, not enforcement.
	go mcp.SweepExpiredKeys(ctx, apiKeyRepo, time.Hour)
```

- [ ] **Step 5: Run the tests**

Run: `cd server && go test ./internal/mcp/ -count=1 && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Update the docs in the same change**

- `docs/guides/security.md`: replace the paragraph stating that an MCP tool call cannot be attributed to a routine because `DASHBOARD_MCP_TOKEN` is one config value. It is still one config value and still belongs to the channel bridge — say that — but the task API is now reached with a per-stage-run credential, so `task:` and `routine:` grants decide MCP tool calls. Name the two expiries and the fixed scope set, including why `keys:manage` is excluded.
- `docs/guides/mcp.md`: document that a pipeline agent is registered with the `dashboard-tasks` server automatically and needs no hand-made key; a human still creates one for their own client.
- `README.md`: one sentence where the capability model is described.
- `CHANGELOG.md`: an `### Added` entry under `## [Unreleased]`, in the house style — what was broken, what changed, and what is still out of scope (interactive sessions, the spawner's allow-list).

- [ ] **Step 7: Full gates and commit**

```bash
cd server && gofmt -l internal serverapp && go vet ./...
cd .. && task test && GOTOOLCHAIN=go1.26.6 task lint
pnpm lint && pnpm typecheck && pnpm test
git checkout -- server/internal/db/ent/ 2>/dev/null || true   # task test regenerates it
git checkout HEAD -- server/frontend/dist/.gitkeep 2>/dev/null || true
git add -A
git commit -m "feat(mcp): sweep expired stage-run credentials, and document the new attribution"
```

Paste the raw output of every gate. A summary is not evidence.

---

## Self-Review

**Spec coverage.** §4.1 storage → Task 1. §4.2 authentication → Tasks 1 (expiry rule) and 3 (`StageRunID`). §4.3 issuance and delivery → Tasks 5 and 6. §4.4 reaching the gate → Tasks 3 and 4. §4.5 lifecycle → Tasks 2, 6 and 7. §4.6 human-facing surfaces → Task 1 (`List` filter). §5 out of scope → nothing implements it, by design. §6 testing → the matrix is distributed across Tasks 1–6, with the decisive one-grant-two-callers test in Task 4. §7 risks → mitigations land in Tasks 1, 2 and 7.

**One spec refinement made here:** §4.2 places the expiry check in the middleware; Task 1 places it in `GetByHash` instead, so that "a usable key" is decided once. The externally visible behaviour is identical (401), and the note is recorded in Task 1.

**Placeholders.** One remains and is deliberate: Task 6 Step 1's spawner test carries a `t.Skip` pointing at the package's existing `fakespawn` harness, because that harness's exact shape must be read before a faithful test can be written. Replace it in that task; do not commit the skip.

**Type consistency.** `CreateApiKeyInput` (Task 1) is consumed with the same field names in Tasks 2 and 7. `StageKeyIssuer.Issue(ctx, stageRunID, stageTimeout)` (Task 2) matches the closure signature wired in Task 6 Step 5 and the `StageContext.IssueTaskAPIKey` type in Task 6 Step 4. `CallerResolver.Contexts(ctx)` (Task 3) is called exactly that way in Task 4. `channelconfig.TaskAPI{URL, Token}` (Task 5) is constructed with those field names in Task 6 Step 4.
