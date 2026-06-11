package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// validStageModelKeys lists the three agent-driven stages that support a per-stage model override.
var validStageModelKeys = [3]string{"implementation", "self_review", "finalization"}

// pipelineConfigResponse is the canonical GET /api/pipeline/config payload.
type pipelineConfigResponse struct {
	MaxParallelOrchestrators int               `json:"maxParallelOrchestrators"`
	StageTimeoutSeconds      int               `json:"stageTimeoutSeconds"`
	MaxAutoRetries           int               `json:"maxAutoRetries"`
	RetryBackoffSeconds      int               `json:"retryBackoffSeconds"`
	ExtraSafeBashCommands    string            `json:"extraSafeBashCommands"`
	StageModels              map[string]string `json:"stageModels"`
}

func (h *Handler) getPipelineConfig(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	maxParallel := int(h.cfgRepo.GetNumber(ctx, "maxParallelOrchestrators", 3))
	stageTimeout := int(h.cfgRepo.GetNumber(ctx, "stageTimeoutSeconds", db.DefaultStageTimeoutSeconds))
	maxAutoRetries := int(h.cfgRepo.GetNumber(ctx, "maxAutoRetries", 3))
	retryBackoffSeconds := int(h.cfgRepo.GetNumber(ctx, "retryBackoffSeconds", 60))
	extraSafeBashCommands := h.cfgRepo.GetString(ctx, "extraSafeBashCommands", "")

	stageModels := make(map[string]string, len(validStageModelKeys))
	for _, stage := range validStageModelKeys {
		stageModels[stage] = h.orchestrator.EffectiveStageModel(ctx, stage)
	}

	return jsonReply(w, http.StatusOK, pipelineConfigResponse{
		MaxParallelOrchestrators: maxParallel,
		StageTimeoutSeconds:      stageTimeout,
		MaxAutoRetries:           maxAutoRetries,
		RetryBackoffSeconds:      retryBackoffSeconds,
		ExtraSafeBashCommands:    extraSafeBashCommands,
		StageModels:              stageModels,
	})
}

func (h *Handler) putPipelineConfig(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		MaxParallelOrchestrators *int              `json:"maxParallelOrchestrators"`
		StageTimeoutSeconds      *int              `json:"stageTimeoutSeconds"`
		MaxAutoRetries           *int              `json:"maxAutoRetries"`
		RetryBackoffSeconds      *int              `json:"retryBackoffSeconds"`
		ExtraSafeBashCommands    *string           `json:"extraSafeBashCommands"`
		StageModels              map[string]string `json:"stageModels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("pipeline_config.put: decode: %w", err)
	}
	ctx := r.Context()
	if body.MaxParallelOrchestrators != nil {
		if *body.MaxParallelOrchestrators < 1 {
			return fmt.Errorf("pipeline_config.put: maxParallelOrchestrators must be >= 1")
		}
		if err := h.cfgRepo.Set(ctx, "maxParallelOrchestrators", strconv.Itoa(*body.MaxParallelOrchestrators)); err != nil {
			return fmt.Errorf("pipeline_config.put: %w", err)
		}
	}
	if body.StageTimeoutSeconds != nil {
		if *body.StageTimeoutSeconds < 0 {
			return fmt.Errorf("pipeline_config.put: stageTimeoutSeconds must be >= 0")
		}
		if err := h.cfgRepo.Set(ctx, "stageTimeoutSeconds", strconv.Itoa(*body.StageTimeoutSeconds)); err != nil {
			return fmt.Errorf("pipeline_config.put: %w", err)
		}
	}
	if body.MaxAutoRetries != nil {
		if *body.MaxAutoRetries < 0 {
			return fmt.Errorf("pipeline_config.put: maxAutoRetries must be >= 0")
		}
		if err := h.cfgRepo.Set(ctx, "maxAutoRetries", strconv.Itoa(*body.MaxAutoRetries)); err != nil {
			return fmt.Errorf("pipeline_config.put: %w", err)
		}
	}
	if body.RetryBackoffSeconds != nil {
		if *body.RetryBackoffSeconds < 0 {
			return fmt.Errorf("pipeline_config.put: retryBackoffSeconds must be >= 0")
		}
		if err := h.cfgRepo.Set(ctx, "retryBackoffSeconds", strconv.Itoa(*body.RetryBackoffSeconds)); err != nil {
			return fmt.Errorf("pipeline_config.put: %w", err)
		}
	}
	if body.ExtraSafeBashCommands != nil {
		if err := h.cfgRepo.Set(ctx, "extraSafeBashCommands", *body.ExtraSafeBashCommands); err != nil {
			return fmt.Errorf("pipeline_config.put: %w", err)
		}
	}
	// Write only the valid stage keys that have a non-empty value.
	for _, stage := range validStageModelKeys {
		model, ok := body.StageModels[stage]
		if !ok || model == "" {
			continue
		}
		if err := h.cfgRepo.Set(ctx, "stageModel."+stage, model); err != nil {
			return fmt.Errorf("pipeline_config.put: stageModel.%s: %w", stage, err)
		}
	}
	h.orchestrator.InvalidateConfigCache()
	raw := h.cfgRepo.GetString(ctx, "extraSafeBashCommands", "")
	permissions.SetExtraSafeBashCommands(permissions.ParseExtraSafeBashCommands(raw))
	return h.getPipelineConfig(w, r)
}

func (h *Handler) getPipelineRecommendation(w http.ResponseWriter, r *http.Request) error {
	// Lightweight heuristic: cap parallel tasks at max(1, numCPU/2).
	cores := runtime.NumCPU()
	recommended := cores / 2
	if recommended < 1 {
		recommended = 1
	}
	return jsonReply(w, http.StatusOK, map[string]int{"maxParallelOrchestrators": recommended})
}
