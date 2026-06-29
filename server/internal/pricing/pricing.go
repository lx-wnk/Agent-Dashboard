// Package pricing provides per-model cost estimation for Claude API token usage.
// It is intentionally kept free of parser, pipeline, and db dependencies so it
// can be imported from any layer without creating a cycle.
package pricing

import "github.com/lx-wnk/agent-dashboard/sdk"

// modelPricingEntry holds per-million-token USD prices for a single model.
type modelPricingEntry struct {
	Input, Output, CacheRead, CacheCreate float64
}

// modelPricing stores per-million-token USD prices for known Claude models.
var modelPricing = map[string]modelPricingEntry{
	"claude-opus-4-6":   {15, 75, 1.5, 18.75},
	"claude-opus-4-0":   {15, 75, 1.5, 18.75},
	"claude-sonnet-4-6": {3, 15, 0.3, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
	"claude-haiku-4-5":  {0.8, 4, 0.08, 1},

	// OpenAI — source: platform.openai.com/pricing (verify before releasing).
	// Cache read = 50% of input price per OpenAI caching docs; cache write = $0.
	"gpt-5":       {5, 20, 2.5, 0},
	"gpt-5-codex": {5, 20, 2.5, 0}, // fixture model; same rate until a separate entry is published

	// Google Gemini — source: ai.google.dev/pricing (verify before releasing).
	// Context caching prices omitted (tier-dependent); set once confirmed.
	"gemini-2.5-pro":   {1.25, 10, 0, 0},
	"gemini-2.5-flash": {0.075, 0.30, 0, 0},
}

const defaultModel = "claude-sonnet-4-6"

// lookupModel returns the pricing entry for the given model, falling back to
// defaultModel when the model string is not recognised.
func lookupModel(model string) modelPricingEntry {
	p, ok := modelPricing[model]
	if !ok {
		p = modelPricing[defaultModel]
	}
	return p
}

// HasPricing reports whether the pricing table contains an exact entry for the
// given model. Used by callers that need to distinguish "we have no idea what
// this costs" (e.g. Codex / Gemini models) from "Claude model, default-priced".
func HasPricing(model string) bool {
	_, ok := modelPricing[model]
	return ok
}

// EstimateCost returns the estimated USD cost for a given token usage and model.
func EstimateCost(usage sdk.TokenUsage, model string) float64 {
	p := lookupModel(model)
	const m = 1_000_000.0
	return float64(usage.InputTokens)*p.Input/m +
		float64(usage.OutputTokens)*p.Output/m +
		float64(usage.CacheReadTokens)*p.CacheRead/m +
		float64(usage.CacheCreationTokens)*p.CacheCreate/m
}

// EstimateCacheCreationCost returns only the cache-write cost component.
func EstimateCacheCreationCost(usage sdk.TokenUsage, model string) float64 {
	p := lookupModel(model)
	return float64(usage.CacheCreationTokens) * p.CacheCreate / 1_000_000.0
}

// EstimateCacheReadCost returns only the cache-read cost component.
func EstimateCacheReadCost(usage sdk.TokenUsage, model string) float64 {
	p := lookupModel(model)
	return float64(usage.CacheReadTokens) * p.CacheRead / 1_000_000.0
}
