// Package analytics implements the /api/analytics/* endpoints.
package analytics

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/analytics"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Handler handles all /api/analytics/* routes.
type Handler struct {
	db      *sql.DB
	cfgRepo repo.PipelineConfigRepo
}

// NewHandler creates a Handler backed by the given *sql.DB and config repo.
func NewHandler(db *sql.DB, cfgRepo repo.PipelineConfigRepo) *Handler {
	return &Handler{db: db, cfgRepo: cfgRepo}
}

// Mount registers analytics routes on the given router.
// Must be called inside a protected (JWT / bypass) group.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/analytics/patterns", h.getPatterns)
	r.Post("/api/analytics/patterns/refresh", h.refreshPatterns)
	r.Get("/api/analytics/heatmap", h.getHeatmap)
	r.Get("/api/analytics/cost-forecast", h.getCostForecast)
}

// ─── GET /api/analytics/patterns ──────────────────────────────────────────────

type patternRow struct {
	ID         int    `json:"id"`
	Tools      string `json:"tools"`
	Frequency  int    `json:"frequency"`
	LastSeenAt string `json:"lastSeenAt"`
}

func (h *Handler) getPatterns(w http.ResponseWriter, r *http.Request) {
	const q = `SELECT id, tools, frequency, last_seen_at FROM workflow_patterns ORDER BY frequency DESC LIMIT 20`
	rows, err := h.db.QueryContext(r.Context(), q)
	if err != nil {
		jsonError(w, "failed to query patterns", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	patterns := make([]patternRow, 0)
	for rows.Next() {
		var p patternRow
		if err := rows.Scan(&p.ID, &p.Tools, &p.Frequency, &p.LastSeenAt); err != nil {
			continue
		}
		patterns = append(patterns, p)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("analytics: patterns row scan error", "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"patterns": patterns})
}

// ─── POST /api/analytics/patterns/refresh ─────────────────────────────────────

func (h *Handler) refreshPatterns(w http.ResponseWriter, r *http.Request) {
	if err := analytics.DiscoverPatterns(h.db); err != nil {
		slog.Error("analytics: discover patterns failed", "err", err)
		jsonError(w, "pattern discovery failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── GET /api/analytics/heatmap ───────────────────────────────────────────────

type heatmapResponse struct {
	// Grid is indexed [dow][hour], dow 0=Sun, hour 0-23.
	Grid [7][24]float64 `json:"grid"`
}

func (h *Handler) getHeatmap(w http.ResponseWriter, r *http.Request) {
	// SQLite stores time as TEXT in RFC3339. strftime works on TEXT if it parses as datetime.
	const q = `
SELECT
    CAST(strftime('%w', recorded_at) AS INTEGER) AS dow,
    CAST(strftime('%H', recorded_at) AS INTEGER) AS hour,
    SUM(cost_usd) AS total_cost
FROM agent_cost_trends
GROUP BY dow, hour
`
	rows, err := h.db.QueryContext(r.Context(), q)
	if err != nil {
		slog.Warn("analytics: heatmap query failed", "err", err)
		writeJSON(w, http.StatusOK, heatmapResponse{})
		return
	}
	defer rows.Close()

	var resp heatmapResponse
	for rows.Next() {
		var dow, hour int
		var cost float64
		if err := rows.Scan(&dow, &hour, &cost); err != nil {
			continue
		}
		if dow >= 0 && dow < 7 && hour >= 0 && hour < 24 {
			resp.Grid[dow][hour] = cost
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/analytics/cost-forecast ─────────────────────────────────────────

type forecastPoint struct {
	T             int64   `json:"t"`             // unix ms
	ProjectedCost float64 `json:"projectedCost"` // USD
}

type forecastResponse struct {
	Forecast []forecastPoint `json:"forecast"`
	Alert    string          `json:"alert"` // "" | "warn" | "critical"
}

func (h *Handler) getCostForecast(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Last 30 days, ordered ASC.
	since := time.Now().UTC().AddDate(0, 0, -30)
	const q = `
SELECT recorded_at, cost_usd
FROM agent_cost_trends
WHERE recorded_at >= ?
ORDER BY recorded_at ASC
`
	rows, err := h.db.QueryContext(ctx, q, since.Format(time.RFC3339))
	if err != nil {
		slog.Warn("analytics: forecast query failed", "err", err)
		writeJSON(w, http.StatusOK, forecastResponse{Forecast: []forecastPoint{}, Alert: ""})
		return
	}
	defer rows.Close()

	// Build cumulative-sum series as {t (unix ms), y (cumulative USD)}.
	var series []dataPoint
	var cumCost float64
	for rows.Next() {
		var recordedAt time.Time
		var cost float64
		if err := rows.Scan(&recordedAt, &cost); err != nil {
			continue
		}
		cumCost += cost
		series = append(series, dataPoint{
			t: float64(recordedAt.UnixMilli()),
			y: cumCost,
		})
	}

	if len(series) == 0 {
		writeJSON(w, http.StatusOK, forecastResponse{Forecast: []forecastPoint{}, Alert: ""})
		return
	}

	slope, intercept := linearRegression(series)

	// Project 7 days forward from now.
	now := time.Now().UTC()
	forecast := make([]forecastPoint, 7)
	for i := 0; i < 7; i++ {
		ft := now.AddDate(0, 0, i+1)
		tMs := float64(ft.UnixMilli())
		projected := math.Max(0, slope*tMs+intercept)
		forecast[i] = forecastPoint{
			T:             ft.UnixMilli(),
			ProjectedCost: projected,
		}
	}

	// Determine alert level from pipeline config.
	warnCents := h.cfgRepo.GetNumber(ctx, "cost_forecast_warn_cents", 1000)
	critCents := h.cfgRepo.GetNumber(ctx, "cost_forecast_critical_cents", 5000)

	projectedCents := forecast[6].ProjectedCost * 100
	alert := ""
	switch {
	case projectedCents >= critCents:
		alert = "critical"
	case projectedCents >= warnCents:
		alert = "warn"
	}

	writeJSON(w, http.StatusOK, forecastResponse{Forecast: forecast, Alert: alert})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

type dataPoint struct{ t, y float64 }

// linearRegression performs OLS on a series of (t, y) points.
// Returns slope and intercept such that y ≈ slope*t + intercept.
func linearRegression(pts []dataPoint) (slope, intercept float64) {
	n := float64(len(pts))
	if n == 0 {
		return 0, 0
	}
	var sumT, sumY, sumTT, sumTY float64
	for i := range pts {
		sumT += pts[i].t
		sumY += pts[i].y
		sumTT += pts[i].t * pts[i].t
		sumTY += pts[i].t * pts[i].y
	}
	denom := n*sumTT - sumT*sumT
	if denom == 0 {
		return 0, sumY / n
	}
	slope = (n*sumTY - sumT*sumY) / denom
	intercept = (sumY - slope*sumT) / n
	return slope, intercept
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	writeJSON(w, status, map[string]string{"error": msg})
}
