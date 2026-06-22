package merger

import (
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/sdk"
)

// pricingAdapter exposes the parser pricing wrappers as a provider.Pricing.
type pricingAdapter struct{}

func (pricingAdapter) HasPricing(model string) bool { return parser.HasPricing(model) }
func (pricingAdapter) EstimateCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCost(u, model)
}
func (pricingAdapter) EstimateCacheCreationCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCacheCreationCost(u, model)
}
func (pricingAdapter) EstimateCacheReadCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCacheReadCost(u, model)
}
