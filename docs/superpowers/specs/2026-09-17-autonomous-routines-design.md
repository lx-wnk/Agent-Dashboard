# Autonomous Routines — Design Spec

> A routine fires and its work runs without anyone moving a card: either as a lightweight **job** — one agent run, no worktree, not on the board — or as a pipeline task that starts in `ready`. A run that needs a human parks without a time limit, costs nothing while it waits, and every answer — including a refusal — lets it continue.

**Parent:** `2026-08-27-agenticos-overview-design.md` (routines, §6.2)
**Related:** `2026-09-16-mcp-applications-and-mail-design.md` §6.1 (the "job" task shape this spec defines), `2026-09-17-applications-in-the-app-design.md` (sibling spec, same brainstorm)

---

## 1. Status Quo

Every reference was re-read on `main` at `4d4bc3d6`.

### What works

- **Routines fire on a schedule and on demand.** `Scheduler.fireOne` and `RunNow` materialize one task per fire (`server/internal/scheduler/scheduler.go`), carrying the routine id and its attached applications (`server/internal/scheduler/materializer.go:90`).
- **Overlapping fires are already refused.** A fire is skipped while the routine's previous task is not terminal (`scheduler.go:159`, check at `scheduler.go:225-233`).
- **A run can wait without a process.** `WaitUserTransition{AgentDone: true}` clears the PID (`server/internal/pipeline/transitions.go:159`); `ResumeFromUser` continues the session (`server/internal/api/tasks/handler.go:1123`).
- **Agents can ask for a tool** through `request_permission` (`server/internal/channel/bridge.go:140`); a human resolves the request at `POST /api/tasks/{id}/permission-requests/{reqID}/resolve` (`handler.go:1086`).
- **Grants already know a routine context** and rank it above project and global (capability gate, #394–#400; routine grants #421).

### What does not

| # | Gap | Evidence | Consequence |
| --- | --- | --- | --- |
| R1 | A routine's task starts in `backlog` with `spec_gated` autonomy | `db.DefaultStage = "backlog"` (`server/internal/db/defaults.go:9`); the materializer sets no stage; `advance` answers `spec approval requires explicit approve_spec` (`server/internal/api/tasks/advance.go:130`) | Measured 2026-09-16 on an isolated instance: `run-now` produced no stage run in 45 s. A routine never runs an agent unattended |
| R2 | A routine fires once and then never again | The stuck `backlog` task is never terminal, so `overlaps` skips every later fire (`scheduler.go:233`); the skip is only logged (`scheduler.go:159`) | A routine silently stops after its first fire until someone advances that task |
| R3 | Every automated run is a coding pipeline | Fixed `StageOrder` (`server/internal/pipeline/types.go:209`); a worktree is required (`orchestrator.go:182`, `:969`; `progress_guards.go:70`) | Mail triage needs a git repository and runs through implementation, self-review and finalization it has no use for |
| R4 | A refused permission request hangs the run | `resolvePermissionRequest` resumes only on `granted` (`handler.go:1118-1125`) | "Deny" leaves the run in `awaiting_user` forever; until now the 4 h reaper hid it |
| R5 | Human waits are timed like busy agents | `sweepAwaitingUserRuns` anchors a 4 h limit at `StartedAt` for runs with no PID too (`server/internal/pipeline/sweeps.go:43`) | Measured 2026-09-16: a parked plan review failed 60.0 s after start with the limit at 60 s |
| R6 | A failed run loses its output | `FailTransition` replaces the output with `{error}` (`transitions.go:135-142`) | The plan a human was reviewing is gone from the run row |
| R7 | The unused `task_schedule.current_stage` column | Set by `task_schedule_repo.go:135`, never read by the materializer; schema default `backlog` (`task_schedule.go:39`) | Suggests a routine chooses its stage; it does not |
| R8 | No push notification for a permission request | `webpush.Service.SendToAll` (`server/internal/webpush/service.go:89`) has no caller outside the push API handler | A parked run waits unnoticed unless the dashboard is open |

---

## 2. What Changes

### 2.1 Task kind `job`

- `tasks.kind`: `pipeline` (default; every existing row) or `job`. Additive column.
- Stage order depends on kind: `pipeline` keeps `StageOrder`; `job` is `job → done`. `NextStage` takes the kind.
- A job never calls `EnsureWorktreeFn` (`types.go:274`). Its agent runs in the task's `cwd`; no git repository is required. The worktree checks at `orchestrator.go:182`, `:969` and `progress_guards.go:70` consider `kind`.
- Stage `job` is one agent stage. Prompt: the task title and description. System prompt: job-specific, without git, review or finalization instructions. Output via `set_stage_output`: `summary` (what was done) and `result` (free text). A valid output moves the task to `done`.
- Reused unchanged: capability gate and grants in the routine's context, application attachment and secrets, token and cost budgets, SSE updates, parking and resuming (§2.3), cost accounting.
- A job task can never move into a pipeline stage; the transition layer refuses it.

### 2.2 Routine run mode

- `task_schedule.run_mode`: `job` or `pipeline`. New routines default to `job`; the migration sets existing rows to `pipeline`.
- `task_schedule.current_stage` is dropped (R7).
- `task_schedule.last_skipped_at` and `skipped_count` record refused fires (R2).
- On fire and on run-now:
  - `job` → task `kind=job`, stage `job`, autonomy `full`.
  - `pipeline` → task `kind=pipeline`, stage `ready`, autonomy `full`.
- Overlap stays as today and now also covers a run waiting for a human: while the routine's last task is not terminal, the fire is skipped and `skipped_count`/`last_skipped_at` are updated.
- Saving a `pipeline` routine whose `cwd` is not a git repository is refused with a message.

### 2.3 Waiting and permission decisions

1. An agent asks through `request_permission`; the run goes to `awaiting_user`, the agent exits (PID cleared).
2. The request appears in the needs-you band, and a web push is sent (R8).
3. The resolve endpoint takes a `decision` instead of `outcome`:

| Decision | Writes | Then |
| --- | --- | --- |
| `allow_once` | task permission for this run (today's behaviour) | resume |
| `allow_routine` | grant `allow`, context `routine=<schedule id>`, the tool and, for Bash, its pattern | resume |
| `deny_routine` | grant `deny`, context `routine=<schedule id>` | resume, the agent is told the tool is permanently denied |
| `deny_once` | nothing durable | resume, the agent is told the request was refused (R4) |

   `allow_routine` and `deny_routine` exist only for tasks with a `routine_id`. A tool denied by default (sibling spec §2.4) gets no decision buttons: the card informs and the run resumes with the refusal.
4. Resuming uses `ResumeFromUser` with a short prompt stating the decision.

**No time limit for human waits (R5):** the wallclock branch of `sweepAwaitingUserRuns` (`sweeps.go:42-67`) skips runs without a PID. A live agent that busy-waits keeps the 4 h limit. Applies to jobs and pipeline tasks alike.

**Failure keeps output (R6):** `FailTransition` merges `error` into the run's existing output instead of replacing it.

### 2.4 Routine page

`SchedulesView.vue` and `ScheduleForm.vue` grow into the routine page:

- Form: name, schedule (natural language or cron), run mode ("Job" / "Pipeline-Aufgabe"), working directory, instruction, applications, budget.
- Run history per routine: status, `summary`, cost, duration, link to the log, from `GET /api/tasks?kind=job&routineId=…` (and `kind=pipeline` for pipeline routines).
- "N times skipped, last at …" from `skipped_count`/`last_skipped_at`.
- "Run now" shows the new run immediately; live updates over the existing task stream.
- Grants in the routine's context are listed and revocable on the page.

`GET /api/tasks` returns `kind=pipeline` unless `kind` is given, so the board never shows jobs.

---

## 3. Error Handling

| Situation | Behaviour |
| --- | --- |
| A job's agent ends without a valid `set_stage_output` | Same as a pipeline stage today: the run fails with the reason, output kept |
| A routine fires while its last run waits for a human | Skipped, counted, visible on the routine page |
| `pipeline` routine saved with a non-git `cwd` | 400 with "working directory is not a git repository" |
| Resolve with `allow_routine` on a task without routine | 400 |
| Push delivery fails | Logged with task and request id; the needs-you card is the source of truth |
| Resume after a decision fails | Logged; the card stays actionable so the human can retry |

---

## 4. Testing

Every new guard gets a test that goes red when the guard is removed, demonstrated and restored.

- Orchestrator: a job runs `job → done`; `EnsureWorktreeFn` is never called for a job (stub counts calls); the agent's working directory is the task `cwd`; a job cannot enter a pipeline stage (red when the refusal is removed); the pipeline order is unchanged.
- Materializer: kind, stage and autonomy per run mode.
- Scheduler: a skipped fire increments `skipped_count` and sets `last_skipped_at`; a waiting run blocks the next fire.
- Schedules API: a `pipeline` routine with a non-git `cwd` is refused; `run_mode` round-trips; `current_stage` is gone.
- Resolve endpoint: each decision writes the right grant row (context, mode, pattern) or none, and resumes (red when the resume on `deny_once` is removed); routine decisions refused without a routine.
- Sweep: an `awaiting_user` run without PID is never timed out (red when the PID check is removed); one with a live PID still is.
- Transitions: a failed run keeps its previous output keys (red with the old replace).
- Tasks API: the default list excludes jobs; `kind=job&routineId=` returns the routine's jobs.
- Web push: a new permission request triggers a send (fake service).
- UI: routine form with run mode; run history renders runs and the skip note; decision buttons per case (routine task, plain task, default-denied tool). Every mounting test unmounts.

---

## 5. Out of Scope

- Event triggers (new mail, webhooks) — mail spec slice 2.
- Changing who may approve (single-user local trust stays; see `docs/guides/security.md`).
- Retrying failed runs automatically beyond today's rate-limit requeue.
- Parallel runs of one routine.

---

## 6. Delivery

1. **Waiting and decisions** — §2.3 without the routine-context decisions (`deny_once` resume, no time limit without PID, output kept on failure, push). Small, independent, fixes R4–R6 and R8.
2. **Job kind and run mode** — §2.1, §2.2, §2.4, and the routine-context decisions of §2.3.
