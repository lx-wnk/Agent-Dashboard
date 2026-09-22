package obsidian

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	obsidianapp "github.com/lx-wnk/kontor/server/internal/apps/obsidian"
)

const graphTTL = 60 * time.Second

// graphCache holds the last vault graph for graphTTL; concurrent misses share one rebuild.
type graphCache struct {
	now   func() time.Time
	group singleflight.Group
	mu    sync.Mutex
	at    time.Time
	graph *obsidianapp.Graph
}

func (c *graphCache) get(ctx context.Context, build func(context.Context) (obsidianapp.Graph, error)) (obsidianapp.Graph, error) {
	c.mu.Lock()
	if c.graph != nil && c.now().Sub(c.at) < graphTTL {
		g := *c.graph
		c.mu.Unlock()
		return g, nil
	}
	c.mu.Unlock()
	v, err, _ := c.group.Do("graph", func() (any, error) {
		// Shared by every waiter, so one caller hanging up must not cancel it.
		g, err := build(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.graph, c.at = &g, c.now()
		c.mu.Unlock()
		return g, nil
	})
	if err != nil {
		return obsidianapp.Graph{}, err
	}
	return v.(obsidianapp.Graph), nil
}
