package merger_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/stretchr/testify/require"
)

// toolCounts builds a ToolCounts map whose values sum to total.
func toolCounts(total int) map[string]int {
	if total <= 0 {
		return map[string]int{}
	}
	return map[string]int{"Bash": total}
}

func TestComputeHealthScore(t *testing.T) {
	tests := []struct {
		name         string
		session      *parser.SessionData
		costEstimate float64
		costUnknown  bool
		baseline     float64
		wantMin      int
		wantMax      int
	}{
		{
			name: "zero_turns_no_data",
			session: &parser.SessionData{
				ConversationTurns: 0,
				ToolCounts:        toolCounts(0),
			},
			wantMin: 50, wantMax: 50,
		},
		{
			// successRate 100, cacheHitPct 80, errorComp 100, cost penalty 100 (at baseline)
			// 0.40*100 + 0.25*80 + 0.25*100 + 0.10*100 = 95
			name: "perfect_session",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 80, CacheCreationTokens: 20},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 1.0,
			baseline:     1.0,
			wantMin:      95, wantMax: 100,
		},
		{
			// otherwise perfect but quota error -> hard cap at 30
			name: "quota_error_hard_cap",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 80, CacheCreationTokens: 20},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
				ErrorState:        sdk.ErrorStateQuotaExhausted,
			},
			costEstimate: 1.0,
			baseline:     1.0,
			wantMin:      0, wantMax: 30,
		},
		{
			// successRate 50, cacheHitPct 50 (neutral, no cache), errorComp 100, cost 100 (no baseline)
			// 0.40*50 + 0.25*50 + 0.25*100 + 0.10*100 = 67.5 -> 68
			name: "high_tool_error_rate",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				Meta:              &sdk.SessionMeta{ToolErrors: 5},
			},
			wantMin: 60, wantMax: 75,
		},
		{
			// successRate 100, cacheHitPct 50 (neutral), errorComp 100, cost 100
			// 0.40*100 + 0.25*50 + 0.25*100 + 0.10*100 = 87.5 -> 88
			name: "no_cache_usage",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			wantMin: 85, wantMax: 90,
		},
		{
			// successRate 100, cacheHitPct 100, errorComp 100, cost penalty 50 (2x baseline)
			// 0.40*100 + 0.25*100 + 0.25*100 + 0.10*50 = 95
			name: "cost_spike_2x",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 100, CacheCreationTokens: 0},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 2.0,
			baseline:     1.0,
			wantMin:      93, wantMax: 97,
		},
		{
			// cost penalty 0 (3x baseline)
			// 0.40*100 + 0.25*100 + 0.25*100 + 0.10*0 = 90
			name: "cost_spike_3x_plus",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 100, CacheCreationTokens: 0},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 3.0,
			baseline:     1.0,
			wantMin:      88, wantMax: 92,
		},
		{
			// perfect session, no baseline -> cost penalty 100
			name: "no_baseline_no_penalty",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 80, CacheCreationTokens: 20},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 5.0,
			baseline:     0,
			wantMin:      95, wantMax: 100,
		},
		{
			// cost unknown -> no cost penalty even with baseline set
			name: "cost_unknown",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 80, CacheCreationTokens: 20},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 0,
			costUnknown:  true,
			baseline:     1.0,
			wantMin:      95, wantMax: 100,
		},
		{
			// nil meta -> treat ToolErrors as 0 -> successRate 100
			// successRate 100, cacheHitPct 50, errorComp 100, cost 100 -> 87.5 -> 88
			name: "nil_meta",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				Meta:              nil,
			},
			wantMin: 85, wantMax: 90,
		},
		{
			// all components max -> exactly 100
			name: "clamp_above_100",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 100, CacheCreationTokens: 0},
				Meta:              &sdk.SessionMeta{ToolErrors: 0},
			},
			costEstimate: 0.5,
			baseline:     1.0,
			wantMin:      100, wantMax: 100,
		},
		{
			// all components 0: successRate 0 (errors > calls), cacheHitPct 0 (create only),
			// errorComp 0 (error state present -> also hard cap 30), cost 0 (>3x)
			// raw = 0; error state present caps to 30 but raw is already 0
			name: "clamp_below_0",
			session: &parser.SessionData{
				ConversationTurns: 5,
				ToolCounts:        toolCounts(10),
				TokenUsage:        sdk.TokenUsage{CacheReadTokens: 0, CacheCreationTokens: 100},
				Meta:              &sdk.SessionMeta{ToolErrors: 100},
				ErrorState:        sdk.ErrorStateRateLimited,
			},
			costEstimate: 10.0,
			baseline:     1.0,
			wantMin:      0, wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := merger.ComputeHealthScore(tt.session, tt.costEstimate, tt.costUnknown, tt.baseline)
			require.GreaterOrEqual(t, got, tt.wantMin, "score below expected min")
			require.LessOrEqual(t, got, tt.wantMax, "score above expected max")
			require.GreaterOrEqual(t, got, 0)
			require.LessOrEqual(t, got, 100)
		})
	}
}
