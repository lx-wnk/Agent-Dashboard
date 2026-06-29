// Package secretbox provides authenticated symmetric encryption (AES-256-GCM)
// for secret values stored at rest, plus master-key bootstrapping.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const secretKeyFileName = "dashboard-secret.key"

// Box encrypts/decrypts strings with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

// New builds a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns base64 ciphertext + base64 nonce.
func (b *Box) Encrypt(plaintext string) (string, string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	ct := b.aead.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(nonce), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(ciphertextB64, nonceB64 string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", err
	}
	pt, err := b.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: decrypt failed: %w", err)
	}
	return string(pt), nil
}

// LoadOrGenerateMasterKey resolves the 32-byte master key: from `existing`
// (DASHBOARD_SECRET_KEY hex) if set; else the persisted file; else generated +
// persisted to $CLAUDE_CONFIG_DIR/dashboard-secret.key (0600). Mirrors the
// hooks-secret bootstrap.
func LoadOrGenerateMasterKey(existing string) ([]byte, error) {
	if existing != "" {
		key, err := hex.DecodeString(strings.TrimSpace(existing))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("secretbox: DASHBOARD_SECRET_KEY must be 64 hex chars (32 bytes)")
		}
		return key, nil
	}

	baseDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Join(home, ".claude")
	}
	path := filepath.Join(baseDir, secretKeyFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			key, derr := hex.DecodeString(trimmed)
			if derr == nil && len(key) == 32 {
				return key, nil
			}
			// Non-empty but invalid — fail hard; overwriting would destroy all existing secrets.
			return nil, fmt.Errorf("secretbox: key file %s is non-empty but invalid; remove it manually to regenerate", path)
		}
		// empty file — fall through to generate
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secretbox: read key file %s: %w", path, err)
	} else if legacyKey, ok := readLegacyKey(baseDir, path); ok {
		// Primary path has no key but the legacy ~/.claude path does (key was
		// generated before CLAUDE_CONFIG_DIR was set). Use it so existing encrypted
		// settings stay readable; do not write it to the new path.
		return legacyKey, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	slog.Info("Generated plugin secret master key — set DASHBOARD_SECRET_KEY to use across machines", "path", path)
	return key, nil
}

// readLegacyKey returns the key persisted at the default ~/.claude path when the
// configured baseDir differs from it. Back-compat only: covers machines where
// CLAUDE_CONFIG_DIR is set but the key was originally generated under ~/.claude.
func readLegacyKey(baseDir, configuredPath string) ([]byte, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	legacyBase := filepath.Join(home, ".claude")
	if legacyBase == baseDir {
		return nil, false
	}
	legacyPath := filepath.Join(legacyBase, secretKeyFileName)
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		return nil, false
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, false
	}
	slog.Warn("plugin secret key loaded from legacy path; consider migrating it to CLAUDE_CONFIG_DIR",
		"legacy", legacyPath, "configured", configuredPath)
	return key, true
}
