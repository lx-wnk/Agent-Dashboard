package provider

import (
	"strconv"
	"strings"
)

// resolveFirst tries each candidate path in order and returns the values from
// the first path that yields at least one. Empty if all miss.
func resolveFirst(root any, paths []string) []any {
	for _, p := range paths {
		if vals := resolvePath(root, p); len(vals) > 0 {
			return vals
		}
	}
	return nil
}

// resolvePath walks a dotted path from root. A segment ending in "[]" descends
// into an array and fans out across its elements. Returns every leaf value
// reached (multiple when an array is traversed).
func resolvePath(root any, path string) []any {
	if path == "" {
		return nil
	}
	cur := []any{root}
	for _, seg := range strings.Split(path, ".") {
		array := strings.HasSuffix(seg, "[]")
		key := strings.TrimSuffix(seg, "[]")
		var next []any
		for _, node := range cur {
			m, ok := node.(map[string]any)
			if !ok {
				continue
			}
			v, ok := m[key]
			if !ok || v == nil {
				continue
			}
			if array {
				if arr, ok := v.([]any); ok {
					next = append(next, arr...)
				}
				continue
			}
			next = append(next, v)
		}
		cur = next
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

// toFloat coerces a decoded JSON value (float64 or numeric string) to float64.
// Non-numeric or nil yields 0. Standard json.Unmarshal into map[string]any
// decodes numbers as float64, so json.Number is not handled.
func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

// firstString returns the first resolved value as a string, or "".
func firstString(root any, paths []string) string {
	for _, v := range resolveFirst(root, paths) {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
