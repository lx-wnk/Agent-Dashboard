// Package system provides system status HTTP handlers.
package system

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var startTime = time.Now()

// HealthHandler handles GET /api/system/health.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"uptime":   time.Since(startTime).Seconds(),
		"go":       runtime.Version(),
		"platform": runtime.GOOS,
	})
}
