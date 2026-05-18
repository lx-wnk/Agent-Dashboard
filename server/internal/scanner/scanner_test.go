package scanner_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
	"github.com/stretchr/testify/require"
)

func TestParseElapsedTime(t *testing.T) {
	tests := []struct {
		name  string
		etime string
		want  int64
	}{
		{"seconds only", "42", 42},
		{"minutes and seconds", "05:30", 330},
		{"hours minutes seconds", "01:05:30", 3930},
		{"days hours minutes seconds", "2-01:05:30", 176730},
		{"leading space", "  12", 12},
		// Edge cases
		{"empty string", "", 0},
		{"zero seconds", "0", 0},
		{"zero minutes and seconds", "0:00", 0},
		{"single minute", "0:05", 5},
		{"one minute thirty seconds", "1:30", 90},
		{"exact hour", "1:00:00", 3600},
		{"90 minutes", "1:30:00", 5400},
		// 1 day, 1 hour, 5 minutes, 30 seconds = 86400 + 3600 + 330 = 90330
		{"one day one hour", "1-01:05:30", 90330},
		// days=2, hours=1, minutes=5, seconds=30 → 2*86400 + 1*3600 + 5*60 + 30 = 176730
		{"two days from task", "2-01:05:30", 176730},
		// Malformed: "1-" → days=1, remainder="" → 0 parsed for HMS → result=1*86400=86400
		{"malformed trailing dash", "1-", 86400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanner.ParseElapsedTime(tt.etime)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseLsofBatch(t *testing.T) {
	input := "p1234\nn/home/user/project\np5678\nn/tmp/other\n"
	got := scanner.ParseLsofBatch(input)
	require.Equal(t, "/home/user/project", got[1234])
	require.Equal(t, "/tmp/other", got[5678])
}

func TestParseLsofBatch_Empty(t *testing.T) {
	got := scanner.ParseLsofBatch("")
	require.Empty(t, got)
}

func TestProjectName(t *testing.T) {
	require.Equal(t, "project", scanner.ProjectName("/home/user/project"))
	require.Equal(t, "agent-dashboard", scanner.ProjectName("/Users/alex/code/agent-dashboard"))
}
