package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func getFile(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/file?path="+path, nil)
	h.File(rec, req)
	return rec
}

func putFile(t *testing.T, h *Handler, body filePutRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config/file", strings.NewReader(string(raw)))
	h.SaveFile(rec, req)
	return rec
}

func TestFileRead_HappyPath(t *testing.T) {
	cfg := t.TempDir()
	cmdPath := filepath.Join(cfg, "commands", "deploy.md")
	writeFile(t, cmdPath, "---\ndescription: Deploy\n---\nrun deploy")

	h := handlerWithConfigDir(t, cfg)
	rec := getFile(t, h, cmdPath)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp fileResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Content, "run deploy")
	require.True(t, resp.Editable)
	require.Equal(t, "user", resp.Source)
	require.NotZero(t, resp.MTime)
}

func TestFileRead_RejectsOutOfScopePath(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "deploy.md"), "body")
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, secret, "top secret")

	h := handlerWithConfigDir(t, cfg)
	rec := getFile(t, h, secret)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestFileRead_RejectsSymlinkEscape(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "deploy.md"), "body")
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, secret, "top secret")
	link := filepath.Join(cfg, "commands", "evil.md")
	require.NoError(t, os.Symlink(secret, link))

	h := handlerWithConfigDir(t, cfg)
	// The symlink path canonicalizes to the secret, which is not in the editable
	// set; enumeration also never lists symlinked command files.
	rec := getFile(t, h, link)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestFileRead_RejectsPlugin(t *testing.T) {
	cfg := t.TempDir()
	// Plugin command: <cfg>/plugins/cache/<marketplace>/<plugin>/commands/x.md
	pluginCmd := filepath.Join(cfg, "plugins", "cache", "mkt", "myplugin", "commands", "x.md")
	writeFile(t, pluginCmd, "---\ndescription: X\n---\nbody")

	h := handlerWithConfigDir(t, cfg)
	rec := getFile(t, h, pluginCmd)

	require.Equal(t, http.StatusForbidden, rec.Code, "plugin command files are not editable")
}

func TestFileWrite_HappyPathAtomicAndMode(t *testing.T) {
	cfg := t.TempDir()
	mem := filepath.Join(cfg, "CLAUDE.md")
	writeFile(t, mem, "old")
	require.NoError(t, os.Chmod(mem, 0o640))

	h := handlerWithConfigDir(t, cfg)
	info, err := os.Stat(mem)
	require.NoError(t, err)

	rec := putFile(t, h, filePutRequest{Path: mem, Content: "new content", BaseMTime: info.ModTime().Unix()})
	require.Equal(t, http.StatusOK, rec.Code)

	got, err := os.ReadFile(mem)
	require.NoError(t, err)
	require.Equal(t, "new content", string(got))

	newInfo, err := os.Stat(mem)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), newInfo.Mode().Perm(), "file mode is preserved across atomic write")
}

func TestFileWrite_ConflictOnMTimeDrift(t *testing.T) {
	cfg := t.TempDir()
	mem := filepath.Join(cfg, "CLAUDE.md")
	writeFile(t, mem, "old")

	h := handlerWithConfigDir(t, cfg)
	rec := putFile(t, h, filePutRequest{Path: mem, Content: "x", BaseMTime: 1})
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestFileWrite_RejectsNonEditable(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "CLAUDE.md"), "x")
	pluginCmd := filepath.Join(cfg, "plugins", "cache", "mkt", "myplugin", "commands", "x.md")
	writeFile(t, pluginCmd, "---\ndescription: X\n---\nbody")

	h := handlerWithConfigDir(t, cfg)
	rec := putFile(t, h, filePutRequest{Path: pluginCmd, Content: "hacked"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestFileWrite_RejectsOversize(t *testing.T) {
	cfg := t.TempDir()
	mem := filepath.Join(cfg, "CLAUDE.md")
	writeFile(t, mem, "old")

	h := handlerWithConfigDir(t, cfg)
	huge := strings.Repeat("a", maxConfigFileBytes+1)
	rec := putFile(t, h, filePutRequest{Path: mem, Content: huge})
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}
