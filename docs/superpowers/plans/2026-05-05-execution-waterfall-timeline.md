# Execution Waterfall Timeline — Implementierungsplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Ziel:** Das flache Pill-Widget `ToolTimeline.vue` (das nur Tool-Namen anzeigt) wird um eine optionale Gantt-artige Wasserfall-Ansicht ergänzt. Jeder Tool-Call wird als horizontaler Balken dargestellt — positioniert nach Startzeitpunkt, Breite = Dauer, Farbe nach Tool-Typ und Outcome. Hover-Tooltip zeigt Tool-Name, Argument-Zusammenfassung, Dauer in ms und Fehler-Flag. Das bestehende Pill-Widget bleibt als Fallback erhalten.

**Architektur:** Der Backend-Parser (`server/jsonlParser.ts`) extrapoliert Timing-Daten aus den Entry-Timestamps: der Timestamp des `assistant`-Eintrags gilt als `startedAt` eines Tool-Calls, der Timestamp des folgenden `user`-Eintrags (mit dem passenden `tool_result`) als `completedAt`. Ein neues Interface `ToolCallEvent` wird in `src/types.ts` ergänzt und über das bestehende SSE/Agent-Array als neues Feld `toolCallEvents` transportiert. Die Visualisierung ist hand-rolled als SVG (kein Chart-Framework), mit CSS-basiertem Viewport-Clipping für Sessions mit 1000+ Tool-Calls — keine externe Abhängigkeit nötig.

**Tech Stack:** TypeScript, Vue 3 Composition API, native SVG, Tailwind CSS, keine neuen npm-Pakete.

---

## Technische Erkenntnisse aus der Analyse

### JSONL-Timing-Modell

Die JSONL-Logs enthalten **keine expliziten `startedAt`/`completedAt`-Felder** pro Tool-Call. Das Timing wird aus den Entry-Timestamps abgeleitet:

```
assistant entry (timestamp: T1) → tool_use block { id, name, input }
user entry      (timestamp: T2) → tool_result block { tool_use_id, is_error }

=> startedAt = T1 (Zeitpunkt, zu dem das LLM den Tool-Call ausgegeben hat)
=> completedAt = T2 (Zeitpunkt, zu dem das Tool Ergebnis zurückgegeben hat)
=> duration = T2 - T1 in ms
```

**Einschränkung:** Claude Code gibt pro LLM-Turn üblicherweise einen einzigen `tool_use`-Block aus (sequentielle Ausführung). Theoretisch können mehrere Tool-Uses in einem Turn vorkommen — in diesem Fall teilen sie denselben `startedAt`-Timestamp und die Duration ist nicht einzeln messbar. Die Implementierung muss dies korrekt handhaben (alle Tools eines Multi-Tool-Turns erhalten denselben `startedAt` und als `completedAt` den Timestamp des nächsten `user`-Eintrags).

### Tail-Read-Beschränkung

`findSessionForProject` liest nur die letzten **32 KB** der JSONL-Datei (TAIL_BYTES). Bei großen Sessions fehlen ältere Tool-Calls. Für die waterfall-Timeline wird ein neuer, gesonderter API-Endpunkt benötigt, der die vollständige (oder letzte N MB große) Session liest — analog zu `parseFullSession`.

### Visualisierung

SVG-Balkendiagramm ohne externe Library (kein D3, keine Vue-Chart-Library). Begründung:
- Projekt hat keine Chart-Bibliothek als Abhängigkeit, und die Visualisierung ist straightforward genug für natives SVG
- SVG ist bei ≤ 1.000 sichtbaren Elementen performant genug
- Vollständige Kontrolle über Tooltip-Verhalten, Farbgebung und Layout

Für Sessions mit > 500 Tool-Calls wird **CSS Viewport Clipping** (IntersectionObserver oder `overflow: hidden` + virtueller Scroll) eingesetzt, sodass nur sichtbare Balken im DOM sind.

---

## File Map

**Erstellt:**
- `server/routes/waterfallRoutes.ts` — `GET /api/agents/:sessionId/waterfall` gibt `ToolCallEvent[]` zurück
- `src/components/WaterfallTimeline.vue` — SVG-Waterfall-Komponente mit Tooltip
- `src/components/WaterfallTooltip.vue` — Tooltip-Overlay (Teleport zu `body`)

**Modifiziert:**
- `src/types.ts` — neues Interface `ToolCallEvent`
- `server/jsonlParser.ts` — neue Funktion `extractToolCallEvents(entries)`
- `server/index.ts` — Mount `waterfallRoutes`
- `src/components/AgentModal.vue` — WaterfallTimeline einbinden (toggle neben bestehender ToolTimeline)
- `src/components/ToolTimeline.vue` — Toggle-Button "Waterfall"-Ansicht hinzufügen (optional, nur wenn Agent die sessionId hat)

---

## Task 1: Typen — ToolCallEvent Interface

**Files:**
- Modify: `src/types.ts`

- [ ] **Step 1: ToolCallEvent zu src/types.ts hinzufügen**

Nach dem `OutputMessage`-Interface einfügen:

```typescript
export interface ToolCallEvent {
  /** Unique: tool_use block.id aus dem JSONL (toolu_...) */
  id: string
  /** Tool-Name z.B. "Read", "Bash", "Edit", "Write", "Grep", "Glob", "Agent" */
  name: string
  /** ISO-8601-Timestamp des assistant-Eintrags (LLM hat den Call ausgegeben) */
  startedAt: string
  /** ISO-8601-Timestamp des user-Eintrags (tool_result empfangen) — null wenn noch kein Ergebnis */
  completedAt: string | null
  /** Dauer in Millisekunden — null wenn completedAt fehlt */
  durationMs: number | null
  /** true wenn tool_result.is_error === true */
  isError: boolean
  /** Kompakte Argument-Zusammenfassung, max 120 Zeichen */
  argSummary: string
}
```

---

## Task 2: Backend — extractToolCallEvents

**Files:**
- Modify: `server/jsonlParser.ts`

- [ ] **Step 2: Hilfsfunktion extractToolCallEvents implementieren**

Nach der bestehenden `extractSessionInfo`-Funktion eine neue Funktion hinzufügen. Sie iteriert über alle JSONL-Einträge und matched `tool_use`-Blocks (aus `assistant`-Einträgen) mit ihren `tool_result`-Blocks (aus nachfolgenden `user`-Einträgen) über `tool_use_id`.

```typescript
export function extractToolCallEvents(entries: any[]): ToolCallEvent[] {
  const events: ToolCallEvent[] = []
  // Map tool_use_id → pending event (noch kein completedAt)
  const pending = new Map<string, ToolCallEvent>()

  for (const entry of entries) {
    if (entry.type === 'assistant' && Array.isArray(entry.message?.content)) {
      const ts = entry.timestamp as string | undefined
      if (!ts) continue
      for (const block of entry.message.content) {
        if (block.type !== 'tool_use' || !block.name || !block.id) continue
        const argSummary = buildArgSummary(block.name, block.input ?? {})
        const event: ToolCallEvent = {
          id: block.id,
          name: block.name,
          startedAt: ts,
          completedAt: null,
          durationMs: null,
          isError: false,
          argSummary,
        }
        pending.set(block.id, event)
        events.push(event)
      }
    }

    if (entry.type === 'user' && Array.isArray(entry.message?.content)) {
      const ts = entry.timestamp as string | undefined
      if (!ts) continue
      for (const block of entry.message.content) {
        if (block.type !== 'tool_result' || !block.tool_use_id) continue
        const event = pending.get(block.tool_use_id)
        if (!event) continue
        const startMs = new Date(event.startedAt).getTime()
        const endMs = new Date(ts).getTime()
        event.completedAt = ts
        event.durationMs = endMs - startMs
        event.isError = block.is_error === true
        pending.delete(block.tool_use_id)
      }
    }
  }

  return events
}
```

- [ ] **Step 3: buildArgSummary-Hilfsfunktion implementieren**

Private Hilfsfunktion (nicht exportiert), die je nach Tool-Name die relevanten Input-Felder zu einem kurzen String formt:

```typescript
function buildArgSummary(toolName: string, input: Record<string, unknown>): string {
  const candidates: Array<string | undefined> = []
  switch (toolName) {
    case 'Read':
    case 'Write':
    case 'Edit':
    case 'MultiEdit':
      candidates.push(input.file_path as string)
      break
    case 'Bash':
      candidates.push(input.command as string)
      break
    case 'Grep':
      candidates.push(`${input.pattern} in ${input.path ?? '.'}`)
      break
    case 'Glob':
      candidates.push(input.pattern as string)
      break
    default:
      candidates.push(JSON.stringify(input).substring(0, 80))
  }
  const raw = candidates.find(Boolean) ?? ''
  return raw.length > 120 ? `${raw.substring(0, 117)}...` : raw
}
```

---

## Task 3: Backend — API-Endpunkt

**Files:**
- Create: `server/routes/waterfallRoutes.ts`
- Modify: `server/index.ts`

- [ ] **Step 4: waterfallRoutes.ts erstellen**

```typescript
import type { ToolCallEvent } from '../../src/types.js'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { Router } from 'express'
import { extractToolCallEvents, parseJsonlLines } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'
import { UUID_RE } from '../constants.js'
import { readdir, stat } from 'node:fs/promises'

export function createWaterfallRouter(): Router {
  const router = Router()

  router.get('/agents/:sessionId/waterfall', async (req, res) => {
    const { sessionId } = req.params
    if (!UUID_RE.test(sessionId)) {
      res.status(400).json({ error: 'Invalid sessionId' })
      return
    }

    // Locate the JSONL file across all project directories
    let sessionFilePath: string | null = null
    try {
      const projectDirs = await readdir(CLAUDE_PROJECTS_DIR, { withFileTypes: true })
      for (const dir of projectDirs) {
        if (!dir.isDirectory()) continue
        const candidate = join(CLAUDE_PROJECTS_DIR, dir.name, `${sessionId}.jsonl`)
        try {
          await stat(candidate)
          sessionFilePath = candidate
          break
        }
        catch { continue }
      }
    }
    catch {
      res.status(500).json({ error: 'Failed to scan projects directory' })
      return
    }

    if (!sessionFilePath) {
      res.status(404).json({ error: 'Session not found' })
      return
    }

    // Read up to 10 MB (same cap as parseFullSession)
    const MAX_BYTES = 10 * 1024 * 1024
    const fileStats = await stat(sessionFilePath)
    let raw: string
    if (fileStats.size <= MAX_BYTES) {
      raw = await readFile(sessionFilePath, 'utf-8')
    }
    else {
      const { open } = await import('node:fs/promises')
      const handle = await open(sessionFilePath, 'r')
      try {
        const { Buffer } = await import('node:buffer')
        const buffer = Buffer.alloc(MAX_BYTES)
        await handle.read(buffer, 0, MAX_BYTES, fileStats.size - MAX_BYTES)
        raw = buffer.toString('utf-8')
      }
      finally {
        await handle.close()
      }
    }

    const entries = parseJsonlLines(raw)
    const events: ToolCallEvent[] = extractToolCallEvents(entries)
    res.json(events)
  })

  return router
}
```

- [ ] **Step 5: Router in server/index.ts mounten**

Im bestehenden `server/index.ts` nach den anderen Router-Importen:

```typescript
import { createWaterfallRouter } from './routes/waterfallRoutes.js'
// ... in der Express-Setup-Sektion:
app.use('/api', createWaterfallRouter())
```

---

## Task 4: Frontend — WaterfallTooltip-Komponente

**Files:**
- Create: `src/components/WaterfallTooltip.vue`

- [ ] **Step 6: WaterfallTooltip.vue erstellen**

Leichtgewichtiger Tooltip, der via Teleport zu `body` gerendert wird, um z-index-Probleme zu vermeiden.

```vue
<script setup lang="ts">
import type { ToolCallEvent } from '../types'
import { Teleport } from 'vue'

const props = defineProps<{
  event: ToolCallEvent | null
  x: number
  y: number
}>()
</script>

<template>
  <Teleport to="body">
    <div
      v-if="event"
      class="fixed z-[9999] pointer-events-none bg-slate-900 dark:bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-xs shadow-xl max-w-[320px]"
      :style="{ left: `${x + 12}px`, top: `${y - 8}px` }"
    >
      <div class="font-semibold text-slate-100 mb-1">{{ event.name }}</div>
      <div class="text-slate-400 font-mono break-all mb-1.5">{{ event.argSummary }}</div>
      <div class="flex gap-3 text-slate-300">
        <span v-if="event.durationMs !== null">{{ event.durationMs }}ms</span>
        <span v-if="event.isError" class="text-red-400 font-semibold">error</span>
        <span v-else-if="event.completedAt" class="text-emerald-400">ok</span>
        <span v-else class="text-amber-400">running...</span>
      </div>
    </div>
  </Teleport>
</template>
```

---

## Task 5: Frontend — WaterfallTimeline-Komponente

**Files:**
- Create: `src/components/WaterfallTimeline.vue`

- [ ] **Step 7: WaterfallTimeline.vue erstellen**

Die Hauptkomponente lädt die Tool-Call-Daten über den neuen Endpunkt und rendert ein SVG-Wasserfall-Diagramm. Farben werden per Tool-Typ vergeben.

**Farbschema:**

| Tool-Gruppe | Farbe |
|---|---|
| Read / Glob / Grep / LS | blau `#3b82f6` |
| Write / Edit / MultiEdit | lila `#a855f7` |
| Bash | orange `#f97316` |
| Agent / Task* | grün `#22c55e` |
| Sonstige / MCP | grau `#94a3b8` |
| Error-Overlay | rot `#ef4444` (Rahmen oder Diagonal-Schraffur) |

**Kernlogik (Pseudocode):**

```
minTime = min(events[*].startedAt)
maxTime = max(events[*].completedAt ?? now)
totalDuration = maxTime - minTime

SVG-Breite = Container-Breite (responsive)
SVG-Höhe = events.length * ROW_HEIGHT (z.B. 18px) + HEADER_HEIGHT

Für jeden Event:
  x = (startedAt - minTime) / totalDuration * SVG_WIDTH
  width = (durationMs / totalDuration) * SVG_WIDTH (min 2px für sehr kurze Calls)
  y = index * ROW_HEIGHT + HEADER_HEIGHT
  fill = toolColor(event.name)
  stroke = event.isError ? '#ef4444' : 'none'
```

Für Sessions mit > 300 Events: SVG-Viewport via `viewBox` und vertikales Scrolling des Container-Divs — keine DOM-Virtualisierung nötig, SVG rendert alle Elemente effizient.

Vollständiges Vue-SFC-Gerüst:

```vue
<script setup lang="ts">
import type { ToolCallEvent } from '../types'
import { computed, onMounted, ref } from 'vue'
import WaterfallTooltip from './WaterfallTooltip.vue'

const props = defineProps<{
  sessionId: string
}>()

const events = ref<ToolCallEvent[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const tooltipEvent = ref<ToolCallEvent | null>(null)
const tooltipX = ref(0)
const tooltipY = ref(0)

const ROW_HEIGHT = 18
const HEADER_HEIGHT = 24
const MIN_BAR_WIDTH = 2
const SVG_WIDTH = 800 // internal; viewBox-basiert, echte Breite via Container

async function load() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch(`/api/agents/${props.sessionId}/waterfall`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    events.value = await res.json()
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Fehler'
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

const timeRange = computed(() => {
  if (!events.value.length) return { min: 0, max: 1 }
  const min = Math.min(...events.value.map(e => new Date(e.startedAt).getTime()))
  const max = Math.max(...events.value.map(e =>
    e.completedAt ? new Date(e.completedAt).getTime() : Date.now()
  ))
  return { min, max: max > min ? max : min + 1 }
})

const svgHeight = computed(() => events.value.length * ROW_HEIGHT + HEADER_HEIGHT)

function barX(event: ToolCallEvent): number {
  const { min, max } = timeRange.value
  return ((new Date(event.startedAt).getTime() - min) / (max - min)) * SVG_WIDTH
}

function barWidth(event: ToolCallEvent): number {
  if (!event.durationMs) return MIN_BAR_WIDTH
  const { min, max } = timeRange.value
  const w = (event.durationMs / (max - min)) * SVG_WIDTH
  return Math.max(w, MIN_BAR_WIDTH)
}

function barY(index: number): number {
  return index * ROW_HEIGHT + HEADER_HEIGHT
}

const TOOL_COLORS: Record<string, string> = {
  Read: '#3b82f6',
  Glob: '#3b82f6',
  Grep: '#3b82f6',
  LS: '#3b82f6',
  Write: '#a855f7',
  Edit: '#a855f7',
  MultiEdit: '#a855f7',
  Bash: '#f97316',
  Agent: '#22c55e',
  TaskCreate: '#22c55e',
  TaskUpdate: '#22c55e',
}

function toolColor(name: string): string {
  return TOOL_COLORS[name] ?? '#94a3b8'
}

function onBarMouseenter(e: MouseEvent, event: ToolCallEvent) {
  tooltipEvent.value = event
  tooltipX.value = e.clientX
  tooltipY.value = e.clientY
}

function onBarMousemove(e: MouseEvent) {
  tooltipX.value = e.clientX
  tooltipY.value = e.clientY
}

function onBarMouseleave() {
  tooltipEvent.value = null
}
</script>

<template>
  <div class="relative">
    <div v-if="loading" class="text-xs text-slate-400 py-2">Lade Waterfall-Daten...</div>
    <div v-else-if="error" class="text-xs text-red-400 py-2">{{ error }}</div>
    <div v-else-if="!events.length" class="text-xs text-slate-400 py-2">Keine Tool-Calls gefunden.</div>
    <div v-else class="overflow-y-auto max-h-[400px]">
      <svg
        :viewBox="`0 0 ${SVG_WIDTH} ${svgHeight}`"
        :style="{ width: '100%', height: `${svgHeight}px` }"
        class="font-mono"
      >
        <!-- X-Achse: Zeitannotationen (Start, Mitte, Ende) -->
        <g class="time-axis">
          <text x="0" :y="HEADER_HEIGHT - 6" font-size="9" fill="#64748b">0ms</text>
          <text :x="SVG_WIDTH / 2" :y="HEADER_HEIGHT - 6" font-size="9" fill="#64748b" text-anchor="middle">
            {{ Math.round((timeRange.max - timeRange.min) / 2) }}ms
          </text>
          <text :x="SVG_WIDTH" :y="HEADER_HEIGHT - 6" font-size="9" fill="#64748b" text-anchor="end">
            {{ timeRange.max - timeRange.min }}ms
          </text>
        </g>

        <!-- Balken -->
        <g v-for="(event, i) in events" :key="event.id">
          <!-- Hintergrund-Zeile (Zebra) -->
          <rect
            x="0"
            :y="barY(i)"
            :width="SVG_WIDTH"
            :height="ROW_HEIGHT - 1"
            :fill="i % 2 === 0 ? 'rgba(148,163,184,0.04)' : 'transparent'"
          />
          <!-- Tool-Balken -->
          <rect
            :x="barX(event)"
            :y="barY(i) + 2"
            :width="barWidth(event)"
            :height="ROW_HEIGHT - 5"
            :fill="toolColor(event.name)"
            :stroke="event.isError ? '#ef4444' : 'none'"
            stroke-width="1.5"
            rx="2"
            class="cursor-pointer opacity-80 hover:opacity-100 transition-opacity"
            @mouseenter="onBarMouseenter($event, event)"
            @mousemove="onBarMousemove"
            @mouseleave="onBarMouseleave"
          />
          <!-- Tool-Label (nur wenn Balken breit genug) -->
          <text
            v-if="barWidth(event) > 30"
            :x="barX(event) + 4"
            :y="barY(i) + ROW_HEIGHT - 6"
            font-size="8"
            fill="white"
            class="pointer-events-none select-none"
          >
            {{ event.name }}
          </text>
        </g>
      </svg>
    </div>

    <!-- Legende -->
    <div class="flex flex-wrap gap-3 mt-2 text-[10px] text-slate-400">
      <span v-for="[name, color] in [['Read/Glob/Grep', '#3b82f6'], ['Write/Edit', '#a855f7'], ['Bash', '#f97316'], ['Agent/Task', '#22c55e'], ['Sonstige', '#94a3b8']]" :key="name" class="flex items-center gap-1">
        <span class="inline-block w-3 h-2 rounded-sm" :style="{ background: color }" />
        {{ name }}
      </span>
      <span class="flex items-center gap-1">
        <span class="inline-block w-3 h-2 rounded-sm border border-red-400" style="background: transparent" />
        Error
      </span>
    </div>

    <WaterfallTooltip :event="tooltipEvent" :x="tooltipX" :y="tooltipY" />
  </div>
</template>
```

---

## Task 6: Frontend — Integration in AgentModal

**Files:**
- Modify: `src/components/AgentModal.vue`

- [ ] **Step 8: WaterfallTimeline in AgentModal einbinden**

Im `<script setup>`-Block:

```typescript
import WaterfallTimeline from './WaterfallTimeline.vue'

const showWaterfall = ref(false)
```

Im Template, innerhalb des "Agent Details"-`<details>`-Blocks, nach dem `<ToolTimeline>`-Block:

```html
<!-- Toggle zwischen Pill-Liste und Waterfall -->
<div v-if="agent.lastTools.length > 0" class="flex items-center gap-2 mb-1">
  <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">
    Tool-Timeline
  </h4>
  <button
    type="button"
    class="text-[10px] px-1.5 py-0.5 rounded border border-slate-300 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 transition-colors"
    @click="showWaterfall = !showWaterfall"
  >
    {{ showWaterfall ? 'Pills' : 'Waterfall' }}
  </button>
</div>

<WaterfallTimeline
  v-if="showWaterfall && agent.sessionId"
  :session-id="agent.sessionId"
/>
<ToolTimeline
  v-else-if="agent.lastTools.length > 0"
  :tools="agent.lastTools"
/>
```

**Hinweis:** `ToolTimeline` erhält keinen eigenen Toggle-Button mehr — der Toggle sitzt im AgentModal, da er den `showWaterfall`-State steuert, der dem Modal gehört.

---

## Task 7: Tests

**Files:**
- Create: `server/jsonlParser.waterfall.test.ts` (Vitest)

- [ ] **Step 9: Unit-Tests für extractToolCallEvents**

```typescript
import { describe, it, expect } from 'vitest'
import { extractToolCallEvents } from './jsonlParser'

describe('extractToolCallEvents', () => {
  it('extrahiert einen sequentiellen Tool-Call mit korrekter Dauer', () => {
    const entries = [
      {
        type: 'assistant',
        timestamp: '2026-01-01T10:00:00.000Z',
        message: {
          content: [{ type: 'tool_use', id: 'toolu_1', name: 'Read', input: { file_path: '/foo/bar.ts' } }],
        },
      },
      {
        type: 'user',
        timestamp: '2026-01-01T10:00:00.500Z',
        message: {
          content: [{ type: 'tool_result', tool_use_id: 'toolu_1', content: 'ok', is_error: false }],
        },
      },
    ]
    const events = extractToolCallEvents(entries)
    expect(events).toHaveLength(1)
    expect(events[0].name).toBe('Read')
    expect(events[0].durationMs).toBe(500)
    expect(events[0].isError).toBe(false)
    expect(events[0].argSummary).toBe('/foo/bar.ts')
  })

  it('markiert Error-Calls korrekt', () => {
    const entries = [
      {
        type: 'assistant',
        timestamp: '2026-01-01T10:00:00.000Z',
        message: {
          content: [{ type: 'tool_use', id: 'toolu_2', name: 'Bash', input: { command: 'ls /nonexistent' } }],
        },
      },
      {
        type: 'user',
        timestamp: '2026-01-01T10:00:00.100Z',
        message: {
          content: [{ type: 'tool_result', tool_use_id: 'toolu_2', content: 'error', is_error: true }],
        },
      },
    ]
    const events = extractToolCallEvents(entries)
    expect(events[0].isError).toBe(true)
  })

  it('lässt completedAt auf null wenn kein tool_result vorhanden', () => {
    const entries = [
      {
        type: 'assistant',
        timestamp: '2026-01-01T10:00:00.000Z',
        message: {
          content: [{ type: 'tool_use', id: 'toolu_3', name: 'Write', input: { file_path: '/tmp/x' } }],
        },
      },
    ]
    const events = extractToolCallEvents(entries)
    expect(events[0].completedAt).toBeNull()
    expect(events[0].durationMs).toBeNull()
  })
})
```

---

## Zusammenfassung

| Phase | Aufwand |
|---|---|
| Task 1: Typen | 0,25 PD |
| Task 2: Backend-Parser | 0,5 PD |
| Task 3: API-Endpunkt | 0,5 PD |
| Task 4: Tooltip-Komponente | 0,25 PD |
| Task 5: Waterfall-SVG-Komponente | 1,0 PD |
| Task 6: AgentModal-Integration | 0,25 PD |
| Task 7: Tests | 0,25 PD |
| **Gesamt (inkl. 20% Buffer)** | **3,6 PD** |

## Risiken

| Risiko | Wahrscheinlichkeit | Impact | Mitigation |
|---|---|---|---|
| Timing-Extrapolation ungenau (LLM-Latenz in T1 eingeschlossen) | Hoch | Niedrig | Dokumentieren im Tooltip; Anwender wissen, dass startedAt = LLM-Response-Zeit, nicht Tool-Start |
| SVG-Performance bei > 500 Tool-Calls | Mittel | Mittel | Max-H Container + overflow-y-auto; Browser rendert SVG außerhalb des Viewports nicht aktiv |
| Tail-Read liefert nur letzte 32 KB (ältere Calls fehlen) | Hoch | Mittel | Neuer Endpunkt liest bis 10 MB — deutlich größerer Scope als Tail-Read |
| Multi-Tool-Turns (mehrere tool_use in einem assistant-Turn) | Niedrig | Niedrig | Alle Tools des Turns erhalten identischen startedAt; Tooltip zeigt dies transparent |
