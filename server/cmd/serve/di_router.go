package main

import (
	"log/slog"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// resolveBypassAuth derives the auth-bypass decision from settings.
// A nil service (no DB) falls back to "none", which keeps bypass active.
func resolveBypassAuth(settingsSvc *settings.Service) bool {
	authMode := "none"
	if settingsSvc != nil {
		authMode = settingsSvc.String("auth.mode")
	}
	return authMode == "none"
}

func provideRouterConfig(cfg config.Config, settingsSvc *settings.Service, oauthProvider authpkg.OAuthProvider, pluginLoginURL string) api.RouterConfig {
	bypassAuth := resolveBypassAuth(settingsSvc)
	if bypassAuth {
		slog.Info("auth bypass active — auth.mode=none; all API requests allowed without login")
	} else if oauthProvider == nil && pluginLoginURL == "" {
		slog.Warn("auth.mode=plugin but no auth provider configured — login will fail; configure DASHBOARD_PLUGIN_DIR with an auth plugin")
	}
	return api.RouterConfig{
		JWTSecret:          cfg.JWTSecret,
		CallbackURL:        cfg.CallbackURL(),
		IsLoopback:         cfg.IsLoopback(),
		BypassAuth:         bypassAuth,
		HooksSecret:        cfg.HooksSecret,
		HooksDebounceMs:    cfg.HooksDebounceMs,
		SpawnRateLimit:     cfg.SpawnRateLimit,
		SpawnRateWindowMs:  cfg.SpawnRateWindowMs,
		InjectRateLimit:    cfg.InjectRateLimit,
		InjectRateWindowMs: cfg.InjectRateWindowMs,
		AuthPluginSecret:   cfg.AuthPluginSecret,
		PluginLoginURL:     pluginLoginURL,
	}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler, cfg.ShutdownTimeout())
}
