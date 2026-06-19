# ADR-0007: Cron Scheduling Engine

**Status:** Accepted
**Date:** 2026-06-18

## Context

The dashboard's task pipeline executes tasks on demand. A recurring-schedule
feature requires tasks to be fired automatically on a time-based cadence —
for example "every weekday at 9 am" or "every 15 minutes". The key design
questions were:

1. Which cron parsing library to use.
2. Whether to store the natural-language input or the parsed cron expression
   as the firing source of truth.
3. How the scheduler package should relate to the existing `pipeline/` package
   to preserve the import-graph layering rules.

## Decision

**Library — `github.com/robfig/cron/v3` (standard 5-field parser).**

Reasons: it is the de-facto Go cron library, supports all standard 5-field
expressions (minute, hour, day-of-month, month, day-of-week), is well-tested,
and compiles to no external OS dependency. Non-standard extensions (seconds
field, `@every`) are intentionally not enabled; 5-field expressions are
sufficient and keep user-facing behaviour predictable.

**Stored cron expression as the firing source of truth.**

NL→cron translation (`NLCron`) runs once at schedule create/edit:

1. A rule-based fast path handles common English phrases ("every weekday at
   9am", "every 15 minutes", "hourly", etc.) without an LLM call.
2. An optional injectable LLM fallback handles phrases the rule-based path
   cannot parse.
3. The resulting 5-field expression is validated by `robfig/cron/v3` and
   stored in the `task_schedules.cron_expr` column.

All subsequent firing reads only the stored expression. NL input is never
re-evaluated at fire time. This means:

- Firing is deterministic and offline-safe — no LLM required at runtime.
- Schedule behaviour is reproducible and auditable.
- Editing a schedule's NL description runs translation once more; the stored
  expression is then updated.

**`scheduler/` as a strict leaf package.**

`server/internal/scheduler/` depends only on `db/ent` and `db/repo`. It does
not import `pipeline/` or `api/`. Task materialisation is decoupled via an
injected `CreateTaskFromInput` closure provided by the DI layer
(`server/cmd/serve/`). This preserves the existing layering rule that
high-level packages (`serve/`) wire dependencies inward; domain packages do
not reach sideways into each other.

## Consequences

**Positive:**

- Firing is deterministic and requires no external service or LLM at runtime.
- The scheduler is independently testable with a fake clock and no pipeline
  state machine involved.
- `pipeline/` import graph is unchanged — scheduler is additive.
- Standard 5-field syntax is widely understood by users who know cron.

**Negative / Trade-offs:**

- A user who edits the NL description of a live schedule re-triggers
  translation; if the rule-based path handles it differently than the prior
  LLM result, the cron expression may change silently. Mitigation: the stored
  expression is surfaced in the UI preview before save.
- Non-standard cron extensions (seconds-level granularity, `@reboot`) are not
  supported.
- The LLM fallback is optional; deployments without an LLM adapter fall back
  to rule-based only, which may fail on unusual phrasings.

## Alternatives Considered

1. **Store the NL input and re-translate at every tick.** Rejected: non-deterministic
   (LLM output can vary), requires an LLM to be reachable at fire time, and
   makes schedule behaviour impossible to audit or reproduce.

2. **Use a seconds-field parser (`robfig/cron/v3` `WithSeconds()` option).**
   Rejected: sub-minute scheduling is not a stated requirement and adds
   confusion for users editing expressions manually.

3. **Inline scheduler logic into `pipeline/`.**  Rejected: the orchestrator
   package is already the most complex package in the server; adding a
   time-based driver would couple the state machine to wall-clock concerns and
   complicate testing. A leaf package keeps the concerns separate.
