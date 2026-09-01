package serverapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// buildObsidianClient returns nil, nil when Obsidian is unconfigured: the
// application is optional, and an absent vault must leave the rest of the
// server running. baseURL, vaultRoot and apiKey are a required trio — if any
// one of them is set, all three must be, because a client built from a
// partial trio would look configured and then fail every request (a 401
// pointing at nothing) instead of refusing to boot. tlsMode is not part of
// the trio: it always has a registry default. obsidian.NewClient validates
// baseURL, vaultRoot and tlsMode, but never apiKey — an empty bearer token
// is valid HTTP — so the trio check below is this function's job, not
// NewClient's.
func buildObsidianClient(ctx context.Context, settingsSvc *settings.Service) (*obsidian.Client, error) {
	baseURL := settingsSvc.String("obsidian.baseURL")
	vaultRoot := settingsSvc.String("obsidian.vaultRoot")
	apiKey, err := settingsSvc.Secret(ctx, "obsidian.apiKey")
	if err != nil {
		return nil, fmt.Errorf("read obsidian.apiKey: %w", err)
	}

	if baseURL == "" && vaultRoot == "" && apiKey == "" {
		slog.Info("obsidian: vault not configured, integration disabled")
		return nil, nil
	}

	var missing []string
	if baseURL == "" {
		missing = append(missing, "obsidian.baseURL")
	}
	if vaultRoot == "" {
		missing = append(missing, "obsidian.vaultRoot")
	}
	if apiKey == "" {
		missing = append(missing, "obsidian.apiKey")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required settings: %s", strings.Join(missing, ", "))
	}

	client, err := obsidian.NewClient(obsidian.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		VaultRoot: vaultRoot,
		TLSMode:   settingsSvc.String("obsidian.tlsMode"),
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}
