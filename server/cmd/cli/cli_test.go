package main

import (
	"os"
	"testing"
)

func TestLoadConfig_DefaultsWhenMissing(t *testing.T) {
	// Point config to a non-existent path by overriding HOME/XDG_CONFIG_HOME.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "http://127.0.0.1:13120" {
		t.Errorf("expected default host, got %q", cfg.Host)
	}
}

func TestSaveAndLoadConfig_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	original := CLIConfig{Host: "http://custom:9999", Token: "tok_abc"}
	if err := saveConfig(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Host != original.Host {
		t.Errorf("host: got %q, want %q", loaded.Host, original.Host)
	}
	if loaded.Token != original.Token {
		t.Errorf("token: got %q, want %q", loaded.Token, original.Token)
	}
}

func TestLoadConfig_EmptyHostDefaulted(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Save a config with empty host.
	if err := saveConfig(CLIConfig{Host: "", Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "http://127.0.0.1:13120" {
		t.Errorf("empty host should be defaulted, got %q", cfg.Host)
	}
}

func TestHelpers(t *testing.T) {
	m := map[string]any{"name": "alice", "cost": 1.23}
	if strField(m, "name") != "alice" {
		t.Error("strField")
	}
	if floatField(m, "cost") != 1.23 {
		t.Error("floatField")
	}
	if strField(m, "missing") != "-" {
		t.Error("strField missing")
	}
}

func TestConfigPath_NotEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Error("configPath returned empty string")
	}
}

// Verify printError is declared (compile-time check only).
var _ = func() {
	if false {
		printError(os.ErrInvalid)
	}
}
