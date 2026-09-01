package serverapp

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// buildObsidianClient returns nil, nil when Obsidian is unconfigured: the
// application is optional, and an absent vault must leave the rest of the
// server running. A partially configured vault is an error rather than a
// silent no-op — obsidian.NewClient already fails closed on each missing
// piece, and reporting that at boot is the only way the operator learns the
// integration is not running.
func buildObsidianClient(ctx context.Context, settingsSvc *settings.Service) (*obsidian.Client, error) {
	baseURL := settingsSvc.String("obsidian.baseURL")
	vaultRoot := settingsSvc.String("obsidian.vaultRoot")
	if baseURL == "" && vaultRoot == "" {
		return nil, nil
	}
	apiKey, err := settingsSvc.Secret(ctx, "obsidian.apiKey")
	if err != nil {
		return nil, fmt.Errorf("obsidian: read api key: %w", err)
	}
	client, err := obsidian.NewClient(obsidian.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		VaultRoot: vaultRoot,
		TLSMode:   settingsSvc.String("obsidian.tlsMode"),
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: %w", err)
	}
	return client, nil
}
