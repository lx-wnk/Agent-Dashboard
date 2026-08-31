package grants_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/grants"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

const testJWTSecret = "test-secret-for-grants"

const testCapability = "test.capability"

// jwtMiddleware signs a JWT for "user-1" and injects it via cookie so
// RequireAuth populates auth.PayloadFromContext for the handlers under test.
func jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1"}, testJWTSecret, 3600)
		if err != nil {
			http.Error(w, "test setup: sign jwt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		r.AddCookie(&http.Cookie{Name: "auth_token", Value: tok})
		auth.RequireAuth(testJWTSecret)(next).ServeHTTP(w, r)
	})
}

// testDeps bundles the handler under test with the repos backing it, so a
// test can assert against the write path directly instead of only the HTTP
// response.
type testDeps struct {
	mux       *chi.Mux
	grantRepo repo.GrantRepo
	capRepo   repo.CapabilityRepo
}

func setupGrantsHandler(t *testing.T) testDeps {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	grantRepo := repo.NewGrantRepo(bundle.Client)
	capRepo := repo.NewCapabilityRepo(bundle.Client)
	_, err = capRepo.Upsert(context.Background(), repo.UpsertCapabilityInput{
		Name:          testCapability,
		Class:         repo.CapClassTool,
		EnforceableBy: []string{"spawn"},
	})
	require.NoError(t, err)

	h := grants.NewHandler(grantRepo, capRepo)
	mux := chi.NewRouter()
	mux.Group(func(r chi.Router) {
		r.Use(jwtMiddleware)
		h.Mount(r)
	})
	return testDeps{mux: mux, grantRepo: grantRepo, capRepo: capRepo}
}

// validCreateBody returns a request body that passes every boundary check,
// so each refusal test can start from it and break exactly one field.
func validCreateBody() map[string]any {
	pattern := "git status*"
	return map[string]any{
		"capabilityName": testCapability,
		"contextKind":    repo.GrantContextGlobal,
		"contextRef":     "",
		"pattern":        &pattern,
		"mode":           repo.GrantModeAllow,
	}
}

func postGrant(t *testing.T, mux *chi.Mux, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/grants", bytes.NewReader(b)))
	return w
}

func TestCreate_MissingPattern(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	delete(body, "pattern")
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreate_UnknownCapability(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["capabilityName"] = "does.not.exist"
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unknown capability")
}

func TestCreate_InvalidMode(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["mode"] = "sometimes"
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid mode")
}

func TestCreate_InvalidContextKind(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["contextKind"] = "planet"
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid context kind")
}

func TestCreate_RefOnGlobal(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["contextKind"] = repo.GrantContextGlobal
	body["contextRef"] = "/some/project"
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "must be empty")
}

func TestCreate_MissingRefOnNonGlobal(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["contextKind"] = repo.GrantContextProject
	body["contextRef"] = ""
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "is required")
}

func TestCreate_LimitCountWithoutWindow(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["limitCount"] = 5
	body["limitWindowSeconds"] = 0
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "limitWindowSeconds")
}

func TestCreate_NonPositiveExpiry(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	zero := 0
	body["expiresInSeconds"] = &zero
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "expiresInSeconds")
}

func TestCreate_ValidGrantReachesRepoWithActingUser(t *testing.T) {
	deps := setupGrantsHandler(t)
	body := validCreateBody()
	body["reason"] = "smoke test"
	w := postGrant(t, deps.mux, body)
	require.Equal(t, http.StatusCreated, w.Code)

	var created ent.Grant
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Equal(t, testCapability, created.CapabilityName)
	require.Equal(t, "user-1", created.GrantedBy)
	require.Equal(t, "smoke test", created.Reason)

	rows, err := deps.grantRepo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "user-1", rows[0].GrantedBy)
}

func TestList_FiltersByCapabilityQueryParam(t *testing.T) {
	deps := setupGrantsHandler(t)
	_, err := deps.capRepo.Upsert(context.Background(), repo.UpsertCapabilityInput{
		Name: "other.capability", Class: repo.CapClassTool, EnforceableBy: []string{"spawn"},
	})
	require.NoError(t, err)

	body1 := validCreateBody()
	require.Equal(t, http.StatusCreated, postGrant(t, deps.mux, body1).Code)

	body2 := validCreateBody()
	body2["capabilityName"] = "other.capability"
	require.Equal(t, http.StatusCreated, postGrant(t, deps.mux, body2).Code)

	w := httptest.NewRecorder()
	deps.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/grants?capability=other.capability", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var rows []ent.Grant
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	require.Len(t, rows, 1)
	require.Equal(t, "other.capability", rows[0].CapabilityName)
}

func TestRevoke_PassesActingUserThrough(t *testing.T) {
	deps := setupGrantsHandler(t)
	w := postGrant(t, deps.mux, validCreateBody())
	require.Equal(t, http.StatusCreated, w.Code)
	var created ent.Grant
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	del := httptest.NewRecorder()
	deps.mux.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/grants/"+created.ID, nil))
	require.Equal(t, http.StatusNoContent, del.Code)

	rows, err := deps.grantRepo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].RevokedAt)
	require.Equal(t, "user-1", rows[0].RevokedBy)
}

func TestRevoke_UnknownIDIs404(t *testing.T) {
	deps := setupGrantsHandler(t)
	w := httptest.NewRecorder()
	deps.mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/grants/does-not-exist", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestListCapabilities_ReturnsCatalogue(t *testing.T) {
	deps := setupGrantsHandler(t)
	w := httptest.NewRecorder()
	deps.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var rows []ent.Capability
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	names := make([]string, len(rows))
	for i, c := range rows {
		names[i] = c.Name
	}
	require.Contains(t, names, testCapability)
}

// TestCreate_BypassAuth_ActingUserIsLocalAdmin exercises the router without
// JWT middleware — the shape DASHBOARD_AUTH=none mounts the handler in
// (router.go's protected group skips RequireAuth but still mounts
// GrantsHandler). GrantedBy must still be non-empty.
func TestCreate_BypassAuth_ActingUserIsLocalAdmin(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	grantRepo := repo.NewGrantRepo(bundle.Client)
	capRepo := repo.NewCapabilityRepo(bundle.Client)
	_, err = capRepo.Upsert(context.Background(), repo.UpsertCapabilityInput{
		Name: testCapability, Class: repo.CapClassTool, EnforceableBy: []string{"spawn"},
	})
	require.NoError(t, err)

	h := grants.NewHandler(grantRepo, capRepo)
	mux := chi.NewRouter()
	h.Mount(mux) // no JWT middleware — simulates bypass auth mode

	w := postGrant(t, mux, validCreateBody())
	require.Equal(t, http.StatusCreated, w.Code)

	rows, err := grantRepo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, auth.BypassUserID, rows[0].GrantedBy)
}
