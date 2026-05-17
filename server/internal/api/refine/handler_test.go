package refine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	apirefine "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
)

const testJWTSecret = "test-secret-32-chars-minimum-here"

// --- fake repos ---

type fakeTurnRepo struct {
	turns []*ent.RefinementTurn
}

func (f *fakeTurnRepo) Create(_ context.Context, inp repo.CreateTurnInput) (*ent.RefinementTurn, error) {
	content := inp.Content
	if len(content) > 8 {
		content = content[:8]
	}
	t := &ent.RefinementTurn{
		ID:        "turn-" + inp.Role + "-" + content,
		TaskID:    inp.TaskID,
		Content:   inp.Content,
		Phase:     inp.Phase,
		CreatedAt: time.Now(),
	}
	f.turns = append(f.turns, t)
	return t, nil
}

func (f *fakeTurnRepo) ListForTask(_ context.Context, taskID string, _ int) ([]*ent.RefinementTurn, error) {
	var out []*ent.RefinementTurn
	for _, t := range f.turns {
		if t.TaskID == taskID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTurnRepo) ListForTaskNewest(_ context.Context, taskID string, _ int) ([]*ent.RefinementTurn, error) {
	return f.ListForTask(context.Background(), taskID, 0)
}

func (f *fakeTurnRepo) DeleteForTask(_ context.Context, _ string) error { return nil }

type fakeTaskRepo struct{}

func (f *fakeTaskRepo) GetByID(_ context.Context, id string) (*ent.Task, error) {
	return &ent.Task{ID: id, Title: "Test Task", Cwd: "/tmp"}, nil
}
func (f *fakeTaskRepo) Create(_ context.Context, _ repo.CreateTaskInput) (*ent.Task, error) {
	return nil, nil
}
func (f *fakeTaskRepo) GetBySlug(_ context.Context, _ string) (*ent.Task, error) { return nil, nil }
func (f *fakeTaskRepo) Update(_ context.Context, _ string, _ repo.UpdateTaskInput) (*ent.Task, error) {
	return nil, nil
}
func (f *fakeTaskRepo) Delete(_ context.Context, _ string) error              { return nil }
func (f *fakeTaskRepo) ListForUser(_ context.Context, _ string, _ bool) ([]*ent.Task, error) {
	return nil, nil
}
func (f *fakeTaskRepo) ListPickable(_ context.Context) ([]*ent.Task, error)       { return nil, nil }
func (f *fakeTaskRepo) ListByStage(_ context.Context, _ string) ([]*ent.Task, error) {
	return nil, nil
}

// --- helpers ---

// authToken returns a signed JWT cookie value for test requests.
func authToken(t *testing.T) string {
	t.Helper()
	tok, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1", Login: "tester", IsAdmin: false}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}
	return tok
}

func makeRouter(turns *fakeTurnRepo, spawner func(context.Context, refine.SpawnConfig) (<-chan string, error)) http.Handler {
	h := apirefine.NewHandler(turns, &fakeTaskRepo{})
	if spawner != nil {
		h = h.WithSpawner(spawner)
	}
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return r
}

func withAuth(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: authToken(t)})
	return req
}

func noopSpawner(_ context.Context, _ refine.SpawnConfig) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

// --- tests ---

func TestListTurns_Empty(t *testing.T) {
	r := makeRouter(&fakeTurnRepo{}, noopSpawner)
	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/refine/task-1/turns", nil))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var turns []any
	if err := json.Unmarshal(rr.Body.Bytes(), &turns); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("want empty list, got %d items", len(turns))
	}
}

func TestListTurns_ReturnsTurns(t *testing.T) {
	phase := "drafting"
	repo := &fakeTurnRepo{
		turns: []*ent.RefinementTurn{
			{ID: "t1", TaskID: "task-1", Content: "Hello", CreatedAt: time.Now()},
			{ID: "t2", TaskID: "task-1", Content: "Hi", Phase: &phase, CreatedAt: time.Now()},
		},
	}
	r := makeRouter(repo, noopSpawner)
	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/refine/task-1/turns", nil))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var turns []map[string]any
	json.Unmarshal(rr.Body.Bytes(), &turns)
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
}

func TestSubmitTurn_StreamsResponse(t *testing.T) {
	spawner := func(_ context.Context, _ refine.SpawnConfig) (<-chan string, error) {
		ch := make(chan string, 2)
		ch <- "Hello"
		ch <- " world"
		close(ch)
		return ch, nil
	}
	r := makeRouter(&fakeTurnRepo{}, spawner)
	body, _ := json.Marshal(map[string]string{"message": "test prompt"})
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/refine/task-1/turn", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("want SSE content-type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "Hello") {
		t.Errorf("SSE body missing streamed content: %s", rr.Body.String())
	}
}

func TestSubmitTurn_RequiresMessage(t *testing.T) {
	r := makeRouter(&fakeTurnRepo{}, noopSpawner)
	body, _ := json.Marshal(map[string]string{"message": "   "})
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/refine/task-1/turn", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestConfirm_StoresSentinel(t *testing.T) {
	repo := &fakeTurnRepo{}
	r := makeRouter(repo, noopSpawner)
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/refine/task-1/confirm", nil))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	found := false
	for _, turn := range repo.turns {
		if turn.Phase != nil && *turn.Phase == "confirmed" {
			found = true
		}
	}
	if !found {
		t.Error("want confirmed sentinel turn to be stored")
	}
}
