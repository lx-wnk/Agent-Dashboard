package main

import (
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func provideDB(cfg config.Config) (*db.DBBundle, error) {
	return db.Open(cfg.DBPath)
}
