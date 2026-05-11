// Package search provides the GET /api/search spotlight-search endpoint.
// It performs FTS5 full-text search over tasks and in-memory substring search
// over running agents.
package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
)

const (
	maxQueryLen  = 200
	defaultLimit = 20
	maxLimit     = 50
	minLimit     = 1
)

// taskSearchResult is the lightweight task shape returned by the search endpoint.
type taskSearchResult struct {
	ID           string  `json:"id"`
	Slug         string  `json:"slug"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	Cwd          string  `json:"cwd"`
	CurrentStage string  `json:"currentStage"`
	Priority     string  `json:"priority"`
	CreatedAt    string  `json:"createdAt"`
}

// searchResponse is the top-level response shape.
type searchResponse struct {
	Tasks  []taskSearchResult `json:"tasks"`
	Agents []sdk.Agent        `json:"agents"`
}

// Handler handles GET /api/search.
type Handler struct {
	db *sql.DB
}

// NewHandler creates a new Handler backed by the given *sql.DB.
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Search handles GET /api/search?q=...&type=...&limit=...
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type")
	limitStr := r.URL.Query().Get("limit")

	if searchType == "" {
		searchType = "all"
	}

	limit := defaultLimit
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		}
	}
	if limit < minLimit {
		limit = minLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	payload, _ := auth.PayloadFromContext(r.Context())

	// Trim and validate query.
	q = strings.TrimSpace(q)
	if len(q) > maxQueryLen {
		q = q[:maxQueryLen]
	}

	resp := searchResponse{
		Tasks:  []taskSearchResult{},
		Agents: []sdk.Agent{},
	}

	if q == "" {
		writeJSON(w, http.StatusOK, resp)
		return nil
	}

	if searchType == "tasks" || searchType == "all" {
		tasks, err := h.searchTasks(r.Context(), q, payload.Sub, payload.IsAdmin, limit)
		if err == nil {
			resp.Tasks = tasks
		}
	}

	if (searchType == "agents" || searchType == "all") && payload.IsAdmin {
		agents, err := merger.GetAgents(r.Context())
		if err == nil {
			resp.Agents = filterAgents(agents, q, limit)
		}
	}

	writeJSON(w, http.StatusOK, resp)
	return nil
}

// searchTasks executes an FTS5 query and returns matching tasks.
func (h *Handler) searchTasks(ctx context.Context, q, userID string, isAdmin bool, limit int) ([]taskSearchResult, error) {
	ftsQuery := sanitizeFtsQuery(q)

	const sqlQuery = `
SELECT t.id, t.slug, t.title, t.description, t.cwd, t.current_stage, t.priority, t.created_at
FROM tasks t
WHERE t.rowid IN (
    SELECT rowid FROM task_fts WHERE task_fts MATCH ?
    ORDER BY rank
    LIMIT ?
)
AND (t.user_id IS NULL OR t.user_id = ? OR ? = 1)
LIMIT ?`

	isAdminInt := 0
	if isAdmin {
		isAdminInt = 1
	}

	rows, err := h.db.QueryContext(ctx, sqlQuery, ftsQuery, limit, userID, isAdminInt, limit)
	if err != nil {
		// Return empty results gracefully — FTS errors should not be surfaced as 500.
		return []taskSearchResult{}, nil
	}
	defer rows.Close()

	var results []taskSearchResult
	for rows.Next() {
		var r taskSearchResult
		var createdAt time.Time
		var desc sql.NullString
		if err := rows.Scan(&r.ID, &r.Slug, &r.Title, &desc, &r.Cwd, &r.CurrentStage, &r.Priority, &createdAt); err != nil {
			continue
		}
		if desc.Valid {
			s := desc.String
			r.Description = &s
		}
		r.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return []taskSearchResult{}, nil
	}
	if results == nil {
		results = []taskSearchResult{}
	}
	return results, nil
}

// sanitizeFtsQuery converts a raw query string into a safe FTS5 MATCH expression.
// Each whitespace-separated token is wrapped in double quotes (internal quotes doubled)
// and given a prefix-match suffix (*).
//
// Example: `hello world` → `"hello"* "world"*`
func sanitizeFtsQuery(raw string) string {
	tokens := strings.Fields(raw)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		escaped := strings.ReplaceAll(tok, `"`, `""`)
		parts = append(parts, `"`+escaped+`"*`)
	}
	return strings.Join(parts, " ")
}

// filterAgents returns agents matching the query by case-insensitive substring on
// projectName, currentAction, or cwd. Results are capped at limit.
func filterAgents(agents []sdk.Agent, q string, limit int) []sdk.Agent {
	lower := strings.ToLower(q)
	var matched []sdk.Agent
	for _, a := range agents {
		if strings.Contains(strings.ToLower(a.ProjectName), lower) ||
			strings.Contains(strings.ToLower(a.CurrentAction), lower) ||
			strings.Contains(strings.ToLower(a.CWD), lower) {
			matched = append(matched, a)
			if len(matched) >= limit {
				break
			}
		}
	}
	if matched == nil {
		matched = []sdk.Agent{}
	}
	return matched
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
