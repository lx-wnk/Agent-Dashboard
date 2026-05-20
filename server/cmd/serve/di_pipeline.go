package main

import (
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// buildSpawnerForName returns the LLMSpawner for the given adapter name,
// reading adapter-specific configuration from cfg. Returns nil for "claude" and
// any unknown name (nil signals stage_handlers.go to use the native SpawnStageAgent path).
func buildSpawnerForName(name string, cfg config.Config) pipeline.LLMSpawner {
	switch name {
	case "ollama":
		return &pipeline.OllamaSpawner{
			Host:         cfg.Adapters.Ollama.Host,
			DefaultModel: cfg.Adapters.Ollama.DefaultModel,
		}
	case "openai":
		return &pipeline.OpenAISpawner{
			BaseURL:      cfg.Adapters.OpenAI.BaseURL,
			APIKeyEnv:    cfg.Adapters.OpenAI.APIKeyEnv,
			DefaultModel: cfg.Adapters.OpenAI.DefaultModel,
		}
	default: // "claude" and any unknown — nil signals native spawn path
		return nil
	}
}

func provideSpawner(cfg config.Config) pipeline.LLMSpawner {
	if cmd := config.SpawnCommandFromEnv(); cmd != "" {
		return &pipeline.CustomCommandSpawner{Command: cmd}
	}

	defaultSpawner := buildSpawnerForName(cfg.Adapters.Default, cfg)

	// If no per-stage overrides are configured, return the default spawner directly.
	if len(cfg.Adapters.Stages) == 0 {
		return defaultSpawner
	}

	// Build per-stage spawners. Only include entries with a non-nil spawner;
	// stages mapped to "claude" (nil) are intentionally omitted so they fall
	// through to DefaultSpawner in PerStageSpawner.Spawn.
	stageSpawners := make(map[string]pipeline.LLMSpawner, len(cfg.Adapters.Stages))
	for stage, adapterName := range cfg.Adapters.Stages {
		if s := buildSpawnerForName(adapterName, cfg); s != nil {
			stageSpawners[stage] = s
		}
	}

	// If no non-nil overrides exist and the default is also nil (all native Claude),
	// no wrapping is needed.
	if defaultSpawner == nil && len(stageSpawners) == 0 {
		return nil
	}

	return &pipeline.PerStageSpawner{
		DefaultSpawner: defaultSpawner,
		StageSpawners:  stageSpawners,
	}
}

func provideOrchestrator(cfg config.Config, client *ent.Client, tb *sse.TaskBroadcaster, systemPromptRepo repo.SystemPromptRepo) (*pipeline.PipelineOrchestrator, error) {
	if client == nil {
		return nil, nil
	}
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		Client:           client,
		TaskRepo:         taskRepo,
		StageRunRepo:     srRepo,
		PermissionRepo:   permRepo,
		AuditRepo:        auditRepo,
		ConfigRepo:       cfgRepo,
		SystemPromptRepo: systemPromptRepo,
		MCPToken:         cfg.MCPToken,
		MCPUrl:           fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		WorktreeRoot:     cfg.WorktreeRoot,
		Spawner:          provideSpawner(cfg),
		OnTaskChanged: func(taskID string, transitionKind string) {
			tb.Broadcast(sse.TaskEvent{
				Type:   "task_changed",
				TaskID: taskID,
				Payload: map[string]string{
					"transitionKind": transitionKind,
				},
			})
		},
	})
	return orch, err
}
