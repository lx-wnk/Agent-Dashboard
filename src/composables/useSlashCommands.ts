import { SLUG_RE } from '../utils/validation'

interface ApiError { error?: string }

export interface SlashCommandDef {
  name: string
  description: string
  usage?: string
  requiresTask?: boolean
}

export interface CommandResult {
  ok: boolean
  message: string
  loading?: boolean
}

export const SLASH_COMMAND_DEFS: SlashCommandDef[] = [
  { name: '/spawn', description: 'Create new pipeline task', usage: '<slug> <description>' },
  { name: '/grant', description: 'Grant open permission request', usage: '<toolName>', requiresTask: true },
  { name: '/cancel', description: 'Cancel current task', requiresTask: true },
  { name: '/retry', description: 'Restart failed stage', requiresTask: true },
  { name: '/promote', description: 'Advance task to next stage (skip approval gate)', requiresTask: true },
  { name: '/help', description: 'List all available commands' },
]

/**
 * Parse a slash command string into [command, args].
 * Supports basic quoted arguments, e.g.: /spawn my-slug "My Task Description"
 * Note: nested quotes are not supported.
 */
export function parseSlashCommand(raw: string): [string, string[]] | null {
  const trimmed = raw.trim()
  if (!trimmed.startsWith('/'))
    return null

  const tokens: string[] = []
  let current = ''
  let inQuote = false

  for (let i = 0; i < trimmed.length; i++) {
    const ch = trimmed[i]
    if (ch === '"') {
      inQuote = !inQuote
    }
    else if (ch === ' ' && !inQuote) {
      if (current.length > 0) {
        tokens.push(current)
        current = ''
      }
    }
    else {
      current += ch
    }
  }
  if (current.length > 0)
    tokens.push(current)
  if (tokens.length === 0)
    return null

  const [cmd, ...args] = tokens
  return [cmd.toLowerCase(), args]
}

export interface DispatchContext {
  taskId?: string
  cwd?: string
}

export async function dispatchSlashCommand(
  cmd: string,
  args: string[],
  ctx: DispatchContext,
): Promise<CommandResult> {
  switch (cmd) {
    case '/help':
      return {
        ok: true,
        message: SLASH_COMMAND_DEFS
          .map(d => `${d.name}${d.usage ? ` ${d.usage}` : ''} — ${d.description}`)
          .join('\n'),
      }

    case '/spawn': {
      const [slug, ...descParts] = args
      const description = descParts.join(' ')
      if (!slug || !description)
        return { ok: false, message: 'Usage: /spawn <slug> <description>' }
      if (!SLUG_RE.test(slug))
        return { ok: false, message: `Invalid slug "${slug}". Format: [a-z0-9][a-z0-9-]{0,63}` }
      if (!ctx.cwd)
        return { ok: false, message: '/spawn requires a working directory. Open an agent with a known cwd.' }
      try {
        const res = await fetch('/api/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ slug, title: description, cwd: ctx.cwd }),
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Error ${res.status}` }
        }
        return { ok: true, message: `Task "${slug}" created.` }
      }
      catch {
        return { ok: false, message: 'Network error creating task.' }
      }
    }

    case '/grant': {
      if (!ctx.taskId)
        return { ok: false, message: '/grant requires a linked pipeline task.' }
      const [toolName] = args
      if (!toolName)
        return { ok: false, message: 'Usage: /grant <toolName>' }
      try {
        // Step 1: load pending permission requests
        const listRes = await fetch(`/api/tasks/${ctx.taskId}/permission-requests`)
        if (!listRes.ok)
          return { ok: false, message: `Could not load permission requests (${listRes.status}).` }
        const requests = await listRes.json() as Array<{ id: string, tool: string }>
        const match = requests.find(r => r.tool.toLowerCase() === toolName.toLowerCase())
        if (!match)
          return { ok: false, message: `No open permission request found for tool "${toolName}".` }

        // Step 2: resolve the permission
        const resolveRes = await fetch(`/api/permission-requests/${match.id}/resolve`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ outcome: 'granted' }),
        })
        if (!resolveRes.ok) {
          const data = await resolveRes.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Error ${resolveRes.status}` }
        }
        return { ok: true, message: `Permission granted for "${toolName}".` }
      }
      catch {
        return { ok: false, message: 'Network error granting permission.' }
      }
    }

    case '/cancel': {
      if (!ctx.taskId)
        return { ok: false, message: '/cancel requires a linked pipeline task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/cancel`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Error ${res.status}` }
        }
        return { ok: true, message: 'Task cancelled.' }
      }
      catch {
        return { ok: false, message: 'Network error cancelling task.' }
      }
    }

    case '/retry': {
      if (!ctx.taskId)
        return { ok: false, message: '/retry requires a linked pipeline task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/retry`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Error ${res.status}` }
        }
        return { ok: true, message: 'Stage restarted.' }
      }
      catch {
        return { ok: false, message: 'Network error retrying stage.' }
      }
    }

    case '/promote': {
      if (!ctx.taskId)
        return { ok: false, message: '/promote requires a linked pipeline task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/progress`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Error ${res.status}` }
        }
        return { ok: true, message: 'Task promoted to next stage.' }
      }
      catch {
        return { ok: false, message: 'Network error promoting task.' }
      }
    }

    default:
      return { ok: false, message: `Unknown command "${cmd}". Type /help for an overview.` }
  }
}

interface DynamicCommand {
  name: string
  description: string
  source: string
}

interface DynamicCommandsResponse {
  commands: DynamicCommand[]
  engineVersion?: string
  builtinsMayBeStale?: boolean
  scopeSource?: string
  scopeLabel?: string
}

/**
 * Scope for resolving a session's slash commands. Prefer sessionId for a live
 * agent (resolves that session's actual CLAUDE_CONFIG_DIR); spawnerId previews
 * a spawner's set; cwd adds project-local <cwd>/.claude/commands.
 */
export interface DynamicCommandScope {
  sessionId?: string
  spawnerId?: string
  cwd?: string
}

const dynamicCommandCache = new Map<string, SlashCommandDef[]>()

function scopeKey(scope: DynamicCommandScope): string {
  return scope.sessionId ?? scope.spawnerId ?? scope.cwd ?? 'default'
}

export async function fetchDynamicCommands(scope: DynamicCommandScope): Promise<SlashCommandDef[]> {
  const key = scopeKey(scope)
  if (dynamicCommandCache.has(key))
    return dynamicCommandCache.get(key)!

  const params = new URLSearchParams()
  if (scope.sessionId)
    params.set('sessionId', scope.sessionId)
  if (scope.spawnerId)
    params.set('spawnerId', scope.spawnerId)
  if (scope.cwd)
    params.set('cwd', scope.cwd)

  try {
    const res = await fetch(`/api/slash-commands?${params.toString()}`)
    if (!res.ok)
      return []
    const data = await res.json() as DynamicCommandsResponse
    const cmds: SlashCommandDef[] = (data.commands ?? []).map(c => ({
      name: c.name,
      description: c.description,
    }))
    dynamicCommandCache.set(key, cmds)
    return cmds
  }
  catch {
    return []
  }
}
