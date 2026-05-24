package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrGenerateHooksSecret_ExistingValue_ReturnedAsIs(t *testing.T) {
	const existing = "already-set-secret"
	got, err := loadOrGenerateHooksSecret(existing)
	if err != nil {
		t.Fatalf("loadOrGenerateHooksSecret with existing secret: unexpected error: %v", err)
	}
	if got != existing {
		t.Errorf("loadOrGenerateHooksSecret: got %q, want %q", got, existing)
	}
}

func TestLoadOrGenerateHooksSecret_FirstBoot_WritesFileAndReturnsSecret(t *testing.T) {
	dir := t.TempDir()
	// Override the home dir so the secret file lands under the temp dir.
	t.Setenv("HOME", dir)

	secret, err := loadOrGenerateHooksSecret("")
	if err != nil {
		t.Fatalf("loadOrGenerateHooksSecret first boot: unexpected error: %v", err)
	}
	if len(secret) < 32 {
		t.Errorf("generated secret too short: %d chars, want >= 32", len(secret))
	}

	// File must exist.
	path := filepath.Join(dir, hooksSecretFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("secret file not created at %s: %v", path, err)
	}
	if strings.TrimSpace(string(data)) != secret {
		t.Errorf("file content %q does not match returned secret %q", strings.TrimSpace(string(data)), secret)
	}

	// File must have 0600 permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secret file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("secret file permissions: got %04o, want 0600", perm)
	}
}

func TestLoadOrGenerateHooksSecret_SubsequentBoot_LoadsFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// First boot — generates file.
	secretFirst, err := loadOrGenerateHooksSecret("")
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}

	// Second boot — must load the same secret from file.
	secretSecond, err := loadOrGenerateHooksSecret("")
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}
	if secretFirst != secretSecond {
		t.Errorf("second boot returned different secret: first=%q second=%q", secretFirst, secretSecond)
	}
}

func TestLoadOrGenerateHooksSecret_CorruptFile_Regenerates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Write a file with content too short to be a valid secret.
	path := filepath.Join(dir, hooksSecretFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	secret, err := loadOrGenerateHooksSecret("")
	if err != nil {
		t.Fatalf("corrupt file case: unexpected error: %v", err)
	}
	if len(secret) < 32 {
		t.Errorf("regenerated secret too short: %d chars", len(secret))
	}
}
