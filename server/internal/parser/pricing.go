package parser

// Pricing functions — thin wrappers that delegate to the canonical
// server/internal/pricing package (ARCH-03).  Kept here so existing callers
// in merger/, pipeline/, and history/ can continue to import via parser without
// a migration churn; the single source of truth is pricing.Table.

import (
	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/pricing"
)

// EstimateCost returns the estimated USD cost for a given token usage and model.
// Delegates to pricing.EstimateCost — the canonical implementation.
func EstimateCost(usage sdk.TokenUsage, model string) float64 {
	return pricing.EstimateCost(usage, model)
}

// EstimateCacheCreationCost returns only the cache-write cost component.
// Delegates to pricing.EstimateCacheCreationCost.
func EstimateCacheCreationCost(usage sdk.TokenUsage, model string) float64 {
	return pricing.EstimateCacheCreationCost(usage, model)
}

// EstimateCacheReadCost returns only the cache-read cost component.
// Delegates to pricing.EstimateCacheReadCost.
func EstimateCacheReadCost(usage sdk.TokenUsage, model string) float64 {
	return pricing.EstimateCacheReadCost(usage, model)
}
