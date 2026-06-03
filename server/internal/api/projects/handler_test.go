package projects

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

// newTestHandler creates a Handler backed by an in-memory SQLite DB with no TaskProjectOps.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return NewHandler(repo.NewProjectRepo(bundle.Client), repo.NewProjectFolderRepo(bundle.Client), nil, nil)
}

func TestCreate_BroadcastsProjectCreated(t *testing.T) {
	bc := sse.NewProjectBroadcaster(sse.NewBroadcaster())
	h := newTestHandler(t)
	h.broadcaster = bc
	ch := bc.Subscribe()
	defer bc.Unsubscribe(ch)

	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest("POST", "/api/projects", bytes.NewBufferString(`{"name":"Proj","slug":"proj"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create: got %d, body=%s", rr.Code, rr.Body.String())
	}

	select {
	case raw := <-ch:
		var ev map[string]any
		if err := json.Unmarshal(stripSSEFrame(raw), &ev); err != nil {
			t.Fatalf("frame not JSON: %v", err)
		}
		if ev["type"] != "project_created" {
			t.Errorf("event type: got %v, want project_created", ev["type"])
		}
		if ev["payload"] == nil {
			t.Error("project_created must carry the project payload")
		}
	case <-time.After(time.Second):
		t.Fatal("no project_created event broadcast")
	}
}
