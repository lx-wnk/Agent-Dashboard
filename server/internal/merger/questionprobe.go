package merger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/askq"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

// questionProbeTimeout bounds the loopback HTTP round-trip so a slow or wedged
// pty broker never stalls a scan tick.
const questionProbeTimeout = 250 * time.Millisecond

// captureTmuxPane is a seam so tests can supply rendered pane rows without a
// real tmux. It returns the visible pane content (already rendered by tmux, so
// no VT emulation is needed) split into rows.
var captureTmuxPane = func(socket, pane string) ([]string, error) {
	args := []string{}
	if socket != "" {
		args = append(args, "-S", socket)
	}
	args = append(args, "capture-pane", "-p", "-t", pane)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(out), "\n"), nil
}

// RealQuestionProbe is the production QuestionProbeFn. It detects a pending
// AskUserQuestion for pid over whichever injection path the session uses: the
// pty broker's GET /question, or — for tmux sessions — a `tmux capture-pane`
// snapshot run through the same detector. Fail-soft by design: any missing
// file, unreachable broker, or decode error yields nil rather than an error,
// since this runs on the hot scan path and must never fail agent building.
func RealQuestionProbe(pid int) *sdk.DetectedQuestion {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if q := probePtyQuestion(home, pid); q != nil {
		return q
	}
	return probeTmuxQuestion(home, pid)
}

// probePtyQuestion queries the pty broker's GET /question for pid, or nil.
func probePtyQuestion(home string, pid int) *sdk.DetectedQuestion {
	data, err := os.ReadFile(channelconfig.DiscoveryPtyFile(home, pid))
	if err != nil {
		return nil
	}
	var disc struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &disc) != nil || disc.Port == 0 || disc.Token == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), questionProbeTimeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/question", disc.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+disc.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	var q sdk.DetectedQuestion
	if json.NewDecoder(resp.Body).Decode(&q) != nil {
		return nil
	}
	return &q
}

// probeTmuxQuestion detects a question from a tmux session's rendered pane, or
// nil. tmux already renders the pane, so the rows go straight to the detector
// without VT emulation.
func probeTmuxQuestion(home string, pid int) *sdk.DetectedQuestion {
	data, err := os.ReadFile(channelconfig.DiscoveryFile(home, pid))
	if err != nil {
		return nil
	}
	var disc struct {
		TmuxPane   string `json:"tmuxPane"`
		TmuxSocket string `json:"tmuxSocket"`
	}
	if json.Unmarshal(data, &disc) != nil || disc.TmuxPane == "" {
		return nil
	}
	rows, err := captureTmuxPane(disc.TmuxSocket, disc.TmuxPane)
	if err != nil {
		return nil
	}
	return askq.DetectQuestion(rows)
}
