package applications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/applications"
	"github.com/lx-wnk/agent-dashboard/server/internal/claudeconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/schema"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

func newMux(t *testing.T) (*chi.Mux, repo.MCPApplicationRepo, repo.GrantRepo, repo.ApplicationSecretRepo, repo.ResourceRepo, repo.TaskScheduleRepo) {
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
	schedules := repo.NewTaskScheduleRepo(bundle.Client)
	_, err = apps.Upsert(context.Background(), repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	require.NoError(t, err)
	mux := chi.NewRouter()
	applications.NewHandler(apps, secrets, mcpapps.Refresher{Now: time.Now}, grants, resources, schedules).Mount(mux)
	return mux, apps, grants, secrets, resources, schedules
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
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/MAIL_PASSWORD", map[string]string{"value": "hunter2"})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "hunter2", "a secret value must never leave the server")
	require.Contains(t, rec.Body.String(), `"envName":"MAIL_PASSWORD"`)
}

func TestSecrets_RejectInvalidVariableNames(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPut, "/api/applications/res-mail/secrets/not-an-env-name", map[string]string{"value": "x"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPatch_SetsAttachAllAndRequiredEnv(t *testing.T) {
	mux, apps, _, _, _, _ := newMux(t)
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
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPatch, "/api/applications/nope", map[string]any{"attachAll": true})
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.False(t, strings.Contains(rec.Body.String(), "panic"))
}

func TestDenies_AppliesPresetAndIsIdempotent(t *testing.T) {
	mux, apps, grants, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"npx","args":["-y","imap-mcp-server"]}`))
	require.NoError(t, err)

	rec := do(t, mux, http.MethodPost, "/api/applications/res-mail/denies", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var first mcpapps.DenyResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	require.Equal(t, "imap-mcp-server", first.Preset)
	require.NotEmpty(t, first.Created)
	require.Empty(t, first.Existing)

	rec = do(t, mux, http.MethodPost, "/api/applications/res-mail/denies", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var second mcpapps.DenyResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	require.Empty(t, second.Created, "applying twice must not duplicate grants")
	require.ElementsMatch(t, first.Created, second.Existing)

	rows, err := grants.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
}

func TestDenies_NoMatchingPresetIsEmptyResult(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications/res-mail/denies", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res mcpapps.DenyResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.Equal(t, mcpapps.DenyResult{Created: []string{}, Existing: []string{}}, res)
}

func TestDenies_UnknownApplicationIs404(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications/nope/denies", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateApplication_Succeeds(t *testing.T) {
	mux, apps, _, _, _, _ := newMux(t)
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
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "Notes Server", "command": "uvx",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "slug must match")
}

func TestCreateApplication_ReservedNameIs400(t *testing.T) {
	for _, name := range []string{"dashboard-channel", "dashboard-tasks"} {
		t.Run(name, func(t *testing.T) {
			mux, _, _, _, _, _ := newMux(t)
			rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
				"name": name, "command": "uvx",
			})
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestCreateApplication_DuplicateNameIs409(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "mail", "command": "uvx",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestCreateApplication_MissingCommandIs400(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "notes",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "command is required")
}

func TestCreateApplication_InvalidEnvKeyIs400(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "notes", "command": "uvx", "env": map[string]string{"lower": "x"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestPatch_ReplacesEntry(t *testing.T) {
	mux, apps, _, _, _, _ := newMux(t)
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
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodGet, "/api/applications", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"entry":{}`)
}

func TestPatch_EntryKeepsFieldsTheAppDoesNotEdit(t *testing.T) {
	mux, apps, _, _, _, _ := newMux(t)
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

// deletableApp gives res-mail everything a delete has to clean up: a registry
// row to orphan, a catalogue so there is a capability to revoke, a live grant
// on it and one secret.
func deletableApp(t *testing.T, apps repo.MCPApplicationRepo, grants repo.GrantRepo, secrets repo.ApplicationSecretRepo, resources repo.ResourceRepo) string {
	t.Helper()
	ctx := context.Background()
	res, err := resources.Upsert(ctx, repo.UpsertResourceInput{
		Kind: repo.ResourceKindApplication, Slug: "mcp-mail", Name: "mail",
		Scope: repo.GlobalScope(), State: repo.ResourceStateDiscovered,
		Origin: repo.ResourceOriginLocal, OriginRef: "mail",
	})
	require.NoError(t, err)
	_, err = apps.Upsert(ctx, repo.UpsertMCPApplicationInput{ResourceID: res.ID, ServerName: "notes"})
	require.NoError(t, err)
	require.NoError(t, apps.RecordCatalogue(ctx, res.ID, []schema.CatalogueTool{{Name: "read"}}, "", time.Now()))
	require.NoError(t, secrets.Set(ctx, res.ID, "NOTES_TOKEN", "hunter2"))
	_, err = grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: mcpapps.CapabilityName("notes", "read"),
		Context:        repo.GrantContextFor("application", res.ID),
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "tester",
	})
	require.NoError(t, err)
	return res.ID
}

func attachRoutine(t *testing.T, schedules repo.TaskScheduleRepo, name, resourceID string) {
	t.Helper()
	_, err := schedules.Create(context.Background(), repo.CreateTaskScheduleInput{
		Name: name, CronExpr: "*/5 * * * *", SlugPrefix: name,
		Title: name, Cwd: "/tmp", Priority: "medium",
		MaxIterations: 20, StageTimeoutSeconds: 1800,
		Applications: []string{resourceID},
	})
	require.NoError(t, err)
}

func liveGrants(t *testing.T, grants repo.GrantRepo, capName string) int {
	t.Helper()
	rows, err := grants.ListForCapability(context.Background(), capName)
	require.NoError(t, err)
	live := 0
	for _, g := range rows {
		if g.RevokedAt == nil {
			live++
		}
	}
	return live
}

func TestDeleteApplication_RemovesRowSecretsAndGrantsAndOrphansTheResource(t *testing.T) {
	mux, apps, grants, secrets, resources, _ := newMux(t)
	ctx := context.Background()
	resID := deletableApp(t, apps, grants, secrets, resources)

	rec := do(t, mux, http.MethodDelete, "/api/applications/"+resID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	_, err := apps.GetByResourceID(ctx, resID)
	require.True(t, ent.IsNotFound(err))

	meta, err := secrets.List(ctx, resID)
	require.NoError(t, err)
	require.Empty(t, meta)

	require.Equal(t, 0, liveGrants(t, grants, mcpapps.CapabilityName("notes", "read")))

	res, err := resources.Get(ctx, repo.ResourceKindApplication, repo.GlobalScope(), "mcp-mail")
	require.NoError(t, err, "the resource row must survive so grants anchored to it still resolve")
	require.Equal(t, repo.ResourceStateOrphaned, res.State)
}

func TestDeleteApplication_AttachedToRoutinesIs409AndRemovesNothing(t *testing.T) {
	mux, apps, grants, secrets, resources, schedules := newMux(t)
	ctx := context.Background()
	resID := deletableApp(t, apps, grants, secrets, resources)
	attachRoutine(t, schedules, "inbox", resID)
	attachRoutine(t, schedules, "nightly", resID)

	rec := do(t, mux, http.MethodDelete, "/api/applications/"+resID, nil)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "inbox")
	require.Contains(t, rec.Body.String(), "nightly")

	_, err := apps.GetByResourceID(ctx, resID)
	require.NoError(t, err, "nothing may be removed while a routine still attaches it")
	meta, err := secrets.List(ctx, resID)
	require.NoError(t, err)
	require.Len(t, meta, 1)
	require.Equal(t, 1, liveGrants(t, grants, mcpapps.CapabilityName("notes", "read")))
}

func TestDeleteApplication_UnknownIdIs404(t *testing.T) {
	mux, _, _, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodDelete, "/api/applications/res-nope", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPatch_ExportToClaudeWritesFileAndStoresHash(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mux, apps, _, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"uvx"}`))
	require.NoError(t, err)

	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{"exportToClaude": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	servers, err := claudeconfig.UserMCPServers()
	require.NoError(t, err)
	require.Contains(t, servers, "mail")

	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)
	require.True(t, app.ExportToClaude)
	require.NotEmpty(t, app.ExportedHash)
	require.Equal(t, mcpapps.EntryHash(app.Entry), app.ExportedHash)
}

func TestPatch_UnexportRemovesFileEntryAndClearsHash(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mux, apps, _, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"uvx"}`))
	require.NoError(t, err)
	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{"exportToClaude": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{"exportToClaude": false})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	servers, err := claudeconfig.UserMCPServers()
	require.NoError(t, err)
	require.NotContains(t, servers, "mail")

	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)
	require.False(t, app.ExportToClaude)
	require.Equal(t, "", app.ExportedHash)
}

func TestPatch_EntryChangeWhileExportedRewritesFileAndHash(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mux, apps, _, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"uvx"}`))
	require.NoError(t, err)
	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{"exportToClaude": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{
		"entry": map[string]any{"command": "npx"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	servers, err := claudeconfig.UserMCPServers()
	require.NoError(t, err)
	var entry mcpapps.ServerEntry
	require.NoError(t, json.Unmarshal(servers["mail"], &entry))
	require.Equal(t, "npx", entry.Command)

	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)
	require.Equal(t, mcpapps.EntryHash(app.Entry), app.ExportedHash)
}

func TestPatch_ExportWriteFailureIs502AndLeavesRowUntouched(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	target := dir + "/real.json"
	require.NoError(t, os.WriteFile(target, []byte(`{"mcpServers":{}}`), 0o600))
	require.NoError(t, os.Symlink(target, dir+"/.claude.json"))

	mux, apps, _, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"uvx"}`))
	require.NoError(t, err)

	rec := do(t, mux, http.MethodPatch, "/api/applications/res-mail", map[string]any{"exportToClaude": true})
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	app, err := apps.GetByResourceID(ctx, "res-mail")
	require.NoError(t, err)
	require.False(t, app.ExportToClaude)
	require.Equal(t, "", app.ExportedHash)
}

func TestDrift_ReportsServerWithNoApplicationRow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json", []byte(`{"mcpServers":{"notes":{"command":"x"}}}`), 0o600))
	mux, _, _, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodGet, "/api/applications/drift", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"found":["notes"],"changed":[]}`, rec.Body.String())
}

func TestImportApplication_CopiesEntryFromClaudeConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json", []byte(`{"mcpServers":{"notes":{"command":"uvx","args":["notes-mcp"]}}}`), 0o600))
	mux, apps, _, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications/import", map[string]any{"name": "notes"})
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
	require.Equal(t, mcpapps.ServerEntry{Command: "uvx", Args: []string{"notes-mcp"}}, view.Entry)

	app, err := apps.GetByResourceID(context.Background(), view.ResourceID)
	require.NoError(t, err)
	storedEntry, err := mcpapps.ParseEntry(app.Entry)
	require.NoError(t, err)
	require.Equal(t, mcpapps.ServerEntry{Command: "uvx", Args: []string{"notes-mcp"}}, storedEntry)
}

func TestImportApplication_UnknownNameIs404(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mux, _, _, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications/import", map[string]any{"name": "mail"})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	var errBody struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	require.Equal(t, `no server named "mail" in Claude's config`, errBody.Error)
}

func TestImportApplication_ExistingRowIs409(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json", []byte(`{"mcpServers":{"mail":{"command":"x"}}}`), 0o600))
	mux, _, _, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications/import", map[string]any{"name": "mail"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestImportApplication_ReservedNameIs400(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	mux, _, _, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications/import", map[string]any{"name": "dashboard-channel"})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestDrift_UnreadableConfigReportsNothingRatherThanEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json", []byte(`{ not json`), 0o600))
	mux, apps, _, _, _, _ := newMux(t)
	ctx := context.Background()
	_, err := apps.SetEntry(ctx, "res-mail", json.RawMessage(`{"command":"x"}`))
	require.NoError(t, err)
	_, err = apps.SetExport(ctx, "res-mail", true, mcpapps.EntryHash([]byte(`{"command":"x"}`)))
	require.NoError(t, err)

	rec := do(t, mux, http.MethodGet, "/api/applications/drift", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"found":[],"changed":[]}`, rec.Body.String(),
		"a config that cannot be read is no information, not evidence of an edit")
}

func TestDeleteApplication_TakesItsMirrorOutOfClaudeConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json",
		[]byte(`{"mcpServers":{"notes":{"command":"x"},"mine":{"command":"y"}}}`), 0o600))
	mux, apps, grants, secrets, resources, _ := newMux(t)
	ctx := context.Background()
	resID := deletableApp(t, apps, grants, secrets, resources)
	_, err := apps.SetEntry(ctx, resID, json.RawMessage(`{"command":"y"}`))
	require.NoError(t, err)
	rec := do(t, mux, http.MethodPatch, "/api/applications/"+resID, map[string]any{"exportToClaude": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, mux, http.MethodDelete, "/api/applications/"+resID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	servers, err := claudeconfig.UserMCPServers()
	require.NoError(t, err)
	require.NotContains(t, servers, "notes", "a mirror the app wrote is removed with the application")
	require.Contains(t, servers, "mine", "a server the app never mirrored stays")
}

func liveDenies(t *testing.T, grants repo.GrantRepo, capName string) int {
	t.Helper()
	rows, err := grants.ListForCapability(context.Background(), capName)
	require.NoError(t, err)
	n := 0
	for _, g := range rows {
		if g.RevokedAt == nil && g.Mode == repo.GrantModeDeny && g.ContextKind == repo.GrantContextGlobal {
			n++
		}
	}
	return n
}

func TestCreateApplication_DeniesTheDangerousToolsAtOnce(t *testing.T) {
	mux, _, grants, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "inbox", "command": "npx", "args": []string{"-y", "imap-mcp-server"},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Equal(t, 1, liveDenies(t, grants, mcpapps.CapabilityName("inbox", "imap_send_email")),
		"a new server's dangerous tools are denied before anyone refreshes its catalogue")
	require.Equal(t, 1, liveDenies(t, grants, mcpapps.CapabilityName("inbox", "imap_bulk_delete")))
}

func TestCreateApplication_UnknownServerGetsNoGrants(t *testing.T) {
	mux, _, grants, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "notes", "command": "npx", "args": []string{"-y", "notes-mcp"},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rows, err := grants.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows, "a server with no preset gets no grants at all")
}

func TestImportApplication_DeniesTheDangerousTools(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/.claude.json",
		[]byte(`{"mcpServers":{"inbox":{"command":"npx","args":["-y","imap-mcp-server"]}}}`), 0o600))
	mux, _, grants, _, _, _ := newMux(t)

	rec := do(t, mux, http.MethodPost, "/api/applications/import", map[string]any{"name": "inbox"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Equal(t, 1, liveDenies(t, grants, mcpapps.CapabilityName("inbox", "imap_send_email")))
}

func TestDenies_AppliedTwiceLeavesOneGrant(t *testing.T) {
	mux, _, grants, _, _, _ := newMux(t)
	rec := do(t, mux, http.MethodPost, "/api/applications", map[string]any{
		"name": "inbox", "command": "npx", "args": []string{"-y", "imap-mcp-server"},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		ResourceID string `json:"resourceId"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	rec = do(t, mux, http.MethodPost, "/api/applications/"+created.ResourceID+"/denies", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Equal(t, 1, liveDenies(t, grants, mcpapps.CapabilityName("inbox", "imap_send_email")),
		"re-applying the denies must not stack a second grant")
}
