package sessions

import (
	"encoding/json"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// SlashCommands handles GET /api/slash-commands.
// Returns Claude built-in commands plus all installed skills from the plugin cache.
func SlashCommands(w http.ResponseWriter, r *http.Request) {
	cmds := parser.GetSlashCommands()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cmds)
}
