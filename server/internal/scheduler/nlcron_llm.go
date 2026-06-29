package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecLLMTranslator implements LLMTranslator via a one-shot `claude -p` subprocess.
// It mirrors the exec pattern in internal/refine (runExecPath, nil spawner variant)
// but uses Output() instead of streaming since only the first line is needed.
type ExecLLMTranslator struct{}

// NewExecLLMTranslator returns an ExecLLMTranslator.
func NewExecLLMTranslator() *ExecLLMTranslator { return &ExecLLMTranslator{} }

const cronTranslatePrompt = "Return ONLY a single 5-field POSIX cron expression " +
	"(minute hour dom month dow) for this schedule phrase — no explanation, no other text: %s"

// TranslateToCron runs `claude -p <prompt>` and returns the first non-empty output
// line. The caller (NLCron.Translate) validates and rejects invalid expressions.
func (e *ExecLLMTranslator) TranslateToCron(ctx context.Context, phrase string) (string, error) {
	prompt := fmt.Sprintf(cronTranslatePrompt, phrase)
	out, err := exec.CommandContext(ctx, "claude", "-p", prompt).Output()
	if err != nil {
		return "", fmt.Errorf("nlcron: llm exec: %w", err)
	}
	sc := bufio.NewScanner(strings.NewReader(strings.TrimSpace(string(out))))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("nlcron: llm returned empty output")
}
