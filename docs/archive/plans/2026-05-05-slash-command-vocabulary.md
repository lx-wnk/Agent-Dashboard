# Slash-Command-Vocabulary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Erweiterung des Chat/Prompt-Eingabefelds um ein strukturiertes Slash-Command-Vokabular. Befehle werden im Frontend geparst und direkt als REST-API-Aufrufe abgewickelt — nur reiner Freitext wird an den Agenten weitergeleitet. Eine Autocomplete-Palette erscheint beim Tippen von `/`.

**Architecture:** Drei klar getrennte Schichten:
1. `src/composables/useSlashCommands.ts` — Command-Parser und REST-Dispatcher (neue Datei)
2. `src/composables/useAgentPrompt.ts` — Intercept-Hook vor dem Absenden
3. `src/components/PromptInput.vue` — Autocomplete-Palette (erweitert die bereits vorhandene `SLASH_COMMANDS`-Liste)

Kein Channel-Bridge-Support (spawned agents sehen keine Slash-Commands) — die Befehle sind Dashboard-Actions, kein Agent-Protokoll.

**Tech Stack:** Vue 3, TypeScript, Vitest, Express 5 (REST-Endpunkte bereits vorhanden)

**Argument-Parsing-Regel:** Quoted strings (`"foo bar"`) werden als ein Argument behandelt. Unbekannte Commands geben eine inline-Fehlermeldung zurück ohne den Text an den Agenten zu senden.

---

## Parallelisierungs-Map

```
Task 1 — src/composables/useSlashCommands.ts        ─┐
         (Parser, Dispatcher, Tests)                  │  Task 1 zuerst
Task 2 — src/composables/useAgentPrompt.ts           ─┤  Task 2 + 3 nach Task 1
Task 3 — src/components/PromptInput.vue              ─┘
```

---

## Task 1: Slash-Command-Composable erstellen

**Files:**
- Create: `src/composables/useSlashCommands.ts`
- Create: `src/composables/useSlashCommands.test.ts`

> **Why:** Die gesamte Command-Logik lebt in einer eigenen, testbaren Einheit. `useAgentPrompt` und `PromptInput` bleiben dünn. Der Parser kennt keine Vue-Reaktivität.

### Schritt 1: Command-Definitionen und Typen

Erstelle `src/composables/useSlashCommands.ts` mit folgendem Inhalt:

```typescript
/**
 * Slash-Command-Vokabular für das Dashboard-Prompt-Eingabefeld.
 *
 * Befehle werden LOKAL abgefangen und per REST-API abgewickelt.
 * Kein Freitext wird an den Agenten gesendet, wenn ein Befehl erkannt wurde.
 */

export interface SlashCommandDef {
  name: string
  description: string
  /** Kurze Argument-Syntax für die Palette, z.B. "<slug> <beschreibung>" */
  usage?: string
  /** Benötigt eine verknüpfte Pipeline-Task-ID (agent.pipelineTaskId) */
  requiresTask?: boolean
}

export interface CommandResult {
  ok: boolean
  message: string
}

/** Vollständige Command-Liste (wird auch in PromptInput für die Palette genutzt) */
export const SLASH_COMMAND_DEFS: SlashCommandDef[] = [
  {
    name: '/spawn',
    description: 'Neuen Pipeline-Task anlegen',
    usage: '<slug> <beschreibung>',
  },
  {
    name: '/grant',
    description: 'Offene Permission-Anfrage genehmigen',
    usage: '<toolName>',
    requiresTask: true,
  },
  {
    name: '/cancel',
    description: 'Aktuellen Task abbrechen',
    requiresTask: true,
  },
  {
    name: '/retry',
    description: 'Fehlgeschlagene Stage erneut starten',
    requiresTask: true,
  },
  {
    name: '/promote',
    description: 'Task an nächste Stage weiterleiten (Approval-Gate überspringen)',
    requiresTask: true,
  },
  {
    name: '/help',
    description: 'Alle verfügbaren Befehle anzeigen',
  },
]

// ─── Argument-Parser ────────────────────────────────────────────────────────

/**
 * Parst einen Rohtext in [commandName, ...args].
 * Quoted strings (`"foo bar"`) zählen als ein Argument.
 * Gibt null zurück, wenn der Text nicht mit `/` beginnt.
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

// ─── Command-Dispatcher ─────────────────────────────────────────────────────

export interface DispatchContext {
  /** pipelineTaskId des verknüpften Agenten, falls vorhanden */
  taskId?: string
  /** Arbeitsverzeichnis des Agenten (für /spawn als cwd genutzt) */
  cwd?: string
}

/**
 * Führt einen erkannten Slash-Command aus.
 * Gibt { ok, message } zurück. Wirft niemals.
 */
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
      if (!slug || !description) {
        return { ok: false, message: 'Verwendung: /spawn <slug> <beschreibung>' }
      }
      // Slug-Validierung (muss dem Backend-Pattern entsprechen: kebab-case, a-z0-9-)
      if (!/^[a-z][a-z0-9-]*$/.test(slug)) {
        return { ok: false, message: `Ungültiger Slug "${slug}". Nur Kleinbuchstaben, Zahlen und Bindestriche erlaubt (muss mit Buchstabe beginnen).` }
      }
      try {
        const res = await fetch('/api/tasks', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ slug, title: description, cwd: ctx.cwd ?? '.' }),
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          return { ok: false, message: (data as any).error ?? `Fehler ${res.status}` }
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
        // Offene Permission-Requests des Tasks laden
        const listRes = await fetch(`/api/tasks/${ctx.taskId}/permission-requests`)
        if (!listRes.ok)
          return { ok: false, message: `Konnte Permission-Requests nicht laden (${listRes.status}).` }
        const listData = await listRes.json()
        const requests: Array<{ id: string, tool: string }> = listData.requests ?? listData ?? []
        const match = requests.find(r => r.tool.toLowerCase() === toolName.toLowerCase())
        if (!match)
          return { ok: false, message: `Keine offene Permission-Anfrage für Tool "${toolName}" gefunden.` }
        const resolveRes = await fetch(`/api/permission-requests/${match.id}/resolve`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ outcome: 'granted' }),
        })
        if (!resolveRes.ok) {
          const data = await resolveRes.json().catch(() => ({}))
          return { ok: false, message: (data as any).error ?? `Fehler ${resolveRes.status}` }
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
          const data = await res.json().catch(() => ({}))
          return { ok: false, message: (data as any).error ?? `Fehler ${res.status}` }
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
          const data = await res.json().catch(() => ({}))
          return { ok: false, message: (data as any).error ?? `Fehler ${res.status}` }
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
          const data = await res.json().catch(() => ({}))
          return { ok: false, message: (data as any).error ?? `Fehler ${res.status}` }
        }
        return { ok: true, message: 'Task wurde zur nächsten Stage weitergeleitet.' }
      }
      catch {
        return { ok: false, message: 'Netzwerkfehler beim Weiterleiten.' }
      }
    }

    default:
      return {
        ok: false,
        message: `Unbekannter Befehl "${cmd}". Tippe /help für eine Übersicht.`,
      }
  }
}
```

- [ ] **Schritt 1 abgeschlossen** — Datei erstellt, TypeScript-Fehler geprüft: `pnpm typecheck`

### Schritt 2: Unit-Tests schreiben

Erstelle `src/composables/useSlashCommands.test.ts`:

```typescript
import { describe, expect, it, vi } from 'vitest'
import { dispatchSlashCommand, parseSlashCommand } from './useSlashCommands'

// ─── Parser ────────────────────────────────────────────────────────────────

describe('parseSlashCommand', () => {
  it('returns null for non-slash input', () => {
    expect(parseSlashCommand('hello world')).toBeNull()
    expect(parseSlashCommand('')).toBeNull()
  })

  it('parses a simple command without args', () => {
    expect(parseSlashCommand('/help')).toEqual(['/help', []])
  })

  it('parses a command with plain args', () => {
    expect(parseSlashCommand('/spawn my-slug My Task Title')).toEqual([
      '/spawn',
      ['my-slug', 'My', 'Task', 'Title'],
    ])
  })

  it('parses quoted args as a single token', () => {
    expect(parseSlashCommand('/spawn my-slug "My Task Title"')).toEqual([
      '/spawn',
      ['my-slug', 'My Task Title'],
    ])
  })

  it('normalizes command to lowercase', () => {
    expect(parseSlashCommand('/HELP')).toEqual(['/help', []])
  })

  it('handles extra whitespace between tokens', () => {
    const result = parseSlashCommand('/spawn  slug  desc')
    expect(result).toEqual(['/spawn', ['slug', 'desc']])
  })
})

// ─── Dispatcher ────────────────────────────────────────────────────────────

describe('dispatchSlashCommand', () => {
  it('/help lists all commands', async () => {
    const result = await dispatchSlashCommand('/help', [], {})
    expect(result.ok).toBe(true)
    expect(result.message).toContain('/spawn')
    expect(result.message).toContain('/grant')
    expect(result.message).toContain('/cancel')
  })

  it('unknown command returns ok:false', async () => {
    const result = await dispatchSlashCommand('/unknown', [], {})
    expect(result.ok).toBe(false)
    expect(result.message).toContain('/help')
  })

  it('/spawn validates slug format', async () => {
    const result = await dispatchSlashCommand('/spawn', ['Bad Slug!', 'desc'], {})
    expect(result.ok).toBe(false)
  })

  it('/spawn validates missing description', async () => {
    const result = await dispatchSlashCommand('/spawn', ['my-slug'], {})
    expect(result.ok).toBe(false)
  })

  it('/spawn calls POST /api/tasks on success', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: '1' }) })
    vi.stubGlobal('fetch', mockFetch)

    const result = await dispatchSlashCommand('/spawn', ['my-slug', 'My Title'], { cwd: '/repo' })
    expect(result.ok).toBe(true)
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/tasks',
      expect.objectContaining({ method: 'POST' }),
    )
    vi.unstubAllGlobals()
  })

  it('/grant requires taskId', async () => {
    const result = await dispatchSlashCommand('/grant', ['Bash'], {})
    expect(result.ok).toBe(false)
    expect(result.message).toContain('Pipeline-Task')
  })

  it('/cancel calls DELETE endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)

    await dispatchSlashCommand('/cancel', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/cancel', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/retry calls retry endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)

    await dispatchSlashCommand('/retry', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/retry', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/promote calls progress endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)

    await dispatchSlashCommand('/promote', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/progress', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })
})
```

- [ ] **Schritt 2 abgeschlossen** — Tests laufen grün: `pnpm test src/composables/useSlashCommands.test.ts`

---

## Task 2: useAgentPrompt.ts — Slash-Command-Intercept einbauen

**Files:**
- Modify: `src/composables/useAgentPrompt.ts`

> **Why:** Vor dem eigentlichen Absenden prüft `handleSend`, ob die Eingabe ein Slash-Command ist. Wenn ja: dispatchen, Status setzen, Input leeren — fertig. Kein Text geht an den Agenten.

- [ ] **Schritt 1: Import und Signatur erweitern**

Am Anfang von `src/composables/useAgentPrompt.ts` folgende Imports ergänzen:

```typescript
import { dispatchSlashCommand, parseSlashCommand } from './useSlashCommands'
```

Die Funktion `useAgentPrompt` erhält einen optionalen zweiten Parameter `ctx`:

```typescript
export interface AgentPromptContext {
  taskId?: string
  cwd?: string
}

export function useAgentPrompt(
  getAgent: () => Agent | null,
  onMessageSent?: OnMessageSent,
  ctx?: AgentPromptContext,
) {
```

- [ ] **Schritt 2: Intercept-Block in handleSend einbauen**

Direkt nach der `msg`-Validierung (vor dem `isSending.value = true`) folgenden Block einfügen:

```typescript
// Slash-Command-Intercept: lokale Befehle werden nicht an den Agenten gesendet
const parsed = parseSlashCommand(msg)
if (parsed) {
  const [cmd, args] = parsed
  promptInput.value = ''
  isSending.value = true
  try {
    const result = await dispatchSlashCommand(cmd, args, {
      taskId: ctx?.taskId ?? agent?.pipelineTaskId,
      cwd: ctx?.cwd ?? agent?.cwd,
    })
    sendStatus.value = result.ok ? 'sent' : 'error'
    sendError.value = result.ok ? '' : result.message
    // /help-Ausgabe als lokale Nachricht anzeigen
    if (result.ok && result.message) {
      onMessageSent?.({ role: 'channel_reply', content: result.message, timestamp: new Date().toISOString() })
    }
  }
  finally {
    isSending.value = false
    setTimeout(() => { sendStatus.value = null }, 5000)
  }
  return
}
```

- [ ] **Schritt 3: Typecheck + manuelle Prüfung** — `pnpm typecheck`

---

## Task 3: PromptInput.vue — Autocomplete-Palette auf neue Commands erweitern

**Files:**
- Modify: `src/components/PromptInput.vue`

> **Why:** Die vorhandene `SLASH_COMMANDS`-Konstante und die Autocomplete-Logik müssen auf das neue Vokabular umgestellt werden. Gleichzeitig sollen Commands mit Pflichtargumenten (`requiresTask`) ausgegraut werden, wenn kein Pipeline-Task verknüpft ist.

- [ ] **Schritt 1: SLASH_COMMANDS durch SLASH_COMMAND_DEFS ersetzen**

Import ergänzen:

```typescript
import { SLASH_COMMAND_DEFS } from '../composables/useSlashCommands'
```

Die hartcodierte `SLASH_COMMANDS`-Konstante entfernen und durch den Import ersetzen. Alle Stellen, die `cmd.name` / `cmd.description` nutzen, bleiben kompatibel (Felder existieren in `SlashCommandDef`).

- [ ] **Schritt 2: Props erweitern**

```typescript
const props = withDefaults(defineProps<{
  agent: Agent | null
  variant?: 'compact' | 'full'
}>(), {
  variant: 'compact',
})
```

Bleibt unverändert — `agent.pipelineTaskId` wird bereits über `agent` übergeben.

- [ ] **Schritt 3: Ausgrauen von requiresTask-Commands wenn kein Task verknüpft**

In `slashSuggestions` computed: Commands mit `requiresTask: true` AND ohne `agent?.pipelineTaskId` werden mit `disabled: true` markiert (nicht gefiltert, damit der User sie sieht und versteht warum sie nicht nutzbar sind).

```typescript
const slashSuggestions = computed(() => {
  const val = promptInput.value.trim()
  if (!val.startsWith('/'))
    return []
  if (val.includes(' '))
    return []
  const query = val.toLowerCase()
  return SLASH_COMMAND_DEFS
    .filter(c => c.name.startsWith(query))
    .map(c => ({
      ...c,
      disabled: !!c.requiresTask && !props.agent?.pipelineTaskId,
    }))
})
```

- [ ] **Schritt 4: Palette-Template anpassen**

In der `<button>` im Suggestions-Template:
- `:disabled="cmd.disabled"` hinzufügen
- CSS: `disabled:opacity-40 disabled:cursor-not-allowed`
- Wenn `cmd.usage` vorhanden: kurze graue Usage-Hint nach der Description anzeigen (`<span class="text-slate-300 dark:text-slate-700 text-[10px] ml-1">{{ cmd.usage }}</span>`)

- [ ] **Schritt 5: useAgentPrompt-Aufruf mit ctx konfigurieren**

```typescript
const { promptInput, isSending, sendStatus, sendError, handleSend } = useAgentPrompt(
  () => props.agent,
  msg => emit('messageSent', msg),
  { taskId: props.agent?.pipelineTaskId, cwd: props.agent?.cwd },
)
```

Da `props.agent` beim Setup-Call noch reaktiv ist, muss `ctx` als computed übergeben werden oder `useAgentPrompt` muss einen Getter akzeptieren. Einfachste Lösung: `ctx` als `() => AgentPromptContext` (Getter) statt statisches Objekt, analog zu `getAgent`.

- [ ] **Schritt 6: Typecheck + `pnpm lint`**

---

## Abschluss-Checks

- [ ] `pnpm typecheck` — keine TypeScript-Fehler
- [ ] `pnpm lint` — kein ESLint-Fehler
- [ ] `pnpm test` — alle Unit-Tests grün (inkl. neue Slash-Command-Tests)
- [ ] Manuelle Smoke-Tests:
  - `/help` → Ausgabe erscheint als Channel-Reply im Chat
  - `/spawn test-slug "Mein erster Task"` → Task wird in der Pipeline angelegt
  - `/unknown` → Inline-Fehlermeldung, kein Text an Agent gesendet
  - `/cancel` ohne verknüpften Task → Fehlermeldung mit Hinweis
  - Autocomplete: `/` tippen → Palette erscheint; `Tab`/`Enter` vervollständigt; `Esc` leert Input
  - requiresTask-Command ohne Pipeline-Task → in Palette ausgegraut
- [ ] `git add src/composables/useSlashCommands.ts src/composables/useSlashCommands.test.ts src/composables/useAgentPrompt.ts src/components/PromptInput.vue`
- [ ] `git commit -m "feat(ui): add slash-command vocabulary with autocomplete palette"`
- [ ] `git push -u origin feature/slash-command-vocabulary`
