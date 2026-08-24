package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// permissionHookTimeoutSeconds is the per-hook timeout written into settings.
// It must exceed the server's own hold plus the script's curl budget, so the
// side that gives up first is ours. When it does expire, Claude Code falls back
// to asking in the terminal -- it neither allows nor denies on its own.
const permissionHookTimeoutSeconds = 30

// notificationHookTimeoutSeconds bounds the fire-and-forget Notification hook.
// It must stay at or above the script's own curl budget for that path.
const notificationHookTimeoutSeconds = 5

// permissionGatedTools is the PreToolUse matcher. Only tools Claude Code can
// actually prompt about are worth intercepting: the hook fires before the
// permission system runs, so a matcher of "*" makes every Read, Grep and
// TodoWrite call pay a round trip to the dashboard for a decision that was
// never going to be asked for.
const permissionGatedTools = "Bash|Write|Edit|MultiEdit|NotebookEdit|WebFetch"

// newHooksCmd builds `agent-dashboard hooks`.
//
// The permission bridge only reaches sessions whose settings name the hook, and
// settings are read at session start. This command writes that entry; sessions
// already running are unaffected and have to be restarted to pick it up.
func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install or remove the dashboard's Claude Code hooks",
	}
	cmd.AddCommand(newHooksInstallCmd(), newHooksUninstallCmd())
	return cmd
}

func newHooksInstallCmd() *cobra.Command {
	var settingsPath, scriptPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the permission bridge as a PreToolUse and Notification hook",
		Long: "Adds two hook entries to the Claude Code settings file so a permission\n" +
			"prompt can be answered in the dashboard instead of in the session's\n" +
			"terminal. Existing hooks are preserved; running it twice changes nothing.\n\n" +
			"Sessions read settings at start, so restart any session that should use it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveSettingsPath(settingsPath)
			if err != nil {
				return err
			}
			script, err := resolveHookScript(scriptPath)
			if err != nil {
				return err
			}

			settings, err := readSettings(path)
			if err != nil {
				return err
			}
			changed := applyPermissionHooks(settings, script)
			if !changed {
				fmt.Fprintf(cmd.OutOrStdout(), "already installed: %s\n", path)
				return nil
			}
			if dryRun {
				out, _ := json.MarshalIndent(settings, "", "  ")
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", out)
				return nil
			}
			if err := writeSettings(path, settings); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"installed the permission bridge in %s\nscript: %s\nRestart any running session to pick it up.\n",
				path, script)
			return nil
		},
	}
	cmd.Flags().StringVar(&settingsPath, "settings", "", "settings file to edit (default ~/.claude/settings.json)")
	cmd.Flags().StringVar(&scriptPath, "script", "", "path to dashboard-permission.sh (default: next to the binary, then the repo checkout)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the resulting settings instead of writing them")
	return cmd
}

func newHooksUninstallCmd() *cobra.Command {
	var settingsPath string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the dashboard's permission-bridge hooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveSettingsPath(settingsPath)
			if err != nil {
				return err
			}
			settings, err := readSettings(path)
			if err != nil {
				return err
			}
			if !removePermissionHooks(settings) {
				fmt.Fprintf(cmd.OutOrStdout(), "nothing to remove in %s\n", path)
				return nil
			}
			if err := writeSettings(path, settings); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed the permission bridge from %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&settingsPath, "settings", "", "settings file to edit (default ~/.claude/settings.json)")
	return cmd
}

func resolveSettingsPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	// CLAUDE_CONFIG_DIR moves the whole config tree, settings included, so it
	// wins over the default location.
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// resolveHookScript finds dashboard-permission.sh. It is looked up rather than
// embedded because Claude Code executes it as a command: it has to exist as a
// file on disk at a stable path for as long as the hook is registered.
func resolveHookScript(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("hook script not found at %s: %w", abs, err)
		}
		return abs, nil
	}

	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "hooks", "dashboard-permission.sh"),
			filepath.Join(dir, "..", "scripts", "hooks", "dashboard-permission.sh"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "scripts", "hooks", "dashboard-permission.sh"),
			filepath.Join(wd, "..", "scripts", "hooks", "dashboard-permission.sh"),
		)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", errors.New("could not find dashboard-permission.sh — pass --script with its path")
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		// Refuse rather than overwrite: this file holds the user's own
		// configuration and a parse failure is not a reason to discard it.
		return nil, fmt.Errorf("%s is not valid JSON — fix or move it first: %w", path, err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// hookMarker identifies the entries this command owns, so uninstall removes
// exactly them and install can recognise its own previous run.
const hookMarker = "dashboard-permission.sh"

// applyPermissionHooks adds the two entries idempotently and reports whether
// anything changed.
func applyPermissionHooks(settings map[string]any, script string) bool {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := addHookEntry(hooks, "PreToolUse", map[string]any{
		"matcher": permissionGatedTools,
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": script,
			"timeout": permissionHookTimeoutSeconds,
		}},
	})
	// Not folded into the condition above: both entries must be attempted, and
	// || would skip the second one whenever the first already reported a change.
	if addHookEntry(hooks, "Notification", map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": script + " notification",
			"timeout": notificationHookTimeoutSeconds,
		}},
	}) {
		changed = true
	}
	if changed {
		settings["hooks"] = hooks
	}
	return changed
}

func addHookEntry(hooks map[string]any, event string, entry map[string]any) bool {
	existing, _ := hooks[event].([]any)
	for _, e := range existing {
		if entryMentionsMarker(e) {
			return false
		}
	}
	hooks[event] = append(existing, entry)
	return true
}

func removePermissionHooks(settings map[string]any) bool {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	changed := false
	for _, event := range []string{"PreToolUse", "Notification"} {
		existing, _ := hooks[event].([]any)
		kept := make([]any, 0, len(existing))
		for _, e := range existing {
			if entryMentionsMarker(e) {
				changed = true
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return changed
}

func entryMentionsMarker(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	inner, _ := m["hooks"].([]any)
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); cmd != "" && filepath.Base(firstField(cmd)) == hookMarker {
			return true
		}
	}
	return false
}

// firstField returns the command word of a command line, so `…/x.sh notification`
// is recognised by the same marker as `…/x.sh`.
func firstField(s string) string {
	for i := range len(s) {
		if s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
