package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func (h *Handler) getPipelineConfig(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	maxParallel := int(h.cfgRepo.GetNumber(ctx, "maxParallelOrchestrators", 3))
	stageTimeout := int(h.cfgRepo.GetNumber(ctx, "stageTimeoutSeconds", db.DefaultStageTimeoutSeconds))
	maxAutoRetries := int(h.cfgRepo.GetNumber(ctx, "maxAutoRetries", 3))
	retryBackoffSeconds := int(h.cfgRepo.GetNumber(ctx, "retryBackoffSeconds", 60))
	return jsonReply(w, http.StatusOK, map[string]int{
		"maxParallelOrchestrators": maxParallel,
		"stageTimeoutSeconds":      stageTimeout,
		"maxAutoRetries":           maxAutoRetries,
		"retryBackoffSeconds":      retryBackoffSeconds,
	})
}

func (h *Handler) putPipelineConfig(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		MaxParallelOrchestrators *int `json:"maxParallelOrchestrators"`
		StageTimeoutSeconds      *int `json:"stageTimeoutSeconds"`
		MaxAutoRetries           *int `json:"maxAutoRetries"`
		RetryBackoffSeconds      *int `json:"retryBackoffSeconds"`
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
	h.orchestrator.InvalidateConfigCache()
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
