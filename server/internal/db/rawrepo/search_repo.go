package rawrepo

import (
	"context"
	"database/sql"
	"time"
)

// TaskSearchRow is the lightweight task shape returned by FTS5 search.
type TaskSearchRow struct {
	ID           string
	Slug         string
	Title        string
	Description  sql.NullString
	Cwd          string
	CurrentStage string
	Priority     string
	CreatedAt    time.Time
}

// SearchRepo wraps hand-written FTS5 SQL queries used by the search handler.
type SearchRepo interface {
	// SearchTasks runs an FTS5 MATCH query and returns matching task rows.
	// isAdmin=true lifts the user_id filter. Results are capped at limit.
	SearchTasks(ctx context.Context, ftsQuery, userID string, isAdmin bool, limit int) ([]TaskSearchRow, error)
}

type sqlSearchRepo struct{ db *sql.DB }

// NewSearchRepo returns a SearchRepo backed by db.
func NewSearchRepo(db *sql.DB) SearchRepo {
	return &sqlSearchRepo{db: db}
}

func (r *sqlSearchRepo) SearchTasks(ctx context.Context, ftsQuery, userID string, isAdmin bool, limit int) ([]TaskSearchRow, error) {
	const q = `
SELECT t.id, t.slug, t.title, t.description, t.cwd, t.current_stage, t.priority, t.created_at
FROM tasks t
WHERE t.rowid IN (
    SELECT rowid FROM task_fts WHERE task_fts MATCH ?
    LIMIT ?
)
AND (t.user_id IS NULL OR t.user_id = ? OR ? = 1)
LIMIT ?`

	isAdminInt := 0
	if isAdmin {
		isAdminInt = 1
	}

	rows, err := r.db.QueryContext(ctx, q, ftsQuery, limit*4, userID, isAdminInt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TaskSearchRow
	for rows.Next() {
		var row TaskSearchRow
		if err := rows.Scan(&row.ID, &row.Slug, &row.Title, &row.Description, &row.Cwd, &row.CurrentStage, &row.Priority, &row.CreatedAt); err != nil {
			continue
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
