package llmadapter

import (
	"fmt"
	"os"
	"os/exec"
)

// resolveAnthropicSpawnerPath locates the out-of-process anthropic-spawner
// binary: the DASHBOARD_ANTHROPIC_SPAWNER_CMD env var wins; otherwise it is
// looked up on PATH. Returns a clear error when unresolvable so a misconfigured
// deployment fails loudly rather than silently producing no agent output.
func resolveAnthropicSpawnerPath() (string, error) {
	if p := os.Getenv("DASHBOARD_ANTHROPIC_SPAWNER_CMD"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("anthropic-spawner"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("anthropic adapter: spawner binary not found — set DASHBOARD_ANTHROPIC_SPAWNER_CMD to the anthropic-spawner path")
}
