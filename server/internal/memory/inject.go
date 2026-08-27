package memory

import (
	"context"
	"strings"
)

// InjectorFunc retrieves ranked memory entries for a spawn's user prompt. It
// is the same shape as (*Retriever).Retrieve, so a bound Retriever method
// value satisfies it directly: the push injector and the memory_search MCP
// tool both consume the one retrieval path instead of each answering
// "what's relevant" on their own.
type InjectorFunc func(ctx context.Context, q Query) ([]Entry, error)

// memoryBlockHeader introduces the injected block inside the user prompt.
const memoryBlockHeader = "## Relevant memory\n"

// Selection is the result of packing ranked entries into a character
// budget: the rendered block, how many characters it used, how many entries
// were dropped, and — for the caller that writes the memory_injection audit
// row — exactly which entries made the cut.
type Selection struct {
	Block   string
	Used    int
	Dropped int
	Entries []Entry
}

// Select greedily packs entries — assumed already ranked, most relevant
// first — into budget characters. An entry whose rendered line would not
// fit in what's left is skipped rather than truncated, so a later, smaller
// entry still gets a chance at the remaining space; truncating mid-entry
// would hand the model a fragment with no indication that it is one.
//
// budget <= 0 fails closed: a non-positive budget must never be read as
// "unbounded", so nothing is selected and every entry counts as dropped
// rather than an uncapped block being emitted.
//
// When nothing fits — no entries offered, or none survive the budget — the
// result carries no block at all, not an empty section: an empty heading in
// the prompt would invite the model to fill it in.
func Select(entries []Entry, budget int) Selection {
	if budget <= 0 {
		return Selection{Dropped: len(entries)}
	}

	remaining := budget - len(memoryBlockHeader)
	var chosen []Entry
	var lines []string
	dropped := 0
	for _, e := range entries {
		line := "- " + e.Summary + "\n"
		if len(line) > remaining {
			dropped++
			continue
		}
		chosen = append(chosen, e)
		lines = append(lines, line)
		remaining -= len(line)
	}
	if len(chosen) == 0 {
		return Selection{Dropped: len(entries)}
	}

	var b strings.Builder
	b.WriteString(memoryBlockHeader)
	for _, l := range lines {
		b.WriteString(l)
	}
	block := b.String()
	return Selection{Block: block, Used: len(block), Dropped: dropped, Entries: chosen}
}

// BuildBlock renders entries into a single ranked memory block for a spawn's
// user prompt, never emitting more than budget characters. It is the
// text-and-counts view of Select, for callers that don't need to know which
// entries made the cut.
func BuildBlock(entries []Entry, budget int) (block string, used int, dropped int) {
	s := Select(entries, budget)
	return s.Block, s.Used, s.Dropped
}
