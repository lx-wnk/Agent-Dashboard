# Refinement Phase Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the refinement agent declare phase completion (`analysis → spec → implementation_plan → approval`) and show that progress as in-chat dividers and a stepper in the concept-step status panel.

**Architecture:** The agent emits inline `__phase_done: <phase>` markers. The `refine.Runner` parses them, records the phase on the persisted assistant turn, and strips them from stored content. The frontend parses the same marker live while streaming and reads `turn.phase` on reload; a stepper in `RefineStatusPanel` is derived from the completed phases.

**Tech Stack:** Go 1.26 (testing), Vue 3 + TypeScript (Vitest).

**Spec:** `docs/superpowers/specs/2026-05-30-refinement-phase-tracking-design.md`

---

## File Structure

**Phase A — backend**
- Create: `server/internal/refine/phases.go` — `Phases` SSOT slice, `phaseDoneRE`, `ExtractPhases(s) (cleaned string, phases []string)`, `IsValidPhase`.
- Create: `server/internal/refine/phases_test.go` — ExtractPhases unit tests.
- Modify: `server/internal/refine/runner.go` — persist step sets `turn.phase` + stores cleaned content.
- Modify: `server/internal/refine/runner_test.go` — fakeTurns captures `Phase`; assert phase persisted.
- Modify: `server/internal/refine/spawner.go` — `promptTmpl` instructs the agent to emit `__phase_done:` markers.

**Phase B — frontend**
- Modify: `src/composables/useRefinementChat.ts` — parse `__phase_done:` in the SSE loop; keep it out of displayed content; add `completedPhases` + `PHASE_ORDER` to the returned API.
- Modify: `src/composables/__tests__/useRefinementChat.test.ts` — streamed marker drives `completedPhases`/`approvalReady`.
- Modify: `src/components/RefineStatusPanel.vue` — add `completedPhases` prop + 4-step stepper.
- Modify: `src/components/__tests__/RefineStatusPanel.test.ts` — stepper state tests.
- Modify: `src/components/TaskModal.vue` — derive `completedPhases` from fetched turns; pass to panel.

---

## PHASE A — Backend

### Task A1: Phase SSOT + `ExtractPhases`

**Files:**
- Create: `server/internal/refine/phases.go`
- Test: `server/internal/refine/phases_test.go`

- [ ] **Step 1: Write the failing tests**

Create `server/internal/refine/phases_test.go`:

```go
package refine

import (
	"reflect"
	"testing"
)

func TestExtractPhases_SingleMarkerStrippedAndCaptured(t *testing.T) {
	cleaned, phases := ExtractPhases("analysis text\n__phase_done: spec\nmore text")
	if want := "analysis text\nmore text"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
	if !reflect.DeepEqual(phases, []string{"spec"}) {
		t.Errorf("phases: got %v, want [spec]", phases)
	}
}

func TestExtractPhases_MultipleMarkersOrderedAndAllStripped(t *testing.T) {
	cleaned, phases := ExtractPhases("__phase_done: analysis\nbody\n__phase_done: spec\n")
	if !reflect.DeepEqual(phases, []string{"analysis", "spec"}) {
		t.Errorf("phases: got %v, want [analysis spec]", phases)
	}
	if want := "body"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
}

func TestExtractPhases_UnknownPhaseIgnored(t *testing.T) {
	cleaned, phases := ExtractPhases("__phase_done: bogus\nkept")
	if len(phases) != 0 {
		t.Errorf("phases: got %v, want empty", phases)
	}
	if want := "kept"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
}

func TestExtractPhases_NoMarker(t *testing.T) {
	cleaned, phases := ExtractPhases("just prose")
	if len(phases) != 0 || cleaned != "just prose" {
		t.Errorf("got (%q, %v), want (\"just prose\", [])", cleaned, phases)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/refine/ -run TestExtractPhases -v`
Expected: FAIL — `ExtractPhases` undefined.

- [ ] **Step 3: Implement phases.go**

Create `server/internal/refine/phases.go`:

```go
package refine

import (
	"regexp"
	"strings"
)

// Phases is the canonical, ordered set of refinement phases. SSOT for the
// backend; the frontend PHASE_LABELS keys must match these strings exactly.
var Phases = []string{"analysis", "spec", "implementation_plan", "approval"}

// phaseDoneRE matches an inline phase-completion marker, e.g. "__phase_done: spec".
var phaseDoneRE = regexp.MustCompile(`__phase_done:\s*(\w+)`)

// IsValidPhase reports whether p is one of the canonical Phases.
func IsValidPhase(p string) bool {
	for _, v := range Phases {
		if v == p {
			return true
		}
	}
	return false
}

// ExtractPhases returns the content with all "__phase_done: …" markers removed
// and the ordered list of VALID phases they declared (unknown phases ignored).
// Cleaning collapses the whitespace a stripped marker line leaves behind so the
// persisted content reads naturally.
func ExtractPhases(s string) (cleaned string, phases []string) {
	for _, m := range phaseDoneRE.FindAllStringSubmatch(s, -1) {
		if IsValidPhase(m[1]) {
			phases = append(phases, m[1])
		}
	}
	cleaned = phaseDoneRE.ReplaceAllString(s, "")
	// Tidy: drop blank lines left by a removed marker, then trim.
	lines := strings.Split(cleaned, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" && len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
			continue // collapse consecutive blanks
		}
		kept = append(kept, ln)
	}
	cleaned = strings.TrimSpace(strings.Join(kept, "\n"))
	return cleaned, phases
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/refine/ -run TestExtractPhases -v`
Expected: PASS (all 4). If `TestExtractPhases_MultipleMarkersOrderedAndAllStripped` fails on whitespace, adjust the blank-collapsing so `"__phase_done: analysis\nbody\n__phase_done: spec\n"` cleans to exactly `"body"`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/refine/phases.go server/internal/refine/phases_test.go
git commit -m "feat(refine): phase SSOT + ExtractPhases marker parser"
```

---

### Task A2: Runner persists `turn.phase` + cleaned content

**Files:**
- Modify: `server/internal/refine/runner.go`
- Modify: `server/internal/refine/runner_test.go`

- [ ] **Step 1: Extend fakeTurns to capture Phase, write the failing test**

In `server/internal/refine/runner_test.go`, the existing `fakeTurns.Create` appends `repo.CreateTurnInput` to `created`. `CreateTurnInput` has a `Phase *string` field, so the captured input already records it — no struct change needed. Add this test:

```go
func TestRunner_Start_PersistsPhaseAndStripsMarker(t *testing.T) {
	turns := &fakeTurns{}
	spawn := func(_ context.Context, _ SpawnConfig, _ *ent.Spawner) (<-chan string, error) {
		ch := make(chan string, 2)
		ch <- "Here is the spec."
		ch <- "__phase_done: spec"
		close(ch)
		return ch, nil
	}
	r := NewRunner(turns, spawn)
	out, _ := r.Start("task-1", SpawnConfig{}, nil)
	for range out { //nolint:revive
	}
	waitFor(t, func() bool { s, _ := r.State("task-1"); return s == StatusDone }, "done")

	turns.mu.Lock()
	defer turns.mu.Unlock()
	var asst *repo.CreateTurnInput
	for i := range turns.created {
		if turns.created[i].Role == "assistant" {
			asst = &turns.created[i]
		}
	}
	if asst == nil {
		t.Fatal("no assistant turn persisted")
	}
	if asst.Phase == nil || *asst.Phase != "spec" {
		t.Errorf("persisted phase: got %v, want spec", asst.Phase)
	}
	if strings.Contains(asst.Content, "__phase_done") {
		t.Errorf("persisted content still contains marker: %q", asst.Content)
	}
}
```

Add `"strings"` to the test imports if not present.

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/refine/ -run TestRunner_Start_PersistsPhase -v`
Expected: FAIL — phase is nil (Runner does not set it yet).

- [ ] **Step 3: Update the Runner persist step**

In `server/internal/refine/runner.go`, replace the `default:` branch of the persist `switch` (currently creating the turn with `Content: resp`) with:

```go
		default:
			cleaned, phases := ExtractPhases(resp)
			in := repo.CreateTurnInput{
				TaskID:  taskID,
				Role:    "assistant",
				Content: cleaned,
			}
			if len(phases) > 0 {
				last := phases[len(phases)-1]
				in.Phase = &last
			}
			_, _ = r.turns.Create(context.Background(), in)
			r.setState(taskID, StatusDone, "")
		}
```

The `resp == ""` and `[ERROR]` branches are unchanged. (Note: an output that is ONLY a marker, e.g. `"__phase_done: spec"`, cleans to `""` — that is acceptable; the phase is still recorded and an empty-content assistant turn is harmless. Do NOT reclassify it as failed.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/refine/ -v`
Expected: PASS — all runner + phases tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/refine/runner.go server/internal/refine/runner_test.go
git commit -m "feat(refine): persist refinement phase + strip markers from stored content"
```

---

### Task A3: Prompt the agent to emit phase markers

**Files:**
- Modify: `server/internal/refine/spawner.go`

- [ ] **Step 1: Update the prompt template (no unit test — prompt text)**

In `server/internal/refine/spawner.go`, the `promptTmpl` `<system>` block currently ends before the `</system>` tag. Extend it to instruct phase markers. Replace the template string's `<system>…</system>` portion with:

```go
var promptTmpl = template.Must(template.New("refinement").Parse(`<system>
You are a refinement assistant helping to clarify and improve a software task.
Task: {{.TaskTitle}}
{{- if .TaskDescription}}
Description: {{.TaskDescription}}
{{- end}}

Guide the task through these phases in order: analysis, spec, implementation_plan, approval.
When you have completed a phase, output a line on its own containing exactly:
__phase_done: <phase>
using one of these keys: analysis, spec, implementation_plan, approval.
When the task is clarified enough for the user to confirm, output:
__phase_done: approval
Emit each marker at most once, only when that phase is genuinely complete. Do not
explain the markers to the user.
</system>
{{range .History}}
<{{.Role}}>{{.Content}}</{{.Role}}>
{{end}}
<user>{{.UserMessage}}</user>`))
```

- [ ] **Step 2: Build + verify the template still parses**

Run: `cd server && go build ./... && go test ./internal/refine/ -run TestRun -v`
Expected: build clean; existing spawner/run tests still PASS (template.Must would panic at init if malformed — a passing test confirms it parses).

- [ ] **Step 3: Commit**

```bash
git add server/internal/refine/spawner.go
git commit -m "feat(refine): instruct refinement agent to emit phase markers"
```

---

## PHASE B — Frontend

### Task B1: Parse phase markers live in `sendMessage`

**Files:**
- Modify: `src/composables/useRefinementChat.ts`
- Test: `src/composables/__tests__/useRefinementChat.test.ts`

- [ ] **Step 1: Write the failing test**

Add to `src/composables/__tests__/useRefinementChat.test.ts`:

```ts
it('parses a streamed __phase_done marker into completedPhases + approvalReady, hidden from content', async () => {
  const frames = 'data: Looks ready.\n\ndata: __phase_done: approval\n\n'
  const chunks = [new TextEncoder().encode(frames)]
  let i = 0
  const reader = {
    read: async () => i < chunks.length
      ? { done: false, value: chunks[i++] }
      : { done: true, value: undefined },
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, body: { getReader: () => reader } }))

  const chat = useRefinementChat(() => 'task-1')
  await chat.sendMessage('is it ready?')

  expect(chat.approvalReady.value).toBe(true)
  expect(chat.completedPhases.value.has('approval')).toBe(true)
  const assistant = chat.messages.value.at(-1)
  expect(assistant?.content).not.toContain('__phase_done')
  expect(assistant?.content).toContain('Looks ready.')
})
```

This requires `completedPhases` to be exposed on the composable's return. It is not today — Step 3 adds it.

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/composables/__tests__/useRefinementChat.test.ts`
Expected: FAIL — `chat.completedPhases` is undefined (not returned) and/or the marker is treated as text.

- [ ] **Step 3: Implement marker parsing + expose `completedPhases`/`PHASE_ORDER`**

In `src/composables/useRefinementChat.ts`:

(a) Add a phase-order export and a shared marker regex near `PHASE_LABELS`:

```ts
export const PHASE_ORDER = ['analysis', 'spec', 'implementation_plan', 'approval'] as const
const PHASE_DONE_LINE_RE = /__phase_done:\s*(\w+)/
```

(b) In `sendMessage`, inside the per-`part` loop, BEFORE the JSON-parse fallback block, intercept marker frames. Replace the section that computes `const raw = dataLine.slice(5).trimStart()` … through the `data.text` handling with:

```ts
          const raw = dataLine.slice(5).trimStart()

          // Phase marker: record progress, never show it in the message.
          const phaseMatch = raw.match(PHASE_DONE_LINE_RE)
          if (phaseMatch && PHASE_LABELS[phaseMatch[1]]) {
            const phase = phaseMatch[1]
            completedPhases.value.add(phase)
            messages.value[assistantIdx].phase = phase
            if (phase === 'approval')
              approvalReady.value = true
            // Strip the marker from the raw line; if nothing else remains, skip.
            const remainder = raw.replace(PHASE_DONE_LINE_RE, '').trim()
            if (remainder === '')
              continue
          }

          // The backend forwards `claude -p` output line-by-line as `data: <line>`.
          // Default claude output is plain text, not JSON — fall back to raw text.
          let data: any
          try {
            data = JSON.parse(raw)
            if (typeof data !== 'object' || data === null)
              data = { text: raw }
          }
          catch {
            data = { text: raw }
          }
          const event = eventLine ? eventLine.slice(7) : 'message'

          if (event === 'phase_change' && data.phase) {
            completedPhases.value.add(data.phase)
            messages.value[assistantIdx].phase = data.phase
            if (data.phase === 'approval')
              approvalReady.value = true
          }
          else if (data.text) {
            // Guard: never let a marker leak into displayed content.
            const text = String(data.text).replace(PHASE_DONE_LINE_RE, '')
            if (text) {
              assistantContent += text
              messages.value[assistantIdx].content = assistantContent
            }
          }
```

(c) Add `completedPhases` (already declared as a ref in the composable) and `PHASE_ORDER` to the returned object:

```ts
  return {
    messages,
    isStreaming,
    error,
    approvalReady,
    completedPhases,
    loadHistory,
    sendMessage,
    confirm,
    phaseLabel,
  }
```

(`PHASE_ORDER` is a module-level export, consumed directly by components — no need to return it.)

- [ ] **Step 4: Run to verify it passes + full composable suite**

Run: `pnpm test src/composables/__tests__/useRefinementChat.test.ts && pnpm typecheck`
Expected: PASS, typecheck clean.

- [ ] **Step 5: Lint + commit**

Run: `pnpm exec eslint src/composables/useRefinementChat.ts`
Then:

```bash
git add src/composables/useRefinementChat.ts src/composables/__tests__/useRefinementChat.test.ts
git commit -m "feat(refine): parse phase markers from the live stream"
```

---

### Task B2: Phase stepper in `RefineStatusPanel`

**Files:**
- Modify: `src/components/RefineStatusPanel.vue`
- Test: `src/components/__tests__/RefineStatusPanel.test.ts`

- [ ] **Step 1: Write the failing tests**

Add to `src/components/__tests__/RefineStatusPanel.test.ts`:

```ts
it('renders a phase stepper marking completed phases done', () => {
  const w = mount(RefineStatusPanel, {
    props: { status: 'running', error: null, lastOutput: '', completedPhases: ['analysis', 'spec'] },
  })
  const text = w.text()
  expect(text).toContain('Analysis')
  expect(text).toContain('Spec')
  expect(text).toContain('Implementation Plan')
  expect(text).toContain('Approval')
  // analysis + spec are done; the first incomplete (implementation_plan) is current.
  expect(w.findAll('[data-phase-state="done"]').length).toBe(2)
  expect(w.findAll('[data-phase-state="current"]').length).toBe(1)
})

it('marks no phase current when status is done', () => {
  const w = mount(RefineStatusPanel, {
    props: { status: 'done', error: null, lastOutput: 'x', completedPhases: ['analysis', 'spec', 'implementation_plan', 'approval'] },
  })
  expect(w.findAll('[data-phase-state="current"]').length).toBe(0)
  expect(w.findAll('[data-phase-state="done"]').length).toBe(4)
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/components/__tests__/RefineStatusPanel.test.ts`
Expected: FAIL — no stepper / `completedPhases` prop unknown.

- [ ] **Step 3: Add the prop + stepper**

In `src/components/RefineStatusPanel.vue`, import the phase order and labels and add the prop. Update `<script setup>`:

```ts
import { computed, ref } from 'vue'
import { PHASE_ORDER } from '../composables/useRefinementChat'

const PHASE_LABELS: Record<string, string> = {
  analysis: 'Analysis',
  spec: 'Spec',
  implementation_plan: 'Implementation Plan',
  approval: 'Approval',
}

const props = defineProps<{
  status: 'idle' | 'running' | 'done' | 'failed' | null
  error: string | null
  lastOutput: string
  completedPhases?: string[]
}>()

const expanded = ref(false)
const show = computed(() => props.status === 'running' || props.status === 'done' || props.status === 'failed')

const done = computed(() => new Set(props.completedPhases ?? []))
const currentPhase = computed(() => {
  if (props.status !== 'running')
    return null
  return PHASE_ORDER.find(p => !done.value.has(p)) ?? null
})
function phaseState(p: string): 'done' | 'current' | 'pending' {
  if (done.value.has(p))
    return 'done'
  if (p === currentPhase.value)
    return 'current'
  return 'pending'
}
const showStepper = computed(() => props.status === 'running' || done.value.size > 0)

const badge = computed(() => {
  switch (props.status) {
    case 'running': return { text: 'Running…', cls: 'text-blue-600 dark:text-blue-300' }
    case 'done': return { text: 'Done', cls: 'text-green-600 dark:text-green-400' }
    case 'failed': return { text: 'Failed', cls: 'text-red-600 dark:text-red-400' }
    default: return { text: '', cls: '' }
  }
})
```

In the `<template>`, inside the outer `v-if="show"` container, AFTER the badge button and BEFORE the error/lastOutput block, add the stepper:

```html
    <ol v-if="showStepper" class="flex flex-wrap gap-x-3 gap-y-1 mt-2 text-[11px]">
      <li
        v-for="p in PHASE_ORDER"
        :key="p"
        :data-phase-state="phaseState(p)"
        class="flex items-center gap-1"
        :class="{
          'text-green-600 dark:text-green-400': phaseState(p) === 'done',
          'text-blue-600 dark:text-blue-300 font-semibold': phaseState(p) === 'current',
          'text-muted': phaseState(p) === 'pending',
        }"
      >
        <span>{{ phaseState(p) === 'done' ? '✓' : phaseState(p) === 'current' ? '◷' : '○' }}</span>
        <span>{{ PHASE_LABELS[p] }}</span>
      </li>
    </ol>
```

Add `PHASE_ORDER` to the template scope: since it is imported in `<script setup>`, it is available in the template directly. `PHASE_LABELS` and `phaseState` likewise.

- [ ] **Step 4: Run to verify it passes (incl. the original 4 panel tests)**

Run: `pnpm test src/components/__tests__/RefineStatusPanel.test.ts && pnpm typecheck`
Expected: PASS (6 total), typecheck clean.

- [ ] **Step 5: Lint + commit**

Run: `pnpm exec eslint src/components/RefineStatusPanel.vue`
Then:

```bash
git add src/components/RefineStatusPanel.vue src/components/__tests__/RefineStatusPanel.test.ts
git commit -m "feat(refine): phase stepper in the refinement status panel"
```

---

### Task B3: Derive + pass `completedPhases` in TaskModal

**Files:**
- Modify: `src/components/TaskModal.vue`

- [ ] **Step 1: Derive completedPhases from the fetched turns**

In `src/components/TaskModal.vue`, the existing `loadLastRefineOutput(taskId)` fetches `/api/refine/{taskId}/turns` and sets `lastRefineOutput`. Add a `completedRefinePhases` ref and populate it from the same fetch. Read the function and adapt — it currently does roughly:

```ts
const res = await fetch(`/api/refine/${taskId}/turns`)
if (!res.ok) return
const turns = await res.json() as Array<{ role: string, content: string }>
const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
lastRefineOutput.value = lastAssistant?.content ?? ''
```

Change the type to include `phase` and collect phases:

```ts
const completedRefinePhases = ref<string[]>([])

async function loadLastRefineOutput(taskId: string) {
  try {
    const res = await fetch(`/api/refine/${taskId}/turns`)
    if (!res.ok)
      return
    const turns = await res.json() as Array<{ role: string, content: string, phase?: string | null }>
    const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
    lastRefineOutput.value = lastAssistant?.content ?? ''
    completedRefinePhases.value = turns.flatMap(t => (t.phase ? [t.phase] : []))
  }
  catch { /* leave empty */ }
}
```

(Keep the existing `lastRefineOutput` ref declaration; only add `completedRefinePhases` and the one assignment.)

- [ ] **Step 2: Pass the prop to the panel**

Update the `<RefineStatusPanel … />` usage in the concept block to add:

```html
:completed-phases="completedRefinePhases"
```

- [ ] **Step 3: Typecheck + lint + build**

Run: `pnpm typecheck && pnpm exec eslint src/components/TaskModal.vue && pnpm build`
Expected: clean; build succeeds.

- [ ] **Step 4: Revert the rebuilt SPA artifact (tracked placeholder)**

`pnpm build` overwrites the tracked `server/frontend/dist/index.html` placeholder. Do NOT commit it:

Run: `git checkout -- server/frontend/dist/index.html`

- [ ] **Step 5: Commit**

```bash
git add src/components/TaskModal.vue
git commit -m "feat(refine): show phase stepper in the concept-step panel"
```

---

## Final verification

- [ ] **Go suite:** `cd server && go test ./...` → all PASS (SSH signing key unlocked for worktree tests).
- [ ] **Frontend suite:** `pnpm test` → refine/panel tests PASS (the pre-existing `useRemotes.test.ts` failure is unrelated to this work — do not fix it here).
- [ ] **Typecheck + scoped lint:** `pnpm typecheck && pnpm exec eslint src/composables/useRefinementChat.ts src/components/RefineStatusPanel.vue src/components/TaskModal.vue`
- [ ] **Manual smoke (task dev):** open a concept task → the panel shows the 4-phase stepper; as the refinement agent emits `__phase_done:` markers, completed phases tick green in both the panel and the chat dividers; reaching `approval` enables the Confirm button; close + reopen → completed phases persist.

## Notes for the implementer

- **SSOT:** the four phase keys live in `refine.Phases` (Go) and `PHASE_ORDER`/`PHASE_LABELS` (TS). They MUST stay identical: `analysis`, `spec`, `implementation_plan`, `approval`.
- **Marker leakage:** the marker must never reach displayed content. It is stripped in three places defensively — backend persist (`ExtractPhases`), the live stream parser, and `cleanContent`'s display-time `PHASE_DONE_RE`. That redundancy is intentional.
- **Layering:** `phases.go` is pure (regexp + strings) — no new imports beyond stdlib. The Runner already owns persistence; no new cross-package dependency.
- **`completedPhases` is a `Set` in the composable** but a `string[]` prop on the panel and in TaskModal — convert where they meet (the composable exposes the Set for the chat; TaskModal builds an array from turns).
