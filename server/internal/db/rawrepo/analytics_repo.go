package rawrepo

import (
	"context"
	"database/sql"
	"time"
)

// PatternRow is a single workflow_patterns record.
type PatternRow struct {
	ID         int
	Tools      string
	Frequency  int
	LastSeenAt string
}

// HeatmapPoint is a (day-of-week, hour, cost) aggregate.
type HeatmapPoint struct {
	DOW  int
	Hour int
	Cost float64
}

// CostSample is a single (recorded_at, cost_usd) row from agent_cost_trends.
type CostSample struct {
	RecordedAt time.Time
	CostUSD    float64
}

// AnalyticsRepo wraps hand-written SQL queries used by the analytics handler.
type AnalyticsRepo interface {
	// GetPatterns returns the top workflow_patterns rows ordered by frequency DESC.
	GetPatterns(ctx context.Context) ([]PatternRow, error)
	// GetHeatmap returns aggregated cost per (day-of-week, hour).
	GetHeatmap(ctx context.Context) ([]HeatmapPoint, error)
	// GetCostSince returns all agent_cost_trends rows recorded on or after since, ASC.
	GetCostSince(ctx context.Context, since time.Time) ([]CostSample, error)
}

type sqlAnalyticsRepo struct{ db *sql.DB }

// NewAnalyticsRepo returns an AnalyticsRepo backed by db.
func NewAnalyticsRepo(db *sql.DB) AnalyticsRepo {
	return &sqlAnalyticsRepo{db: db}
}

func (r *sqlAnalyticsRepo) GetPatterns(ctx context.Context) ([]PatternRow, error) {
	const q = `SELECT id, tools, frequency, last_seen_at FROM workflow_patterns ORDER BY frequency DESC LIMIT 20`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PatternRow
	for rows.Next() {
		var p PatternRow
		if err := rows.Scan(&p.ID, &p.Tools, &p.Frequency, &p.LastSeenAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *sqlAnalyticsRepo) GetHeatmap(ctx context.Context) ([]HeatmapPoint, error) {
	// strftime operates on TEXT ISO-8601 values. SQLite has no timezone awareness,
	// so DOW/hour are in whatever timezone recorded_at was stored in — the app
	// writes UTC (time.RFC3339) so grid values are always UTC-based.
	const q = `
SELECT
    CAST(strftime('%w', recorded_at) AS INTEGER) AS dow,
    CAST(strftime('%H', recorded_at) AS INTEGER) AS hour,
    SUM(cost_usd) AS total_cost
FROM agent_cost_trends
GROUP BY dow, hour
`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HeatmapPoint
	for rows.Next() {
		var p HeatmapPoint
		if err := rows.Scan(&p.DOW, &p.Hour, &p.Cost); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *sqlAnalyticsRepo) GetCostSince(ctx context.Context, since time.Time) ([]CostSample, error) {
	const q = `
SELECT recorded_at, cost_usd
FROM agent_cost_trends
WHERE recorded_at >= ?
ORDER BY recorded_at ASC
`
	rows, err := r.db.QueryContext(ctx, q, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CostSample
	for rows.Next() {
		var s CostSample
		if err := rows.Scan(&s.RecordedAt, &s.CostUSD); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
