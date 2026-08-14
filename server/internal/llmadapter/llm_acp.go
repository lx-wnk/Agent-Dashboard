package llmadapter

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	sdkacp "github.com/coder/acp-go-sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/acp"
)

// acpConn is the part of an ACP client connection the adapter drives. It exists
// so tests can run the adapter without starting a process.
type acpConn interface {
	Initialize(ctx context.Context, p sdkacp.InitializeRequest) (sdkacp.InitializeResponse, error)
	NewSession(ctx context.Context, p sdkacp.NewSessionRequest) (sdkacp.NewSessionResponse, error)
	SetSessionMode(ctx context.Context, p sdkacp.SetSessionModeRequest) (sdkacp.SetSessionModeResponse, error)
	Prompt(ctx context.Context, p sdkacp.PromptRequest) (sdkacp.PromptResponse, error)
}

// ACPSpawner runs one stage on an agent that speaks the Agent Client Protocol.
// Unlike the subprocess adapters it owns the connection for the whole turn,
// because an ACP agent blocks on permission requests until the client answers.
type ACPSpawner struct {
	Command    string
	Args       []string
	Permission func(context.Context, acp.PermissionRequest) (acp.PermissionDecision, error)

	newConn func(client *acp.Client, in io.Writer, out io.Reader) acpConn
}

func (s *ACPSpawner) Name() string { return "acp" }

func (s *ACPSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	var mu sync.Mutex
	var reply strings.Builder
	client := &acp.Client{
		OnEvent: func(e acp.Event) {
			if e.Kind != "agent_message" {
				return
			}
			mu.Lock()
			reply.WriteString(e.Text)
			mu.Unlock()
		},
		OnPermission: s.Permission,
	}

	conn, closeConn, err := s.connect(ctx, client)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	defer closeConn()

	if _, err := conn.Initialize(ctx, sdkacp.InitializeRequest{
		ProtocolVersion: sdkacp.ProtocolVersionNumber,
	}); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: initialize: %w", err)
	}

	sess, err := conn.NewSession(ctx, sdkacp.NewSessionRequest{
		Cwd: args.WorkDir, McpServers: []sdkacp.McpServer{},
	})
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: new session: %w", err)
	}

	// A session that cannot be pinned is a session without a permission gate.
	if err := acp.EnsureMode(ctx, conn, sess.SessionId, sess.Modes, acp.ModeGated); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: %w", err)
	}

	prompt := args.SystemPrompt
	if prompt != "" && args.UserPrompt != "" {
		prompt += "\n\n"
	}
	prompt += args.UserPrompt

	if _, err := conn.Prompt(ctx, sdkacp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdkacp.ContentBlock{sdkacp.TextBlock(prompt)},
	}); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: prompt: %w", err)
	}

	mu.Lock()
	text := reply.String()
	mu.Unlock()

	file, err := writeSyntheticSession(args.WorkDir, args.StageRunID, text)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("acp adapter: %w", err)
	}
	return LLMSpawnResult{PID: 0, SessionID: string(sess.SessionId), SessionFile: file}, nil
}

// connect starts the agent process and returns the driven connection plus a
// teardown that stops the process and releases its pipes.
func (s *ACPSpawner) connect(ctx context.Context, client *acp.Client) (acpConn, func(), error) {
	if s.newConn != nil {
		return s.newConn(client, io.Discard, strings.NewReader("")), func() {}, nil
	}
	if s.Command == "" {
		return nil, nil, fmt.Errorf("acp adapter: no command configured")
	}
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("acp adapter: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, fmt.Errorf("acp adapter: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, fmt.Errorf("acp adapter: start %q: %w", s.Command, err)
	}
	conn := sdkacp.NewClientSideConnection(client, stdin, stdout)
	return conn, func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}
