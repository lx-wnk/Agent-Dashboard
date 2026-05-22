// Package refine provides the spawner for refinement chat turns.
package refine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// SpawnConfig holds the parameters for a single refinement turn.
type SpawnConfig struct {
	TaskTitle       string
	TaskDescription string
	// History is the last N turns to include as context (alternating user/assistant).
	History []Turn
	// UserMessage is the latest user message to send.
	UserMessage string
	// WorkDir is the working directory for the claude process.
	WorkDir string
}

// Turn is a single conversation turn in the refinement history.
type Turn struct {
	Role    string // "user" or "assistant"
	Content string
}

var promptTmpl = template.Must(template.New("refinement").Parse(`<system>
You are a refinement assistant helping to clarify and improve a software task.
Task: {{.TaskTitle}}
{{- if .TaskDescription}}
Description: {{.TaskDescription}}
{{- end}}
</system>
{{range .History}}
<{{.Role}}>{{.Content}}</{{.Role}}>
{{end}}
<user>{{.UserMessage}}</user>`))

// RunRefinementTurn dispatches to the resolved spawner. sp may be nil — in that
// case the legacy `claude -p` exec path is used. The returned channel is closed
// when the process exits, the stream ends, or ctx is cancelled.
func RunRefinementTurn(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner) (<-chan string, error) {
	var buf bytes.Buffer
	if err := promptTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("refine: build prompt: %w", err)
	}
	prompt := strings.TrimSpace(buf.String())

	switch {
	case sp == nil, sp.AdapterType == "", sp.AdapterType == "claude":
		return runExecPath(ctx, cfg, sp, prompt)
	default:
		return runAdapterPath(ctx, cfg, sp, prompt)
	}
}

func runExecPath(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner, prompt string) (<-chan string, error) {
	binary := "claude"
	var extraArgs []string
	if sp != nil {
		if sp.Command != "" {
			binary = sp.Command
		}
		extraArgs = append(extraArgs, sp.Args...)
	}
	finalArgs := append(extraArgs, "-p", prompt)
	cmd := exec.CommandContext(ctx, binary, finalArgs...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmd.Env = mergeEnv(sp)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("refine: stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("refine: start %s: %w", binary, err)
	}

	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case ch <- line:
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- "[ERROR] scanner: " + err.Error()
		}
		if err := cmd.Wait(); err != nil {
			msg := "[ERROR] claude exited: " + err.Error()
			if s := strings.TrimSpace(stderrBuf.String()); s != "" {
				msg += " — " + s
			}
			ch <- msg
		}
	}()
	return ch, nil
}

func runAdapterPath(ctx context.Context, cfg SpawnConfig, sp *ent.Spawner, prompt string) (<-chan string, error) {
	adapter, err := pipeline.NewLLMSpawnerFromSpawner(sp)
	if err != nil {
		return nil, fmt.Errorf("refine: build adapter: %w", err)
	}
	if adapter == nil {
		return nil, fmt.Errorf("refine: adapter factory returned nil for type %q", sp.AdapterType)
	}
	streamer, ok := adapter.(pipeline.StreamingLLMSpawner)
	if !ok {
		return nil, fmt.Errorf("refine: adapter %q does not support streaming", sp.AdapterType)
	}
	args := pipeline.LLMSpawnArgs{
		SystemPrompt: "You are a refinement assistant helping to clarify and improve a software task.",
		UserPrompt:   prompt,
		WorkDir:      cfg.WorkDir,
	}
	if sp.ModelOverride != nil {
		args.Model = *sp.ModelOverride
	}
	return streamer.SpawnStream(ctx, args)
}
