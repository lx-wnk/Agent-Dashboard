-- Task Pipeline Schema v1
-- See plan: ~/.claude/plans/distributed-hatching-waffle.md

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

-- Schema version tracking for future migrations
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

-- Main task entity
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  slug TEXT UNIQUE NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  cwd TEXT NOT NULL,
  worktree_path TEXT,
  source_branch TEXT,
  target_branch TEXT,
  -- `failed` is intentionally NOT a valid task stage. Failure lives on
  -- stage_runs.status — the task stays on the stage where the run died
  -- so the UI can surface retry/analyze actions on the same stage.
  current_stage TEXT NOT NULL CHECK (current_stage IN (
    'backlog','pruefung','refinement','planning','approval1',
    'umsetzungskonzept','approval2','umsetzung','selbstreview',
    'finalisierung','done','on_hold','cancelled'
  )),
  parent_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  max_iterations INTEGER NOT NULL DEFAULT 20,
  token_budget INTEGER,
  cost_budget_cents INTEGER,
  stage_timeout_seconds INTEGER NOT NULL DEFAULT 1800,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  metadata TEXT, -- JSON: screenshots, custom fields, review_feedback
  silver_bullet INTEGER NOT NULL DEFAULT 0, -- 0|1: jump-the-queue flag
  priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low'))
);

CREATE INDEX IF NOT EXISTS idx_tasks_stage ON tasks(current_stage);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id);
-- Note: idx_tasks_picker (silver_bullet, priority, created_at) is created
-- in the runtime migration in server/db/client.ts AFTER the ALTER TABLE
-- statements that add those columns to legacy databases. Creating it here
-- would fail on a pre-existing DB before the columns exist.

-- Stage runs: one row per stage execution (iterations create multiple rows)
CREATE TABLE IF NOT EXISTS stage_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  stage TEXT NOT NULL CHECK (stage IN (
    'backlog','pruefung','refinement','planning','approval1',
    'umsetzungskonzept','approval2','umsetzung','selbstreview',
    'finalisierung','done','on_hold','cancelled'
  )),
  session_id TEXT,
  session_name TEXT,
  pid INTEGER,
  status TEXT NOT NULL CHECK (status IN (
    'pending','running','awaiting_user','on_hold','done','failed'
  )),
  started_at TEXT,
  ended_at TEXT,
  iteration INTEGER NOT NULL DEFAULT 0,
  output TEXT, -- JSON: stage result
  tokens_used INTEGER NOT NULL DEFAULT 0,
  cost_cents INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_stage_runs_task ON stage_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_stage_runs_status ON stage_runs(status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_stage_runs_session ON stage_runs(session_id) WHERE session_id IS NOT NULL;
-- Composite index for getLatestStageRun hot path (task_id, stage, iteration DESC)
CREATE INDEX IF NOT EXISTS idx_stage_runs_latest ON stage_runs(task_id, stage, iteration DESC);

-- Task-scoped permissions (both pre-approved and runtime-granted)
CREATE TABLE IF NOT EXISTS task_permissions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  tool TEXT NOT NULL,
  pattern TEXT, -- null = all usages of this tool
  granted INTEGER NOT NULL, -- 0 or 1 (SQLite bool)
  pre_approved INTEGER NOT NULL, -- 0 or 1
  requested_at TEXT NOT NULL,
  decided_at TEXT,
  decided_by TEXT -- 'user' | 'auto'
);

CREATE INDEX IF NOT EXISTS idx_task_permissions_task ON task_permissions(task_id);

-- Runtime permission requests (agent asked mid-stage)
CREATE TABLE IF NOT EXISTS permission_requests (
  id TEXT PRIMARY KEY,
  stage_run_id TEXT NOT NULL REFERENCES stage_runs(id) ON DELETE CASCADE,
  tool TEXT NOT NULL,
  pattern TEXT,
  reason TEXT,
  requested_at TEXT NOT NULL,
  resolved_at TEXT,
  outcome TEXT -- 'granted' | 'denied' | 'timeout'
);

CREATE INDEX IF NOT EXISTS idx_permission_requests_stage ON permission_requests(stage_run_id);
CREATE INDEX IF NOT EXISTS idx_permission_requests_outcome ON permission_requests(outcome);

-- Audit log for all task-related events
CREATE TABLE IF NOT EXISTS audit_log (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  actor TEXT NOT NULL, -- user|agent|orchestrator|system
  action TEXT NOT NULL,
  timestamp TEXT NOT NULL,
  details TEXT -- JSON
);

CREATE INDEX IF NOT EXISTS idx_audit_log_task ON audit_log(task_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp);

-- Notification preferences per event type (channels is JSON array)
CREATE TABLE IF NOT EXISTS notification_preferences (
  event_type TEXT PRIMARY KEY, -- on_hold|approval_needed|completed|failed|budget_exceeded|iteration_warning
  channels TEXT NOT NULL, -- JSON array: ["email","webhook","browser","system"]
  enabled INTEGER NOT NULL DEFAULT 1
);

-- Key-value config for notification adapters (SMTP host, webhook URL, etc.)
CREATE TABLE IF NOT EXISTS notification_config (
  key TEXT PRIMARY KEY,
  value TEXT
);

-- Pipeline global config (max parallel, etc.)
CREATE TABLE IF NOT EXISTS pipeline_config (
  key TEXT PRIMARY KEY,
  value TEXT
);

-- User-authored feedback on approval-gated artifacts (planning,
-- umsetzungskonzept). A row is created when the user clicks
-- "Änderungen anfordern" on an approval gate; the task regresses to
-- the reviewed stage and the prompt builder for that stage consumes
-- all unresolved rows as a feedback prefix. A row is marked resolved
-- when a subsequent stage_run on the reviewed stage transitions to a
-- successful `next` state.
--
-- `iteration` is a separate counter from `stage_runs.iteration`
-- (ADR: R2 — user-feedback cycles are decoupled from schema-retry
-- cycles so the max_iterations budget doesn't starve human review).
CREATE TABLE IF NOT EXISTS task_feedback (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  stage TEXT NOT NULL CHECK (stage IN ('planning','umsetzungskonzept')),
  stage_run_id TEXT REFERENCES stage_runs(id) ON DELETE SET NULL,
  iteration INTEGER NOT NULL,
  feedback TEXT NOT NULL,
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  resolved_by_stage_run_id TEXT REFERENCES stage_runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_task_feedback_task_stage ON task_feedback(task_id, stage);
CREATE INDEX IF NOT EXISTS idx_task_feedback_unresolved ON task_feedback(task_id, stage, resolved_at);

-- MCP API keys for external and internal agent authentication
CREATE TABLE IF NOT EXISTS api_keys (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  key_hash     TEXT NOT NULL UNIQUE,   -- SHA-256 of raw token (never store plain)
  scopes       TEXT NOT NULL,          -- JSON array: ['tasks:read','tasks:write',...]
  active       INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  last_used_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(active);

-- Task dependency links: task_id must wait for depends_on_id to reach required_stage.
-- ON DELETE CASCADE: removing either task automatically cleans up its dependency rows.
CREATE TABLE IF NOT EXISTS task_dependencies (
  id               TEXT PRIMARY KEY,
  task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  required_stage   TEXT NOT NULL DEFAULT 'done'
                   CHECK (required_stage IN ('done', 'cancelled')),
  on_cancel_action TEXT NOT NULL DEFAULT 'on_hold'
                   CHECK (on_cancel_action IN ('cancel', 'start', 'on_hold')),
  created_at       TEXT NOT NULL,
  UNIQUE(task_id, depends_on_id)
);

CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id);
