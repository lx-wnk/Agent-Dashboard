package eval

import (
	"math"
	"testing"
)

func TestDetectDrift_RateDropFires(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.60, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricSuccessRate: {Mean: 0.80, Stddev: 0.05, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.MetricKey != MetricSuccessRate {
		t.Errorf("wrong metric key: %s", f.MetricKey)
	}
	if f.Direction != "rate_drop" {
		t.Errorf("wrong direction: %s", f.Direction)
	}
	// 0.80 - 0.60 = 0.20 → 20pp; threshold = 10pp
	wantDelta := 20.0
	if math.Abs(f.Delta-wantDelta) > 0.001 {
		t.Errorf("delta: got %.3f, want %.3f", f.Delta, wantDelta)
	}
	if f.Threshold != 10.0 {
		t.Errorf("threshold: got %.1f, want 10.0", f.Threshold)
	}
}

func TestDetectDrift_RateDropBelowThresholdSilent(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	// 5pp drop, threshold is 10pp → should NOT fire
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.75, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricSuccessRate: {Mean: 0.80, Stddev: 0.05, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
}

func TestDetectDrift_ContinuousIncreaseFires(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "implement"}
	// baseline mean=100, stddev=10, k=2 → threshold=120; recent=130 → fires
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricMeanCostCents, Value: 130, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricMeanCostCents: {Mean: 100, Stddev: 10, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Direction != "continuous_increase" {
		t.Errorf("direction: %s", f.Direction)
	}
	if math.Abs(f.Delta-30.0) > 0.001 {
		t.Errorf("delta: got %.3f, want 30.0", f.Delta)
	}
	if math.Abs(f.Threshold-20.0) > 0.001 {
		t.Errorf("threshold: got %.3f, want 20.0", f.Threshold)
	}
}

func TestDetectDrift_ContinuousBelowThresholdSilent(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "implement"}
	// recent=115 < 100+2*10=120 → silent
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricMeanCostCents, Value: 115, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricMeanCostCents: {Mean: 100, Stddev: 10, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
}

func TestDetectDrift_MinSampleGuardSuppressesRecent(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	// Only 3 recent samples — below MinSamples=5 → suppressed
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.50, SampleCount: 3}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricSuccessRate: {Mean: 0.90, Stddev: 0.02, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings (thin recent), got %d", len(findings))
	}
}

func TestDetectDrift_MinSampleGuardSuppressesBaseline(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	// Only 3 baseline samples — below MinSamples=5 → suppressed
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.50, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricSuccessRate: {Mean: 0.90, Stddev: 0.02, SampleCount: 3}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings (thin baseline), got %d", len(findings))
	}
}

func TestDetectDrift_ImprovementDoesNotFire(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	// success_rate rising from 0.70 to 0.95 — improvement, never an alert
	recent := map[Dimension][]MetricValue{
		dim: {
			{Key: MetricSuccessRate, Value: 0.95, SampleCount: 20},
			// cost falling is also an improvement for HigherIsWorse=true metric
			{Key: MetricMeanCostCents, Value: 50, SampleCount: 20},
		},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {
			MetricSuccessRate:   {Mean: 0.70, Stddev: 0.05, SampleCount: 50},
			MetricMeanCostCents: {Mean: 100, Stddev: 5, SampleCount: 50},
		},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings for improvements, got %d", len(findings))
	}
}

func TestDetectDrift_NoBaselineSilent(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "gpt-4", Stage: "review"}
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.50, SampleCount: 20}},
	}
	// baseline is empty — no reference → skip
	baseline := map[Dimension]map[string]Baseline{}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 0 {
		t.Errorf("expected no findings (no baseline), got %d", len(findings))
	}
}

func TestDetectDrift_DeterministicOrdering(t *testing.T) {
	dimA := Dimension{SpawnerID: "sp1", Model: "m", Stage: "aaa"}
	dimB := Dimension{SpawnerID: "sp1", Model: "m", Stage: "zzz"}
	recent := map[Dimension][]MetricValue{
		dimA: {{Key: MetricSuccessRate, Value: 0.50, SampleCount: 20}},
		dimB: {{Key: MetricSuccessRate, Value: 0.50, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dimA: {MetricSuccessRate: {Mean: 0.90, Stddev: 0.01, SampleCount: 50}},
		dimB: {MetricSuccessRate: {Mean: 0.90, Stddev: 0.01, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Dim.Stage > findings[1].Dim.Stage {
		t.Errorf("results not sorted by stage: %s > %s", findings[0].Dim.Stage, findings[1].Dim.Stage)
	}
}

func TestDetectDrift_FindingFieldsPopulated(t *testing.T) {
	dim := Dimension{SpawnerID: "myspawner", Model: "claude-opus-4", Stage: "implement"}
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricSuccessRate, Value: 0.55, SampleCount: 30}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricSuccessRate: {Mean: 0.80, Stddev: 0.03, SampleCount: 100}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]

	if f.Dim != dim {
		t.Errorf("Dim mismatch: %+v", f.Dim)
	}
	if f.MetricKey != MetricSuccessRate {
		t.Errorf("MetricKey: %s", f.MetricKey)
	}
	if math.Abs(f.BaselineValue-0.80) > 0.001 {
		t.Errorf("BaselineValue: %.3f", f.BaselineValue)
	}
	if math.Abs(f.RecentValue-0.55) > 0.001 {
		t.Errorf("RecentValue: %.3f", f.RecentValue)
	}
	if f.SampleCount != 30 {
		t.Errorf("SampleCount: %d", f.SampleCount)
	}
}

func TestDetectDrift_HigherIsWorseRateFires(t *testing.T) {
	dim := Dimension{SpawnerID: "sp1", Model: "m", Stage: "review"}
	// awaiting_user_rate: HigherIsWorse=true; rising from 0.05 to 0.20 = +15pp → fires
	recent := map[Dimension][]MetricValue{
		dim: {{Key: MetricAwaitingUserRate, Value: 0.20, SampleCount: 20}},
	}
	baseline := map[Dimension]map[string]Baseline{
		dim: {MetricAwaitingUserRate: {Mean: 0.05, Stddev: 0.01, SampleCount: 50}},
	}
	th := Thresholds{RateDropPP: 10, StddevK: 2.0, MinSamples: 5}

	findings := DetectDrift(recent, baseline, th)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Direction != "rate_drop" {
		t.Errorf("direction: %s", findings[0].Direction)
	}
}
