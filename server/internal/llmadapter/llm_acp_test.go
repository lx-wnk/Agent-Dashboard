package llmadapter

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/acp"
	"github.com/stretchr/testify/require"
)

// fakeConn stands in for a live ACP connection so no process is started.
type fakeConn struct {
	client    *acp.Client
	modes     *sdkacp.SessionModeState
	setModes  []sdkacp.SessionModeId
	gotPrompt []sdkacp.ContentBlock
	initErr   error
	newErr    error
	promptErr error
	reply     string
}

func (f *fakeConn) Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error) {
	return sdkacp.InitializeResponse{ProtocolVersion: sdkacp.ProtocolVersionNumber}, f.initErr
}

func (f *fakeConn) NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error) {
	return sdkacp.NewSessionResponse{SessionId: "sess-1", Modes: f.modes}, f.newErr
}

func (f *fakeConn) SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error) {
	f.setModes = append(f.setModes, p.ModeId)
	return sdkacp.SetSessionModeResponse{}, nil
}

func (f *fakeConn) Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error) {
	f.gotPrompt = p.Prompt
	if f.promptErr != nil {
		return sdkacp.PromptResponse{}, f.promptErr
	}
	_ = f.client.SessionUpdate(ctx, sdkacp.SessionNotification{
		SessionId: "sess-1",
		Update: sdkacp.SessionUpdate{
			AgentMessageChunk: &sdkacp.SessionUpdateAgentMessageChunk{Content: sdkacp.TextBlock(f.reply)},
		},
	})
	return sdkacp.PromptResponse{StopReason: sdkacp.StopReasonEndTurn}, nil
}

func gatedModes() *sdkacp.SessionModeState {
	return &sdkacp.SessionModeState{
		CurrentModeId:  "auto",
		AvailableModes: []sdkacp.SessionMode{{Id: "auto", Name: "auto"}, {Id: acp.ModeGated, Name: "default"}},
	}
}

// spawnerWith wires an ACPSpawner to a fakeConn, bypassing process start.
func spawnerWith(t *testing.T, f *fakeConn) *ACPSpawner {
	t.Helper()
	s := &ACPSpawner{Command: "true"}
	s.newConn = func(c *acp.Client, _ io.Writer, _ io.Reader) acpConn {
		f.client = c
		return f
	}
	return s
}

func testArgs(t *testing.T) LLMSpawnArgs {
	t.Helper()
	return LLMSpawnArgs{
		TaskID: "task-1", StageRunID: "sr-1", Stage: "review",
		SystemPrompt: "sys", UserPrompt: "do the thing", WorkDir: t.TempDir(),
	}
}

func TestACPSpawnerName(t *testing.T) {
	require.Equal(t, "acp", (&ACPSpawner{}).Name())
}

func TestACPSpawnerWritesTheAgentReplyToASessionFile(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "the answer"}
	res, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.NoError(t, err)
	require.Equal(t, 0, res.PID, "the adapter owns the process, so it reports no PID")
	require.NotEmpty(t, res.SessionFile)

	b, readErr := os.ReadFile(res.SessionFile)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "the answer")
}

func TestACPSpawnerPinsTheGatedMode(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "ok"}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.NoError(t, err)
	require.Equal(t, []sdkacp.SessionModeId{acp.ModeGated}, f.setModes)
}

func TestACPSpawnerRefusesASessionItCannotGate(t *testing.T) {
	f := &fakeConn{
		modes: &sdkacp.SessionModeState{CurrentModeId: "auto",
			AvailableModes: []sdkacp.SessionMode{{Id: "auto", Name: "auto"}}},
		reply: "ok",
	}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))

	require.Error(t, err, "an ungatable session must not run")
	require.Empty(t, f.setModes)
}

func TestACPSpawnerFailsWhenTheSessionCannotStart(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), newErr: errors.New("refused")}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))
	require.Error(t, err)
}

func TestACPSpawnerFailsWhenThePromptFails(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), promptErr: errors.New("model down")}
	_, err := spawnerWith(t, f).Spawn(context.Background(), testArgs(t))
	require.Error(t, err)
}

// promptText reads the text out of a recorded prompt the same way client.go
// does: Text is a pointer, so it must be nil-checked rather than dereferenced
// blindly.
func promptText(t *testing.T, blocks []sdkacp.ContentBlock) string {
	t.Helper()
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	return blocks[0].Text.Text
}

func TestACPSpawnerSendsBothPromptParts(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "ok"}
	args := testArgs(t)
	_, err := spawnerWith(t, f).Spawn(context.Background(), args)
	require.NoError(t, err)

	text := promptText(t, f.gotPrompt)
	require.Contains(t, text, args.SystemPrompt)
	require.Contains(t, text, args.UserPrompt)
	require.Less(t, strings.Index(text, args.SystemPrompt), strings.Index(text, args.UserPrompt),
		"system prompt must appear before the user prompt")
}

func TestACPSpawnerOmitsTheSeparatorWhenThereIsNoSystemPrompt(t *testing.T) {
	f := &fakeConn{modes: gatedModes(), reply: "ok"}
	args := testArgs(t)
	args.SystemPrompt = ""
	_, err := spawnerWith(t, f).Spawn(context.Background(), args)
	require.NoError(t, err)

	require.Equal(t, args.UserPrompt, promptText(t, f.gotPrompt))
}
