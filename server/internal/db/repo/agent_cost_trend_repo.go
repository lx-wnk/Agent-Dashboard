package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/agentcosttrend"
)

// AgentCostRow is the input type for Upsert.
type AgentCostRow struct {
	SessionID    string
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	RecordedAt   time.Time
}

// AgentCostTrendRepo defines read/write operations for the agent_cost_trend table.
type AgentCostTrendRepo interface {
	Upsert(ctx context.Context, rows []AgentCostRow) error
	ListByTimeRange(ctx context.Context, from, to time.Time) ([]*ent.AgentCostTrend, error)
}

type entAgentCostTrendRepo struct{ client *ent.Client }

// NewAgentCostTrendRepo creates an AgentCostTrendRepo backed by the given ent client.
func NewAgentCostTrendRepo(client *ent.Client) AgentCostTrendRepo {
	return &entAgentCostTrendRepo{client: client}
}

// Upsert writes all rows, updating model, input_tokens, output_tokens, cost_usd,
// and recorded_at on conflict with an existing session_id. Empty slice is a no-op.
//
// ent's sql/upsert feature only generates OnConflict on the single-entity Create
// builder (not CreateBulk), so each row is upserted individually inside one
// transaction for atomicity. UpdateNewValues() refreshes every mutable column
// from the incoming row and leaves the existing id untouched.
func (r *entAgentCostTrendRepo) Upsert(ctx context.Context, rows []AgentCostRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("agentcosttrend.Upsert: begin tx: %w", err)
	}
	for _, row := range rows {
		if err := tx.AgentCostTrend.Create().
			SetID(uuid.New().String()).
			SetSessionID(row.SessionID).
			SetModel(row.Model).
			SetInputTokens(row.InputTokens).
			SetOutputTokens(row.OutputTokens).
			SetCostUsd(row.CostUSD).
			SetRecordedAt(row.RecordedAt).
			OnConflictColumns(agentcosttrend.FieldSessionID).
			UpdateNewValues().
			Exec(ctx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("agentcosttrend.Upsert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("agentcosttrend.Upsert: commit: %w", err)
	}
	return nil
}

// ListByTimeRange returns all rows whose recorded_at is in [from, to].
func (r *entAgentCostTrendRepo) ListByTimeRange(ctx context.Context, from, to time.Time) ([]*ent.AgentCostTrend, error) {
	rows, err := r.client.AgentCostTrend.Query().
		Where(
			agentcosttrend.RecordedAtGTE(from),
			agentcosttrend.RecordedAtLTE(to),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentcosttrend.ListByTimeRange: %w", err)
	}
	return rows, nil
}
