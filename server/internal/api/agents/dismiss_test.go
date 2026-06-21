package agents

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

func discoveryFile(t *testing.T, home string, pid int, suffix string) string {
	t.Helper()
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, strconv.Itoa(pid)+suffix)
	if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func doDismiss(t *testing.T, pid int) *httptest.ResponseRecorder {
	t.Helper()
	h := NewSpawnHandler(nil)
	r := chi.NewRouter()
	r.Delete("/api/agents/{pid}/channel", h.DismissChannel)
	req := httptest.NewRequest(http.MethodDelete, "/api/agents/"+strconv.Itoa(pid)+"/channel", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDismissChannel_DeletesFilesForDeadPID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pid := 999999 // almost certainly dead
	bridge := discoveryFile(t, home, pid, ".json")
	pty := discoveryFile(t, home, pid, ".pty.json")

	rec := doDismiss(t, pid)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, err := os.Stat(bridge); !os.IsNotExist(err) {
		t.Errorf("bridge file still present")
	}
	if _, err := os.Stat(pty); !os.IsNotExist(err) {
		t.Errorf("pty file still present")
	}
}

func TestDismissChannel_IdempotentWhenAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if rec := doDismiss(t, 999998); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestDismissChannel_RefusesLivePID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pid := os.Getpid() // current process is alive
	bridge := discoveryFile(t, home, pid, ".json")

	rec := doDismiss(t, pid)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if _, err := os.Stat(bridge); err != nil {
		t.Errorf("live agent's discovery file was deleted")
	}
}

func TestDismissChannel_RejectsBadPID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewSpawnHandler(nil)
	r := chi.NewRouter()
	r.Delete("/api/agents/{pid}/channel", h.DismissChannel)
	req := httptest.NewRequest(http.MethodDelete, "/api/agents/abc/channel", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
