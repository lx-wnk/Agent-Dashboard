package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/stretchr/testify/require"
)

type fakeSpawners struct{ bySlug map[string]*ent.Spawner }

func (f fakeSpawners) GetByID(_ context.Context, _ string) (*ent.Spawner, error) {
	return nil, errors.New("not used")
}
func (f fakeSpawners) GetBySlug(_ context.Context, slug string) (*ent.Spawner, error) {
	if sp, ok := f.bySlug[slug]; ok {
		return sp, nil
	}
	return nil, errors.New("not found")
}
func (f fakeSpawners) GetDefault(_ context.Context) (*ent.Spawner, error) {
	for _, sp := range f.bySlug {
		if sp.IsDefault {
			return sp, nil
		}
	}
	return nil, errors.New("not found")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// defaultSpawnerScope wires a default spawner whose config dir is a temp tree,
// so the handler resolves to it when no spawnerId/sessionId is supplied.
func handlerWithConfigDir(t *testing.T, cfgDir string) *Handler {
	t.Helper()
	def := &ent.Spawner{
		Slug:        "claude-default",
		Command:     "claude",
		Env:         map[string]string{"CLAUDE_CONFIG_DIR": cfgDir},
		AdapterType: "claude",
	}
	return NewHandler(fakeSpawners{bySlug: map[string]*ent.Spawner{"claude-default": def}}, nil, nil)
}

// fakeCwdPolicy denies every cwd unless it equals allow.
type fakeCwdPolicy struct{ allow string }

func (p fakeCwdPolicy) Allow(_ context.Context, cwd string) error {
	if cwd == p.allow {
		return nil
	}
	return errors.New("cwd outside allowed roots")
}

// memoryHasProjectFile reports whether the /api/config/memory response for the
// given cwd lists a project-scoped file (i.e. the project layer was admitted).
func memoryHasProjectFile(t *testing.T, h *Handler, cwd string) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Memory(rec, httptest.NewRequest(http.MethodGet, "/api/config/memory?cwd="+cwd, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp memoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	for _, m := range resp.Memory {
		if m.Scope == "project" {
			return true
		}
	}
	return false
}

func TestResolve_CwdPolicyDropsDisallowedProjectLayer(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "CLAUDE.md"), "# project memory")

	def := &ent.Spawner{
		Slug:        "claude-default",
		Command:     "claude",
		Env:         map[string]string{"CLAUDE_CONFIG_DIR": cfg},
		AdapterType: "claude",
	}
	spawners := fakeSpawners{bySlug: map[string]*ent.Spawner{"claude-default": def}}

	denied := NewHandler(spawners, nil, fakeCwdPolicy{allow: "/some/other/root"})
	require.False(t, memoryHasProjectFile(t, denied, project),
		"cwd outside allowed roots must not enumerate the project layer")

	allowed := NewHandler(spawners, nil, fakeCwdPolicy{allow: project})
	require.True(t, memoryHasProjectFile(t, allowed, project),
		"cwd under an allowed root must enumerate the project layer")
}

func TestCommandsEndpoint_ScopedToSpawnerConfigDir(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "deploy.md"), "---\ndescription: Deploy\n---\nrun deploy")

	h := handlerWithConfigDir(t, cfg)
	rec := httptest.NewRecorder()
	h.Commands(rec, httptest.NewRequest(http.MethodGet, "/api/config/commands", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp commandsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "default", resp.ScopeSource)
	require.Equal(t, "claude-default", resp.ScopeLabel)

	var found bool
	for _, c := range resp.Commands {
		if c.Name == "/deploy" {
			found = true
			require.Equal(t, "user", c.Source)
			require.Equal(t, "Deploy", c.Description)
			require.Contains(t, c.Body, "run deploy", "command body is included")
		}
	}
	require.True(t, found, "user command from the spawner's config dir must appear")
}

func TestSkillsEndpoint_Envelope(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: A\n---")

	h := handlerWithConfigDir(t, cfg)
	rec := httptest.NewRecorder()
	h.Skills(rec, httptest.NewRequest(http.MethodGet, "/api/config/skills", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp skillsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Skills, 1)
	require.Equal(t, "alpha", resp.Skills[0].Name)
	require.Equal(t, "user", resp.Skills[0].Source)
}

func TestMemoryEndpoint_UserScopeFromConfigDir(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "CLAUDE.md"), "# user memory")

	h := handlerWithConfigDir(t, cfg)
	rec := httptest.NewRecorder()
	h.Memory(rec, httptest.NewRequest(http.MethodGet, "/api/config/memory", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp memoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	var foundUser bool
	for _, m := range resp.Memory {
		if m.Scope == "user" && m.Path == filepath.Join(cfg, "CLAUDE.md") {
			foundUser = true
		}
	}
	require.True(t, foundUser, "user memory resolved from the spawner config dir")
}
