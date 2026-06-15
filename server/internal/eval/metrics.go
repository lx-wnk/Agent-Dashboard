// Package eval provides metric collection and drift detection for pipeline stage runs.
package eval

// Metric key constants — single source of truth for all metric identifiers.
const (
	MetricSuccessRate              = "success_rate"
	MetricMeanIterations           = "mean_iterations_to_success"
	MetricFirstIterValidationFail  = "first_iter_validation_fail_rate"
	MetricAwaitingUserRate         = "awaiting_user_rate"
	MetricEscalationRate           = "escalation_rate"
	MetricMeanDurationSeconds      = "mean_duration_seconds"
	MetricMeanCostCents            = "mean_cost_cents"
	MetricMeanTokens               = "mean_tokens"
	MetricTimeoutRate              = "timeout_rate"
)

// MetricKind classifies how a metric is interpreted by the drift algorithm.
type MetricKind int

const (
	KindRate       MetricKind = iota // value in [0,1]; threshold in percentage points
	KindContinuous                   // unbounded; threshold via mean + k*stddev
)

// MetricDef describes a single metric: its identity, kind, and worsening direction.
type MetricDef struct {
	Key           string
	Kind          MetricKind
	HigherIsWorse bool
	Label         string
}

// AllMetrics is the authoritative slice of all tracked metrics. Drift detection
// and snapshot insertion iterate this slice — no other file may hardcode metric keys.
var AllMetrics = []MetricDef{
	{Key: MetricSuccessRate, Kind: KindRate, HigherIsWorse: false, Label: "Success Rate"},
	{Key: MetricMeanIterations, Kind: KindContinuous, HigherIsWorse: true, Label: "Mean Iterations to Success"},
	{Key: MetricFirstIterValidationFail, Kind: KindRate, HigherIsWorse: true, Label: "First-Iter Validation Fail Rate"},
	{Key: MetricAwaitingUserRate, Kind: KindRate, HigherIsWorse: true, Label: "Awaiting User Rate"},
	{Key: MetricEscalationRate, Kind: KindRate, HigherIsWorse: true, Label: "Escalation Rate"},
	{Key: MetricMeanDurationSeconds, Kind: KindContinuous, HigherIsWorse: true, Label: "Mean Duration (s)"},
	{Key: MetricMeanCostCents, Kind: KindContinuous, HigherIsWorse: true, Label: "Mean Cost (cents)"},
	{Key: MetricMeanTokens, Kind: KindContinuous, HigherIsWorse: true, Label: "Mean Tokens"},
	{Key: MetricTimeoutRate, Kind: KindRate, HigherIsWorse: true, Label: "Timeout Rate"},
}

// metricIndex is a lookup map built once at init time for O(1) access by key.
var metricIndex = func() map[string]MetricDef {
	m := make(map[string]MetricDef, len(AllMetrics))
	for _, d := range AllMetrics {
		m[d.Key] = d
	}
	return m
}()

// MetricDefFor returns the MetricDef for the given key and whether it was found.
func MetricDefFor(key string) (MetricDef, bool) {
	d, ok := metricIndex[key]
	return d, ok
}
