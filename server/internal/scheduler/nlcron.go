// Package scheduler turns natural-language schedule phrases into validated cron
// expressions and fires recurring pipeline tasks from stored schedules.
//
// Layering: this is a leaf service. pipeline/ MUST NOT import scheduler/ — the
// dependency direction is one-way (scheduler depends on repo + task-create core).
package scheduler

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	cron "github.com/robfig/cron/v3"
)

// ErrUnparseable is returned when neither the rule-based fast-path nor the LLM
// fallback could produce a valid cron expression for a phrase. Callers surface
// this as a 422 (unprocessable) to the user.
var ErrUnparseable = fmt.Errorf("could not translate phrase to a cron expression")

// LLMTranslator is the injectable fallback used when the rule-based fast-path
// cannot parse a phrase. Implementations spawn a one-shot LLM turn that must
// return a single 5-field cron expression. Nil is allowed — the translator then
// relies on the rule-based path alone.
type LLMTranslator interface {
	// TranslateToCron returns a bare 5-field cron expression for the phrase, or
	// an error. Implementations should NOT validate; the caller validates.
	TranslateToCron(ctx context.Context, phrase string) (string, error)
}

// standardParser parses 5-field POSIX cron (minute hour dom month dow). Shared
// across validation, due-detection, and preview so all three agree on syntax.
var standardParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

// NLCron converts natural-language phrases to cron expressions. The rule-based
// fast-path covers common phrases offline; an optional LLM fallback handles the
// long tail. The produced expression is always validated before return.
type NLCron struct {
	llm LLMTranslator
}

// NewNLCron builds a translator. llm may be nil (rule-based only).
func NewNLCron(llm LLMTranslator) *NLCron {
	return &NLCron{llm: llm}
}

// Translate converts a phrase to a validated 5-field cron expression. It tries,
// in order: (1) an already-valid raw cron expression, (2) the rule-based
// fast-path, (3) the injected LLM fallback. The result is always validated via
// the standard parser before return. Returns ErrUnparseable when all paths fail.
func (n *NLCron) Translate(ctx context.Context, phrase string) (string, error) {
	trimmed := strings.TrimSpace(phrase)
	if trimmed == "" {
		return "", ErrUnparseable
	}

	// (1) Raw cron passthrough — accept an expression the user typed directly.
	if _, err := standardParser.Parse(trimmed); err == nil {
		return trimmed, nil
	}

	// (2) Rule-based fast-path.
	if expr, ok := ruleBasedCron(trimmed); ok {
		if _, err := standardParser.Parse(expr); err == nil {
			return expr, nil
		}
	}

	// (3) LLM fallback.
	if n.llm != nil {
		raw, err := n.llm.TranslateToCron(ctx, trimmed)
		if err == nil {
			expr := strings.TrimSpace(raw)
			if _, perr := standardParser.Parse(expr); perr == nil {
				return expr, nil
			}
		}
	}

	return "", ErrUnparseable
}

// Validate reports whether expr is a valid 5-field cron expression.
func Validate(expr string) error {
	if _, err := standardParser.Parse(strings.TrimSpace(expr)); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return nil
}

// NextRuns returns the next n fire times after `after` for expr, in expr order.
// Returns an error when expr is invalid.
func NextRuns(expr string, after time.Time, n int) ([]time.Time, error) {
	sched, err := standardParser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	runs := make([]time.Time, 0, n)
	t := after
	for range n {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		runs = append(runs, t)
	}
	return runs, nil
}

// dayOfWeek maps weekday words to cron day-of-week numbers (0=Sunday).
var dayOfWeek = map[string]int{
	"sunday": 0, "sun": 0,
	"monday": 1, "mon": 1,
	"tuesday": 2, "tue": 2, "tues": 2,
	"wednesday": 3, "wed": 3,
	"thursday": 4, "thu": 4, "thur": 4, "thurs": 4,
	"friday": 5, "fri": 5,
	"saturday": 6, "sat": 6,
}

var everyNMinutesRe = regexp.MustCompile(`every\s+(\d+)\s+min`)
var everyNHoursRe = regexp.MustCompile(`every\s+(\d+)\s+hour`)
var atTimeRe = regexp.MustCompile(`(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)

// ruleBasedCron implements the offline fast-path. It returns (expr, true) when a
// phrase matches a known pattern. Patterns are matched case-insensitively. The
// caller still validates the returned expression.
func ruleBasedCron(phrase string) (string, bool) {
	p := strings.ToLower(strings.TrimSpace(phrase))

	// Fixed phrases.
	switch p {
	case "every minute":
		return "* * * * *", true
	case "hourly", "every hour":
		return "0 * * * *", true
	case "daily", "every day", "everyday":
		return "0 0 * * *", true
	case "at midnight", "midnight":
		return "0 0 * * *", true
	case "at noon", "noon":
		return "0 12 * * *", true
	case "weekly", "every week":
		return "0 0 * * 0", true
	case "monthly", "every month":
		return "0 0 1 * *", true
	case "every weekday", "weekdays", "on weekdays":
		return "0 0 * * 1-5", true
	case "every weekend", "weekends", "on weekends":
		return "0 0 * * 0,6", true
	}

	// "every N minutes" / "every N hours".
	if m := everyNMinutesRe.FindStringSubmatch(p); m != nil {
		if step, err := strconv.Atoi(m[1]); err == nil && step > 0 && step < 60 {
			return fmt.Sprintf("*/%d * * * *", step), true
		}
	}
	if m := everyNHoursRe.FindStringSubmatch(p); m != nil {
		if step, err := strconv.Atoi(m[1]); err == nil && step > 0 && step < 24 {
			return fmt.Sprintf("0 */%d * * *", step), true
		}
	}

	// Composite phrases with a time-of-day and/or a day constraint, e.g.
	// "every weekday at 9am", "every monday at 8:30", "daily at 18:00".
	minute, hour, hasTime := parseTimeOfDay(p)
	dowSpec, hasDow := parseDaySpec(p)

	if hasTime || hasDow {
		min := "0"
		hr := "*"
		if hasTime {
			min = strconv.Itoa(minute)
			hr = strconv.Itoa(hour)
		}
		dow := "*"
		if hasDow {
			dow = dowSpec
			// A day constraint with no explicit time means midnight on those days.
			if !hasTime {
				min, hr = "0", "0"
			}
		}
		return fmt.Sprintf("%s %s * * %s", min, hr, dow), true
	}

	return "", false
}

// parseTimeOfDay extracts an "at HH[:MM][am|pm]" component. Returns ok=false
// when no time token is present. Bare numbers without "at"/am/pm are ignored to
// avoid misreading "every 5 …" as a time.
func parseTimeOfDay(p string) (minute, hour int, ok bool) {
	_, segment, found := strings.Cut(p, "at ")
	if !found {
		return 0, 0, false
	}
	m := atTimeRe.FindStringSubmatch(strings.TrimSpace(segment))
	if m == nil {
		return 0, 0, false
	}
	h, err := strconv.Atoi(m[1])
	if err != nil || h > 23 {
		return 0, 0, false
	}
	min := 0
	if m[2] != "" {
		if min, err = strconv.Atoi(m[2]); err != nil || min > 59 {
			return 0, 0, false
		}
	}
	switch m[3] {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	if h > 23 {
		return 0, 0, false
	}
	return min, h, true
}

// parseDaySpec extracts a day-of-week constraint from a phrase. Handles single
// weekdays ("monday"), the weekday range, and the weekend set. Returns the cron
// day-of-week field and ok=true when a constraint is found.
func parseDaySpec(p string) (string, bool) {
	if strings.Contains(p, "weekday") {
		return "1-5", true
	}
	if strings.Contains(p, "weekend") {
		return "0,6", true
	}
	var days []int
	seen := map[int]bool{}
	for word, num := range dayOfWeek {
		// Only match whole words to avoid "sun" inside "sunday" double-counting;
		// longest names are distinct, short aliases are bounded by spaces.
		if containsWord(p, word) && !seen[num] {
			days = append(days, num)
			seen[num] = true
		}
	}
	if len(days) == 0 {
		return "", false
	}
	// Sort ascending for a stable, readable cron field.
	for i := 0; i < len(days); i++ {
		for j := i + 1; j < len(days); j++ {
			if days[j] < days[i] {
				days[i], days[j] = days[j], days[i]
			}
		}
	}
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(d)
	}
	return strings.Join(parts, ","), true
}

// containsWord reports whether word appears in s bounded by non-letter runes.
func containsWord(s, word string) bool {
	for {
		idx := strings.Index(s, word)
		if idx < 0 {
			return false
		}
		beforeOK := idx == 0 || !isLetter(rune(s[idx-1]))
		end := idx + len(word)
		afterOK := end >= len(s) || !isLetter(rune(s[end]))
		if beforeOK && afterOK {
			return true
		}
		s = s[idx+len(word):]
	}
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
