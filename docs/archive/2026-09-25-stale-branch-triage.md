# Stale Branch & Worktree Triage — 2026-09-25

Analysis-only. No branch, worktree or file was modified during this triage.
All git evidence collected from the `stale-branch-worktree-triage` worktree on `chore/stale-branch-worktree-triage`.

---

## Decision Table

| # | Item | Recommendation | Evidence |
|---|------|----------------|----------|
| 1 | `feat/quick-wins-multi-provider-metrics` | **Discard** | All 3 concerns superseded on develop |
| 2 | `fix/tool-activity-detail` | **Discard** | Both commits superseded on develop |
| 3 | `test/all-fixes` + `local-test` worktree | **Discard** | All 6 commits merged to develop as PRs #312–#318 |
| 4 | `~/dashboard-worktrees/_freeze` | **No action** | Directory absent |
| 5 | `.claude/worktrees/agent-*` (4 dirs) | **Remove** | 3 stale harness workers + 1 broken foreign-repo link; operator confirms |

---

## Item 1 — `feat/quick-wins-multi-provider-metrics`

### Branch state

```text
git log origin/develop..feat/quick-wins-multi-provider-metrics --oneline
15be022d chore: checkpoint multi-provider metrics work
00a32d5e feat(quick-wins): multi-provider detection, metrics panel, markdown utils, SDK types
e6828507 feat: auto-create git worktrees for pipeline tasks with sourceBranch
```

- Branched from: `f29c9b65` (2026-05-20)
- Last commit: 2026-08-17
- Module path: `github.com/lx-wnk/agent-dashboard` (pre-kontor rename)

### Supersession evidence

**e6828507 — worktree auto-creation:** develop has `server/internal/worktree/` with
`BranchCheckedOutAt`, `DefaultRoot`, `PathFor`, `CreateBranch` across PRs #149, #201, #202,
#250. The branch's inline `ensureTaskWorktree` (61 lines, direct git exec) is a prior
generation.

**00a32d5e — multi-provider detection:** develop has `server/internal/provider/` with
`adapter.go`, `descriptor.go`, `engine.go`, `embed.go`, `enabled.go`, `ollama.go` — a full
registry with `ProviderDetector` interface injected into the scanner (PR #213 "pluggable
opt-in agent providers"). The branch's `server/internal/parser/providers.go` (134 lines) does
not exist on develop and is superseded by this package.

`ProviderBadge.vue` already exists on develop (PR #77, updated in PR #194).

**15be022d — checkpoint:** `SystemMetricsPanel.vue`, `useSystemMetrics.ts`, format/markdown
utils and tests. Some overlap with develop's post-#376 tool history work.

### Recommendation

**Discard.** All three commit scopes are covered by develop. Module rename (`5b1c316d`) makes
mechanical rebase impractical. Clean reimplementation of any remaining gaps (if any) is
cheaper.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
# Remove worktree first
git worktree remove --force \
  /Users/alexanderwink/dashboard-worktrees/quick-wins-multi-provider-metrics

# Then delete branch
git branch -D feat/quick-wins-multi-provider-metrics
git push origin --delete feat/quick-wins-multi-provider-metrics 2>/dev/null || true
# ══════════════════════════════════════════════════
```

---

## Item 2 — `fix/tool-activity-detail`

### Branch state

```text
git log origin/develop..fix/tool-activity-detail --oneline
74398cae feat(agents): show what each recent tool call actually did
04572084 refactor(agents): remove the broken waterfall view from the agent modal
```

- Branched from: `22ac5a56` (2026-08-18)
- Last commit: 2026-08-18
- Module path: `github.com/lx-wnk/agent-dashboard` (pre-kontor rename)

### Supersession evidence

**04572084 — waterfall removal:** Commit `7c4e0cd2` on develop has the identical message
("refactor(agents): remove the broken waterfall view from the agent modal") and identical
date (2026-08-18 22:11:29). `ExecutionWaterfall.vue` does not exist on develop. Already done.

**74398cae — tool-call detail:** Commit `0cda3670` on develop ("fix(agents): honest stalled
cards, useful tool history, working desktop hot-reload", PR #376, 2026-08-25) addresses the
same concern — making the tool history useful. `AgentModal.vue` has since been refactored
into `src/features/agents/` (#297), and the modal body extracted into `AgentSessionPane`
(`5a3bf2f2`). The branch targets files that no longer exist at those paths.

### Recommendation

**Discard.** Both commits are superseded. The waterfall removal already landed on develop.
The tool-call detail concept was absorbed into PR #376.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
git worktree remove --force \
  /Users/alexanderwink/dashboard-worktrees/tool-activity

git branch -D fix/tool-activity-detail
git push origin --delete fix/tool-activity-detail 2>/dev/null || true
# ══════════════════════════════════════════════════
```

---

## Item 3 — `test/all-fixes` + `local-test` worktree

### Branch state

```text
git cherry -v origin/develop test/all-fixes
+ da0faa72 feat(pwa): maskable manifest icon (SEO-P3-1b) + launch/discoverability playbook
+ 594a06e5 build(taskfile): rename desktop:build -> build:desktop, add to build:all, add dev:desktop
+ de39bf84 fix(channel): submit injected prompts by writing the CR separately (Bug 4)
+ 9a47c421 feat(ui): hover-reveal card prompt, status/badge clarity, output fade, MEM warning color
+ 768bf209 fix(prompt): image @-path attach + hide empty template picker (Bug 1/2/3)
+ 6261f057 fix(prompt): paste images from the clipboard (Cmd/Ctrl+V), not just the file picker
```

- Last commit: 2026-07-15
- All 6 show `+` (SHA not on develop) — but they were squash-merged with different SHAs

### Supersession evidence (topic-by-topic)

| Branch commit | Develop equivalent | PR |
|---|---|---|
| `da0faa72` maskable icon | `bdae87a3` | #312 |
| `594a06e5` taskfile rename | `bc2ffb9e` | #315 |
| `de39bf84` channel CR fix | `282ad6ee` | #316 |
| `9a47c421` hover-reveal | `d854cebe` | #318 |
| `768bf209` image @-path | `d1160402` | #317 |
| `6261f057` clipboard paste | Present in develop's `PromptInput.vue` (same `onPaste` handler) | via #317 |

The `local-test` worktree at `dashboard-worktrees/local-test` still exists in `git worktree
list` (at `64c3dd13`).

### Recommendation

**Discard.** All content on develop. Zero content loss.

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
# Remove worktree
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/dashboard-worktrees/local-test

# Delete branch
git branch -D test/all-fixes
git push origin --delete test/all-fixes 2>/dev/null || true
# ══════════════════════════════════════════════════
```

---

## Item 4 — `~/dashboard-worktrees/_freeze` tarballs

### Evidence

```text
ls ~/dashboard-worktrees/_freeze
# → No such file or directory
```

The stalled-card investigation (recorded 2026-08-31) produced commits `d015cb5c` ("freeze")
and `71a460b2` ("archive") on develop itself — the artifacts were committed and the directory
was removed. Investigation is closed.

### Recommendation

**No action.** Investigation closed by absence — artifacts are in the git history.

---

## Item 5 — `.claude/worktrees/agent-*` harness worktrees

Location: `/Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/`

### Evidence

| Directory | Branch | Tip | Status | Notes |
|---|---|---|---|---|
| `agent-a0ac1a8efd7b4611d` | `worktree-agent-a0ac…` | `4729f24d` (#421, 2026-09-02) | No live process | Rename drift: 782 modified files |
| `agent-a3eec7efca2722403` | `worktree-agent-a3ee…` | `4729f24d` (#421, 2026-09-02) | No live process | Rename drift: 781 modified files |
| `agent-af3438375a35d2185` | `worktree-agent-af34…` | `4729f24d` (#421, 2026-09-02) | No live process | Rename drift: 782 modified files |
| `agent-a87f91179028f8d97` | **ERROR** | — | Dead | Broken foreign-repo link to `claude-agent-overview` |

All three active worktrees share commit `4729f24d` (PR #421). The 782 modified files are the
kontor module rename (`5b1c316d` on develop) showing as local diffs — no actual work was done
after that rename. No `ps aux` match for any agent session ID.

`agent-a87f91179028f8d97` is not registered in `git worktree list`. Its `.git` file points to
`/Users/alexanderwink/code/_privat/claude-agent-overview/.git/worktrees/agent-a87f91179028f8d97`
which no longer exists — a broken link from a different repository.

### Recommendation

**Remove all four.** The `worktree-agent-*` branches are purely local (never pushed).

### Operator commands (when ready to act)

```bash
# ══ LOKAL ═════════════════════════════════════════
# 1. Verify no pipeline task references these paths
sqlite3 "$DASHBOARD_DB_PATH" \
  "SELECT slug, worktree_path FROM tasks WHERE worktree_path LIKE '%agent-a%';"

# 2. If empty: remove registered worktrees
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a0ac1a8efd7b4611d
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a3eec7efca2722403
git worktree remove --force \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-af3438375a35d2185

# 3. For the broken foreign-repo entry: plain rm
rm -rf \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/.claude/worktrees/agent-a87f91179028f8d97

# 4. Clean up local worktree-agent-* branches
git branch -D worktree-agent-a0ac1a8efd7b4611d \
               worktree-agent-a3eec7efca2722403 \
               worktree-agent-af3438375a35d2185
# ══════════════════════════════════════════════════
```

---

*Generated 2026-09-25 — analysis only, no files were mutated.*
