// Package memory provides HTTP handlers for reading and writing ~/.claude memory files.
package memory

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var safeSegmentRE = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// claudeRoot returns ~/.claude.
// It is a var so tests can override it without changing production behaviour.
var claudeRoot = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// safePath validates and resolves a path of the form
// "projects/{project}/memory/{file}.md" to an absolute filesystem path.
// Returns empty string if the path is invalid or escapes ~/.claude/projects.
func safePath(encoded string) string {
	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		return ""
	}
	parts := strings.Split(decoded, "/")
	if len(parts) != 4 {
		return ""
	}
	head, project, memDir, file := parts[0], parts[1], parts[2], parts[3]
	if head != "projects" || memDir != "memory" {
		return ""
	}
	if !safeSegmentRE.MatchString(project) || !safeSegmentRE.MatchString(file) {
		return ""
	}
	if !strings.HasSuffix(file, ".md") || file == ".md" {
		return ""
	}
	root := claudeRoot()
	resolved := filepath.Join(root, "projects", project, "memory", file)
	expectedPrefix := filepath.Join(root, "projects") + string(filepath.Separator)
	if !strings.HasPrefix(resolved, expectedPrefix) {
		return ""
	}
	// Resolve symlinks to prevent escape via symlinked directories.
	real, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		// File may not exist yet (PUT creates it) — check the parent dir instead.
		parentReal, err2 := filepath.EvalSymlinks(filepath.Dir(resolved))
		if err2 != nil {
			return ""
		}
		expectedParent := filepath.Join(root, "projects", project, "memory") + string(filepath.Separator)
		if !strings.HasPrefix(parentReal+string(filepath.Separator), expectedParent) {
			return ""
		}
		return resolved
	}
	// Tighten the prefix check to the specific project's memory directory so a
	// symlink inside one project cannot escape into a sibling project's files.
	expectedRealPrefix := filepath.Join(root, "projects", project, "memory") + string(filepath.Separator)
	if !strings.HasPrefix(real+string(filepath.Separator), expectedRealPrefix) {
		return ""
	}
	return resolved
}

// List handles GET /api/memory.
// Returns all *.md memory files under ~/.claude/projects/*/memory/.
//
// Security note (PRIV-002): authentication is enforced at the router level via
// RequireAuth (auth mode) or RequireSameOriginForMutations (bypass mode). In
// bypass mode this is a single-user local dashboard so no per-user scoping is
// needed. In auth mode, Claude project paths are opaque encoded CWDs — there is
// no stable mapping from a GitHub user ID to a project path — so filesystem-level
// per-user filtering is not feasible without a separate user→projects registry.
// Callers in auth mode see all projects on the host; this is an accepted limitation
// of the local-dashboard model documented in the architecture decision records.
func List(w http.ResponseWriter, r *http.Request) {
	projectsDir := filepath.Join(claudeRoot(), "projects")
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{}})
		return
	}

	type fileEntry struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	var files []fileEntry

	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		memDir := filepath.Join(projectsDir, p.Name(), "memory")
		entries, err := os.ReadDir(memDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			relPath := filepath.Join("projects", p.Name(), "memory", e.Name())
			files = append(files, fileEntry{
				Path: filepath.ToSlash(relPath),
				Name: p.Name() + "/" + e.Name(),
			})
		}
	}
	if files == nil {
		files = []fileEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
}

// Get handles GET /api/memory/*.
// Returns the content of the specified memory file.
// Auth is enforced at router level; per-user scoping limitation applies (see List).
func Get(w http.ResponseWriter, r *http.Request) {
	encoded := chi.URLParam(r, "*")
	safe := safePath(encoded)
	if safe == "" {
		http.Error(w, `{"error":"path traversal detected"}`, http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(safe)
	if err != nil {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
}

// Put handles PUT /api/memory/*.
// Writes content to the specified memory file.
// Auth is enforced at router level; per-user scoping limitation applies (see List).
func Put(w http.ResponseWriter, r *http.Request) {
	encoded := chi.URLParam(r, "*")
	safe := safePath(encoded)
	if safe == "" {
		http.Error(w, `{"error":"path traversal detected"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(safe, []byte(body.Content), 0o600); err != nil {
		http.Error(w, `{"error":"failed to write file"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
