package adapters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

// TestPutConfig_PersistsToDisk verifies that a successful PUT writes the
// adapter name to the config file on disk.
func TestPutConfig_PersistsToDisk(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "adapter-cfg-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	cfg := &config.AdapterConfig{Default: "claude"}
	h := NewHandler(cfg, f.Name())

	body := `{"default":"ollama"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/adapters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	apierr.ErrorMiddleware(h.putConfig).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if !strings.Contains(string(data), "ollama") {
		t.Errorf("expected 'ollama' in persisted file, got: %s", string(data))
	}
}

// TestPutConfig_NoCfgFile_NoError verifies that a PUT with no config file
// returns 200 without panicking.
func TestPutConfig_NoCfgFile_NoError(t *testing.T) {
	t.Parallel()

	cfg := &config.AdapterConfig{Default: "claude"}
	h := NewHandler(cfg, "") // no file

	body := `{"default":"openai"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/adapters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	apierr.ErrorMiddleware(h.putConfig).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPutConfig_PersistFailure_NonFatal verifies that a persistence failure
// does not prevent the handler from returning 200 with restartRequired:true.
func TestPutConfig_PersistFailure_NonFatal(t *testing.T) {
	t.Parallel()

	cfg := &config.AdapterConfig{Default: "claude"}
	h := NewHandler(cfg, "/dev/null/impossible") // unwritable path

	body := `{"default":"custom"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/adapters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	apierr.ErrorMiddleware(h.putConfig).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	restart, _ := resp["restartRequired"].(bool)
	if !restart {
		t.Errorf("expected restartRequired:true in response, got: %v", resp)
	}
}
