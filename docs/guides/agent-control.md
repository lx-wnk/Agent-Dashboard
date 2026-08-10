# Controlling & Spawning Agents

## Controlling running agents

Agents spawned from the dashboard get the `dashboard-channel` MCP server automatically. When a channel is active, a green **CH** badge appears in the agent table. Open the agent modal to send follow-up messages, send `/btw` interrupts, and view replies.

For agents started **manually** outside the dashboard, inject the channel binary yourself:

```bash
claude --mcp-config '{"mcpServers":{"dashboard-channel":{"command":"/path/to/bin/dashboard-channel"}}}'
```

Use the built-in `agent-dashboard live` command, which loads the channel MCP automatically and selects the best transport (tmux if available, pty broker otherwise):

```bash
agent-dashboard live
agent-dashboard live --resume <session-id>
agent-dashboard live --yolo   # adds --dangerously-skip-permissions
```

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

Typing `/` in the prompt input opens a menu with two kinds of command.

**Dashboard commands** are executed by the dashboard itself against its own API — the agent never
sees them:

| Command | Arguments | Needs a linked task |
| --- | --- | --- |
| `/spawn` | `<slug> <description>` | no |
| `/grant` | `<toolName>` | yes |
| `/cancel` | — | yes |
| `/retry` | — | yes |
| `/promote` | — | yes |
| `/help` | — | no |

**Session commands** are everything the connected Claude session itself knows — its built-ins, your
`~/.claude/commands`, project commands, plugin commands, and every installed skill (each skill is
typeable as `/<name>`). They are discovered per session via `GET /api/slash-commands` and forwarded
to the agent verbatim, so what works is whatever that session supports.

Claude's own built-in commands are the one group the dashboard cannot discover — the CLI exposes no
machine-readable listing, so they are curated per version (`CuratedBuiltinsVersion`). When a session
reports a different version, the menu says so on every `/` query — including one that matches no
command at all, which is the case the note exists for: a command missing from the list may still
work if you type it in full. Re-curating means checking both directions — the CLI binary ships a "Recently
changed surfaces" document naming removed and renamed commands, while additions have to come from
the release notes.

Each entry shows its argument template next to the name, read from the command file's
`argument-hint:` frontmatter — `/branch-review` displays `[base-branch] [--apply-fixes]`, for
example. Commands without that key show no template; built-ins never carry one, since they have no
file on disk to read it from.

## Permissions

Stage agents run with an allow-list derived from `task_permissions` rows. Grants flow through a single validated path (`bulkGrantPermissions`) checked against an allow-list and a dangerous-bash block-list. Permission templates provide quick presets: `feature_implementation`, `research_only`, `test_only`, `review_only`.

Spawned agents request anything missing via the channel's `request_permission` MCP tool — prefer the bulk form so the user grants everything as one batch decision. The full self-service flow is documented in [`.agent-context/permissions.md`](../../.agent-context/permissions.md).
