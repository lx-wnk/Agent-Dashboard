package services

import (
	"os"
	"path/filepath"
	"testing"
)

// setAllowedCommands sets the spawner extra allow-list for the duration of the
// test, resetting it afterwards.
func setAllowedCommands(t *testing.T, cmds ...string) {
	t.Helper()
	SetSpawnerAllowedCommands(cmds)
	t.Cleanup(func() { SetSpawnerAllowedCommands(nil) })
}

// writeExecutable creates an empty file at path with 0o755 and returns path.
func writeExecutable(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestValidateSpawnerCommand_BareAllowed(t *testing.T) {
	for _, name := range []string{"claude", "claude-code", "npx"} {
		ok, reason := ValidateSpawnerCommand(name)
		if !ok {
			t.Errorf("expected %q allowed, got reason %q", name, reason)
		}
	}
}

func TestValidateSpawnerCommand_BareDenied(t *testing.T) {
	ok, reason := ValidateSpawnerCommand("rm")
	if ok {
		t.Fatalf("expected %q denied", "rm")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for denied bare command")
	}
}

func TestValidateSpawnerCommand_Empty(t *testing.T) {
	ok, reason := ValidateSpawnerCommand("")
	if ok || reason == "" {
		t.Fatalf("expected empty command denied with reason, got ok=%v reason=%q", ok, reason)
	}
}

func TestValidateSpawnerCommand_EnvExtraBareAllowed(t *testing.T) {
	setAllowedCommands(t, "mytool", "othertool")
	if ok, reason := ValidateSpawnerCommand("mytool"); !ok {
		t.Errorf("expected env-extra bare name allowed, got reason %q", reason)
	}
	if ok, _ := ValidateSpawnerCommand("nottool"); ok {
		t.Error("expected non-listed bare name denied")
	}
}

func TestValidateSpawnerCommand_AbsUnderTrustedAllowed(t *testing.T) {
	// /bin/sh exists on both macOS and Linux; /bin is a trusted bin dir.
	if ok, reason := ValidateSpawnerCommand("/bin/sh"); !ok {
		t.Errorf("expected /bin/sh allowed, got reason %q", reason)
	}
}

func TestValidateSpawnerCommand_AbsUnderTempDenied(t *testing.T) {
	bin := writeExecutable(t, filepath.Join(t.TempDir(), "bin", "evil"))
	ok, reason := ValidateSpawnerCommand(bin)
	if ok {
		t.Fatalf("expected abs path under temp dir denied: %s", bin)
	}
	if reason == "" {
		t.Fatal("expected reason for untrusted abs path")
	}
}

func TestValidateSpawnerCommand_SymlinkIntoTempDenied(t *testing.T) {
	dir := t.TempDir()
	real := writeExecutable(t, filepath.Join(dir, "real"))
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Symlink resolves into an untrusted dir → must be denied after EvalSymlinks.
	if ok, _ := ValidateSpawnerCommand(link); ok {
		t.Fatal("expected symlink resolving into untrusted dir to be denied")
	}
}

func TestValidateSpawnerCommand_EnvTrustedDirAllowed(t *testing.T) {
	dir := t.TempDir()
	bin := writeExecutable(t, filepath.Join(dir, "claude"))
	setAllowedCommands(t, dir)
	if ok, reason := ValidateSpawnerCommand(bin); !ok {
		t.Errorf("expected command under env-trusted dir allowed, got reason %q", reason)
	}
}

func TestValidateSpawnerCommand_UnresolvableDenied(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "claude")
	ok, reason := ValidateSpawnerCommand(missing)
	if ok {
		t.Fatalf("expected unresolvable abs path denied: %s", missing)
	}
	if reason == "" {
		t.Fatal("expected reason for unresolvable abs path")
	}
}
