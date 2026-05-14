package analytics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

const epsilon = 1e-9

func TestLinearRegression_PerfectLinearData(t *testing.T) {
	// y = 2t + 1
	pts := []dataPoint{
		{t: 0, y: 1},
		{t: 1, y: 3},
		{t: 2, y: 5},
		{t: 3, y: 7},
		{t: 4, y: 9},
	}
	slope, intercept := linearRegression(pts)
	assert.InDelta(t, 2.0, slope, epsilon, "slope should be 2")
	assert.InDelta(t, 1.0, intercept, epsilon, "intercept should be 1")
}

func TestLinearRegression_HorizontalLine(t *testing.T) {
	// All y values equal → slope must be 0.
	pts := []dataPoint{
		{t: 0, y: 5},
		{t: 1, y: 5},
		{t: 2, y: 5},
		{t: 3, y: 5},
	}
	slope, intercept := linearRegression(pts)
	assert.InDelta(t, 0.0, slope, epsilon, "slope should be 0 for horizontal line")
	assert.InDelta(t, 5.0, intercept, epsilon, "intercept should equal the constant y value")
}

func TestLinearRegression_SinglePoint(t *testing.T) {
	pts := []dataPoint{{t: 3, y: 7}}
	slope, intercept := linearRegression(pts)
	// denom == 0 with one point → slope=0, intercept=sumY/n=y[0].
	assert.InDelta(t, 0.0, slope, epsilon, "slope should be 0 for single point")
	assert.InDelta(t, 7.0, intercept, epsilon, "intercept should equal y[0] for single point")
}

func TestLinearRegression_EmptyInput(t *testing.T) {
	slope, intercept := linearRegression(nil)
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 0.0, intercept)
}

func TestLinearRegression_EmptySlice(t *testing.T) {
	slope, intercept := linearRegression([]dataPoint{})
	assert.Equal(t, 0.0, slope)
	assert.Equal(t, 0.0, intercept)
}

func TestLinearRegression_LargeUnixMsScale(t *testing.T) {
	// Simulate unix-millisecond-scale timestamps (around 2024 era) normalized to t0.
	// With normalization the values start at 0; this mirrors getCostForecast behavior.
	t0 := float64(1_700_000_000_000) // roughly Nov 2023 in unix ms
	pts := make([]dataPoint, 30)
	dayMs := float64(24 * 60 * 60 * 1000)
	for i := range pts {
		tNorm := float64(i) * dayMs
		pts[i] = dataPoint{t: tNorm, y: float64(i)*0.5 + 10.0} // y = 0.5*t/dayMs + 10
	}
	_ = t0

	slope, intercept := linearRegression(pts)

	// Verify no NaN or Inf.
	assert.False(t, math.IsNaN(slope), "slope must not be NaN")
	assert.False(t, math.IsInf(slope, 0), "slope must not be Inf")
	assert.False(t, math.IsNaN(intercept), "intercept must not be NaN")
	assert.False(t, math.IsInf(intercept, 0), "intercept must not be Inf")

	// The true slope per day-ms unit is 0.5/dayMs; verify rough magnitude.
	expectedSlope := 0.5 / dayMs
	assert.InDelta(t, expectedSlope, slope, 1e-20, "slope should match y=0.5*(t/dayMs)+10 regression")
}

func TestLinearRegression_TwoPoints(t *testing.T) {
	// Exact fit: (0,0) and (1,2) → y = 2t.
	pts := []dataPoint{{t: 0, y: 0}, {t: 1, y: 2}}
	slope, intercept := linearRegression(pts)
	assert.InDelta(t, 2.0, slope, epsilon)
	assert.InDelta(t, 0.0, intercept, epsilon)
}

func TestLinearRegression_NegativeSlope(t *testing.T) {
	// y = -3t + 10.
	pts := []dataPoint{
		{t: 0, y: 10},
		{t: 1, y: 7},
		{t: 2, y: 4},
		{t: 3, y: 1},
	}
	slope, intercept := linearRegression(pts)
	assert.InDelta(t, -3.0, slope, epsilon)
	assert.InDelta(t, 10.0, intercept, epsilon)
}
