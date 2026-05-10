package parser_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"home dir", "/home/user/project", "-home-user-project"},
		{"dot claude", "/home/user/.claude", "-home-user--claude"},
		{"underscore", "/home/user/my_project", "-home-user-my-project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parser.EncodePath(tt.input))
		})
	}
}
