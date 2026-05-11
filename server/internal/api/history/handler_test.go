package history_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apihistory "github.com/lx-wnk/agent-dashboard/server/internal/api/history"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	histsvc "github.com/lx-wnk/agent-dashboard/server/internal/history"
)

const testJWTSecret = "test-history-secret"

// withAuth returns a copy of req with a valid JWT cookie for user "test-user".
func withAuth(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	token, err := auth.SignJWT(auth.JWTPayload{Sub: "test-user", Login: "tester", IsAdmin: false}, testJWTSecret, 3600)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	return req
}

// newTestRouter builds a chi router with RequireAuth + the history handler.
func newTestRouter(imp *histsvc.Importer) http.Handler {
	h := apihistory.NewHandler(imp)
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(testJWTSecret))
		h.Mount(r)
	})
	return r
}

// blockingRepo.BulkInsert blocks until done is closed.
type blockingRepo struct {
	done <-chan struct{}
}

func (b *blockingRepo) BulkInsert(_ context.Context, _ []repo.AgentCostRow) error {
	<-b.done
	return nil
}
func (b *blockingRepo) ListByTimeRange(_ context.Context, _, _ time.Time) ([]*ent.AgentCostTrend, error) {
	return nil, nil
}

// noopRepo succeeds immediately without doing anything.
type noopRepo struct{}

func (n *noopRepo) BulkInsert(_ context.Context, _ []repo.AgentCostRow) error { return nil }
func (n *noopRepo) ListByTimeRange(_ context.Context, _, _ time.Time) ([]*ent.AgentCostTrend, error) {
	return nil, nil
}

// TestHandler_Import_AlreadyRunning verifies that a second POST returns 409.
func TestHandler_Import_AlreadyRunning(t *testing.T) {
	blocked := make(chan struct{})
	imp := histsvc.NewImporter(&blockingRepo{done: blocked})
	router := newTestRouter(imp)

	// First POST — starts a background goroutine that blocks on BulkInsert.
	req1 := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/history/import", nil))
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	// Let the goroutine enter the running state.
	time.Sleep(20 * time.Millisecond)

	// Second POST — must return 409.
	req2 := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/history/import", nil))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&body))
	assert.Contains(t, body["error"], "already in progress")

	// Unblock the goroutine to avoid a goroutine leak.
	close(blocked)
}

// TestHandler_SSE_ReceivesProgress verifies that an SSE client receives Done progress.
func TestHandler_SSE_ReceivesProgress(t *testing.T) {
	imp := histsvc.NewImporter(&noopRepo{})
	router := newTestRouter(imp)

	// Trigger an import (runs against an empty projects dir — completes immediately).
	postReq := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/history/import", nil))
	postW := httptest.NewRecorder()
	router.ServeHTTP(postW, postReq)
	require.Equal(t, http.StatusOK, postW.Code)

	// Give the goroutine time to finish.
	time.Sleep(50 * time.Millisecond)

	// Connect an SSE client — should receive the final Done progress immediately
	// because the job is already finished.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	sseReq := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/history/import/status", nil).WithContext(ctx))
	sseW := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(sseW, sseReq)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("SSE stream did not close in time")
	}

	body := sseW.Body.String()
	require.True(t, strings.Contains(body, "data:"), "expected at least one SSE data line, got: %q", body)

	// Parse the last event and verify Done=true.
	scanner := bufio.NewScanner(strings.NewReader(body))
	var lastProgress histsvc.ImportProgress
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		require.NoError(t, json.Unmarshal([]byte(payload), &lastProgress))
	}
	assert.True(t, lastProgress.Done, "expected last SSE event to have Done=true")
}
