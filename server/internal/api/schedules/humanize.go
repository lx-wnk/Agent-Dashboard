package schedules

import (
	"fmt"
	"strconv"
	"strings"
)

var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// describeCron renders a best-effort human description of a 5-field cron
// expression. It recognizes common shapes and falls back to echoing the raw
// expression. This is a display aid, not a parser — the cron string remains the
// source of truth.
func describeCron(expr string) string {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr
	}
	min, hour, dom, month, dow := f[0], f[1], f[2], f[3], f[4]

	timePart := describeTime(min, hour)
	dayPart := describeDays(dom, month, dow)
	switch {
	case timePart == "" && dayPart == "":
		return expr
	case dayPart == "":
		return strings.TrimSpace(timePart)
	case timePart == "":
		return strings.TrimSpace(dayPart)
	default:
		return strings.TrimSpace(dayPart + " " + timePart)
	}
}

func describeTime(min, hour string) string {
	if step, ok := strings.CutPrefix(min, "*/"); ok {
		return "every " + step + " minutes"
	}
	if min == "*" && hour == "*" {
		return "every minute"
	}
	if hour == "*" {
		if m, err := strconv.Atoi(min); err == nil {
			return fmt.Sprintf("at minute %d of every hour", m)
		}
	}
	if step, ok := strings.CutPrefix(hour, "*/"); ok && min == "0" {
		return "every " + step + " hours"
	}
	m, merr := strconv.Atoi(min)
	h, herr := strconv.Atoi(hour)
	if merr == nil && herr == nil {
		return fmt.Sprintf("at %02d:%02d", h, m)
	}
	return ""
}

func describeDays(dom, month, dow string) string {
	if dom == "*" && month == "*" && dow == "*" {
		return "every day"
	}
	if dow == "1-5" {
		return "every weekday"
	}
	if dow == "0,6" || dow == "6,0" {
		return "every weekend"
	}
	if dow != "*" {
		if names := weekdayList(dow); names != "" {
			return "every " + names
		}
	}
	if dom != "*" && dow == "*" {
		return "on day " + dom + " of the month"
	}
	return ""
}

func weekdayList(dow string) string {
	parts := strings.Split(dow, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 6 {
			return ""
		}
		names = append(names, weekdayNames[n])
	}
	return strings.Join(names, ", ")
}
