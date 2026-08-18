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

// TestValidateSpawnerCommand_SymlinkFromTrustedDirAllowed covers a package
// manager's own symlink indirection (Homebrew's npx -> .../npm/bin/npx-cli.js):
// the named path's parent is trusted even though the resolved target's parent
// is not, and the trusted party controls both ends.
func TestValidateSpawnerCommand_SymlinkFromTrustedDirAllowed(t *testing.T) {
	// t.TempDir, not the shared os.TempDir: this test creates a symlink under
	// base, so a shared path makes the second run fail with "file exists".
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
	}
	trustedDir := filepath.Join(base, "trusted-bin")
	untrustedDir := filepath.Join(base, "untrusted-target")
	setAllowedCommands(t, trustedDir)

	target := writeExecutable(t, filepath.Join(untrustedDir, "real"))
	if err := os.MkdirAll(trustedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(trustedDir, "npx")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if ok, reason := ValidateSpawnerCommand(link); !ok {
		t.Errorf("expected symlink named under a trusted dir allowed regardless of resolved target, got reason %q", reason)
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

// TestValidateSpawnerCommand_OtherWritableDirDenied covers a world-writable
// trusted dir (e.g. a misconfigured /usr/local/bin): any local user could
// unlink and replant the binary there, so the dir must not grant trust even
// though it is on the configured allow-list.
func TestValidateSpawnerCommand_OtherWritableDirDenied(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	bin := writeExecutable(t, filepath.Join(dir, "claude"))
	setAllowedCommands(t, dir)

	ok, reason := ValidateSpawnerCommand(bin)
	if ok {
		t.Fatal("expected command under an other-writable dir denied")
	}
	if reason == "" {
		t.Fatal("expected reason for other-writable dir")
	}
}

// TestValidateSpawnerCommand_OtherWritableFileDenied covers a tight (0o700)
// dir holding a file with a loose mode: the dir alone is not enough, the
// resolved file's own mode must also be checked.
func TestValidateSpawnerCommand_OtherWritableFileDenied(t *testing.T) {
	dir := t.TempDir()
	bin := writeExecutable(t, filepath.Join(dir, "claude"))
	// os.WriteFile's perm is masked by umask, so the other-write bit needs an
	// explicit chmod (chmod bypasses umask).
	if err := os.Chmod(bin, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	setAllowedCommands(t, dir)

	ok, reason := ValidateSpawnerCommand(bin)
	if ok {
		t.Fatal("expected other-writable file denied even in a tight dir")
	}
	if reason == "" {
		t.Fatal("expected reason for other-writable file")
	}
}

// TestValidateSpawnerCommand_GroupWritableDirAllowed pins the boundary: a
// group-writable (but not other-writable) trusted dir -- e.g. Homebrew's
// /opt/homebrew/bin -- must stay accepted. A group-writable rule would
// re-break Homebrew acceptance.
func TestValidateSpawnerCommand_GroupWritableDirAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	bin := writeExecutable(t, filepath.Join(dir, "claude"))
	setAllowedCommands(t, dir)

	if ok, reason := ValidateSpawnerCommand(bin); !ok {
		t.Errorf("expected group-writable-but-not-other-writable dir allowed, got reason %q", reason)
	}
}

// TestValidateSpawnerCommand_Mode0755DirAllowed pins the ordinary case: a
// normal, non-writable-by-others trusted dir keeps working.
func TestValidateSpawnerCommand_Mode0755DirAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	bin := writeExecutable(t, filepath.Join(dir, "claude"))
	setAllowedCommands(t, dir)

	if ok, reason := ValidateSpawnerCommand(bin); !ok {
		t.Errorf("expected normal 0755 dir allowed, got reason %q", reason)
	}
}
