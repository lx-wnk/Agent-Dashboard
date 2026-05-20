package main

import (
	"log/slog"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

func provideRouterConfig(cfg config.Config, oauthProvider authpkg.OAuthProvider, pluginLoginURL string) api.RouterConfig {
	bypassAuth := cfg.Auth == "none"
	if bypassAuth {
		slog.Info("auth bypass active — DASHBOARD_AUTH=none; all API requests allowed without login")
	} else if oauthProvider == nil && pluginLoginURL == "" {
		slog.Warn("DASHBOARD_AUTH=github but no auth provider configured — login will fail; configure DASHBOARD_PLUGIN_DIR with an auth plugin")
	}
	return api.RouterConfig{
		JWTSecret:         cfg.JWTSecret,
		CallbackURL:       cfg.CallbackURL(),
		IsLoopback:        cfg.IsLoopback(),
		BypassAuth:        bypassAuth,
		HooksSecret:       cfg.HooksSecret,
		HooksDebounceMs:   cfg.HooksDebounceMs,
		SpawnRateLimit:    cfg.SpawnRateLimit,
		SpawnRateWindowMs: cfg.SpawnRateWindowMs,
		AuthPluginSecret:  cfg.AuthPluginSecret,
		PluginLoginURL:    pluginLoginURL,
	}
}

func provideServer(cfg config.Config, handler http.Handler) *api.Server {
	return api.NewServer(cfg.Addr(), handler, cfg.ShutdownTimeout())
}
