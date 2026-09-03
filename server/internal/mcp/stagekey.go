package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// StageRunScopes is the fixed transport scope set every stage-run key gets.
//
// It is deliberately narrow, and narrower than the first draft. Only the
// memory and obsidian tools resolve through capability.Decide; every other
// MCP tool is gated by its scope alone. So a scope handed out here is a
// capability granted outright, not one the gate will narrow later —
// "pipeline:control" would let a spawned agent approve its own spec and
// resolve its own permission requests, and "tasks:write" would let it widen
// its own permissions through manage_task. Neither is something an agent
// should be able to do to itself.
//
// keys:manage is excluded for the same family of reason: an agent able to
// mint keys could mint one with no stage run and escape its own attribution.
var StageRunScopes = []string{
	"tasks:read", "agent:coord",
	"memory:read", "memory:write", "obsidian:read", "obsidian:write",
}

// StageKeyTTLBuffer is added to a stage's timeout when setting expires_at. It
// covers the window between an agent hitting its timeout and the orchestrator
// recording the transition, so the key does not die under a run that is still
// being wound down.
const StageKeyTTLBuffer = 5 * time.Minute

// StageKeyIssuer mints and revokes the ephemeral MCP credentials a pipeline
// stage run presents to /api/mcp. It is the only writer of
// repo.ApiKeyKindStageRun rows.
type StageKeyIssuer struct {
	Keys repo.ApiKeyRepo
}

// Issue mints a bearer token for stageRunID and returns it. The token is
// returned once and never stored in clear — only its hash reaches the row.
func (i StageKeyIssuer) Issue(ctx context.Context, stageRunID string, stageTimeout time.Duration) (string, error) {
	if i.Keys == nil {
		return "", fmt.Errorf("mcp: StageKeyIssuer has no key repo")
	}
	if stageRunID == "" {
		return "", fmt.Errorf("mcp: refusing to issue a key with no stage run — it would be unattributable and unrevocable")
	}
	token := GenerateAPIToken()
	expires := time.Now().Add(stageTimeout + StageKeyTTLBuffer)
	if _, err := i.Keys.Create(ctx, repo.CreateApiKeyInput{
		Name:       "stage-run " + stageRunID,
		Hash:       HashToken(token),
		Scopes:     StageRunScopes,
		Kind:       repo.ApiKeyKindStageRun,
		StageRunID: stageRunID,
		ExpiresAt:  &expires,
	}); err != nil {
		return "", fmt.Errorf("mcp: issue stage-run key: %w", err)
	}
	return token, nil
}

// Revoke deactivates every key issued for stageRunID. A stage run that never
// received a key is not an error — the orchestrator calls this on every
// terminal transition, including spawns that ran without a credential.
func (i StageKeyIssuer) Revoke(ctx context.Context, stageRunID string) error {
	if i.Keys == nil || stageRunID == "" {
		return nil
	}
	if _, err := i.Keys.RevokeForStageRun(ctx, stageRunID); err != nil {
		return fmt.Errorf("mcp: revoke stage-run keys: %w", err)
	}
	return nil
}
