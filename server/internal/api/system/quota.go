package system

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// Quota handles GET /api/quota.
// Reads the most recent usage JSON from ~/.claude/usage-data/ and returns
// periodStart, periodEnd, tokensUsed, and limit.
func Quota(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	usageDir := filepath.Join(home, ".claude", "usage-data")

	entries, err := os.ReadDir(usageDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"limit": nil})
		return
	}

	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	if len(jsonFiles) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"limit": nil})
		return
	}

	sort.Sort(sort.Reverse(sort.StringSlice(jsonFiles)))
	raw, err := os.ReadFile(filepath.Join(usageDir, jsonFiles[0]))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"limit": nil})
		return
	}

	var data struct {
		PeriodStart *string  `json:"periodStart"`
		PeriodEnd   *string  `json:"periodEnd"`
		TokensUsed  *int     `json:"tokensUsed"`
		Limit       *float64 `json:"limit"` // null when no limit set
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"limit": nil})
		return
	}

	tokensUsed := 0
	if data.TokensUsed != nil {
		tokensUsed = *data.TokensUsed
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"periodStart": data.PeriodStart,
		"periodEnd":   data.PeriodEnd,
		"tokensUsed":  tokensUsed,
		"limit":       data.Limit,
	})
}
