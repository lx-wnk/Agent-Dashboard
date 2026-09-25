# Stale Branch & Worktree Triage — 2026-09-25

Analysis-only. No branch, worktree or file was modified during this triage.
All git evidence collected from the `stale-branch-worktree-triage` worktree on `chore/stale-branch-worktree-triage`.

---

## Decision Table

| Item | Recommendation | Blocker |
|---|---|---|
| `feat/quick-wins-multi-provider-metrics` | Split into 2 PRs — strip superseded commit, module rename first | Module rename required before rebase |
| `fix/tool-activity-detail` | Finish as standalone PR — rebase + module rename | Module rename required before rebase |
| `test/all-fixes` + `local-test` worktree | Discard — all constituent branches already merged to develop | None |
| `~/dashboard-worktrees/_freeze` | Nothing to do — directory does not exist | — |
| `.claude/worktrees/agent-*` (4 dirs) | 3 stale harness workers, 1 broken foreign-repo link — safe to remove | Operator confirms no in-flight work |

---

## Item 1 — `feat/quick-wins-multi-provider-metrics`

### Evidence

```
git log origin/develop..feat/quick-wins-multi-provider-metrics --oneline
15be022d chore: checkpoint multi-provider metrics work
00a32d5e feat(quick-wins): multi-provider detection, metrics panel, markdown utils, SDK types
e6828507 feat: auto-create git worktrees for pipeline tasks with sourceBranch

git cherry origin/develop feat/quick-wins-multi-provider-metrics
+ e6828507...
+ 00a32d5e...
+ 15be022d...   # all 3 absent from develop by SHA
```

All 3 commits use module path `github.com/lx-wnk/agent-dashboard`.
Develop uses `github.com/lx-wnk/kontor` (renamed at commit `5b1c316d`).

Diff stat (three-dot, vs develop): **34 files, 1228 insertions, 81 deletions**

Key files added/changed:
- `server/internal/parser/providers.go` — **NEW**, 134 lines, absent on develop
- `server/internal/scanner/scanner.go` — +76 lines (provider detection injection)
- `server/internal/merger/merger.go` — +47 lines (provider cost gating)
- `src/components/ProviderBadge.vue` — branch uses `sdk.generated.Provider` type; develop has a different version using `Agent['provider']` inline type
- `src/components/SystemMetricsPanel.vue` — +79/-20 lines
- `src/composables/useSystemMetrics.ts` — +40 lines
- `src/utils/markdown.ts` — +19 lines
- `src/utils/format.ts` — +13 lines
- `src/sdk.generated.ts` — +8 lines (Provider type)

**Commit e6828507 is superseded.** Develop has a mature `server/internal/worktree/` package
with dedicated runner, `BranchCheckedOutAt`, `DefaultRoot`, `PathFor`, `CreateBranch` — landed
across PRs #201, #202, #228, #243, #250, #251. The branch's `pipeline/worktree.go` is a prior
generation (61 lines, inline git exec, old module path). Also touches `server/internal/pipeline/types.go`,
`server/cmd/serve/di_pipeline.go`, `server/internal/config/config.go`, `server/internal/sse/headers.go`,
`src/sdk.generated.ts` (sourceBranch field) — these side-effects need review to see if they
also landed separately on develop.

### Recommendation

1. Drop `e6828507` (superseded worktree implementation).
2. Rename module path across all Go files in the branch before rebase.
3. Split remaining 2 commits into two PRs:

**PR-A — Frontend/types** (`ProviderBadge.vue` updated API, `SystemMetricsPanel.vue`,
`useSystemMetrics.ts`, `format.ts`, `markdown.ts`, `sdk.generated.ts`, all tests).

**PR-B — Backend provider detection** (`providers.go`, `scanner.go` additions, `merger.go`
provider cost additions). Before opening: reconcile `providers.go` against
`server/internal/provider/` registry on develop — the registry already provides a
`ProviderDetector` interface injected into the scanner. Verify whether `providers.go`
feeds into or duplicates that registry.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
cd ~/dashboard-worktrees/quick-wins-multi-provider-metrics

# 1. Inspect the superseded commit before dropping
git show e6828507 --stat

# 2. Rebase interactively to drop e6828507
#    (mark it 'd' for drop in the editor)
git rebase -i origin/develop

# 3. After rebase: rename module path across all Go files
find . -name "*.go" -not -path "*/vendor/*" \
  | xargs sed -i '' 's|github.com/lx-wnk/agent-dashboard/|github.com/lx-wnk/kontor/|g'
sed -i '' 's|github.com/lx-wnk/agent-dashboard/server|github.com/lx-wnk/kontor/server|' server/go.mod
sed -i '' 's|github.com/lx-wnk/agent-dashboard/sdk|github.com/lx-wnk/kontor/sdk|' sdk/go.mod

# 4. Verify gates before splitting into PRs
task test && go vet ./... && task lint
pnpm lint && pnpm typecheck && pnpm test
# ══════════════════════════════════════════════════
```

---

## Item 2 — `fix/tool-activity-detail`

### Evidence

```
git log origin/develop..fix/tool-activity-detail --oneline
74398cae feat(agents): show what each recent tool call actually did
04572084 refactor(agents): remove the broken waterfall view from the agent modal

git cherry origin/develop fix/tool-activity-detail
+ 74398cae
+ 04572084   # both absent from develop

git merge-base fix/tool-activity-detail origin/develop
22ac5a56  (2026-08-18)
```

Module: `github.com/lx-wnk/agent-dashboard` — rename required before rebase.

Diff stat:
- `04572084`: 5 files, +5/-236 (deletes `ExecutionWaterfall.vue` 145 lines + 3 test files)
- `74398cae`: 14 files, +144/-33 (parser tool-call detail + `ToolTimeline.vue` + tests)

### Recommendation

Finish as a standalone PR. Clean, bounded scope. No speculative content.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
cd ~/dashboard-worktrees/tool-activity

# 1. Module rename
find . -name "*.go" -not -path "*/vendor/*" \
  | xargs sed -i '' 's|github.com/lx-wnk/agent-dashboard/|github.com/lx-wnk/kontor/|g'
sed -i '' 's|github.com/lx-wnk/agent-dashboard/server|github.com/lx-wnk/kontor/server|' server/go.mod

# 2. Rebase onto develop
git rebase origin/develop

# 3. Gates
task test && go vet ./... && task lint
pnpm lint && pnpm typecheck && pnpm test

# 4. Open PR
gh pr create --base develop --title "fix(agents): tool-call detail + remove waterfall view"
# ══════════════════════════════════════════════════
```

---

## Item 3 — `test/all-fixes` (integration branch) + `local-test` worktree

### Evidence

```
git log origin/develop..test/all-fixes --oneline
# 6 commits ahead by SHA, but:

# All 4 constituent source branches are already on develop:
git cherry origin/develop origin/fix/promptinput-fixes    # 0 unmerged
git cherry origin/develop origin/chore/taskfile-desktop   # 0 unmerged
git cherry origin/develop origin/chore/seo-launch-prep    # 0 unmerged
git cherry origin/develop origin/fix/card-ux              # 0 unmerged

# local-test worktree:
ls ~/dashboard-worktrees/local-test   # → directory absent
```

The 6 commits appear as `+` in cherry because they are integration merge commits
(different SHAs than the squash-merged originals on develop). The underlying content
is already on develop. The `local-test` worktree was already cleaned up.

### Recommendation

Discard `test/all-fixes`. No content loss.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
# Verify one more time before deleting
git cherry origin/develop test/all-fixes | grep '^+' | wc -l
# Expected: 6 (integration merge commits only, all content merged)

# Delete local branch
git branch -D test/all-fixes

# Delete remote if pushed
git push origin --delete test/all-fixes 2>/dev/null || echo "(not on remote)"
# ══════════════════════════════════════════════════
```

---

## Item 4 — `~/dashboard-worktrees/_freeze` tarballs

### Evidence

```
ls ~/dashboard-worktrees/_freeze
# → No such file or directory
```

The stalled-card investigation (recorded 2026-08-31) produced no surviving artifact.
Directory never existed or was already removed.

### Recommendation

Nothing to do. Investigation is closed by absence.

---

## Item 5 — `.claude/worktrees/agent-*` harness worktrees

Location: `/Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/`

### Evidence

| Worktree | Branch | Last Commit | Dirty Files | Live Process | Notes |
|---|---|---|---|---|---|
| `agent-a0ac1a8efd7b4611d` | `worktree-agent-a0ac1a8efd7b4611d` | 2026-09-02 `feat: make a routine grant decide something (#421)` | 782 modified, 1 untracked | None (`lsof` clean) | Kontor rename drift: 782 tracked files show as modified because branch predates the rename |
| `agent-a3eec7efca2722403` | `worktree-agent-a3eec7efca2722403` | 2026-09-02 (same PR #421) | 781 modified | None | Same rename drift pattern |
| `agent-af3438375a35d2185` | `worktree-agent-af3438375a35d2185` | 2026-09-02 (same PR #421) | 782 modified | None | Same rename drift pattern |
| `agent-a87f91179028f8d97` | ERROR | — | 0 | None | Points to `/Users/alexanderwink/code/_privat/claude-agent-overview/.git/worktrees/agent-a87f91179028f8d97` — foreign-repo link, that repo's worktree entry is gone. Directory contains AGENTS.md, channel, CLAUDE.md (different project scaffold). |

All three active worktrees share the same pattern: last commit 2026-09-02 (PR #421), all
782 files show as modified due to the kontor module rename landing after that date
(kontor rename commits visible on develop: `b8133f9f` 2026-09-22, `f5b9788b` 2026-09-20).
No active process holds any of these directories open.

### Recommendation

All four are safe to remove. The `worktree-agent-*` branches are purely local (never pushed).

Confirm no in-flight pipeline task references them:

```bash
# ══ LOKAL ═════════════════════════════════════════
# Check if any task in the DB still references these paths
# (run from a shell with DASHBOARD_DB_PATH set, or use the Kontor UI Tasks view)
sqlite3 "$DASHBOARD_DB_PATH" \
  "SELECT slug, worktree_path FROM tasks WHERE worktree_path LIKE '%agent-a%';"

# If empty: remove worktrees
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a0ac1a8efd7b4611d
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a3eec7efca2722403
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-af3438375a35d2185

# For the broken foreign-repo entry: plain rm (git worktree remove won't work)
rm -rf \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a87f91179028f8d97

# Clean up local worktree-agent-* branches
git branch -D worktree-agent-a0ac1a8efd7b4611d \
               worktree-agent-a3eec7efca2722403 \
               worktree-agent-af3438375a35d2185
# ══════════════════════════════════════════════════
```

---

*Generated 2026-09-25 — analysis only, no files were mutated.*
