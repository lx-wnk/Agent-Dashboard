package merger

import (
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

func TestBuildAgent_WorkingFromTurnOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	open := &parser.SessionData{SessionID: "s1", LastActivity: time.Now(), TurnOpen: true}
	a := buildAgent(scanner.ProcessInfo{PID: 1, CWD: "/p", Provider: sdk.ProviderClaude}, open, 0)
	if !a.Working {
		t.Error("TurnOpen → agent.Working must be true")
	}
	done := &parser.SessionData{SessionID: "s2", LastActivity: time.Now(), TurnOpen: false}
	b := buildAgent(scanner.ProcessInfo{PID: 2, CWD: "/p", Provider: sdk.ProviderClaude}, done, 0)
	if b.Working {
		t.Error("closed turn (no B signal here) → agent.Working must be false")
	}
}
