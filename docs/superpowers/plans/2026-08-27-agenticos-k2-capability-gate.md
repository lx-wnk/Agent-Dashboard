# AgenticOS K2 — Capability Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One decision function answers "may this happen?" for agents, applications and routines alike, over a capability vocabulary that can express actions no tool name covers.

**Architecture:** A `capability` catalogue and a `grant` table sit beside the permission tables that already exist rather than replacing them. A pure `Decider` resolves a request by context specificity, with deny beating allow at the most specific level that has any grant. Three enforcers consume the same `Decision` and differ only in where they intercept — and each declares its own failure posture, because the hook enforcer genuinely fails open and pretending otherwise would ship a guarantee the system cannot keep.

**Tech Stack:** Go 1.26, ent v0.14.6 (`--feature sql/upsert`), modernc SQLite, chi. Frontend untouched by this plan.

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-capability-gate-design.md`
Umbrella: `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md`
Conventions: `docs/superpowers/specs/2026-08-27-agenticos-conventions-design.md`

**Depends on:** K1 (`docs/superpowers/plans/2026-08-27-agenticos-k1-resource-registry.md`). This plan consumes `repo.Scope`, `repo.ResourceRepo`, `repo.WithTx`, `repo.IsNotFound` and the `resource` table. Do not start until K1 is merged.

## Global Constraints

These apply to **every** task. They are not repeated per task.

- **Go workspace** with `./sdk` and `./server`. All work is in `./server`. `go build ./...` from the repository root FAILS — use `go build ./server/...`.
- **`go test` regenerates the ent tree.** After any test run, `git status --short server/internal/db/ent/` must be empty before committing; if not, `git checkout -- server/internal/db/ent/`. The two tasks that change schema commit their regeneration deliberately; every other task must not.
- **Never amend a commit.** Add a new one. History that shows what a review caught is worth more than a tidy log.
- **ent index changes rebuild SQLite tables and crash on populated databases.** Every new unique index is pre-created under ent's exact generated name — read from `server/internal/db/ent/migrate/schema.go`, never guessed — in a pre-migration before `Schema.Create`. Reference: `server/internal/db/client.go`, PR #207.
- **JSON columns added to populated tables need a raw `entsql.Default("{}")`.** Not the escaped form. Reference: `server/internal/db/ent/schema/spawner.go:25-33`.
- **Empty string, not NULL, is the global-scope sentinel.** SQLite treats two NULLs as distinct.
- **Upsert must not clobber lifecycle columns.** K1 learned this the hard way: blanket `UpdateNewValues()` on a conflict clause silently resets state. Update only mutable metadata explicitly. Reference: `server/internal/db/repo/plugin_repo.go:51-53` and K1's `resource_repo.go` Upsert.
- **Everything that ships is English** — code, identifiers, comments, commit messages, PR text.
- **Conventional Commits**, never referencing a task number or plan phase.
- **Gate commands** (paste raw output; a summary is not evidence):
  - `go build ./server/...`
  - `cd server && go vet ./...` and `cd sdk && go vet ./...` (module-wide on purpose — a narrow package scope misses `_test.go` files in sibling packages that reference a changed exported type)
  - `go test -race -count=1 ./server/internal/db/... ./server/internal/permissions/... ./server/internal/pipeline/... ./server/internal/mcp/... ./server/internal/api/...`
  - `task test` before the final commit
  - `gofmt -l` on every changed file
  - `task lint` is currently broken in local environments for toolchain reasons unrelated to any branch; CI runs a pinned version and is the authority.

## Already done — do not re-implement

The `outcome` vocabulary normalization that the spec lists as G6 **shipped separately** in the ACP fix. `repo.OutcomeGranted`, `repo.OutcomeDenied` and `repo.OutcomeExpired` already exist in `server/internal/db/repo/permission_repo.go`, every writer and validator uses them, and a boot migration rewrites the legacy `"approved"` value. Verify with `grep -n "OutcomeGranted" server/internal/db/repo/permission_repo.go` before starting; if it is absent, stop and report — the dependency assumption is wrong.

## What exists today, verified

Every signature below was read from the tree at plan time. Verify each again before the task that consumes it.

| Symbol | Location | Shape |
|---|---|---|
| `permissions.IsAllowedTool` | `server/internal/permissions/allowlist.go:254` | `func(name string) bool` |
| `permissions.IsSafeBashPattern` | `allowlist.go:144` | `func(pattern string) (bool, string)` |
| `permissions.ValidateGrantEntry` | `allowlist.go:243` | `func(tool, pattern string) error` |
| `permissions.ValidateGrantEntryWithOverride` | `allowlist.go:216` | `func(tool, pattern string, override bool) error` |
| `permissions.IsWriteTool` | `allowlist.go:267` | `func(name string) bool` |
| `permissions.WriteToolNames` | `allowlist.go:249` | exported slice with **zero non-test readers** |
| `permissions.TemplateTools` | `templates.go:14` | `map[string][]string` |
| `permissions.ResolveTemplate` | `templates.go:28` | `func(name string) ([]string, error)` |
| `pipeline.BuildAllowList` | `server/internal/pipeline/spawner.go:98` | `func(autonomy string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) []string` |
| `pipeline.BuildDenyList` | `spawner.go:88` | `func(autonomy string, allowGitPush bool) []string` |
| `repo.GrantEntry` | `server/internal/db/repo/permission_repo.go` | `{Tool string; Pattern *string; ExpiresAt *time.Time; ManualOverride bool}` |
| `repo.CreateTaskPermissionInput` | same file | `{TaskID, Tool string; Pattern *string; Granted, PreApproved, ManualOverride bool; ExpiresAt *time.Time}` |
| `validKeyScopes` | `server/internal/mcp/tools/keys.go:17` | unexported; **omits `agent:coord`** |
| `ToolScopeMap`, `scopeImplies` | `server/internal/mcp/auth.go:18,52` | one-level expansion, not transitive |

---

### Task 1: Pattern algebra

Pure Go, no database, no dependencies. Everything later resolves through it, and today's coverage logic is exact-string equality duplicated in two functions.

**Files:**
- Create: `server/internal/capability/pattern.go`
- Test: `server/internal/capability/pattern_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `capability.Pattern` (string type); `capability.Match(grantPattern, requested string) bool`; `capability.ParsePattern(s string) (Pattern, error)`.

Semantics, all three of which the codebase needs and none of which it currently has in one place:

- an empty grant pattern matches anything (today's nil-pattern wildcard)
- `exact` — byte equality
- `prefix` — a trailing `*`, e.g. `git status*` matches `git status --short` but `git status` does **not** match `git status --short` without the star
- `domain:` — e.g. `domain:docs.example.com` matches that host and its subdomains, never a suffix collision like `evildocs.example.com`

- [ ] **Step 1: Write the failing test**

```go
package capability_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		grant   string
		request string
		want    bool
	}{
		{"empty grant is a wildcard", "", "anything at all", true},
		{"exact matches itself", "git status", "git status", true},
		{"exact does not cover a longer command", "git status", "git status --short", false},
		{"prefix covers the longer command", "git status*", "git status --short", true},
		{"prefix matches the bare prefix too", "git status*", "git status", true},
		{"prefix does not cover a different command", "git status*", "git push", false},
		{"domain matches the host", "domain:docs.example.com", "https://docs.example.com/a", true},
		{"domain matches a subdomain", "domain:example.com", "https://docs.example.com/a", true},
		{"domain rejects a suffix collision", "domain:example.com", "https://evilexample.com/a", false},
		{"domain rejects a different host", "domain:example.com", "https://other.test/a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capability.Match(tt.grant, tt.request); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.grant, tt.request, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/capability/ -run TestMatch -count=1`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Implement**

Write `Match` so that each case above holds. The suffix-collision case is the one that matters: a naive `strings.HasSuffix(host, domain)` passes every other test and fails that one, which is precisely why it is in the table.

Sketch — fill in the host extraction:

```go
package capability

import (
	"net/url"
	"strings"
)

// Match reports whether a grant's pattern covers a requested value.
// An empty grant pattern is a wildcard, matching the nil-pattern convention the
// permission tables already use.
func Match(grantPattern, requested string) bool {
	switch {
	case grantPattern == "":
		return true
	case strings.HasPrefix(grantPattern, "domain:"):
		return matchDomain(strings.TrimPrefix(grantPattern, "domain:"), requested)
	case strings.HasSuffix(grantPattern, "*"):
		return strings.HasPrefix(requested, strings.TrimSuffix(grantPattern, "*"))
	default:
		return grantPattern == requested
	}
}

// matchDomain matches a host or any of its subdomains. It compares label by
// label rather than by string suffix, so "example.com" does not match
// "evilexample.com".
func matchDomain(domain, requested string) bool {
	u, err := url.Parse(requested)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	return host == domain || strings.HasSuffix(host, "."+domain)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/capability/ -run TestMatch -count=1 -v`
Expected: PASS, all ten subtests.

- [ ] **Step 5: Commit**

```bash
gofmt -l server/internal/capability/
git add server/internal/capability/
git commit -m "feat(capability): add the shared grant pattern algebra

Grant coverage was exact-string equality duplicated across two unexported
helpers, so a grant for 'git status' did not cover 'git status --short' and
no caller could express that it should.

Domain matching compares labels rather than string suffixes: example.com must
not match evilexample.com, and the test table pins that case because a naive
suffix check passes every other case in it."
```

---

### Task 2: Capability catalogue

**Files:**
- Create: `server/internal/db/ent/schema/capability.go`
- Modify: `server/internal/db/client.go` (index pre-migration plus its call site)
- Create: `server/internal/db/repo/capability_repo.go`
- Test: `server/internal/db/repo/capability_repo_test.go`
- Regenerated and committed as part of this task: `server/internal/db/ent/**`

**Interfaces:**
- Consumes: K1's `IDTimestampsMixin` in `server/internal/db/ent/schema/mixins.go`.
- Produces: ent entity `Capability`; `repo.CapabilityRepo` with `Upsert(ctx, UpsertCapabilityInput) (*ent.Capability, error)`, `Get(ctx, name string) (*ent.Capability, error)`, `List(ctx) ([]*ent.Capability, error)`; constants `repo.CapClassTool`, `CapClassReach`, `CapClassResource`, `CapClassSpend`; `repo.EnforcerServer`, `EnforcerSpawn`, `EnforcerHook`.

Schema fields, per spec §4.1:

```
name              string, unique   // "mail.send", "Bash", "memory.write"
class             string           // tool | reach | resource | spend
enforceable_by    JSON []string    // subset of server, spawn, hook
requires_pattern  bool
reversible        bool
description       string
```

- [ ] **Step 1: Write the schema**

Mirror K1's `resource.go` in style. `enforceable_by` is the one JSON column — give it `Default([]string{})` **and** the raw `entsql.Default("[]")` annotation, for the same reason `spawner.adapter_config` carries one: SQLite's `ALTER TABLE ADD COLUMN` on a populated table gets no default from ent's Go-side default alone.

- [ ] **Step 2: Regenerate and read the generated index name**

Run: `cd server && go generate ./internal/db/ent/`
Run: `grep -n 'capability_name\|CapabilitiesTable' server/internal/db/ent/migrate/schema.go | head`

Write down the exact unique-index name. Use that string in Step 4 — never the one you expect.

- [ ] **Step 3: Write the failing repo test**

```go
func TestCapabilityUpsertIsIdempotent(t *testing.T) {
	r, ctx := newCapabilityRepo(t)
	in := repo.UpsertCapabilityInput{
		Name:          "mail.send",
		Class:         repo.CapClassReach,
		EnforceableBy: []string{repo.EnforcerServer},
		Reversible:    false,
		Description:   "Send mail on the user's behalf",
	}
	first, err := r.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	in.Description = "Send an email"
	second, err := r.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("upsert created a second row: %s != %s", first.ID, second.ID)
	}
	if second.Description != "Send an email" {
		t.Errorf("description = %q, want the updated value", second.Description)
	}
}

func TestCapabilityIrreversibleIsPersisted(t *testing.T) {
	r, ctx := newCapabilityRepo(t)
	got, err := r.Upsert(ctx, repo.UpsertCapabilityInput{
		Name:          "obsidian.delete",
		Class:         repo.CapClassReach,
		EnforceableBy: []string{repo.EnforcerServer},
		Reversible:    false,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got.Reversible {
		t.Error("reversible must persist as false — it gates the auto-grant rule")
	}
}
```

Write `newCapabilityRepo(t)` in the same shape as K1's `newResourceRepo(t)`: open `db.Open(":memory:")`, register cleanup, return the repo and a context.

- [ ] **Step 4: Run the test, then implement**

Run it first and record the compile failure. Then write the schema, the pre-migration under the exact generated index name, and the repo.

**The Upsert conflict clause updates `class`, `enforceable_by`, `requires_pattern`, `reversible`, `description` and `updated_at` — explicitly, never `UpdateNewValues()`.** K1 shipped that bug and had to fix it in the final review; do not repeat it.

- [ ] **Step 5: Verify and commit**

Run: `cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/db/...`
Expected: PASS.

The regenerated ent tree **is** the change here and gets committed.

```bash
git add server/internal/db/ent/ server/internal/db/client.go server/internal/db/repo/
git commit -m "feat(db): add the capability catalogue

A capability is a named permission coarser than a tool name, so an action like
sending mail can be expressed at all — the existing allow-list can only name
Claude Code tools.

enforceable_by records where a capability can actually be enforced. It exists
because enforcement is not uniform: the PreToolUse hook fails open by design,
so a capability that only holds for orchestrated agents must say so rather
than implying a guarantee the system cannot keep."
```

---

### Task 3: Grant schema and repository

**Files:**
- Create: `server/internal/db/ent/schema/grant.go`
- Modify: `server/internal/db/client.go` (index pre-migration plus call site)
- Create: `server/internal/db/repo/grant_repo.go`
- Test: `server/internal/db/repo/grant_repo_test.go`
- Regenerated and committed: `server/internal/db/ent/**`

**Interfaces:**
- Consumes: K1's `repo.Scope` and `IDTimestampsMixin`; Task 2's capability constants.
- Produces: ent entity `Grant`; `repo.GrantRepo` with `Create(ctx, CreateGrantInput) (*ent.Grant, error)`, `ListForCapability(ctx, capabilityName string) ([]*ent.Grant, error)`, `Revoke(ctx, id, revokedBy string) error`; constants `repo.GrantModeAllow`, `GrantModeDeny`, `GrantModeAsk`; context-kind constants `repo.GrantContextGlobal`, `GrantContextProject`, `GrantContextTask`, `GrantContextRoutine`, `GrantContextApplication`, `GrantContextAgentSession`.

Schema fields, per spec §4.2:

```
capability_name        string
context_kind           string
context_ref            string          // "" for global — the sentinel, not NULL
pattern                string
mode                   string          // allow | deny | ask
limit_count            int             // 0 means unlimited
limit_window_seconds   int
expires_at             time, nillable
granted_by             string          // REQUIRED, not nillable
granted_at             time
revoked_at             time, nillable
reason                 string
node_id                string          // "local" until the node registry lands
```

Index on `(capability_name, context_kind, context_ref)`; a second on `(revoked_at)`.

`granted_by` is **required**. The mistake this replaces is `task_permission.decided_by`, which is nillable and consequently written nowhere — making "who allowed this" unanswerable. Do not make it optional "for now".

- [ ] **Step 1: Write the failing test**

```go
func TestGrantRequiresGrantedBy(t *testing.T) {
	r, ctx := newGrantRepo(t)
	_, err := r.Create(ctx, repo.CreateGrantInput{
		CapabilityName: "Bash",
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Mode:           repo.GrantModeAllow,
		Pattern:        "git status*",
		// GrantedBy deliberately omitted
	})
	if err == nil {
		t.Fatal("a grant without granted_by must be refused — identity on a decision is not optional")
	}
}

func TestGrantRevokeIsATombstone(t *testing.T) {
	r, ctx := newGrantRepo(t)
	g, err := r.Create(ctx, repo.CreateGrantInput{
		CapabilityName: "Bash",
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Mode:           repo.GrantModeAllow,
		Pattern:        "git status*",
		GrantedBy:      "user:alex",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.Revoke(ctx, g.ID, "user:alex"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	all, err := r.ListForCapability(ctx, "Bash")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("revoke must not delete the row, got %d rows", len(all))
	}
	if all[0].RevokedAt == nil {
		t.Error("revoked_at must be set — revocation is a tombstone, not a delete")
	}
}
```

- [ ] **Step 2: Run it, confirm the compile failure, then implement**

Follow Task 2's shape: schema, generated-name pre-migration, repo with an explicit conflict clause if you use upsert at all (a grant is normally created, not upserted).

- [ ] **Step 3: Verify and commit**

```bash
cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/db/...
git add server/internal/db/ent/ server/internal/db/client.go server/internal/db/repo/
git commit -m "feat(db): add context-bound grants with expiry, limits and revocation

A grant binds a capability to a context — this project, this routine, this
agent — and carries the three things the permission tables cannot express: an
expiry, a rate limit, and a negative mode that a narrower context can use to
overrule a broader allow.

granted_by is required rather than nillable. Its predecessor, decided_by, was
optional and is written nowhere, which is why nothing in this system can
currently answer who allowed a given action."
```

---

### Task 4: The Decider

**Files:**
- Create: `server/internal/capability/decide.go`
- Test: `server/internal/capability/decide_test.go`

**Interfaces:**
- Consumes: Task 1's `Match`; Task 3's grant rows (passed in as a slice, so the Decider stays pure and database-free).
- Produces:
  - `capability.Request{Capability string; Value string; Contexts []Context}`
  - `capability.Context{Kind string; Ref string}` — the caller supplies the full chain from most specific to least
  - `capability.Effect` with `EffectAllow`, `EffectDeny`, `EffectAsk`
  - `capability.Decision{Effect Effect; GrantID string; Reason string; Enforceable []string}`
  - `capability.Decide(req Request, grants []GrantView, cap CapabilityView) Decision`
  - `capability.GrantView` and `capability.CapabilityView` — narrow read-only projections so this package never imports ent

Resolution rules, per spec §4.3, in order:

1. Drop grants that are revoked or expired.
2. Drop grants whose pattern does not `Match` the requested value.
3. Rank the remainder by context specificity: `agent_session` > `task` > `routine` > `application` > `project` > `global`.
4. At the most specific level that has **any** grant: `deny` beats `allow` beats `ask`.
5. No grant at any level → the capability's default: `ask` for `tool` and `reach`, `deny` for `spend`.

- [ ] **Step 1: Write the failing test**

```go
func TestDecideSpecificityAndDenyPrecedence(t *testing.T) {
	cap := capability.CapabilityView{Name: "Bash", Class: "tool", EnforceableBy: []string{"spawn"}}

	tests := []struct {
		name   string
		grants []capability.GrantView
		want   capability.Effect
		why    string
	}{
		{
			name:   "no grants falls back to ask",
			grants: nil,
			want:   capability.EffectAsk,
			why:    "a tool capability defaults to asking",
		},
		{
			name: "a global allow is honoured",
			grants: []capability.GrantView{
				{ID: "g1", ContextKind: "global", Mode: "allow", Pattern: "git status*"},
			},
			want: capability.EffectAllow,
		},
		{
			name: "a task deny overrules a global allow",
			grants: []capability.GrantView{
				{ID: "g1", ContextKind: "global", Mode: "allow", Pattern: "git status*"},
				{ID: "g2", ContextKind: "task", ContextRef: "t1", Mode: "deny", Pattern: "git status*"},
			},
			want: capability.EffectDeny,
			why:  "the more specific context wins outright, it does not merge",
		},
		{
			name: "a global deny does NOT overrule a task allow",
			grants: []capability.GrantView{
				{ID: "g1", ContextKind: "global", Mode: "deny", Pattern: "git status*"},
				{ID: "g2", ContextKind: "task", ContextRef: "t1", Mode: "allow", Pattern: "git status*"},
			},
			want: capability.EffectAllow,
			why:  "specificity is decided before mode; deny only wins within a level",
		},
		{
			name: "deny beats allow inside one level",
			grants: []capability.GrantView{
				{ID: "g1", ContextKind: "task", ContextRef: "t1", Mode: "allow", Pattern: "git status*"},
				{ID: "g2", ContextKind: "task", ContextRef: "t1", Mode: "deny", Pattern: "git status*"},
			},
			want: capability.EffectDeny,
		},
		{
			name: "a non-matching pattern does not count as a grant",
			grants: []capability.GrantView{
				{ID: "g1", ContextKind: "task", ContextRef: "t1", Mode: "allow", Pattern: "git push*"},
			},
			want: capability.EffectAsk,
			why:  "the grant exists but does not cover this value, so the level is empty",
		},
	}

	req := capability.Request{
		Capability: "Bash",
		Value:      "git status --short",
		Contexts: []capability.Context{
			{Kind: "task", Ref: "t1"},
			{Kind: "project", Ref: "/p"},
			{Kind: "global", Ref: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capability.Decide(req, tt.grants, cap)
			if got.Effect != tt.want {
				t.Errorf("Effect = %v, want %v (%s)", got.Effect, tt.want, tt.why)
			}
		})
	}
}

func TestDecideExpiredAndRevokedAreIgnored(t *testing.T) {
	cap := capability.CapabilityView{Name: "Bash", Class: "tool"}
	past := time.Now().Add(-time.Hour)
	req := capability.Request{
		Capability: "Bash",
		Value:      "git status",
		Contexts:   []capability.Context{{Kind: "global"}},
	}

	expired := []capability.GrantView{
		{ID: "g1", ContextKind: "global", Mode: "allow", Pattern: "", ExpiresAt: &past},
	}
	if got := capability.Decide(req, expired, cap); got.Effect != capability.EffectAsk {
		t.Errorf("expired grant: Effect = %v, want ask", got.Effect)
	}

	revoked := []capability.GrantView{
		{ID: "g1", ContextKind: "global", Mode: "allow", Pattern: "", RevokedAt: &past},
	}
	if got := capability.Decide(req, revoked, cap); got.Effect != capability.EffectAsk {
		t.Errorf("revoked grant: Effect = %v, want ask", got.Effect)
	}
}

func TestDecisionCarriesEnforceability(t *testing.T) {
	cap := capability.CapabilityView{Name: "mail.send", Class: "reach", EnforceableBy: []string{"server"}}
	req := capability.Request{Capability: "mail.send", Contexts: []capability.Context{{Kind: "global"}}}
	got := capability.Decide(req, []capability.GrantView{
		{ID: "g1", ContextKind: "global", Mode: "allow"},
	}, cap)
	if len(got.Enforceable) != 1 || got.Enforceable[0] != "server" {
		t.Errorf("Enforceable = %v, want the capability's own list — the UI states this where the grant is made", got.Enforceable)
	}
}
```

- [ ] **Step 2: Run to confirm the failure, then implement**

Note the fourth case: **specificity is resolved before mode.** A global deny does not overrule a task allow. That is deliberate — a broad deny that silently outranks a narrow, deliberate allow makes per-task grants useless — and the test names it so nobody "fixes" it later.

Rate limits are **not** evaluated here. The Decider is pure and has no counter; limits belong to the enforcers, which know how many calls have happened. Task 8 handles that.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go test -race ./internal/capability/ -count=1 -v
gofmt -l server/internal/capability/
git add server/internal/capability/
git commit -m "feat(capability): resolve a permission request to one decision

Specificity is resolved before mode: the most specific context that has any
matching grant decides, and deny beats allow only within that level. A global
deny therefore does not overrule a deliberate per-task allow, which is what
makes narrow grants worth granting.

The decision carries the capability's enforceability list, so a caller can
tell the user where the answer actually holds rather than implying it holds
everywhere."
```

---

### Task 5: Golden parity harness

Before any enforcer changes behaviour, prove the new path reproduces the old one exactly. This task adds no production code.

**Files:**
- Create: `server/internal/pipeline/allowlist_parity_test.go`
- Test: itself

**Interfaces:**
- Consumes: `pipeline.BuildAllowList`, `pipeline.BuildDenyList`, Task 4's `capability.Decide`.
- Produces: nothing. It is a gate for Task 6.

- [ ] **Step 1: Write the parity test**

Build a fixture set of `[]*ent.TaskPermission` covering: a granted Bash pattern, an expired one, a `manual_override` one, a bare `WebFetch` (which today is dropped), a blanket Bash with no pattern (dropped), and an autonomy value in `{manual, spec_gated, full}`.

For each fixture, assert that the allow list produced by translating the same fixture into grants and resolving through `capability.Decide` equals `BuildAllowList`'s output, element for element and order for order.

Where the two genuinely differ, **do not adjust the assertion** — record the difference in the test as an explicit, named exception with a comment saying why it is intended. An unexplained difference is the finding this task exists to surface.

- [ ] **Step 2: Run it**

Run: `cd server && go test ./internal/pipeline/ -run TestAllowListParity -count=1 -v`

If it fails, that is the point: reconcile the Decider or record the intended exception before proceeding to Task 6. Do not weaken the test to make it green.

- [ ] **Step 3: Commit**

```bash
git add server/internal/pipeline/allowlist_parity_test.go
git commit -m "test(pipeline): pin allow-list parity between the old and new gates

The capability gate replaces a filter chain that has accumulated six distinct
drop rules. This fixture set asserts the new resolution reproduces every one of
them before any caller is switched over, so a behaviour change during the
migration is a test failure rather than a production surprise."
```

---

### Task 6: Spawn enforcer

**Files:**
- Create: `server/internal/capability/enforcer_spawn.go`
- Modify: `server/internal/pipeline/spawner.go` — `BuildAllowList`'s body only
- Test: `server/internal/capability/enforcer_spawn_test.go`

**Interfaces:**
- Consumes: `capability.Decide`, `capability.Decision`, `capability.EffectAllow` (Task 4).
- Produces: `capability.SpawnEnforcer` with `Point() string` returning `EnforcerSpawn`, and `AllowList(decisions []Decision, entries []AllowEntry) []string`; `capability.AllowEntry{Tool, Pattern string}`.

`BuildAllowList` keeps its exported signature exactly — `func(autonomy string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) []string`. Every caller stays untouched. Only the body changes, and Task 5's parity test is what proves the change is behaviour-preserving.

- [ ] **Step 1: Write the failing test**

```go
package capability_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

func TestSpawnEnforcerEmitsOnlyAllowedEntries(t *testing.T) {
	e := capability.SpawnEnforcer{}
	decisions := []capability.Decision{
		{Effect: capability.EffectAllow},
		{Effect: capability.EffectDeny},
		{Effect: capability.EffectAsk},
	}
	entries := []capability.AllowEntry{
		{Tool: "Bash", Pattern: "git status*"},
		{Tool: "Bash", Pattern: "curl evil"},
		{Tool: "WebFetch", Pattern: "domain:docs.example.com"},
	}

	got := e.AllowList(decisions, entries)
	want := []string{"Bash(git status*)"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AllowList = %v, want %v — only an allow becomes a settings entry", got, want)
	}
}

func TestSpawnEnforcerAskIsNotAnAllow(t *testing.T) {
	e := capability.SpawnEnforcer{}
	got := e.AllowList(
		[]capability.Decision{{Effect: capability.EffectAsk}},
		[]capability.AllowEntry{{Tool: "Bash", Pattern: "rm -rf"}},
	)
	if len(got) != 0 {
		t.Errorf("AllowList = %v, want empty — the spawn point cannot ask, so ask means not allowed here", got)
	}
}

func TestSpawnEnforcerPoint(t *testing.T) {
	if got := (capability.SpawnEnforcer{}).Point(); got != capability.EnforcerSpawn {
		t.Errorf("Point() = %q, want %q", got, capability.EnforcerSpawn)
	}
}
```

The second test carries the load-bearing semantic: the spawn point writes a static settings file before the process starts, so it has no way to ask anyone anything. `ask` therefore resolves to "not in the allow list" — the agent will hit its own permission prompt, which is the correct fallback.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/capability/ -run TestSpawnEnforcer -count=1`
Expected: FAIL — `undefined: capability.SpawnEnforcer`.

- [ ] **Step 3: Implement the enforcer**

```go
package capability

import "fmt"

// AllowEntry is one tool-and-pattern pair a decision was made about.
type AllowEntry struct {
	Tool    string
	Pattern string
}

// SpawnEnforcer turns decisions into the allow list written into a spawned
// agent's settings file.
//
// It is the one enforcement point that cannot ask: the file is written before
// the process starts, so EffectAsk resolves to omission and the agent falls
// back to its own permission prompt.
type SpawnEnforcer struct{}

// Point identifies this enforcement point.
func (SpawnEnforcer) Point() string { return EnforcerSpawn }

// AllowList renders the allowed entries. decisions and entries are parallel
// slices; a length mismatch is a programming error and panics rather than
// silently dropping permissions.
func (SpawnEnforcer) AllowList(decisions []Decision, entries []AllowEntry) []string {
	if len(decisions) != len(entries) {
		panic(fmt.Sprintf("capability: AllowList got %d decisions for %d entries", len(decisions), len(entries)))
	}
	out := make([]string, 0, len(entries))
	for i, d := range decisions {
		if d.Effect != EffectAllow {
			continue
		}
		e := entries[i]
		if e.Pattern == "" {
			out = append(out, e.Tool)
			continue
		}
		out = append(out, fmt.Sprintf("%s(%s)", e.Tool, e.Pattern))
	}
	return out
}
```

The panic on a length mismatch is deliberate. Silently truncating would drop permissions in whichever direction the slices disagree, and a permission dropped by accident is indistinguishable from one denied on purpose.

- [ ] **Step 4: Route `BuildAllowList` through it**

In `server/internal/pipeline/spawner.go`, keep the signature and the allow-all short-circuit. Replace the filter chain body with: translate each `*ent.TaskPermission` into a grant view and an `AllowEntry`, call `capability.Decide` per entry, then `SpawnEnforcer{}.AllowList`.

The git-push gate and the channel entries stay where they are — they are not capability decisions and moving them is out of scope for this task.

- [ ] **Step 5: Run the parity test — this is the acceptance criterion**

Run: `cd server && go test ./internal/pipeline/ -run TestAllowListParity -count=1 -v`
Expected: PASS.

Then the full package: `cd server && go test -race -count=1 ./internal/pipeline/`

If parity fails, do not weaken the test. Either the translation is wrong, or you have found a genuine difference that belongs in the test as a named exception with a reason. Report which.

- [ ] **Step 6: Commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/capability/ server/internal/pipeline/spawner.go
git add server/internal/capability/ server/internal/pipeline/
git commit -m "feat(pipeline): resolve the spawn allow-list through the capability gate

BuildAllowList keeps its signature and every caller; only its body changes,
and the parity fixture proves the six drop rules it accumulated still hold.

The spawn point cannot ask anyone anything — its settings file is written
before the process exists — so an ask resolves to omission and the agent falls
back to its own prompt, which is the honest behaviour rather than a silent
allow."
```

---

### Task 7: Server enforcer

**Files:**
- Create: `server/internal/capability/enforcer_server.go`
- Test: `server/internal/capability/enforcer_server_test.go`

**Interfaces:**
- Consumes: `capability.Decision`, the effect constants.
- Produces: `capability.ServerEnforcer` with `Point() string`, `Enforce(ctx context.Context, d Decision) error`; sentinel errors `capability.ErrDenied` and `capability.ErrAskRequired`; the `capability.Asker` interface with `Ask(ctx context.Context, d Decision) (bool, error)`.

This is the complete enforcement point: it sits in-process in front of application calls, so nothing routes around it. Unlike the spawn point it can ask, and unlike the hook point it cannot be bypassed by a timeout.

- [ ] **Step 1: Write the failing test**

```go
package capability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

type recordingAsker struct {
	called bool
	answer bool
	err    error
}

func (a *recordingAsker) Ask(_ context.Context, _ capability.Decision) (bool, error) {
	a.called = true
	return a.answer, a.err
}

func TestServerEnforcerAllowPasses(t *testing.T) {
	asker := &recordingAsker{}
	e := capability.ServerEnforcer{Asker: asker}
	if err := e.Enforce(context.Background(), capability.Decision{Effect: capability.EffectAllow}); err != nil {
		t.Fatalf("allow must pass: %v", err)
	}
	if asker.called {
		t.Error("an allow must not consult the asker")
	}
}

func TestServerEnforcerDenyReturnsSentinel(t *testing.T) {
	e := capability.ServerEnforcer{Asker: &recordingAsker{}}
	err := e.Enforce(context.Background(), capability.Decision{
		Effect: capability.EffectDeny,
		Reason: "denied by a task-scoped grant",
	})
	if !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("Enforce = %v, want it to wrap ErrDenied", err)
	}
	if !strings.Contains(err.Error(), "denied by a task-scoped grant") {
		t.Errorf("error text lost the reason: %v", err)
	}
}

func TestServerEnforcerAskConsultsAndHonoursTheAnswer(t *testing.T) {
	granted := &recordingAsker{answer: true}
	e := capability.ServerEnforcer{Asker: granted}
	if err := e.Enforce(context.Background(), capability.Decision{Effect: capability.EffectAsk}); err != nil {
		t.Fatalf("a granted ask must pass: %v", err)
	}
	if !granted.called {
		t.Error("an ask must consult the asker")
	}

	refused := &recordingAsker{answer: false}
	e = capability.ServerEnforcer{Asker: refused}
	if err := e.Enforce(context.Background(), capability.Decision{Effect: capability.EffectAsk}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("a refused ask must deny, got %v", err)
	}
}

func TestServerEnforcerWithoutAskerFailsClosed() {
	// A missing asker is a wiring bug. It must not silently allow.
}
```

Write that last one out properly: construct `capability.ServerEnforcer{}` with a nil `Asker`, enforce an `EffectAsk`, and assert the result wraps `ErrDenied`. A misconfigured enforcer that lets everything through is the worst possible failure for this component, and it is the kind of bug a nil check silently creates.

Add `"strings"` to the test imports.

- [ ] **Step 2: Run to confirm the failure**

Run: `cd server && go test ./internal/capability/ -run TestServerEnforcer -count=1`
Expected: FAIL — `undefined: capability.ServerEnforcer`.

- [ ] **Step 3: Implement**

```go
package capability

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrDenied means the action was refused. Callers surface it to the agent
	// as a denial it can act on, never as a silent no-op.
	ErrDenied = errors.New("capability denied")
	// ErrAskRequired means a decision needed a human and no asker was wired.
	ErrAskRequired = errors.New("capability requires approval but no asker is configured")
)

// Asker routes an ask-effect decision to whoever answers it.
type Asker interface {
	Ask(ctx context.Context, d Decision) (bool, error)
}

// ServerEnforcer intercepts in-process application calls. It is the only
// enforcement point with complete coverage: nothing routes around it, and
// unlike the hook it cannot be bypassed by a timeout.
type ServerEnforcer struct {
	Asker Asker
}

// Point identifies this enforcement point.
func (ServerEnforcer) Point() string { return EnforcerServer }

// Enforce returns nil when the action may proceed.
func (e ServerEnforcer) Enforce(ctx context.Context, d Decision) error {
	switch d.Effect {
	case EffectAllow:
		return nil
	case EffectDeny:
		return fmt.Errorf("%w: %s", ErrDenied, d.Reason)
	case EffectAsk:
		if e.Asker == nil {
			// A missing asker is a wiring bug. Failing closed is the only safe
			// reading: an enforcer that allows what it cannot adjudicate is
			// worse than one that is absent.
			return fmt.Errorf("%w: %s", ErrAskRequired, d.Reason)
		}
		ok, err := e.Asker.Ask(ctx, d)
		if err != nil {
			return fmt.Errorf("capability ask: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: refused when asked", ErrDenied)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown effect %q", ErrDenied, d.Effect)
	}
}
```

Note the `default` branch denies. An effect this enforcer does not recognise is a version skew, and skew must not open a gate.

`ErrAskRequired` does not wrap `ErrDenied`, so the test asserting a nil asker denies must check both — adjust the test to `errors.Is(err, capability.ErrAskRequired)` if you prefer that distinction, but the call must not succeed either way. Decide, and make the test say which.

- [ ] **Step 4: Run to verify**

Run: `cd server && go test -race ./internal/capability/ -run TestServerEnforcer -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l server/internal/capability/
git add server/internal/capability/
git commit -m "feat(capability): add the in-process enforcement point

This is the complete one: application calls run through it in-process, so
nothing routes around it and no timeout can bypass it.

Both failure paths fail closed. A nil asker is a wiring bug and an unknown
effect is version skew, and an enforcer that allows what it cannot adjudicate
is worse than no enforcer at all."
```

---

### Task 8: Rate limits

**Files:**
- Create: `server/internal/db/ent/schema/grant_usage.go`
- Create: `server/internal/db/repo/grant_usage_repo.go`
- Modify: `server/internal/capability/enforcer_server.go`
- Test: `server/internal/capability/limit_test.go`, `server/internal/db/repo/grant_usage_repo_test.go`
- Regenerated and committed: `server/internal/db/ent/**`

**Interfaces:**
- Produces: ent entity `GrantUsage` with `grant_id string` and `used_at time`; `repo.GrantUsageRepo` with `Record(ctx, grantID string) error` and `CountSince(ctx, grantID string, since time.Time) (int, error)`; `capability.WithinLimit(g GrantView, usedInWindow int) bool`.

- [ ] **Step 1: Write the failing pure test**

```go
func TestWithinLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		used  int
		want  bool
	}{
		{"zero limit means unlimited", 0, 9999, true},
		{"under the limit", 3, 2, true},
		{"at the limit is exhausted", 3, 3, false},
		{"over the limit", 3, 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := capability.GrantView{LimitCount: tt.limit, LimitWindowSeconds: 3600}
			if got := capability.WithinLimit(g, tt.used); got != tt.want {
				t.Errorf("WithinLimit(limit=%d, used=%d) = %v, want %v", tt.limit, tt.used, got, tt.want)
			}
		})
	}
}
```

"At the limit is exhausted" is the boundary that matters: a limit of 3 permits three calls, not four.

- [ ] **Step 2: Run red, then implement `WithinLimit`**

Pure function, no clock, no database — the caller supplies the count.

- [ ] **Step 3: Write the failing sliding-window test**

Seed usage rows at known times through the repo, then assert `CountSince` excludes rows older than the window. Pass the times in explicitly rather than sleeping; a test that sleeps to prove a window is a slow test that still proves nothing about the boundary.

- [ ] **Step 4: Wire the limit into `ServerEnforcer`**

An exhausted limit yields `EffectAsk`, **never a silent deny**, and the reason names the limit — spec §6. A user who hits a cap must be told which cap, not merely refused.

- [ ] **Step 5: Verify and commit**

```bash
cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/capability/ ./internal/db/...
git add server/internal/db/ent/ server/internal/db/repo/ server/internal/capability/
git commit -m "feat(capability): enforce grant rate limits by asking, not refusing

An exhausted limit produces an ask whose reason names the limit. Refusing
silently would be indistinguishable from having no grant at all, and the user
could not tell which cap they hit.

A limit of three permits three calls: the boundary case is pinned by test
because off-by-one here is a permission bug, not a counting bug."
```

---

### Task 9: Hook enforcer, with its posture declared

**Files:**
- Modify: `server/internal/api/hooks/permission.go` — route through the Decider; rename `PermissionBridge` to `HookEnforcer` per the conventions spec
- Modify: call sites of the rename (`server/internal/api/router.go`, `server/internal/agentbroadcast/permission_enricher.go`, DI wiring)
- Test: `server/internal/api/hooks/permission_test.go` (extend)

**Interfaces:**
- Produces: `hooks.HookEnforcer` with `Point() string` returning `capability.EnforcerHook`. Every existing exported method keeps its name and signature; only the type name and the decision source change.

**The hook stays fail-open, and that is not a bug to fix.** Its timeout budget is unchanged — a 25 s server hold inside a 28 s curl budget inside a 30 s Claude Code timeout, so the side that gives up first is ours and Claude Code falls back to drawing its own terminal prompt. Making it fail closed would mean a dashboard outage blocks every hand-started session.

What changes: the posture becomes a declared property. A capability whose `enforceable_by` omits `hook` is one the UI must not present as protected for hand-started sessions.

- [ ] **Step 1: Write the failing test**

```go
func TestHookEnforcerTimeoutYieldsNoDecision(t *testing.T) {
	// Construct an enforcer with a hold shorter than the test's patience,
	// submit a request nobody answers, and assert the response is the
	// no-decision object rather than an allow or a deny.
	//
	// The assertion that matters: the body is Claude Code's "carry on as
	// usual" shape, so the session falls back to its own prompt. A test that
	// only checked for a non-error would pass on a silent allow.
}

func TestHookEnforcerPoint(t *testing.T) {
	if got := (&hooks.HookEnforcer{}).Point(); got != capability.EnforcerHook {
		t.Errorf("Point() = %q, want %q", got, capability.EnforcerHook)
	}
}
```

Write the first one out against the existing test file's helpers — it already constructs bridges and drives holds; follow its shape rather than inventing a new harness.

- [ ] **Step 2: Run red, then rename and route**

The rename is mechanical but touches wiring: `grep -rn "PermissionBridge" server/ --include='*.go'` and change every site. The build is the check — an unrenamed reference does not compile.

- [ ] **Step 3: Verify**

Run: `cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/api/hooks/`
Expected: PASS, including the pre-existing hold, arming and deny-rule tests.

- [ ] **Step 4: Commit**

```bash
git checkout -- server/internal/db/ent/
gofmt -l server/internal/api/hooks/
git add server/internal/api/ server/internal/agentbroadcast/
git commit -m "refactor(hooks): route the terminal permission hold through the capability gate

The hold keeps failing open on timeout, deliberately: the budget is sized so
our side gives up first and Claude Code draws its own prompt, and a dashboard
outage must not block every hand-started session.

What changes is that the posture is now declared rather than implicit. A
capability whose enforceable_by omits the hook is one this path cannot
guarantee, and the UI can say so instead of implying uniform protection."
```

---

### Task 10: Backfill migration

**Files:**
- Modify: `server/internal/db/client.go` — a post-migration plus its call site
- Test: `server/internal/db/client_test.go` (append)

**Interfaces:**
- Produces: `migrateBackfillGrants(db *sql.DB) error`.

Every `task_permission` row becomes a grant with `context_kind = task`, `context_ref = task_id`, `mode = allow`, and `capability_name` taken from the tool name. Every `permission_preset` row becomes a grant with `context_kind = project`, `context_ref = project_cwd`.

- [ ] **Step 1: Write the failing test**

```go
func TestBackfillGrantsIsIdempotent(t *testing.T) {
	// Open an in-memory database, seed one task_permission and one
	// permission_preset through their repos, then run the migration twice.
	//
	// Assert: after the first run the grant count is 2; after the second it is
	// still 2. A migration that runs on every boot must be a no-op once
	// settled, and the second-run assertion is the only thing that proves it.
}

func TestBackfillGrantsMarksLegacyIdentity(t *testing.T) {
	// Assert every backfilled grant has granted_by == "migration:legacy".
	//
	// granted_by is required and the legacy rows carry no identity. An empty
	// string would be indistinguishable from a bug; the marker says "unknown
	// because it predates identity" out loud.
}
```

Write both out against the repo helpers the `db` package tests already use.

- [ ] **Step 2: Run red, then implement**

Guard the insert with a `NOT EXISTS` on `(capability_name, context_kind, context_ref, pattern)`, mirroring the guard style `migrateCopyAuditLogsToAuditEvents` already uses in this file.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/db/...
git checkout -- server/internal/db/ent/
git add server/internal/db/
git commit -m "feat(db): backfill existing permissions and presets as grants

Runs on every boot and inserts nothing once settled, guarded by a NOT EXISTS
on the grant's identifying columns in the same shape as the audit-log copy
migration beside it.

Backfilled grants carry granted_by = migration:legacy rather than an empty
string, so a later audit can tell rows that predate identity from rows where
somebody forgot to record it."
```

---

### Task 11: Normalization cleanup

Four removals and one schema correction. Each is small; together they remove constructs that actively mislead.

**Files:**
- Modify: `server/internal/db/ent/schema/task_permission.go` — drop `pre_approved`
- Modify: `server/internal/permissions/allowlist.go` — drop `WriteToolNames`
- Modify: `server/internal/db/ent/schema/permission_preset.go` — empty-string sentinel for `user_id` and `pattern`
- Modify: `server/internal/api/tasks/permission_service.go`, `permission_request_routes.go`, `handler.go` — write `decided_by` and `decided_at`
- Test: one focused test per behaviour change
- Regenerated and committed: `server/internal/db/ent/**`

Each item's justification, verified at plan time:

| Item | Evidence | Action |
|---|---|---|
| `pre_approved` | four writers, **zero readers**; no decision depends on it | drop the column |
| `WriteToolNames` | exported slice, **zero non-test readers**; `IsWriteTool` is the real source | drop the slice, keep the function |
| `permission_preset` unique index | its own schema comment says SQLite treats two NULLs as distinct, so duplicate `(NULL user_id, cwd, tool, NULL pattern)` rows are possible today | empty-string sentinel on both nullable columns, matching `pipeline_config.project_id` |
| `decided_by` / `decided_at` | never written outside generated ent, which is why "who allowed this" is unanswerable | wire on every resolve path |

- [ ] **Step 1: Write the failing tests**

For `decided_by`: resolve a permission request through each of the three paths that answer one — the REST single resolve, the REST bulk resolve, and the MCP tool — and assert the stored row carries a non-empty `decided_by`. Three assertions, because three paths is exactly how the field ended up written by none of them.

For the preset sentinel: insert the same `(cwd, tool)` twice with a nil user and a nil pattern, and assert the second insert is refused. That test fails today.

- [ ] **Step 2: Run red, then apply each change**

Dropping a column regenerates ent. Confirm the generated migration drops it cleanly on an existing database before committing — SQLite column drops are a rebuild, and this schema has already crashed once on one.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/db/... ./internal/api/... ./internal/permissions/...
git add server/internal/db/ent/ server/internal/db/ server/internal/api/ server/internal/permissions/
git commit -m "refactor(permissions): remove write-only fields and record decision identity

pre_approved had four writers and no readers; WriteToolNames was exported with
no non-test reader while IsWriteTool did the actual work. Both are gone.

decided_by and decided_at are now written on all three resolve paths. They
existed as columns and were set by none of them, which is why this system
could not answer who allowed a given action.

permission_preset's unique index now holds: its nullable columns use the
empty-string sentinel, because SQLite treats two NULLs as distinct and the
index has therefore never prevented the duplicates its own comment warns about."
```

---

### Task 12: Documentation and the full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` — the permissions bullet
- Modify: `docs/guides/security.md` — the three enforcement points and their postures
- Modify: `PRIVACY.md` — the per-table disclosure list gains `capabilities`, `grants` and `grant_usages`

The security guide is the honest core of this unit. A reader must be able to learn, without reading Go, that the terminal-session hook fails open on timeout and what that means for them.

- [ ] **Step 1: Write the documentation**

In `docs/guides/security.md`, add a section covering:

- what a capability is, and why tool names were not enough
- the three enforcement points, in a table: server (complete), spawn (complete for agents the dashboard spawned, cannot ask), hook (fails open on timeout, falls back to the terminal prompt)
- that a capability declares where it is enforceable, and that the UI shows this where a grant is made

State the fail-open behaviour plainly. Do not bury it in a parenthesis.

- [ ] **Step 2: Run the full gate and paste raw output**

```bash
go build ./server/...
cd server && go vet ./...
cd sdk && go vet ./...
task test
gofmt -l <every changed Go file>
```

`task lint` is broken in local environments for toolchain reasons unrelated to this branch; CI runs a pinned version and is the authority. Record the local failure text, do not attempt to repair the linter.

- [ ] **Step 3: Verify the ent tree**

Run: `git status --short server/internal/db/ent/`
Expected: empty. Tasks 2, 3, 8 and 11 committed regenerations deliberately; anything appearing now is test-run noise, so `git checkout -- server/internal/db/ent/`.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md docs/guides/security.md PRIVACY.md
git commit -m "docs: document the capability gate and its three enforcement points

The security guide now states plainly that the terminal-session hook fails
open on timeout and falls back to Claude Code's own prompt. That is the one
property of this system a reader must not have to infer from the source."
```

- [ ] **Step 5: Stop**

Do **not** push and do **not** open a pull request. Both are outward-facing and belong to the human, not to an automated loop.

---

## Out of scope for this plan

| Item | Where it belongs |
|---|---|
| Memory store, retrieval, delivery | Plan 3 (K3) |
| Obsidian application and effort tuning | Plan 4 (A1, S2) |
| Cross-node grant synchronisation | V2, with the node registry |
| Team roles and shared grants | Deferred with everything team-shaped |
| Replacing the static Bash allow-list with a parser | Crude but fails safe; replacing it is a security change deserving its own spec |
| Web push for pending permission requests | Real gap — `webpush.Service.SendToAll` has zero production callers — but it is a notification feature, not a gate feature |
| Adopting `WithTx` in the repositories that still hand-roll transactions | Inherited from K1's out-of-scope list; still true |
| Routing the four hardcoded slug-pattern messages through `validation.SlugPatternMessage` | Inherited from K1's out-of-scope list; still true |
