package capability

import "sort"

// IsValidContextKind reports whether kind is a key of contextRank, mirroring
// the check Decide uses to silently drop a grant, so a write path can reject
// what Decide would otherwise discard.
func IsValidContextKind(kind string) bool {
	_, known := contextRank[kind]
	return known
}

// IsValidMode reports whether mode is a key of modeRank, mirroring the check
// Decide uses to silently drop a grant, so a write path can reject what
// Decide would otherwise discard.
func IsValidMode(mode string) bool {
	_, known := modeRank[mode]
	return known
}

// ContextKinds returns every valid context kind, most specific first.
func ContextKinds() []string {
	return rankedKeys(contextRank)
}

// Modes returns every valid grant mode, ordered deny, allow, ask.
func Modes() []string {
	return rankedKeys(modeRank)
}

func rankedKeys(ranks map[string]int) []string {
	keys := make([]string, 0, len(ranks))
	for k := range ranks {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return ranks[keys[i]] < ranks[keys[j]] })
	return keys
}
