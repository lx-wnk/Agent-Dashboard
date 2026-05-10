---
name: FTS5 external content table incompatibility with Bun SQLite
description: FTS5 content='table' external content tables fail silently in Bun's bundled SQLite — use standalone FTS5 tables instead
type: feedback
---

Use standalone FTS5 tables (no `content=` parameter) in this project. The `content='tasks'` external content table approach fails silently — queries return empty results rather than throwing an error.

**Why:** Bun bundles its own SQLite build which does not support the FTS5 external-content table feature at the time of writing (confirmed 2026-05-10). The catch block in searchRoutes.ts masked the error.

**How to apply:** Any FTS5 virtual table in server/db/client.ts must use the standalone form:
```sql
CREATE VIRTUAL TABLE task_fts USING fts5(task_id UNINDEXED, title, description)
```
Update triggers use `DELETE FROM task_fts WHERE task_id = old.id` (not the `INSERT ... VALUES ('delete', ...)` special command).
