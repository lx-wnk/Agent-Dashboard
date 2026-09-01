package tools

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/obsidian"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// ObsidianDeps holds the dependencies required by the Obsidian vault MCP
// tools. Client enforces nothing itself (see its own doc comment) — Gate is
// what makes every handler below safe to call.
//
// Unlike the manual index trigger's Gate (internal/api/obsidian/handler.go,
// built with no Asker in serverapp/di.go because nobody is watching that
// run resolve a capability question), this Gate is built WITH an Asker in
// production: an MCP tool call has an agent genuinely waiting on the
// response, so an ask decision may legitimately hold for a human's answer
// instead of having to fail closed.
type ObsidianDeps struct {
	Client *obsidian.Client
	Gate   memory.Gate
}

// obsidianScope is the context every Authorize call below runs against. The
// vault is one machine-wide resource — obsidian.Register catalogues its
// application resource at repo.GlobalScope(), and there is no notion of a
// per-project vault — so there is no caller-supplied scope to parse, unlike
// the memory tools.
func obsidianScope() repo.Scope { return repo.GlobalScope() }

// RegisterObsidianTools registers the 4 Obsidian vault MCP tools into the
// given registry. When d.Client is nil — the vault is unconfigured — no
// tools are registered at all: an agent discovering a tool it can never use
// is worse than not discovering it, and the registry supports conditional
// registration trivially since this is just an ordinary function call.
func RegisterObsidianTools(registry mcp.ToolRegistry, d ObsidianDeps) {
	if d.Client == nil {
		return
	}
	registerObsidianRead(registry, d)
	registerObsidianSearch(registry, d)
	registerObsidianWrite(registry, d)
	registerObsidianDelete(registry, d)
}

func registerObsidianRead(registry mcp.ToolRegistry, d ObsidianDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "obsidian_read",
		Description: "Read the raw content of a note from the configured Obsidian vault.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Vault-relative note path"},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			notePath, err := mcp.StringArg(args, "path")
			if err != nil {
				return nil, err
			}
			// The note path is the capability value, so a grant can be
			// narrowed to a subtree by pattern instead of the vault as a whole.
			if err := d.Gate.Authorize(ctx, obsidian.CapabilityRead, notePath, obsidianScope()); err != nil {
				return nil, mcp.Fail("obsidian_read: " + err.Error())
			}
			content, err := d.Client.Read(ctx, notePath)
			if err != nil {
				return nil, mcp.Fail("obsidian_read: " + err.Error())
			}
			return mcp.OK(map[string]any{"path": notePath, "content": content})
		},
	})
}

func registerObsidianSearch(registry mcp.ToolRegistry, d ObsidianDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "obsidian_search",
		Description: "Search the whole Obsidian vault for notes matching a query.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Text to search for"},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			query, err := mcp.StringArg(args, "query")
			if err != nil {
				return nil, err
			}
			// A search fans out across the whole vault rather than naming
			// one target, so "" is capability.Match's documented wildcard —
			// the same reasoning memory_search applies to CapabilityMemoryRead.
			if err := d.Gate.Authorize(ctx, obsidian.CapabilitySearch, "", obsidianScope()); err != nil {
				return nil, mcp.Fail("obsidian_search: " + err.Error())
			}
			results, err := d.Client.Search(ctx, query)
			if err != nil {
				return nil, mcp.Fail("obsidian_search: " + err.Error())
			}
			return mcp.OK(map[string]any{"results": results})
		},
	})
}

func registerObsidianWrite(registry mcp.ToolRegistry, d ObsidianDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "obsidian_write",
		Description: "Create or overwrite a note in the configured Obsidian vault. Destructive: overwrites any existing content at the path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "Vault-relative note path"},
				"content": map[string]any{"type": "string", "description": "Full note content (Markdown)"},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			notePath, err := mcp.StringArg(args, "path")
			if err != nil {
				return nil, err
			}
			content, err := mcp.StringArg(args, "content")
			if err != nil {
				return nil, err
			}
			if err := d.Gate.Authorize(ctx, obsidian.CapabilityWrite, notePath, obsidianScope()); err != nil {
				return nil, mcp.Fail("obsidian_write: " + err.Error())
			}
			if err := d.Client.Write(ctx, notePath, content); err != nil {
				return nil, mcp.Fail("obsidian_write: " + err.Error())
			}
			return mcp.OK(map[string]any{"path": notePath})
		},
	})
}

func registerObsidianDelete(registry mcp.ToolRegistry, d ObsidianDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "obsidian_delete",
		Description: "Delete a note from the configured Obsidian vault. Irreversible.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Vault-relative note path"},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			notePath, err := mcp.StringArg(args, "path")
			if err != nil {
				return nil, err
			}
			if err := d.Gate.Authorize(ctx, obsidian.CapabilityDelete, notePath, obsidianScope()); err != nil {
				return nil, mcp.Fail("obsidian_delete: " + err.Error())
			}
			if err := d.Client.Delete(ctx, notePath); err != nil {
				return nil, mcp.Fail("obsidian_delete: " + err.Error())
			}
			return mcp.OK(map[string]any{"path": notePath, "deleted": true})
		},
	})
}
