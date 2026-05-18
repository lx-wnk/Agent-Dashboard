// Package pricing provides per-model token cost data for Claude models.
package pricing

import "github.com/lx-wnk/agent-dashboard/sdk"

// ModelPrice holds per-million-token USD prices for a single model.
type ModelPrice struct {
	Input, Output, CacheRead, CacheCreate float64
}

// DefaultModel is the fallback model used when a session's model is not in the table.
const DefaultModel = "claude-sonnet-4-6"

// Table stores per-million-token USD prices for known Claude models.
var Table = map[string]ModelPrice{
	"claude-opus-4-6":   {15, 75, 1.5, 18.75},
	"claude-opus-4-0":   {15, 75, 1.5, 18.75},
	"claude-sonnet-4-6": {3, 15, 0.3, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
	"claude-haiku-4-5":  {0.8, 4, 0.08, 1},
}

// Lookup returns the price entry for model, falling back to DefaultModel if not found.
func Lookup(model string) ModelPrice {
	if p, ok := Table[model]; ok {
		return p
	}
	return Table[DefaultModel]
}

// EstimateCost returns the estimated USD cost for a given token usage and model.
func EstimateCost(usage sdk.TokenUsage, model string) float64 {
	p := Lookup(model)
	const m = 1_000_000.0
	return float64(usage.InputTokens)*p.Input/m +
		float64(usage.OutputTokens)*p.Output/m +
		float64(usage.CacheReadTokens)*p.CacheRead/m +
		float64(usage.CacheCreationTokens)*p.CacheCreate/m
}

// EstimateCacheCreationCost returns only the cache-write cost component.
func EstimateCacheCreationCost(usage sdk.TokenUsage, model string) float64 {
	p := Lookup(model)
	return float64(usage.CacheCreationTokens) * p.CacheCreate / 1_000_000.0
}

// EstimateCacheReadCost returns only the cache-read cost component.
func EstimateCacheReadCost(usage sdk.TokenUsage, model string) float64 {
	p := Lookup(model)
	return float64(usage.CacheReadTokens) * p.CacheRead / 1_000_000.0
}
