// Package search provides the GET /api/search spotlight-search endpoint.
// It performs FTS5 full-text search over tasks and in-memory substring search
// over running agents.
package search

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
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
	searchRepo rawrepo.SearchRepo
}

// NewHandler creates a new Handler backed by the given SearchRepo.
func NewHandler(searchRepo rawrepo.SearchRepo) *Handler {
	return &Handler{searchRepo: searchRepo}
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

	payload, ok := auth.PayloadFromContext(r.Context())
	if !ok {
		return apierr.ErrForbidden
	}

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
		tasks, err := h.searchTasks(r, q, payload.Sub, payload.IsAdmin, limit)
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

// searchTasks executes an FTS5 query via the SearchRepo and returns matching tasks.
func (h *Handler) searchTasks(r *http.Request, q, userID string, isAdmin bool, limit int) ([]taskSearchResult, error) {
	ftsQuery := sanitizeFtsQuery(q)

	rows, err := h.searchRepo.SearchTasks(r.Context(), ftsQuery, userID, isAdmin, limit)
	if err != nil {
		// Return empty results gracefully — FTS errors should not be surfaced as 500.
		slog.Warn("search: FTS query failed", "err", err, "q", ftsQuery)
		return []taskSearchResult{}, nil
	}

	results := make([]taskSearchResult, 0, len(rows))
	for _, row := range rows {
		res := taskSearchResult{
			ID:           row.ID,
			Slug:         row.Slug,
			Title:        row.Title,
			Cwd:          row.Cwd,
			CurrentStage: row.CurrentStage,
			Priority:     row.Priority,
			CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339),
		}
		if row.Description.Valid {
			s := row.Description.String
			res.Description = &s
		}
		results = append(results, res)
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
