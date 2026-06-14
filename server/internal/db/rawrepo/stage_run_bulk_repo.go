package rawrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// StageRunBulkRepo provides bulk raw-SQL helpers for stage_runs that cannot
// be expressed efficiently through ent (e.g. window-function queries).
type StageRunBulkRepo interface {
	// LatestPerTask returns the most-recent stage_run per task_id for the given
	// task IDs using a ROW_NUMBER() window function. Correctness is exact
	// regardless of iteration count, unlike the Go-side heuristic limit.
	LatestPerTask(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error)

	// AllForTaskIDs returns all stage_runs for the given task IDs grouped by
	// task_id. Used for bulk eager-loading in export routes.
	AllForTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*ent.StageRun, error)
}

type sqlStageRunBulkRepo struct{ db *sql.DB }

// NewStageRunBulkRepo returns a StageRunBulkRepo backed by db.
func NewStageRunBulkRepo(db *sql.DB) StageRunBulkRepo {
	return &sqlStageRunBulkRepo{db: db}
}

// LatestPerTask uses ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at DESC)
// to select exactly one row per task_id — the most recently created stage_run.
// This replaces the Go-side heuristic `Limit(len(ids)*20+20)` which silently
// misses the latest run when a task has more than 20 iterations.
func (r *sqlStageRunBulkRepo) LatestPerTask(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error) {
	if len(taskIDs) == 0 {
		return map[string]*ent.StageRun{}, nil
	}

	placeholders, args := buildInArgs(taskIDs)
	q := fmt.Sprintf(`
SELECT id, task_id, stage, session_id, session_name, pid, status, iteration,
       output, tokens_used, cost_cents, started_at, ended_at, last_grant_at, created_at,
       retry_count, next_retry_at
FROM (
    SELECT id, task_id, stage, session_id, session_name, pid, status, iteration,
           output, tokens_used, cost_cents, started_at, ended_at, last_grant_at, created_at,
           retry_count, next_retry_at,
           ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at DESC) AS rn
    FROM stage_runs
    WHERE task_id IN (%s)
)
WHERE rn = 1`, placeholders)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stagerun.LatestPerTask: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*ent.StageRun, len(taskIDs))
	for rows.Next() {
		sr, err := scanStageRun(rows)
		if err != nil {
			return nil, fmt.Errorf("stagerun.LatestPerTask scan: %w", err)
		}
		result[sr.TaskID] = sr
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stagerun.LatestPerTask rows: %w", err)
	}
	return result, nil
}

// AllForTaskIDs returns all stage_runs for the given task IDs in a single
// query, grouped into a map[taskID][]*ent.StageRun ordered by iteration ASC.
func (r *sqlStageRunBulkRepo) AllForTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*ent.StageRun, error) {
	if len(taskIDs) == 0 {
		return map[string][]*ent.StageRun{}, nil
	}

	placeholders, args := buildInArgs(taskIDs)
	q := fmt.Sprintf(`
SELECT id, task_id, stage, session_id, session_name, pid, status, iteration,
       output, tokens_used, cost_cents, started_at, ended_at, last_grant_at, created_at,
       retry_count, next_retry_at
FROM stage_runs
WHERE task_id IN (%s)
ORDER BY task_id, iteration ASC`, placeholders)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stagerun.AllForTaskIDs: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]*ent.StageRun, len(taskIDs))
	for rows.Next() {
		sr, err := scanStageRun(rows)
		if err != nil {
			return nil, fmt.Errorf("stagerun.AllForTaskIDs scan: %w", err)
		}
		result[sr.TaskID] = append(result[sr.TaskID], sr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stagerun.AllForTaskIDs rows: %w", err)
	}
	return result, nil
}

// buildInArgs builds a comma-separated placeholder string and a matching args
// slice for use in SQL IN (...) clauses. Each element in ids becomes a single
// "?" placeholder.
func buildInArgs(ids []string) (string, []any) {
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	return placeholders, args
}

// scanStageRun scans a single stage_run row into an *ent.StageRun. The column
// order must match the SELECT list in LatestPerTask and AllForTaskIDs.
func scanStageRun(rows *sql.Rows) (*ent.StageRun, error) {
	var sr ent.StageRun
	var (
		sessionID   sql.NullString
		sessionName sql.NullString
		pid         sql.NullInt64
		outputRaw   sql.NullString
		startedAt   sql.NullTime
		endedAt     sql.NullTime
		lastGrantAt sql.NullTime
		nextRetryAt sql.NullTime
	)
	err := rows.Scan(
		&sr.ID, &sr.TaskID, &sr.Stage,
		&sessionID, &sessionName, &pid,
		&sr.Status, &sr.Iteration,
		&outputRaw, &sr.TokensUsed, &sr.CostCents,
		&startedAt, &endedAt, &lastGrantAt,
		&sr.CreatedAt,
		&sr.RetryCount, &nextRetryAt,
	)
	if err != nil {
		return nil, err
	}
	if sessionID.Valid {
		sr.SessionID = &sessionID.String
	}
	if sessionName.Valid {
		sr.SessionName = &sessionName.String
	}
	if pid.Valid {
		p := int(pid.Int64)
		sr.Pid = &p
	}
	if outputRaw.Valid && outputRaw.String != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(outputRaw.String), &m); err == nil {
			sr.Output = m
		}
	}
	if startedAt.Valid {
		t := startedAt.Time
		sr.StartedAt = &t
	}
	if endedAt.Valid {
		t := endedAt.Time
		sr.EndedAt = &t
	}
	if lastGrantAt.Valid {
		t := lastGrantAt.Time
		sr.LastGrantAt = &t
	}
	if nextRetryAt.Valid {
		t := nextRetryAt.Time
		sr.NextRetryAt = &t
	}
	return &sr, nil
}
