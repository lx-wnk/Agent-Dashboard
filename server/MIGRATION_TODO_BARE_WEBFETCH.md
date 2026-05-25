# TODO: bare-WebFetch grant migration

Branch: fix/permissions-tighten (PR #86)
User pick: 4a.B (hard-deny + migration script) — 2026-05-25
Status: Deferred to follow-up PR after 2 failed agent attempts on schema.

## Plan

- At server startup (or as a one-shot ent migration step), run:
  `DELETE FROM task_permissions WHERE tool = 'WebFetch' AND pattern IS NULL`
- Before delete, SELECT COUNT(*) and slog.Warn with the count.
- Update `server/internal/permissions/templates.go::feature_implementation` template — remove WebFetch OR set a safe default pattern.
- Find migration registration via `grep -rn "Migration\|migrate" server/internal/db/`.
