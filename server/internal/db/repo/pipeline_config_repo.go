package repo

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/pipelineconfig"
)

type PipelineConfigRepo interface {
	GetNumber(ctx context.Context, key string, fallback float64) float64
	Set(ctx context.Context, key, value string) error
	GetAll(ctx context.Context) (map[string]string, error)
}

type entPipelineConfigRepo struct{ client *ent.Client }

func NewPipelineConfigRepo(client *ent.Client) PipelineConfigRepo {
	return &entPipelineConfigRepo{client: client}
}

func (r *entPipelineConfigRepo) GetNumber(ctx context.Context, key string, fallback float64) float64 {
	cfg, err := r.client.PipelineConfig.Query().Where(pipelineconfig.ID(key)).Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			slog.Error("pipeline_config: db lookup", "key", key, "err", err)
		}
		return fallback
	}
	n, err := strconv.ParseFloat(cfg.Value, 64)
	if err != nil {
		slog.Error("pipeline_config: parse float", "key", key, "err", err)
		return fallback
	}
	return n
}

func (r *entPipelineConfigRepo) Set(ctx context.Context, key, value string) error {
	// Try create first; on unique constraint error, update.
	err := r.client.PipelineConfig.Create().
		SetID(key).
		SetValue(value).
		Exec(ctx)
	if err == nil {
		return nil
	}
	if ent.IsConstraintError(err) {
		updateErr := r.client.PipelineConfig.UpdateOneID(key).SetValue(value).Exec(ctx)
		if updateErr != nil {
			return fmt.Errorf("pipelineconfig.Set: %w", updateErr)
		}
		return nil
	}
	return fmt.Errorf("pipelineconfig.Set: %w", err)
}

func (r *entPipelineConfigRepo) GetAll(ctx context.Context) (map[string]string, error) {
	cfgs, err := r.client.PipelineConfig.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("pipelineconfig.GetAll: %w", err)
	}
	result := make(map[string]string, len(cfgs))
	for _, cfg := range cfgs {
		result[cfg.ID] = cfg.Value
	}
	return result, nil
}
