// Package claudemodel holds the one list of Claude models the dashboard
// accepts and derives every default model from it, so a default follows the
// newest model of its series instead of pinning an ID. It imports nothing
// from this module and can be used from any layer.
package claudemodel

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Model series a default is drawn from.
const (
	Opus   = "opus"
	Sonnet = "sonnet"
	Haiku  = "haiku"
	Fable  = "fable"
)

// ids is the server-side mirror of AVAILABLE_MODELS in src/utils/models.ts.
// Go cannot import TypeScript; catalog_test.go fails when the two drift.
var ids = map[string]bool{
	"claude-fable-5-1":  true,
	"claude-opus-5":     true,
	"claude-sonnet-5":   true,
	"claude-opus-4-8":   true,
	"claude-opus-4-6":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
}

// IDs returns the known model IDs in sorted order.
func IDs() []string {
	list := slices.Collect(maps.Keys(ids))
	slices.Sort(list)
	return list
}

// IsKnown reports whether id is a known, supported Claude model.
func IsKnown(id string) bool {
	return ids[id]
}

// Latest returns the newest known model of a series. Versions compare
// numerically part by part, so claude-opus-5 beats claude-opus-4-8 and 4-10
// beats 4-8.
func Latest(series string) string {
	prefix := "claude-" + series + "-"
	var latest string
	var latestVersion []int
	for id := range ids {
		rest, ok := strings.CutPrefix(id, prefix)
		if !ok {
			continue
		}
		version := parseVersion(id, rest)
		if latest == "" || slices.Compare(version, latestVersion) > 0 {
			latest, latestVersion = id, version
		}
	}
	if latest == "" {
		panic("claudemodel: no known model in series " + series)
	}
	return latest
}

func parseVersion(id, version string) []int {
	parts := strings.Split(version, "-")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			panic("claudemodel: model " + id + " has a non-numeric version")
		}
		nums[i] = n
	}
	return nums
}
