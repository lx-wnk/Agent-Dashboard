package main

import (
	"log/slog"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

func provideRouterConfig(cfg config.Config, oauthProvider authpkg.OAuthProvider, pluginLoginURL string) api.RouterConfig {
	bypassAuth := cfg.IsLoopback() && oauthProvider == nil && pluginLoginURL == ""
	if bypassAuth {
		slog.Info("auth bypass active — loopback + no auth_provider plugin configured; all API requests allowed without login")
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
