// server/internal/agentbroadcast/loop.go
package agentbroadcast

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// Run starts a ticker loop that scans agents every interval and broadcasts
// the JSON result to all SSE subscribers. Stops when ctx is cancelled.
func Run(ctx context.Context, broadcaster *sse.Broadcaster, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			agents, err := merger.GetAgents(ctx)
			if err != nil {
				slog.Error("agent scan failed", "err", err)
				continue
			}
			// Wrap in {agents, trend} — SSE client expects this shape.
			// Typed struct avoids reflection-based marshaling of map[string]any.
			type broadcastFrame struct {
				Agents []sdk.Agent `json:"agents"`
				Trend  []any       `json:"trend"`
			}
			data, err := json.Marshal(broadcastFrame{Agents: agents, Trend: []any{}})
			if err != nil {
				slog.Error("agent marshal failed", "err", err)
				continue
			}
			broadcaster.Broadcast(data)
		case <-ctx.Done():
			return
		}
	}
}
