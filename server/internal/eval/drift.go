package eval

import "sort"

// Thresholds controls when drift is considered significant.
type Thresholds struct {
	RateDropPP float64 // minimum percentage-point worsening to fire a rate alert
	StddevK    float64 // multiplier on baseline stddev for continuous alerts
	MinSamples int     // minimum sample count on both sides before alerting
}

// Baseline summarises historical metric values for one (dimension, metric) pair.
type Baseline struct {
	Mean        float64
	Stddev      float64
	SampleCount int
}

// DriftFinding describes one fired alert from DetectDrift.
type DriftFinding struct {
	Dim           Dimension
	MetricKey     string
	Direction     string
	BaselineValue float64
	RecentValue   float64
	Delta         float64 // signed; magnitude of the worsening change
	Threshold     float64
	SampleCount   int // recent sample count
}

// DetectDrift compares recent metric values against their baselines and returns
// one DriftFinding per (dimension, metric) pair that exceeds the configured
// threshold. Improvements never produce findings. Output is sorted by
// (Stage, MetricKey) for deterministic results.
func DetectDrift(
	recent map[Dimension][]MetricValue,
	baseline map[Dimension]map[string]Baseline,
	th Thresholds,
) []DriftFinding {
	var findings []DriftFinding

	for dim, metrics := range recent {
		dimBaseline, hasDimBaseline := baseline[dim]
		if !hasDimBaseline {
			continue
		}
		for _, mv := range metrics {
			bl, hasMetricBaseline := dimBaseline[mv.Key]
			if !hasMetricBaseline {
				continue
			}

			if mv.SampleCount < th.MinSamples || bl.SampleCount < th.MinSamples {
				continue
			}

			def, known := MetricDefFor(mv.Key)
			if !known {
				continue
			}

			var fired bool
			var direction string
			var delta, threshold float64

			switch def.Kind {
			case KindRate:
				// Convert fraction values to percentage points for comparison.
				recentPP := mv.Value * 100
				baselinePP := bl.Mean * 100

				var worsening float64
				if def.HigherIsWorse {
					// A rise in this rate is a worsening (e.g. awaiting_user_rate).
					worsening = recentPP - baselinePP
				} else {
					// A drop in this rate is a worsening (e.g. success_rate).
					worsening = baselinePP - recentPP
				}

				if worsening >= th.RateDropPP {
					fired = true
					direction = "rate_drop"
					delta = worsening
					threshold = th.RateDropPP
				}

			case KindContinuous:
				// Only HigherIsWorse direction triggers alerts; improvements are silent.
				if def.HigherIsWorse && mv.Value > bl.Mean+th.StddevK*bl.Stddev {
					fired = true
					direction = "continuous_increase"
					delta = mv.Value - bl.Mean
					threshold = th.StddevK * bl.Stddev
				}
			}

			if fired {
				findings = append(findings, DriftFinding{
					Dim:           dim,
					MetricKey:     mv.Key,
					Direction:     direction,
					BaselineValue: bl.Mean,
					RecentValue:   mv.Value,
					Delta:         delta,
					Threshold:     threshold,
					SampleCount:   mv.SampleCount,
				})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Dim.Stage != b.Dim.Stage {
			return a.Dim.Stage < b.Dim.Stage
		}
		return a.MetricKey < b.MetricKey
	})

	return findings
}
