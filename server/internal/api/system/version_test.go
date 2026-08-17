package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/version"
)

// The health endpoint is the only runtime answer to "which build am I looking
// at". Three build paths embed their own copy of the SPA, so a window from a
// stale bundle is otherwise indistinguishable from a fresh one.
func TestHealthHandler_ReportsTheBuildVersion(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })
	version.Version = "v1.2.3-test"

	rec := httptest.NewRecorder()
	HealthHandler(rec, httptest.NewRequest(http.MethodGet, "/api/system/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "v1.2.3-test", body["version"])
	require.Equal(t, "ok", body["status"])
}

// The two build paths stamp different symbols: goreleaser stamps main.version
// and the Taskfile stamps this package directly. A caller forwarding its own
// unstamped default must not overwrite a value stamped here — that turned a
// correctly stamped binary back into "dev".
func TestVersionSet_KeepsAStampedValue(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	for _, forwarded := range []string{"", version.Unstamped} {
		version.Version = "v9"
		version.Set(forwarded)
		require.Equal(t, "v9", version.Version, "Set(%q) erased a stamped version", forwarded)
	}

	version.Version = version.Unstamped
	version.Set("v1.2.3")
	require.Equal(t, "v1.2.3", version.Version, "a real version must still be recorded")
}
