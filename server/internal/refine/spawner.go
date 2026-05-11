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

// RunRefinementTurn spawns `claude` with a system prompt + windowed history and
// streams the assistant's text response into the returned channel.
// The channel is closed when the process exits. Never returns a nil channel.
// ctx cancellation kills the process.
func RunRefinementTurn(ctx context.Context, cfg SpawnConfig) (<-chan string, error) {
	var buf bytes.Buffer
	if err := promptTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("refine: build prompt: %w", err)
	}
	prompt := strings.TrimSpace(buf.String())

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("refine: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("refine: start claude: %w", err)
	}

	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
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
		if err := cmd.Wait(); err != nil {
			ch <- "[ERROR] claude exited: " + err.Error()
		}
	}()

	return ch, nil
}
