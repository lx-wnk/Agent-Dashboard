# AI Edit Gate Diff Preview — Implementierungsplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Ziel:** Wenn ein Task auf `awaiting_user` hält (Stage-Run wartet auf Benutzer-Freigabe), soll die UI nicht mehr nur einen generischen "Progress"-Button anzeigen, sondern ein strukturiertes Diff-Panel rendern. Dieses Panel zeigt nebeneinander den vorherigen Stage-Output und den neuen (vom Agenten produzierten) Vorschlag, und bietet "Anwenden" (übergibt die Stage vorwärts) sowie "Ablehnen mit Feedback" (schickt strukturiertes Feedback an den produzierenden Agenten für eine neue Iteration).

**Ansatz (gewählt: Option B — pure-diff-Util + benutzerdefinierte Vue-Komponente):**
Keine externe Diff-Renderer-Bibliothek. Stattdessen: Die `diff`-Bibliothek (jsdiff, bereits im Node-Ökosystem verfügbar via transitiver Abhängigkeiten) erzeugt ein strukturiertes `Change[]`-Array aus zwei JSON-Strings. Eine neue Vue-Komponente `ApprovalDiffPanel.vue` rendert dieses Array mit Tailwind-Klassen als zweispaltiges (oder single-column split-view) Diff. Das Feedback-Feld ist ein `<textarea>`, das beim Absenden als `userAdditionalPrompt` an den vorhandenen `POST /tasks/:id/retry`-Endpunkt gesendet wird.

**Begründung Option B über Option A (diff2html/vue-diff):**
- `vue-diff` ist seit 4 Jahren nicht mehr gewartet.
- `diff2html` setzt Git-Unified-Diff-Format voraus und eignet sich gut für Datei-Patches, nicht für strukturierte JSON-Objekt-Vergleiche.
- Die `diff`-Bibliothek (jsdiff) ist tiny (~40 kB), zero-dependency, typensicher und erzeugt das `diffJson`-Array nativ — ideal für JSON-zu-JSON-Vergleich.
- Eine eigene Komponente bleibt im Tailwind/Vue-3-Pattern des Projekts und braucht keine externe CSS-Einbindung.

**Datenfluss:**
1. Stage-Run geht in `awaiting_user` → enthält in `stageRun.output` den neuen Vorschlag.
2. `TaskModal.vue` holt bereits `stageRuns` via `fetchStageRuns`. Der vorherige Stage-Output ist über `stageRuns[n-2].output` oder `task.metadata` erreichbar.
3. Neues `computed` `approvalDiff` in `TaskModal.vue` berechnet `{ before, after }` aus diesen beiden Quellen.
4. `ApprovalDiffPanel.vue` nimmt `:before` + `:after` als Props und rendert das Diff.
5. "Anwenden": ruft `progressTask(task.id)` auf — der bestehende Endpunkt reicht.
6. "Ablehnen mit Feedback": ruft `retryTask(task.id, feedbackText)` auf — schickt `userAdditionalPrompt` an die neue Iteration.

**Tech Stack:** Vue 3 Composition API, Tailwind CSS, `diff` npm package (jsdiff), TypeScript, Express (keine Backend-Änderungen nötig — nur Frontend + ggf. minimale Route-Anpassung)

---

## Datei-Übersicht

**Neue Dateien:**
- `src/components/ApprovalDiffPanel.vue` — Diff-Panel-Komponente

**Geänderte Dateien:**
- `package.json` — `diff` als Abhängigkeit hinzufügen
- `src/types.ts` — `ApprovalDiffEntry` Interface
- `src/components/TaskModal.vue` — `awaiting_user`-Banner mit `ApprovalDiffPanel` ersetzen; `approvalDiff` computed; Feedback-Logik

---

## Task 1: `diff`-Bibliothek installieren und typisieren

**Dateien:**
- Modify: `package.json`

- [ ] **Schritt 1: `diff` und `@types/diff` installieren**

```bash
pnpm add diff
pnpm add -D @types/diff
```

Erwartetes Ergebnis: `package.json` enthält `"diff"` unter `dependencies` und `"@types/diff"` unter `devDependencies`.

---

## Task 2: `ApprovalDiffEntry` Typ in `src/types.ts` ergänzen

**Dateien:**
- Modify: `src/types.ts`

- [ ] **Schritt 1: Interface am Ende von `src/types.ts` anhängen**

Hinter der letzten Export-Deklaration einfügen:

```ts
/** Represents the before/after payload surfaced in the approval diff panel. */
export interface ApprovalDiffPayload {
  /** JSON-serializable snapshot of the prior stage output (or empty object if none). */
  before: Record<string, unknown>
  /** JSON-serializable snapshot of the proposed new output awaiting approval. */
  after: Record<string, unknown>
  /** Human-readable label for the stage that produced `after`. */
  stageLabel: string
}
```

---

## Task 3: `ApprovalDiffPanel.vue` erstellen

**Dateien:**
- Create: `src/components/ApprovalDiffPanel.vue`

Die Komponente erhält `before` und `after` als Props (beide `Record<string, unknown>`), berechnet ein `diffLines`-Array mit `diffJson` aus dem `diff`-Paket und rendert eine zweispaltige Ansicht. Auf kleinen Displays fällt sie auf eine einzelne Spalte zurück.

- [ ] **Schritt 1: Datei `src/components/ApprovalDiffPanel.vue` anlegen**

```vue
<script setup lang="ts">
import type { Change } from 'diff'
import { diffJson } from 'diff'
import { computed } from 'vue'

const props = defineProps<{
  before: Record<string, unknown>
  after: Record<string, unknown>
  stageLabel: string
}>()

const emit = defineEmits<{
  apply: []
  reject: [feedback: string]
}>()

const feedbackText = ref('')
const showFeedback = ref(false)

const changes = computed<Change[]>(() =>
  diffJson(props.before, props.after),
)

// Split changes into left (removed/unchanged) and right (added/unchanged) lines
// for the side-by-side layout.
interface DiffLine { text: string, kind: 'removed' | 'added' | 'context' }

const leftLines = computed<DiffLine[]>(() =>
  changes.value.flatMap((c) => {
    if (c.added) return []
    const kind: DiffLine['kind'] = c.removed ? 'removed' : 'context'
    return c.value.split('\n').filter((l, i, arr) => !(i === arr.length - 1 && l === '')).map(text => ({ text, kind }))
  }),
)

const rightLines = computed<DiffLine[]>(() =>
  changes.value.flatMap((c) => {
    if (c.removed) return []
    const kind: DiffLine['kind'] = c.added ? 'added' : 'context'
    return c.value.split('\n').filter((l, i, arr) => !(i === arr.length - 1 && l === '')).map(text => ({ text, kind }))
  }),
)

const hasChanges = computed(() => changes.value.some(c => c.added || c.removed))

function onApply() {
  emit('apply')
}

function onRejectSubmit() {
  emit('reject', feedbackText.value.trim())
  feedbackText.value = ''
  showFeedback.value = false
}

// Import ref — needed because the <script setup> block uses it
import { ref } from 'vue'
</script>

<template>
  <div class="flex flex-col gap-3">
    <!-- Header -->
    <div class="flex items-center gap-2 flex-wrap">
      <span class="text-[10px] uppercase tracking-wider font-semibold text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-700/60 px-2 py-0.5 rounded">
        Wartet auf Freigabe
      </span>
      <span class="text-[11px] text-slate-400 dark:text-slate-600">
        Stage <code class="font-mono">{{ stageLabel }}</code> hat einen neuen Vorschlag produziert
      </span>
      <span v-if="!hasChanges" class="text-[11px] text-slate-400 dark:text-slate-600 ml-auto italic">
        (keine Änderungen gegenüber vorherigem Output)
      </span>
    </div>

    <!-- Diff view: side-by-side on md+, stacked on small -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-0.5 rounded-md overflow-hidden border border-slate-200 dark:border-slate-700 text-[11px] font-mono">
      <!-- Left column: BEFORE -->
      <div class="bg-slate-50 dark:bg-slate-950 overflow-auto max-h-[320px]">
        <div class="px-2 py-1 text-[10px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 sticky top-0">
          Vorher
        </div>
        <div class="p-0">
          <div
            v-for="(line, i) in leftLines"
            :key="i"
            class="px-3 py-[1px] leading-relaxed whitespace-pre-wrap break-all"
            :class="{
              'bg-red-50 dark:bg-red-950/30 text-red-700 dark:text-red-300': line.kind === 'removed',
              'text-slate-600 dark:text-slate-400': line.kind === 'context',
            }"
          >
            <span v-if="line.kind === 'removed'" class="select-none text-red-400 dark:text-red-600 mr-1">-</span>
            <span v-else class="select-none mr-1 text-transparent">·</span>{{ line.text }}
          </div>
        </div>
      </div>

      <!-- Right column: AFTER -->
      <div class="bg-slate-50 dark:bg-slate-950 overflow-auto max-h-[320px]">
        <div class="px-2 py-1 text-[10px] uppercase tracking-wider font-semibold text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 sticky top-0">
          Vorschlag
        </div>
        <div class="p-0">
          <div
            v-for="(line, i) in rightLines"
            :key="i"
            class="px-3 py-[1px] leading-relaxed whitespace-pre-wrap break-all"
            :class="{
              'bg-green-50 dark:bg-green-950/30 text-green-700 dark:text-green-300': line.kind === 'added',
              'text-slate-600 dark:text-slate-400': line.kind === 'context',
            }"
          >
            <span v-if="line.kind === 'added'" class="select-none text-green-500 dark:text-green-500 mr-1">+</span>
            <span v-else class="select-none mr-1 text-transparent">·</span>{{ line.text }}
          </div>
        </div>
      </div>
    </div>

    <!-- Action area -->
    <div class="flex flex-col gap-2">
      <!-- Feedback textarea (toggled) -->
      <template v-if="showFeedback">
        <textarea
          v-model="feedbackText"
          rows="3"
          class="w-full bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-slate-900 dark:text-slate-100 text-xs resize-none focus:outline-none focus:border-blue-500 placeholder:text-slate-400 dark:placeholder:text-slate-600"
          placeholder="Feedback an den Agenten — was soll er anders machen? (z.B. fehlende Akzeptanzkriterien, falscher Ansatz…)"
          autofocus
        />
        <div class="flex gap-2 justify-end">
          <button
            type="button"
            class="px-3 py-1.5 text-xs rounded border border-slate-200 dark:border-slate-700 bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 cursor-pointer"
            @click="showFeedback = false"
          >
            Abbrechen
          </button>
          <button
            type="button"
            class="px-3 py-1.5 text-xs rounded bg-red-600 text-white border-none hover:brightness-110 cursor-pointer disabled:opacity-50"
            :disabled="!feedbackText.trim()"
            @click="onRejectSubmit"
          >
            Ablehnen &amp; neue Iteration starten
          </button>
        </div>
      </template>

      <div v-else class="flex gap-2 justify-end">
        <button
          type="button"
          class="px-3 py-1.5 text-xs rounded border border-slate-200 dark:border-slate-700 bg-transparent text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/30 cursor-pointer"
          @click="showFeedback = true"
        >
          Ablehnen mit Feedback
        </button>
        <button
          type="button"
          class="px-3 py-1.5 text-xs rounded bg-green-600 text-white border-none hover:brightness-110 cursor-pointer"
          @click="onApply"
        >
          Vorschlag anwenden
        </button>
      </div>
    </div>
  </div>
</template>
```

**Hinweis zur Positionierung von `import { ref }`:** Da der `<script setup>`-Block `ref` bereits aus Vue importieren muss und `diffJson` aus `diff`, soll der Block nach dem folgenden Muster beginnen — alle Imports oben, `ref` inline bei Verwendung:

```ts
import type { Change } from 'diff'
import { diffJson } from 'diff'
import { computed, ref } from 'vue'
```

Der `import { ref } from 'vue'` am Ende des obigen Blocks muss nach oben verschoben werden (ESLint-Regel: alle Imports oben). Beim Erstellen der Datei direkt korrekt positionieren.

---

## Task 4: `TaskModal.vue` — `awaiting_user`-Zustand mit Diff-Panel anzeigen

**Dateien:**
- Modify: `src/components/TaskModal.vue`

- [ ] **Schritt 1: `ApprovalDiffPanel` importieren**

Im `<script setup>`-Block von `TaskModal.vue` nach den bestehenden Component-Imports einfügen:

```ts
import ApprovalDiffPanel from './ApprovalDiffPanel.vue'
```

- [ ] **Schritt 2: `approvalDiff` computed property hinzufügen**

Nach dem `latestStageRun` computed (ca. Zeile 132) einfügen:

```ts
/**
 * Builds the before/after payload for the approval diff panel.
 * `after` = the awaiting_user stage_run's output (the agent's proposal).
 * `before` = the previous stage_run's output, or task.metadata if no prior run.
 * Both default to `{}` when absent.
 */
const approvalDiff = computed(() => {
  const latest = latestStageRun.value
  if (!latest || latest.status !== 'awaiting_user')
    return null

  const after: Record<string, unknown> = latest.output ?? {}

  // Walk backwards through stageRuns to find the last *done* run of a prior stage.
  const prior = [...stageRuns.value]
    .reverse()
    .find(r => r.id !== latest.id && r.status === 'done' && r.output != null)
  const before: Record<string, unknown> = prior?.output ?? (props.task?.metadata as Record<string, unknown> | null) ?? {}

  return { before, after, stageLabel: latest.stage }
})
```

- [ ] **Schritt 3: `onApprovalApply` und `onApprovalReject` Handler hinzufügen**

Nach `handleAction` (ca. Zeile 244) einfügen:

```ts
async function onApprovalApply(): Promise<void> {
  await handleAction(() => progressTask(props.task!.id))
}

async function onApprovalReject(feedback: string): Promise<void> {
  await handleAction(() => retryTask(props.task!.id, feedback || undefined))
}
```

- [ ] **Schritt 4: Diff-Panel im Overview-Tab einblenden**

Im Template des Overview-Tabs (`<section v-if="activeTab === 'overview'"`), **oberhalb** des bestehenden "Aktuelle Ausgabe"-Blocks, das `ApprovalDiffPanel` einbauen:

```html
<!-- Approval diff panel — nur wenn der aktuelle Stage-Run auf Freigabe wartet -->
<ApprovalDiffPanel
  v-if="approvalDiff"
  :before="approvalDiff.before"
  :after="approvalDiff.after"
  :stage-label="approvalDiff.stageLabel"
  class="border-t border-slate-200 dark:border-slate-700 pt-3"
  @apply="onApprovalApply"
  @reject="onApprovalReject"
/>
```

Einzufügen direkt vor dem Block:
```html
<!-- 3. Aktuelle Ausgabe -->
<div v-if="latestStageRun" class="bg-slate-50 ...">
```

- [ ] **Schritt 5: "Progress"-Button im Footer verstecken, wenn `approvalDiff` aktiv**

Den vorhandenen "Progress →"-Button in der Footer-Section:

```html
<AppButton
  v-if="!isTerminal(task.currentStage) && !isOnHoldStage && !isFailedRun(task)"
  ...
>
  Progress →
</AppButton>
```

Condition erweitern auf:

```html
<AppButton
  v-if="!isTerminal(task.currentStage) && !isOnHoldStage && !isFailedRun(task) && !approvalDiff"
  ...
>
  Progress →
</AppButton>
```

Begründung: Wenn das Diff-Panel aktiv ist, steuert es die Freigabe. Der generische "Progress"-Button würde den Approval-Gate-Mechansmus umgehen.

---

## Task 5: Smoke-Test und Typcheck

**Dateien:**
- keine Änderungen — nur Ausführung

- [ ] **Schritt 1: Typcheck ausführen**

```bash
pnpm typecheck
```

Erwartetes Ergebnis: 0 Fehler. Häufige Stolperfalle: `diffJson` erwartet `object | any[]` als Argumente — `Record<string, unknown>` ist kompatibel.

- [ ] **Schritt 2: Lint ausführen**

```bash
pnpm lint
```

Erwartetes Ergebnis: 0 Fehler. Prüfen: Import-Reihenfolge in `ApprovalDiffPanel.vue`, kein ungenutztes `ref`.

- [ ] **Schritt 3: Dev-Server starten und manuell testen**

```bash
pnpm dev
```

Einen Task in den `awaiting_user`-Zustand bringen (manuell via `POST /tasks/:id/progress` auf einen Task in einem `backlog`-Stage — der `backlogHandler` geht direkt weiter, alternativ: in der DB direkt eine `stage_run` row mit `status = 'awaiting_user'` einfügen).

Erwartetes Ergebnis:
- Das Diff-Panel erscheint im Overview-Tab.
- "Vorschlag anwenden" ruft `progressTask` auf, Task schreitet voran.
- "Ablehnen mit Feedback" öffnet das Textarea, nach Submit wird `retryTask` mit dem Feedback-Text aufgerufen.
- Der "Progress →"-Button ist ausgeblendet, wenn das Diff-Panel sichtbar ist.

- [ ] **Schritt 4: Branch pushen**

```bash
git checkout -b feature/ai-edit-gate-diff-preview
git add src/components/ApprovalDiffPanel.vue src/components/TaskModal.vue src/types.ts package.json pnpm-lock.yaml
git commit -m "feat: AI edit gate diff preview for awaiting_user approval flow"
git push -u origin feature/ai-edit-gate-diff-preview
```

---

## Offene Punkte / Spätere Erweiterungen

- **Backend-Unterstützung für strukturiertes Rejection-Feedback:** Derzeit wird `userAdditionalPrompt` als Freitext an die neue Iteration weitergegeben. Eine spätere Version könnte das Feedback als `rejection_feedback`-Key in `task.metadata` schreiben (wie `review_feedback` beim Selbstreview), damit der Iteration-Prompt gezielt darauf reagiert.
- **Vor/Nach-Label dynamisch:** Wenn `before` leer ist (erster Lauf), statt "Vorher" den Text "Kein vorheriger Output" anzeigen.
- **Diff bei nicht-JSON Outputs:** Wenn ein Stage-Output nur `agentMessage` (String) enthält, auf `diffWords` oder einfachen Textvergleich zurückfallen.
