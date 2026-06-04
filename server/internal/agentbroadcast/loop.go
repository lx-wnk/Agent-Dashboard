// server/internal/agentbroadcast/loop.go
package agentbroadcast

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"time"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// heartbeatInterval is the maximum time between SSE frames. A comment frame
// (SSE `: heartbeat`) is sent when no data frame has been broadcast within
// this window, preventing reverse-proxies from closing idle connections.
const heartbeatInterval = 30 * time.Second

// emptyTrend is a pre-allocated empty slice used in every broadcast frame to
// avoid a per-tick heap allocation for the "trend" field. (F-PERF-014)
var emptyTrend = []any{}

// broadcastFrame is the JSON envelope expected by the SSE client.
// Typed struct avoids reflection-based marshaling of map[string]any.
type broadcastFrame struct {
	Agents []sdk.Agent `json:"agents"`
	Trend  []any       `json:"trend"`
}

// BaselineProvider returns the average per-session cost over the past 7 days
// in USD, used by the agent health score's cost-spike component. A nil provider
// (or one that returns 0) disables the cost penalty. It is called once per scan
// tick.
type BaselineProvider func(ctx context.Context) float64

// Run starts a ticker loop that scans agents every interval and broadcasts
// the JSON result to all SSE subscribers. Stops when ctx is cancelled.
//
// baseline, when non-nil, is consulted once per tick to derive the cost
// baseline injected into merger.GetAgents for the health score. It may be nil
// (no baseline → no cost penalty), which preserves correct behaviour.
//
// Optimisations applied:
//   - F-PERF-001: Skip scan entirely when SubscriberCount == 0.
//   - F-PERF-006: Hash-dedupe frames with FNV-64a; send a heartbeat comment
//     every 30 s so reverse-proxies do not close idle connections.
//   - F-PERF-014: emptyTrend is a package-level var, not a per-tick literal.
func Run(ctx context.Context, broadcaster *sse.Broadcaster, interval time.Duration, baseline BaselineProvider) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	var lastHash uint64
	var lastBroadcast time.Time

	for {
		select {
		case <-ticker.C:
			// F-PERF-001: no subscribers — skip the expensive scan entirely.
			if broadcaster.SubscriberCount() == 0 {
				continue
			}

			var baselineCost float64
			if baseline != nil {
				baselineCost = baseline(ctx)
			}

			agents, err := merger.GetAgents(ctx, merger.GetAgentsOpts{BaselinePerSessionCostUSD: baselineCost})
			if err != nil {
				slog.Error("agent scan failed", "err", err)
				continue
			}

			data, err := json.Marshal(broadcastFrame{Agents: agents, Trend: emptyTrend})
			if err != nil {
				slog.Error("agent marshal failed", "err", err)
				continue
			}

			// F-PERF-006: hash-dedupe — skip broadcast if payload is identical
			// to the last one sent.
			h := fnv64a(data)
			if h == lastHash {
				continue
			}

			lastHash = h
			lastBroadcast = time.Now()
			broadcaster.Broadcast(data)

		case t := <-heartbeat.C:
			// F-PERF-006: send a heartbeat comment frame so proxies/load-balancers
			// do not close idle SSE connections when there is no data change.
			if broadcaster.SubscriberCount() == 0 {
				continue
			}
			if t.Sub(lastBroadcast) >= heartbeatInterval {
				broadcaster.BroadcastComment([]byte("heartbeat"))
				lastBroadcast = t
			}

		case <-ctx.Done():
			return
		}
	}
}

// fnv64a returns the FNV-64a hash of b. Using stdlib hash/fnv avoids
// introducing a new dependency. (F-PERF-006)
func fnv64a(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
