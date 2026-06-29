package pricing_test

import (
	"math"
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/pricing"
	"github.com/stretchr/testify/require"
)

// TestHasPricing_ThirdPartyModels verifies third-party models are priced, not defaulted.
func TestHasPricing_ThirdPartyModels(t *testing.T) {
	for _, m := range []string{"gpt-5", "gpt-5-codex", "gemini-2.5-pro", "gemini-2.5-flash"} {
		if !pricing.HasPricing(m) {
			t.Errorf("HasPricing(%q) = false, want true", m)
		}
	}
}

func TestEstimateCost_CodexNonZeroAndDistinctFromDefault(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "gpt-5-codex")
	require.Greater(t, got, 0.0)
	// Must NOT silently fall back to the claude-sonnet-4-6 default.
	sonnet := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	if math.Abs(got-sonnet) < 0.001 {
		t.Errorf("gpt-5-codex cost (%f) equals sonnet default (%f) — entry missing?", got, sonnet)
	}
}

func TestEstimateCost_GeminiProNonZeroAndDistinctFromDefault(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "gemini-2.5-pro")
	require.Greater(t, got, 0.0)
	sonnet := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	if math.Abs(got-sonnet) < 0.001 {
		t.Errorf("gemini-2.5-pro cost (%f) equals sonnet default (%f) — entry missing?", got, sonnet)
	}
}

// TestEstimateCost_ZeroUsage verifies that zero tokens produce zero cost.
func TestEstimateCost_ZeroUsage(t *testing.T) {
	got := pricing.EstimateCost(sdk.TokenUsage{}, "claude-sonnet-4-6")
	require.Equal(t, 0.0, got)
}

// TestEstimateCost_Sonnet verifies known pricing for claude-sonnet-4-6.
// Rates: input $3/M, output $15/M.
func TestEstimateCost_Sonnet(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, 18.0, got, 0.001)
}

// TestEstimateCost_Opus verifies known pricing for claude-opus-4-6.
// Rates: input $15/M, output $75/M.
func TestEstimateCost_Opus(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "claude-opus-4-6")
	require.InDelta(t, 90.0, got, 0.001)
}

// TestEstimateCost_Haiku verifies known pricing for claude-haiku-4-5.
// Rates: input $0.8/M, output $4/M.
func TestEstimateCost_Haiku(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "claude-haiku-4-5")
	require.InDelta(t, 4.8, got, 0.001)
}

// TestEstimateCost_UnknownModel falls back to the sonnet-4-6 default.
func TestEstimateCost_UnknownModel(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	sonnet := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	unknown := pricing.EstimateCost(usage, "claude-fictional-model")
	require.InDelta(t, sonnet, unknown, 0.0001)
}

// TestEstimateCost_AllComponents verifies all four token types are included.
// Sonnet rates: input $3/M, output $15/M, cacheRead $0.3/M, cacheCreate $3.75/M.
func TestEstimateCost_AllComponents(t *testing.T) {
	usage := sdk.TokenUsage{
		InputTokens:         1_000_000,
		OutputTokens:        1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
	}
	got := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	want := 3.0 + 15.0 + 0.3 + 3.75 // = 22.05
	require.InDelta(t, want, got, 0.001)
}

// TestEstimateCacheCreationCost_Sonnet verifies sonnet cache-write pricing ($3.75/M).
func TestEstimateCacheCreationCost_Sonnet(t *testing.T) {
	usage := sdk.TokenUsage{CacheCreationTokens: 1_000_000}
	got := pricing.EstimateCacheCreationCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, 3.75, got, 0.001)
}

// TestEstimateCacheCreationCost_Zero verifies zero tokens produce zero cost.
func TestEstimateCacheCreationCost_Zero(t *testing.T) {
	got := pricing.EstimateCacheCreationCost(sdk.TokenUsage{}, "claude-sonnet-4-6")
	require.Equal(t, 0.0, got)
}

// TestEstimateCacheReadCost_Sonnet verifies sonnet cache-read pricing ($0.3/M).
func TestEstimateCacheReadCost_Sonnet(t *testing.T) {
	usage := sdk.TokenUsage{CacheReadTokens: 1_000_000}
	got := pricing.EstimateCacheReadCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, 0.3, got, 0.001)
}

// TestEstimateCacheReadCost_Opus verifies opus cache-read pricing ($1.5/M).
func TestEstimateCacheReadCost_Opus(t *testing.T) {
	usage := sdk.TokenUsage{CacheReadTokens: 1_000_000}
	got := pricing.EstimateCacheReadCost(usage, "claude-opus-4-6")
	require.InDelta(t, 1.5, got, 0.001)
}

// TestEstimateCost_PartialTokenCounts verifies fractional pricing (500k tokens each).
func TestEstimateCost_PartialTokenCounts(t *testing.T) {
	// 500k input + 500k output for sonnet = $1.50 + $7.50 = $9.00
	usage := sdk.TokenUsage{InputTokens: 500_000, OutputTokens: 500_000}
	got := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, 9.0, got, 0.001)
}

// TestEstimateCost_Sonnet4_5 verifies claude-sonnet-4-5 has the same price as sonnet-4-6.
func TestEstimateCost_Sonnet4_5(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got45 := pricing.EstimateCost(usage, "claude-sonnet-4-5")
	got46 := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	require.InDelta(t, got46, got45, 0.0001)
}
