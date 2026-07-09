// Package askq detects an AskUserQuestion modal from the visible rows of a
// terminal buffer.
//
// This is a hand-maintained parity port of src/utils/askQuestionScreen.ts.
// The TS and Go implementations are the single source of truth for each
// other's behavior and are kept in parity by hand (no shared module across
// the TS/Go language boundary) plus shared fixture files under testdata/.
// When changing detection logic here, mirror the change in
// src/utils/askQuestionScreen.ts, and vice versa.
package askq

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// DetectedOption and DetectedQuestion are the public wire types for a parsed
// AskUserQuestion modal, defined in sdk since they cross the server/client
// boundary. Aliased here so existing callers within this package keep working
// unqualified.
type (
	DetectedOption   = sdk.DetectedOption
	DetectedQuestion = sdk.DetectedQuestion
)

const (
	typeSomethingLabel = "type something"
	chatAboutLabel     = "chat about this"
)

var (
	trailingPunctRe = regexp.MustCompile(`[\s.]+$`)

	// The box-drawing block (U+2500-U+257F) covers rounded corners, straight edges, and dashes.
	borderOnlyRe  = regexp.MustCompile(`^[\s\x{2500}-\x{257F}=+-]*$`)
	leadingBoxRe  = regexp.MustCompile(`^\s*[│║┃┆┇┊┋]`)
	trailingBoxRe = regexp.MustCompile(`[│║┃┆┇┊┋]\s*$`)
	numberedRowRe = regexp.MustCompile(`^❯?\s*(\d+)\.\s+(\S.*)$`)
	checkboxRe    = regexp.MustCompile(`(?i)^\[[ x✔✓]\]\s*`)
	trailingCRRe  = regexp.MustCompile(`\r$`)
	toggleHintRe  = regexp.MustCompile(`(?i)toggle|space to`)
)

// The meta-row copy drifts between Claude Code releases (e.g. v2.1.205 renders
// "Type something." with a trailing period, v2.1.197 rendered "type something").
// Match on a normalized prefix - lower-cased, trailing punctuation stripped - so
// a cosmetic copy tweak does not silently disable question detection.
func metaLabelMatches(label, meta string) bool {
	normalized := trailingPunctRe.ReplaceAllString(strings.ToLower(label), "")
	return strings.HasPrefix(normalized, meta)
}

func toContentLine(rawRow string) (string, bool) {
	row := trailingCRRe.ReplaceAllString(rawRow, "")
	if borderOnlyRe.MatchString(row) {
		return "", false
	}

	inner := row
	if m := leadingBoxRe.FindString(inner); m != "" {
		inner = inner[len(m):]
	}
	if m := trailingBoxRe.FindString(inner); m != "" {
		inner = inner[:len(inner)-len(m)]
	}

	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

type parsedRow struct {
	text        string
	hasNum      bool
	num         int
	label       string
	hasCheckbox bool
}

type numberedEntry struct {
	row parsedRow
	idx int
}

func parseNumberedRow(text string) parsedRow {
	m := numberedRowRe.FindStringSubmatch(text)
	if m == nil {
		return parsedRow{text: text}
	}

	num, _ := strconv.Atoi(m[1])
	remainder := m[2]

	var label string
	hasCheckbox := false
	if cb := checkboxRe.FindString(remainder); cb != "" {
		hasCheckbox = true
		label = strings.TrimSpace(remainder[len(cb):])
	} else {
		label = strings.TrimSpace(remainder)
	}

	return parsedRow{text: text, hasNum: true, num: num, label: label, hasCheckbox: hasCheckbox}
}

// decideMultiSelect: PRIMARY signal is a checkbox on EVERY option row. Only
// when there is zero checkbox evidence do we fall back to the true footer
// (lines after the last numbered row) for a "toggle"/"space to" hint. Header,
// question, and description lines are never scanned for it - that would let a
// question like "Would you like to toggle X?" flip the mode.
func decideMultiSelect(optionEntries []numberedEntry, contentLines []parsedRow) bool {
	anyCheckbox := false
	for _, e := range optionEntries {
		if e.row.hasCheckbox {
			anyCheckbox = true
			break
		}
	}
	if anyCheckbox {
		for _, e := range optionEntries {
			if !e.row.hasCheckbox {
				return false
			}
		}
		return true
	}

	lastNumberedIdx := -1
	for idx, row := range contentLines {
		if row.hasNum {
			lastNumberedIdx = idx
		}
	}

	for _, l := range contentLines[lastNumberedIdx+1:] {
		if !l.hasNum && toggleHintRe.MatchString(l.text) {
			return true
		}
	}
	return false
}

// DetectQuestion detects an AskUserQuestion modal from the visible rows of a
// terminal buffer.
//
// Keys off structural signals present in the real TUI render rather than
// exact spacing/border glyphs: a contiguous numbered option block followed by
// BOTH UI-injected meta-rows. Requiring both meta-rows - adjacent and
// index-continuous - is what separates a real modal from an ordinary
// numbered list in terminal output.
func DetectQuestion(rows []string) *sdk.DetectedQuestion {
	contentLines := make([]parsedRow, 0, len(rows))
	for _, raw := range rows {
		line, ok := toContentLine(raw)
		if !ok {
			continue
		}
		contentLines = append(contentLines, parseNumberedRow(line))
	}

	numbered := make([]numberedEntry, 0, len(contentLines))
	for idx, row := range contentLines {
		if row.hasNum {
			numbered = append(numbered, numberedEntry{row: row, idx: idx})
		}
	}

	typeRowIdx, chatRowIdx := -1, -1
	for i, e := range numbered {
		if typeRowIdx == -1 && metaLabelMatches(e.row.label, typeSomethingLabel) {
			typeRowIdx = i
		}
		if chatRowIdx == -1 && metaLabelMatches(e.row.label, chatAboutLabel) {
			chatRowIdx = i
		}
	}
	if typeRowIdx == -1 || chatRowIdx == -1 {
		return nil
	}

	typeSomethingIndex := numbered[typeRowIdx].row.num
	chatAboutIndex := numbered[chatRowIdx].row.num

	optionEntries := make([]numberedEntry, 0, len(numbered))
	for i, e := range numbered {
		if i == typeRowIdx || i == chatRowIdx {
			continue
		}
		if e.row.num < typeSomethingIndex {
			optionEntries = append(optionEntries, e)
		}
	}

	// ENFORCED invariant: real options numbered contiguously 1..n, meta-rows at n+1 / n+2.
	// A numbered-looking description line desyncs this - reject rather than emit garbage.
	contiguous := true
	for i, e := range optionEntries {
		if e.row.num != i+1 {
			contiguous = false
			break
		}
	}
	gateOk := len(optionEntries) >= 1 &&
		contiguous &&
		typeSomethingIndex == len(optionEntries)+1 &&
		chatAboutIndex == typeSomethingIndex+1
	if !gateOk {
		return nil
	}

	options := make([]sdk.DetectedOption, 0, len(optionEntries))
	for _, e := range optionEntries {
		option := sdk.DetectedOption{Index: e.row.num, Label: e.row.label}
		if next := e.idx + 1; next < len(contentLines) && !contentLines[next].hasNum {
			option.Description = contentLines[next].text
		}
		options = append(options, option)
	}

	preambleEnd := optionEntries[0].idx
	preamble := make([]string, 0, preambleEnd)
	for _, l := range contentLines[:preambleEnd] {
		preamble = append(preamble, l.text)
	}

	header := ""
	if len(preamble) > 0 {
		header = preamble[0]
	}

	question := header
	if len(preamble) > 0 {
		question = preamble[len(preamble)-1]
	}
	for i := len(preamble) - 1; i >= 0; i-- {
		if strings.HasSuffix(preamble[i], "?") {
			question = preamble[i]
			break
		}
	}

	return &sdk.DetectedQuestion{
		Header:             header,
		Question:           question,
		MultiSelect:        decideMultiSelect(optionEntries, contentLines),
		Options:            options,
		TypeSomethingIndex: typeSomethingIndex,
		ChatAboutIndex:     chatAboutIndex,
	}
}
