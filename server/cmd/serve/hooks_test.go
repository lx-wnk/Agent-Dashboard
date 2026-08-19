package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestApplyPermissionHooksIsIdempotent(t *testing.T) {
	settings := map[string]any{}

	if !applyPermissionHooks(settings, "/opt/dash/dashboard-permission.sh") {
		t.Fatal("first install reported no change")
	}
	if applyPermissionHooks(settings, "/opt/dash/dashboard-permission.sh") {
		t.Fatal("second install changed the settings again")
	}

	hooks := settings["hooks"].(map[string]any)
	for _, event := range []string{"PreToolUse", "Notification"} {
		entries, _ := hooks[event].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s has %d entries, want exactly 1", event, len(entries))
		}
	}
}

// The settings file is the user's own; an install must never drop what is
// already in it.
func TestApplyPermissionHooksKeepsForeignHooks(t *testing.T) {
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Bash",
				"hooks":   []any{map[string]any{"type": "command", "command": "/usr/local/bin/my-own-guard"}},
			}},
		},
		"permissions": map[string]any{"defaultMode": "auto"},
	}

	applyPermissionHooks(settings, "/opt/dash/dashboard-permission.sh")

	hooks := settings["hooks"].(map[string]any)
	entries := hooks["PreToolUse"].([]any)
	if len(entries) != 2 {
		t.Fatalf("PreToolUse has %d entries, want the existing one plus ours", len(entries))
	}
	if settings["permissions"] == nil {
		t.Fatal("an unrelated settings key was dropped")
	}
}

func TestRemovePermissionHooksLeavesForeignHooks(t *testing.T) {
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Bash",
				"hooks":   []any{map[string]any{"type": "command", "command": "/usr/local/bin/my-own-guard"}},
			}},
		},
	}
	applyPermissionHooks(settings, "/opt/dash/dashboard-permission.sh")

	if !removePermissionHooks(settings) {
		t.Fatal("uninstall reported no change")
	}
	hooks := settings["hooks"].(map[string]any)
	entries := hooks["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("PreToolUse has %d entries, want only the foreign one", len(entries))
	}
	if _, ok := hooks["Notification"]; ok {
		t.Fatal("an emptied event key was left behind")
	}
}

func TestRemovePermissionHooksOnCleanSettings(t *testing.T) {
	settings := map[string]any{}
	if removePermissionHooks(settings) {
		t.Fatal("uninstall reported a change with nothing installed")
	}
}

// A settings file that does not parse must abort the command: overwriting it
// would discard configuration the user wrote by hand.
func TestReadSettingsRefusesInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readSettings(path); err == nil {
		t.Fatal("readSettings accepted a malformed file")
	}
}

func TestReadSettingsTreatsAMissingFileAsEmpty(t *testing.T) {
	settings, err := readSettings(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("readSettings: %v", err)
	}
	if len(settings) != 0 {
		t.Fatalf("settings = %v, want empty", settings)
	}
}

func TestWriteSettingsIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeSettings(path, map[string]any{"a": 1}); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600 — the file sits next to the hooks secret", perm)
	}
}

// CLAUDE_CONFIG_DIR relocates the whole config tree, so it has to win over the
// default path or the hook lands in a settings file nothing reads.
func TestResolveSettingsPathHonoursConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	got, err := resolveSettingsPath("")
	if err != nil {
		t.Fatalf("resolveSettingsPath: %v", err)
	}
	if want := filepath.Join(dir, "settings.json"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveSettingsPathPrefersAnExplicitOverride(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if got, _ := resolveSettingsPath("/tmp/explicit.json"); got != "/tmp/explicit.json" {
		t.Fatalf("path = %q, want the override", got)
	}
}

// Round-trip through the real file so the two commands agree on the format.
func TestInstallThenUninstallRestoresTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := map[string]any{"permissions": map[string]any{"defaultMode": "auto"}}
	writeJSON(t, path, original)

	settings, err := readSettings(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	applyPermissionHooks(settings, "/opt/dash/dashboard-permission.sh")
	if err := writeSettings(path, settings); err != nil {
		t.Fatalf("write: %v", err)
	}

	settings, err = readSettings(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	removePermissionHooks(settings)

	if _, ok := settings["hooks"]; ok {
		t.Fatalf("hooks key survived the uninstall: %v", settings["hooks"])
	}
	perms, _ := settings["permissions"].(map[string]any)
	if perms["defaultMode"] != "auto" {
		t.Fatalf("settings = %v, want the original content back", settings)
	}
}
