package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CLIConfig holds the CLI's persisted configuration.
type CLIConfig struct {
	Host  string `json:"host"`  // e.g. "http://127.0.0.1:13120"
	Token string `json:"token"` // Bearer token (MCP API key)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dashboard", "config.json"), nil
}

func loadConfig() (CLIConfig, error) {
	path, err := configPath()
	if err != nil {
		return CLIConfig{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return CLIConfig{Host: "http://127.0.0.1:13120"}, nil
	}
	if err != nil {
		return CLIConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg CLIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CLIConfig{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Host == "" {
		cfg.Host = "http://127.0.0.1:13120"
	}
	return cfg, nil
}

func saveConfig(cfg CLIConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
