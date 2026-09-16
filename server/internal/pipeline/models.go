package pipeline

import (
	"slices"
	"strconv"
	"strings"
)

// allowedModelIDs is the server-side mirror of AVAILABLE_MODELS in src/utils/models.ts.
// Keep in parity with that file — Go cannot import TypeScript; models_parity_test.go fails on drift.
var allowedModelIDs = map[string]bool{
	"claude-fable-5-1":  true,
	"claude-opus-5":     true,
	"claude-sonnet-5":   true,
	"claude-opus-4-8":   true,
	"claude-opus-4-6":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
}

// Model series the coded stage defaults are drawn from.
const (
	SeriesOpus   = "opus"
	SeriesSonnet = "sonnet"
	SeriesHaiku  = "haiku"
)

// IsValidModel reports whether id is a known, supported Claude model.
func IsValidModel(id string) bool {
	return allowedModelIDs[id]
}

// LatestModel returns the newest allowed model of a series. Versions compare
// numerically part by part, so claude-opus-5 beats claude-opus-4-8 and 4-10
// beats 4-8.
func LatestModel(series string) string {
	prefix := "claude-" + series + "-"
	var latest string
	var latestVersion []int
	for id := range allowedModelIDs {
		rest, ok := strings.CutPrefix(id, prefix)
		if !ok {
			continue
		}
		version := parseModelVersion(id, rest)
		if latest == "" || slices.Compare(version, latestVersion) > 0 {
			latest, latestVersion = id, version
		}
	}
	if latest == "" {
		panic("pipeline: no allowed model in series " + series)
	}
	return latest
}

func parseModelVersion(id, version string) []int {
	parts := strings.Split(version, "-")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			panic("pipeline: allowed model " + id + " has a non-numeric version")
		}
		nums[i] = n
	}
	return nums
}
