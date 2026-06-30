# Per-Turn Checkpoint / Revert — Design Spec

> Date: 2026-06-30 · Status: Approved · Branch: `feat/checkpoint-revert` (off `upcoming`)
> Competitor-gap B2 (from Conductor). Gives a pipeline task an undo history of its worktree as the agent edits.

## Why

Pipeline tasks run an agent in a git worktree; the agent edits files across many turns, and only the finalization stage commits. If the agent takes a wrong turn (deletes the wrong file, goes down a bad path), there is no way to roll the worktree back short of discarding the whole task. B2 adds a continuous, per-turn snapshot history with a revert action — a safety net / time machine for agent edits.

## Decisions (user-approved)

| # | Decision | Rationale |
|---|---|---|
| D1 | Capture a checkpoint **per agent turn** | Fine-grained undo — can roll back a single bad turn, not just a whole stage. |
| D2 | Trigger via a **gitignore-aware filesystem watcher + ~2s debounce** on the worktree | The dashboard observes the agent (tails JSONL); it does not drive the turn loop. A debounced FS watcher captures every meaningful edit ≈ per turn, provider-agnostic, no agent cooperation. |
| D3 | Store each checkpoint as a hidden git ref `refs/checkpoints/<taskId>/<seq>` (full tree, tracked + untracked, gitignore-respected) via a `git stash create`-style tree+commit | Captures complete worktree state without touching the agent's index/working files or polluting the branch. gitignore-respect keeps `node_modules`/`dist` out → small trees. |
| D4 | **Revert** = kill the live agent (if running) → snapshot current state (pre-revert, undoable) → hard-restore the worktree to the checkpoint → **park the task** (`awaiting_user`) for manual resume | Safe; never restores under a live writer; revert is itself reversible; does not auto-entangle the orchestrator. |

## Scope

In: a `checkpointer` (watcher + debounced snapshotter) started/stopped with a task's worktree; a `checkpoint` ent table; snapshot/restore via git plumbing; `GET /api/tasks/{id}/checkpoints` + `POST /api/tasks/{id}/checkpoints/{cpId}/revert`; SSE `checkpoint_added`; a checkpoint timeline + Revert button in the task modal; retention/prune + cleanup on worktree removal.

Out: auto re-run of the pipeline after revert (parked for manual resume — D4); restore-into-new-branch mode; per-checkpoint diff viewer (timeline shows summary only; a full diff viewer is B3, separate); cross-task checkpoint browsing.

## Architecture

### Checkpointer (`server/internal/checkpoint/`)
- `Checkpointer` manages one watcher per active task worktree. `Start(taskID, worktreePath)` launches an fsnotify watcher (recursive, but **ignoring** `.git/`, and anything gitignored — load the worktree's ignore rules; at minimum hard-skip `.git`, `node_modules`, `dist`). `Stop(taskID)` tears it down.
- On a filesystem event, reset a debounce timer (default `checkpointDebounceMs`, ~2000). When it fires (quiet period elapsed), call `snapshot(taskID, worktreePath)`.
- `snapshot`: build a tree of the worktree honoring gitignore and capturing untracked files (mechanism: `git add -A` into a **temporary index** (`GIT_INDEX_FILE` pointing at a temp file, never the real index) → `git write-tree` → `git commit-tree <tree> -p <prevCheckpointCommit>` with a fixed message → update `refs/checkpoints/<taskId>/<seq>`). The real working index and HEAD are never touched. If the tree equals the previous checkpoint's tree (no change), skip (no-op). Persist a `checkpoint` row + emit SSE.
- Best-effort: any git/watcher error logs `slog.Warn` and the task continues unaffected.
- Retention: after inserting, if the task has > `checkpointMaxPerTask` (default 50) checkpoints, delete the oldest refs + rows (keep the chain head reachable). On worktree removal, delete all `refs/checkpoints/<taskId>/*` and the rows (or mark stale — see Error handling).

### Wiring (`server/cmd/serve/di_*.go` + worktree lifecycle)
- Start the checkpointer when a task worktree becomes active (the same seam that creates the worktree — alongside `EnsureWorktreeFn`/`SetupWorktreeFn` in the orchestrator path), Stop it when the stage run ends or the worktree is removed. Injected as a nil-safe seam on the orchestrator options (no checkpointer in the no-DB/test path).
- Reuse the existing worktree-removal path to prune refs.

### Restore / revert (`checkpoint` package + `server/internal/api/tasks` or a new `api/checkpoints` handler)
- `Revert(ctx, taskID, checkpointID)`:
  1. Resolve the checkpoint's `commit_sha` + the task's worktree path. If the worktree is gone → `ErrWorktreeMissing` (409/410).
  2. If a stage run is actively running for the task, kill the live agent via the existing stop path (`StopFn`/process kill). If kill fails → abort with error (do not restore).
  3. Take a per-worktree lock (reuse/extend the existing worktree mutex if present, else a package-level keyed mutex).
  4. Snapshot the CURRENT worktree as a fresh checkpoint labelled pre-revert (so revert is undoable).
  5. Restore: `git read-tree <checkpoint tree>` into a temp index then `git checkout-index -a -f --prefix=<worktree>/`, plus remove tracked-but-now-absent files and clean untracked files that aren't in the snapshot (so the worktree exactly matches the snapshot tree). Equivalent to a hard reset of the working dir to the snapshot tree, gitignore-aware. HEAD/branch ref is left as-is (we restore the *working tree*, not branch history).
  6. Park the task: set status to `awaiting_user` with a `pending_user_prompt`/note like "reverted to checkpoint <seq>". Emit SSE.
- `List(ctx, taskID)` → ordered checkpoints (newest first) with seq, stage, files_changed, created_at, and a `pre_revert` flag.

### API (`server/internal/api/...` + router)
- `GET /api/tasks/{id}/checkpoints` (JWT group) → `[]CheckpointView`.
- `POST /api/tasks/{id}/checkpoints/{cpId}/revert` (JWT group; Origin-checked) → reverts, returns the updated task + the new pre-revert checkpoint. Reverting destroys working state → require the same auth posture as other task mutations.

### Frontend (`src/components/` task modal + composable)
- `useCheckpoints(taskId)`: fetch list, subscribe to `checkpoint_added` SSE on the existing task stream, expose `revert(cpId)`.
- A `CheckpointTimeline.vue` tab/section in the task modal: reverse-chronological list (seq · relative time · stage · "N files"), a Revert button per row with a destructive confirm dialog. The newest entry after a revert is the auto-captured pre-revert snapshot. Empty state when no checkpoints yet.

## Data model (ent)
New `checkpoint` table:
- `id` (string, immutable), `task_id` (string, immutable, indexed), `stage_run_id` (string, optional/nillable), `seq` (int), `commit_sha` (string), `files_changed` (int), `pre_revert` (bool, default false), `created_at` (time, default now).
- Index on `task_id`. No change to existing tables.

## Data flow
```
worktree active → Checkpointer.Start → fsnotify (ignore .git/node_modules/dist)
  → agent writes files → debounce ~2s → snapshot (temp-index write-tree → commit-tree → refs/checkpoints/<task>/<seq>)
  → INSERT checkpoint row → SSE checkpoint_added → timeline updates
  → retention prune (> 50)

revert(taskID, cpId) → [kill live agent if running] → worktree-lock
  → snapshot current (pre_revert=true) → restore worktree to checkpoint tree → park task (awaiting_user) → SSE
worktree removed → delete refs/checkpoints/<task>/* + rows
```

## Error handling
- Watcher/snapshot failure → `slog.Warn`, task unaffected (checkpointing is best-effort, never blocks the agent).
- Identical-tree snapshot → skipped (no duplicate checkpoint for a no-op debounce).
- Revert while agent running → kill first; kill failure → abort revert with an error (worktree not modified).
- Worktree missing at revert → `ErrWorktreeMissing` (4xx), checkpoint marked unrevertable in the UI.
- Concurrent revert / snapshot during revert → serialized by the per-worktree lock.
- Node_modules / large dirs → never watched or snapshotted (gitignore-aware + hard-skip), bounding cost.

## Testing
- **Checkpointer:** temp git worktree; write a file → after debounce a `refs/checkpoints/...` ref + `checkpoint` row exist; the snapshot tree restores byte-identical (including an untracked file); a second identical state does NOT create a new checkpoint; `node_modules/x` change does NOT create a checkpoint; retention prunes to N. Use an injectable clock/debounce + a fake DB to keep it deterministic.
- **Revert:** snapshots a pre-revert checkpoint, restores the worktree exactly to the chosen tree (adds back deleted files, removes files added after), parks the task `awaiting_user`; with a fake "running stage" it calls the kill seam first and aborts if kill errors; worktree-lock prevents interleave.
- **API:** list returns ordered views; revert endpoint wires to `Revert` and returns the updated task; Origin/auth enforced.
- **Frontend:** `useCheckpoints` renders the timeline from a mocked list, appends on a mocked `checkpoint_added` SSE event, and `revert()` posts the right endpoint after the confirm.

## Risks / notes
- fsnotify recursion + ignore rules: must add new subdirectories to the watch as they appear but skip ignored ones; the gitignore-aware *snapshot* (git respects ignore) is the real guard on tree size even if the watcher sees an ignored write.
- Debounce tuning: too short → many near-identical snapshots; too long → a fast turn's intermediate state is missed. Default ~2s, configurable via a setting.
- Object growth: hidden refs accumulate loose objects; prune drops refs, and an occasional `git gc --auto` in the worktree keeps it bounded (note, not required for v1).
- This is the worktree-only (no-push) state; checkpoints are local to the worktree and disappear with it — acceptable (they are an in-flight undo history, not durable artifacts).
- Per-provider agnostic: because capture is filesystem-based, it works for Claude, Codex, Gemini, etc. equally.
