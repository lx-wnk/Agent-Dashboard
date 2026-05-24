// Package cost implements the /api/cost/* analytics endpoints — aggregations
// over the agent_cost_trends table for the cost-analytics dashboard view.
package cost

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// Handler serves the /api/cost/* endpoints.
//
// All routes are read-only and execute SQL aggregations against the
// agent_cost_trends table. No new schema or dependencies are introduced —
// the `model` column already exists on agent_cost_trends.
type Handler struct {
	db *sql.DB
}

// NewHandler returns a Handler backed by db. db may be nil; in that case
// every endpoint responds with an empty summary instead of a 500 so the
// dashboard remains operable on fresh installs.
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Mount registers the cost-analytics routes. Must be called inside the
// JWT/auth-bypass protected group.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/cost/summary", h.getSummary)
}

// ─── response types ───────────────────────────────────────────────────────────

// modelBreakdown is the total spend per model across the full retention
// window of agent_cost_trends.
type modelBreakdown struct {
	Model    string  `json:"model"`
	CostUSD  float64 `json:"costUsd"`
	Sessions int     `json:"sessions"`
}

// dayPoint is a per-day, per-model cost slice. Day is ISO-8601 (YYYY-MM-DD,
// UTC). Stacked-bar charts read this directly.
type dayPoint struct {
	Day     string  `json:"day"`
	Model   string  `json:"model"`
	CostUSD float64 `json:"costUsd"`
}

// weekPoint is a per-ISO-week total cost (all models combined). Used by the
// rolling weekly trend line.
type weekPoint struct {
	Week    string  `json:"week"` // YYYY-WW (ISO week)
	CostUSD float64 `json:"costUsd"`
}

// summaryResponse is the full /api/cost/summary payload.
type summaryResponse struct {
	ByModel   []modelBreakdown `json:"byModel"`
	ByDay     []dayPoint       `json:"byDay"`
	ByWeek    []weekPoint      `json:"byWeek"`
	TotalUSD  float64          `json:"totalUsd"`
	UpdatedAt int64            `json:"updatedAt"` // unix ms
}

// ─── GET /api/cost/summary ────────────────────────────────────────────────────

func (h *Handler) getSummary(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		apierr.WriteJSON(w, http.StatusOK, emptySummary())
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	dayFrom := now.AddDate(0, 0, -30)
	weekFrom := now.AddDate(0, 0, -84) // 12 weeks

	byModel, err := h.queryByModel(ctx)
	if err != nil {
		apierr.JSONError(w, http.StatusInternalServerError, fmt.Sprintf("cost.byModel: %v", err))
		return
	}
	byDay, err := h.queryByDay(ctx, dayFrom)
	if err != nil {
		apierr.JSONError(w, http.StatusInternalServerError, fmt.Sprintf("cost.byDay: %v", err))
		return
	}
	byWeek, err := h.queryByWeek(ctx, weekFrom)
	if err != nil {
		apierr.JSONError(w, http.StatusInternalServerError, fmt.Sprintf("cost.byWeek: %v", err))
		return
	}

	var total float64
	for _, m := range byModel {
		total += m.CostUSD
	}

	apierr.WriteJSON(w, http.StatusOK, summaryResponse{
		ByModel:   byModel,
		ByDay:     byDay,
		ByWeek:    byWeek,
		TotalUSD:  total,
		UpdatedAt: now.UnixMilli(),
	})
}

// ─── queries ──────────────────────────────────────────────────────────────────

func (h *Handler) queryByModel(ctx context.Context) ([]modelBreakdown, error) {
	const q = `
SELECT
    COALESCE(NULLIF(model, ''), 'unknown') AS model,
    SUM(cost_usd)                          AS total_cost,
    COUNT(DISTINCT session_id)             AS sessions
FROM agent_cost_trends
GROUP BY model
ORDER BY total_cost DESC
`
	rows, err := h.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]modelBreakdown, 0)
	for rows.Next() {
		var m modelBreakdown
		if err := rows.Scan(&m.Model, &m.CostUSD, &m.Sessions); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// recordedAtSinceClause / recordedAtThreshold encode the cross-format threshold
// for `recorded_at >= ?`.
//
// The ent client persists time.Time via Go's default String() format
// (e.g. "2026-05-23 10:28:54.704002 +0000 UTC"). Historic importer rows may
// also use RFC3339 ("2026-05-23T10:28:54Z"). Both formats start with a
// lexicographically sortable "YYYY-MM-DD..." prefix, so we compare the first
// 10 characters as a date-only window. This is robust against either format
// and avoids strftime() entirely on the column (strftime returns NULL for the
// Go-default String form).
//
// The 10-char window slightly over-includes the boundary day, which is
// acceptable for a 30-day / 12-week rolling aggregation.
const recordedAtSinceClause = `substr(recorded_at, 1, 10) >= ?`

func recordedAtThreshold(since time.Time) string {
	return since.UTC().Format("2006-01-02")
}

func (h *Handler) queryByDay(ctx context.Context, since time.Time) ([]dayPoint, error) {
	// substr(recorded_at, 1, 10) yields the YYYY-MM-DD prefix from either the
	// Go default String() format or RFC3339 — see recordedAtSinceClause.
	q := `
SELECT
    substr(recorded_at, 1, 10)             AS day,
    COALESCE(NULLIF(model, ''), 'unknown') AS model,
    SUM(cost_usd)                          AS total_cost
FROM agent_cost_trends
WHERE ` + recordedAtSinceClause + `
GROUP BY day, model
ORDER BY day ASC, total_cost DESC
`
	rows, err := h.db.QueryContext(ctx, q, recordedAtThreshold(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]dayPoint, 0)
	for rows.Next() {
		var p dayPoint
		if err := rows.Scan(&p.Day, &p.Model, &p.CostUSD); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// queryByWeek aggregates per-ISO-week totals. SQLite can't reliably parse the
// ent-stored time format with strftime, so we read per-day totals and bucket
// them in Go using time.Time.ISOWeek(). 12 weeks × ~7 days = 84 rows max, so
// the in-memory bucketing has negligible cost.
func (h *Handler) queryByWeek(ctx context.Context, since time.Time) ([]weekPoint, error) {
	q := `
SELECT
    substr(recorded_at, 1, 10) AS day,
    SUM(cost_usd)              AS total_cost
FROM agent_cost_trends
WHERE ` + recordedAtSinceClause + `
GROUP BY day
ORDER BY day ASC
`
	rows, err := h.db.QueryContext(ctx, q, recordedAtThreshold(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	weekTotals := make(map[string]float64)
	weekOrder := make([]string, 0)
	for rows.Next() {
		var day string
		var cost float64
		if err := rows.Scan(&day, &cost); err != nil {
			return nil, err
		}
		t, parseErr := time.Parse("2006-01-02", day)
		if parseErr != nil {
			continue
		}
		year, week := t.ISOWeek()
		key := fmt.Sprintf("%04d-W%02d", year, week)
		if _, seen := weekTotals[key]; !seen {
			weekOrder = append(weekOrder, key)
		}
		weekTotals[key] += cost
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]weekPoint, 0, len(weekOrder))
	for _, key := range weekOrder {
		result = append(result, weekPoint{Week: key, CostUSD: weekTotals[key]})
	}
	return result, nil
}

// emptySummary returns a zero-value summary used when the DB is unavailable
// on a fresh install. The frontend handles empty slices gracefully.
func emptySummary() summaryResponse {
	return summaryResponse{
		ByModel:   []modelBreakdown{},
		ByDay:     []dayPoint{},
		ByWeek:    []weekPoint{},
		TotalUSD:  0,
		UpdatedAt: time.Now().UnixMilli(),
	}
}
