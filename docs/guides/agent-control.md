# Controlling & Spawning Agents

## Controlling running agents

Agents spawned from the dashboard get the `dashboard-channel` MCP server automatically. When a channel is active, a green **CH** badge appears in the agent table. Open the agent modal to send follow-up messages, send `/btw` interrupts, and view replies.

For agents started **manually** outside the dashboard, inject the channel binary yourself:

```bash
claude --mcp-config '{"mcpServers":{"dashboard-channel":{"command":"/path/to/bin/dashboard-channel"}}}'
```

A convenience wrapper is shipped at [`scripts/claude-with-channel.sh`](../../scripts/claude-with-channel.sh).

## Spawning new agents

Click **"+ New Agent"** in the header to open the spawn dialog.

| Field | Required | Description |
|---|---|---|
| Prompt | Yes | What the agent should do |
| Working Directory | Yes | Project path the agent runs in |
| Model | No | e.g. `claude-opus-4-6`, `claude-sonnet-4-6`, `claude-haiku-4-5` |
| System Prompt | No | Custom system instructions |
| Enable Channel | No | Dashboard control channel (default: on) |

Spawned agents run **detached** — they survive dashboard restarts and appear in the table within ~3 seconds.

## Slash commands

`/spawn`, `/grant`, `/cancel`, `/status`, `/session` are available from the command palette.

## Permissions

Stage agents run with an allow-list derived from `task_permissions` rows. Grants flow through a single validated path (`bulkGrantPermissions`) checked against an allow-list and a dangerous-bash block-list. Permission templates provide quick presets: `feature_implementation`, `research_only`, `test_only`, `review_only`.

Spawned agents request anything missing via the channel's `request_permission` MCP tool — prefer the bulk form so the user grants everything as one batch decision. The full self-service flow is documented in [`.agent-context/permissions.md`](../../.agent-context/permissions.md).
