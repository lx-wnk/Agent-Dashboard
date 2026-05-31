package spawners

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// stripSSEFrame strips the "data: " prefix and "\n\n" suffix added by Broadcaster.
func stripSSEFrame(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, []byte("data: "))
	raw = bytes.TrimSuffix(raw, []byte("\n\n"))
	return raw
}

// newTestHandlerForPkg creates a Handler backed by an in-memory SQLite DB.
func newTestHandlerForPkg(t *testing.T) *Handler {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return NewHandler(repo.NewSpawnerRepo(bundle.Client), nil)
}

func TestCreate_BroadcastsSpawnerCreated(t *testing.T) {
	bc := sse.NewSpawnerBroadcaster(sse.NewBroadcaster())
	h := newTestHandlerForPkg(t)
	h.broadcaster = bc
	ch := bc.Subscribe()
	defer bc.Unsubscribe(ch)

	r := chi.NewRouter()
	h.Mount(r)
	body := `{"name":"My Spawner","slug":"my-spawner","command":"claude"}`
	req := httptest.NewRequest("POST", "/api/spawners", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	select {
	case raw := <-ch:
		var ev map[string]any
		if err := json.Unmarshal(stripSSEFrame(raw), &ev); err != nil {
			t.Fatalf("frame not JSON: %v", err)
		}
		if ev["type"] != "spawner_created" {
			t.Errorf("event type: got %v, want spawner_created", ev["type"])
		}
		if ev["payload"] == nil {
			t.Error("spawner_created must carry the spawner payload")
		}
	case <-time.After(time.Second):
		t.Fatal("no spawner_created event broadcast")
	}
}
