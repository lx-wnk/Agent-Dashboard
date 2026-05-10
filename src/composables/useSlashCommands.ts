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
}

export const SLASH_COMMAND_DEFS: SlashCommandDef[] = [
  { name: '/spawn', description: 'Neuen Pipeline-Task anlegen', usage: '<slug> <beschreibung>' },
  { name: '/grant', description: 'Offene Permission-Anfrage genehmigen', usage: '<toolName>', requiresTask: true },
  { name: '/cancel', description: 'Aktuellen Task abbrechen', requiresTask: true },
  { name: '/retry', description: 'Fehlgeschlagene Stage erneut starten', requiresTask: true },
  { name: '/promote', description: 'Task an nächste Stage weiterleiten (Approval-Gate überspringen)', requiresTask: true },
  { name: '/help', description: 'Alle verfügbaren Befehle anzeigen' },
]

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
        return { ok: false, message: 'Verwendung: /spawn <slug> <beschreibung>' }
      if (!SLUG_RE.test(slug))
        return { ok: false, message: `Ungültiger Slug "${slug}". Format: [a-z0-9][a-z0-9-]{0,63}` }
      if (!ctx.cwd)
        return { ok: false, message: '/spawn benötigt ein Arbeitsverzeichnis. Öffne einen Agenten mit bekanntem cwd.' }
      try {
        const res = await fetch('/api/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ slug, title: description, cwd: ctx.cwd }),
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Fehler ${res.status}` }
        }
        return { ok: true, message: `Task "${slug}" wurde angelegt.` }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Anlegen des Tasks.' }
      }
    }

    case '/grant': {
      if (!ctx.taskId)
        return { ok: false, message: '/grant benötigt einen verknüpften Pipeline-Task.' }
      const [toolName] = args
      if (!toolName)
        return { ok: false, message: 'Verwendung: /grant <toolName>' }
      try {
        const listRes = await fetch(`/api/tasks/${ctx.taskId}/permission-requests`)
        if (!listRes.ok)
          return { ok: false, message: `Konnte Permission-Requests nicht laden (${listRes.status}).` }
        const requests = await listRes.json() as Array<{ id: string, tool: string }>
        const match = requests.find(r => r.tool.toLowerCase() === toolName.toLowerCase())
        if (!match)
          return { ok: false, message: `Keine offene Permission-Anfrage für Tool "${toolName}" gefunden.` }
        const resolveRes = await fetch(`/api/permission-requests/${match.id}/resolve`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ outcome: 'granted' }),
        })
        if (!resolveRes.ok) {
          const data = await resolveRes.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Fehler ${resolveRes.status}` }
        }
        return { ok: true, message: `Permission für "${toolName}" gewährt.` }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Gewähren der Permission.' }
      }
    }

    case '/cancel': {
      if (!ctx.taskId)
        return { ok: false, message: '/cancel benötigt einen verknüpften Pipeline-Task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/cancel`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Fehler ${res.status}` }
        }
        return { ok: true, message: 'Task wurde abgebrochen.' }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Abbrechen des Tasks.' }
      }
    }

    case '/retry': {
      if (!ctx.taskId)
        return { ok: false, message: '/retry benötigt einen verknüpften Pipeline-Task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/retry`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Fehler ${res.status}` }
        }
        return { ok: true, message: 'Stage wird erneut gestartet.' }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Retry.' }
      }
    }

    case '/promote': {
      if (!ctx.taskId)
        return { ok: false, message: '/promote benötigt einen verknüpften Pipeline-Task.' }
      try {
        const res = await fetch(`/api/tasks/${ctx.taskId}/progress`, { method: 'POST' })
        if (!res.ok) {
          const data = await res.json().catch(() => ({})) as ApiError
          return { ok: false, message: data.error ?? `Fehler ${res.status}` }
        }
        return { ok: true, message: 'Task wurde zur nächsten Stage weitergeleitet.' }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Weiterleiten.' }
      }
    }

    default:
      return { ok: false, message: `Unbekannter Befehl "${cmd}". Tippe /help für eine Übersicht.` }
  }
}
