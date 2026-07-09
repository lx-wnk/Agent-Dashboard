package merger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

// questionProbeTimeout bounds the loopback HTTP round-trip so a slow or wedged
// pty broker never stalls a scan tick.
const questionProbeTimeout = 250 * time.Millisecond

// RealQuestionProbe is the production QuestionProbeFn: it reads the pty
// broker's discovery file for pid and, if present, queries the broker's
// GET /question endpoint. Fail-soft by design — a missing discovery file, an
// unreachable broker, or any decode error all yield nil rather than an error,
// since this runs on the hot scan path and must never fail agent building.
func RealQuestionProbe(pid int) *sdk.DetectedQuestion {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

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
