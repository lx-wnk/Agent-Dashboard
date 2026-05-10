//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func initializeServer(cfg config.Config) (*api.Server, *sse.Broadcaster, error) {
	wire.Build(
		provideDB,
		provideGitHubClient,
		provideRouterConfig,
		provideRouterDeps,
		api.NewRouter,
		provideServer,
		sse.NewBroadcaster,
	)
	return nil, nil, nil
}
