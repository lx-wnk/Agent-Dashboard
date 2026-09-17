package applications_test

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
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/applications"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

func newMux(t *testing.T) (*chi.Mux, repo.MCPApplicationRepo, repo.GrantRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	box, err := secretbox.New(make([]byte, 32))
	require.NoError(t, err)
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	secrets := repo.NewApplicationSecretRepo(bundle.Client, box)
	grants := repo.NewGrantRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	_, err = apps.Upsert(context.Background(), repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	mux := chi.NewRouter()
	applications.NewHandler(apps, secrets, mcpapps.Refresher{Now: time.Now}, grants, resources).Mount(mux)
	return mux, apps, grants
}

func do(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func TestSecrets_AreWriteOnly(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/MAIL_PASSWORD", map[string]string{"value": "hunter2"})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "hunter2", "a secret value must never leave the server")
	require.Contains(t, rec.Body.String(), `"envName":"MAIL_PASSWORD"`)
}

func TestSecrets_RejectInvalidVariableNames(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/not-an-env-name", map[string]string{"value": "x"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPatch_SetsAttachAllAndRequiredEnv(t *testing.T) {
	mux, apps, _ := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"attachAll": true, "requiredEnv": []string{"MAIL_PASSWORD"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	app, err := apps.GetByResourceID(context.Background(), "res-mail")
	require.NoError(t, err)
	require.True(t, app.AttachAll)
	require.Equal(t, []string{"MAIL_PASSWORD"}, app.RequiredEnv)
}

func TestUnknownApplicationIs404(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/nope", map[string]any{"attachAll": true})
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.False(t, strings.Contains(rec.Body.String(), "panic"))
}

func TestApplyPreset_MissingRoutineIdIs400(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications/res-mail/presets/imap-mcp-server", map[string]any{})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestApplyPreset_UnconfirmedPresetIs409(t *testing.T) {
	mux, _, grants := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications/res-mail/presets/imap-mcp-server", map[string]any{"routineId": "routine-1"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	rows, err := grants.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestApplyPreset_UnknownPresetIs404(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications/res-mail/presets/no-such-preset", map[string]any{"routineId": "routine-1"})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestCreateApplication_Succeeds(t *testing.T) {
	mux, apps, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name":    "notes",
		"command": "uvx",
		"args":    []string{"notes-mcp"},
		"env":     map[string]string{"NOTES_DIR": "/tmp"},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var view struct {
		ResourceID     string              `json:"resourceId"`
		ServerName     string              `json:"serverName"`
		ExportToClaude bool                `json:"exportToClaude"`
		Entry          mcpapps.ServerEntry `json:"entry"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.Equal(t, "notes", view.ServerName)
	require.False(t, view.ExportToClaude)
	wantEntry := mcpapps.ServerEntry{Command: "uvx", Args: []string{"notes-mcp"}, Env: map[string]string{"NOTES_DIR": "/tmp"}}
	require.Equal(t, wantEntry, view.Entry)

	rec = do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"serverName":"notes"`)

	app, err := apps.GetByResourceID(context.Background(), view.ResourceID)
	require.NoError(t, err)
	storedEntry, err := mcpapps.ParseEntry(app.Entry)
	require.NoError(t, err)
	require.Equal(t, wantEntry, storedEntry)
}

func TestCreateApplication_InvalidSlugIs400(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "Notes Server", "command": "uvx",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "slug must match")
}

func TestCreateApplication_ReservedNameIs400(t *testing.T) {
	for _, name := range []string{"dashboard-channel", "dashboard-tasks"} {
		t.Run(name, func(t *testing.T) {
			mux, _, _ := newMux(t)
			rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
				"name": name, "command": "uvx",
			})
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestCreateApplication_DuplicateNameIs409(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "mail", "command": "uvx",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestCreateApplication_MissingCommandIs400(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "notes",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "command is required")
}

func TestCreateApplication_InvalidEnvKeyIs400(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "notes", "command": "uvx", "env": map[string]string{"lower": "x"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestPatch_ReplacesEntry(t *testing.T) {
	mux, apps, _ := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"attachAll": true,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"entry": map[string]any{"command": "uvx", "args": []string{"x"}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	app, err := apps.GetByResourceID(context.Background(), "res-mail")
	require.NoError(t, err)
	entry, err := mcpapps.ParseEntry(app.Entry)
	require.NoError(t, err)
	require.Equal(t, mcpapps.ServerEntry{Command: "uvx", Args: []string{"x"}}, entry)
	require.True(t, app.AttachAll)
}

func TestView_NoEntryIsEmptyObject(t *testing.T) {
	mux, _, _ := newMux(t)
	rec := do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"entry":{}`)
}

func TestPatch_EntryKeepsFieldsTheAppDoesNotEdit(t *testing.T) {
	mux, apps, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"type":"http","url":"https://x","headers":{"A":"b"},"args":["old"]}`))
	require.NoError(t, err)

	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"entry": map[string]any{"type": "http", "url": "https://y"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(app.Entry, &obj))
	require.Equal(t, map[string]any{"A": "b"}, obj["headers"], "a key the app does not edit must survive an edit")
	require.Equal(t, "https://y", obj["url"])
	require.NotContains(t, obj, "args", "a field cleared in the form must be removed")
}
