package memory

import (
	"math"
	"sort"
	"time"
)

// Candidate is one memory entry considered for retrieval, carrying only the
// signals Score needs — not the full entry payload, so ranking never depends
// on how much of summary or content happens to be loaded.
type Candidate struct {
	EntryID    string
	Lexical    float64 // FTS5 bm25 relevance, normalised to 0..1 (1 = best match in the retrieved set)
	ScopeKind  string  // "global" | "project" | "application"
	CreatedAt  time.Time
	Confidence float64 // 0..1
	Kind       string  // "fact" | "preference" | "lesson" | "entity" | "pointer"
}

// Composite score weights (sum to 1.0). Fixed in source, following the one
// weighted-sum idiom this codebase already has (server/internal/merger/health.go)
// rather than inventing a second shape.
const (
	weightLexical    = 0.40
	weightScope      = 0.20
	weightRecency    = 0.15
	weightConfidence = 0.15
	weightKind       = 0.10

	// recencyHalfLifeDays: age at which the recency component halves.
	recencyHalfLifeDays = 30.0

	// Scope-specificity weights are absolute, not relative to the querying
	// context: a project-scoped entry always outranks an equally relevant
	// global one, per spec §6.
	scopeWeightApplication = 1.0
	scopeWeightProject     = 0.6
	scopeWeightGlobal      = 0.2
	// scopeWeightUnknown is the fail-closed floor for a scope_kind this
	// package does not recognise — it must never outrank a known scope.
	scopeWeightUnknown = 0.0

	// kindWeightPush covers preference/lesson: entries that change what the
	// agent does, which the push budget should favour.
	kindWeightPush = 1.0
	// kindWeightRecall covers fact/entity/pointer: usually looked up on
	// demand rather than needed unprompted.
	kindWeightRecall = 0.5
	// kindWeightUnknown is the fail-closed floor for a kind this package does
	// not recognise.
	kindWeightUnknown = 0.0
)

// Score returns a composite 0..1 relevance score for a candidate: a weighted
// sum of lexical relevance, scope specificity, recency, stored confidence and
// a kind weight that favours preference/lesson over fact for the push.
//
// A candidate with no entry id cannot be resolved back to a real entry, so it
// is hard-capped to 0 regardless of its other fields — the same shape as
// health.go's post-compute cap on a qualitative error state. Every other
// component floors an unrecognised or non-finite input to its fail-closed
// worst case rather than a neutral middle: a candidate whose score cannot be
// computed cleanly must not silently rank high.
func Score(c Candidate, now time.Time) float64 {
	if c.EntryID == "" {
		return 0
	}

	raw := weightLexical*clamp01(c.Lexical) +
		weightScope*scopeComponent(c.ScopeKind) +
		weightRecency*recencyComponent(c.CreatedAt, now) +
		weightConfidence*clamp01(c.Confidence) +
		weightKind*kindComponent(c.Kind)

	return clamp01(raw)
}

// Rank sorts candidates by Score, highest first. Ties are broken by EntryID
// so the result is deterministic regardless of input order — resolving a tie
// by map iteration would make the injected set differ between runs on
// identical data.
func Rank(cs []Candidate, now time.Time) []Candidate {
	out := make([]Candidate, len(cs))
	copy(out, cs)
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := Score(out[i], now), Score(out[j], now)
		if si != sj {
			return si > sj
		}
		return out[i].EntryID < out[j].EntryID
	})
	return out
}

func scopeComponent(scopeKind string) float64 {
	switch scopeKind {
	case "application":
		return scopeWeightApplication
	case "project":
		return scopeWeightProject
	case "global":
		return scopeWeightGlobal
	default:
		return scopeWeightUnknown
	}
}

func kindComponent(kind string) float64 {
	switch kind {
	case "preference", "lesson":
		return kindWeightPush
	case "fact", "entity", "pointer":
		return kindWeightRecall
	default:
		return kindWeightUnknown
	}
}

// recencyComponent decays exponentially with age, halving every
// recencyHalfLifeDays. A candidate timestamped in the future (clock skew) is
// clamped to age 0 rather than producing a component above 1.
func recencyComponent(createdAt, now time.Time) float64 {
	ageDays := now.Sub(createdAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	return math.Pow(0.5, ageDays/recencyHalfLifeDays)
}

// clamp01 bounds v to [0, 1], flooring a non-finite value (NaN/±Inf) to 0
// rather than passing it through — a broken input must not inflate the
// composite score it feeds.
func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
