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
		{"zero", "0", 0},
		{"empty string", "", 0},
		{"whitespace only", "   ", 0},
		{"one second", "1", 1},
		{"exactly one minute", "01:00", 60},
		{"exactly one hour", "01:00:00", 3600},
		{"one day", "1-00:00:00", 86400},
		{"large days", "10-00:00:00", 864000},
		{"trailing space", "30  ", 30},
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
