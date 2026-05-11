package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/agentcosttrend"
)

// AgentCostRow is the input type for BulkInsert.
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
	BulkInsert(ctx context.Context, rows []AgentCostRow) error
	ListByTimeRange(ctx context.Context, from, to time.Time) ([]*ent.AgentCostTrend, error)
}

type entAgentCostTrendRepo struct{ client *ent.Client }

// NewAgentCostTrendRepo creates an AgentCostTrendRepo backed by the given ent client.
func NewAgentCostTrendRepo(client *ent.Client) AgentCostTrendRepo {
	return &entAgentCostTrendRepo{client: client}
}

// BulkInsert inserts all rows in a single batch. Empty slice is a no-op.
func (r *entAgentCostTrendRepo) BulkInsert(ctx context.Context, rows []AgentCostRow) error {
	if len(rows) == 0 {
		return nil
	}
	builders := make([]*ent.AgentCostTrendCreate, len(rows))
	for i, row := range rows {
		builders[i] = r.client.AgentCostTrend.Create().
			SetID(uuid.New().String()).
			SetSessionID(row.SessionID).
			SetModel(row.Model).
			SetInputTokens(row.InputTokens).
			SetOutputTokens(row.OutputTokens).
			SetCostUsd(row.CostUSD).
			SetRecordedAt(row.RecordedAt)
	}
	if _, err := r.client.AgentCostTrend.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("agentcosttrend.BulkInsert: %w", err)
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
