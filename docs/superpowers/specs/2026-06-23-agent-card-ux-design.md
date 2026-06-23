# Agent Card UX — Design Spec

**Date:** 2026-06-23
**Status:** Approved (design) — pending implementation plan
**Topic:** Fix and polish the roster agent card (`AgentCard.vue`): a real output preview, reliable card-click → modal, a readable/prominent project name, a glance-friendly metric row with full detail on hover/in-modal, and a consistently bottom-docked prompt input.

---

## 1. Problem & Goals

The agent card (`src/components/AgentCard.vue`, rendered in a grid by `AgentCardGrid.vue`, selected up through `App.vue`) has five concrete UX problems:

1. **"No output yet" is wrong for active agents.** The body renders `agent.lastOutput`, which the backend populates **only** from BTW ("Between-Tool-Work") messages (`server/internal/parser/parser.go:802`). An agent in a pure tool-use loop has an empty `lastOutput` → shows "No output yet" despite real activity (tokens, uptime).
2. **Clicking the card often does nothing.** The click target is an `absolute inset-0` button that is the **first** child of the card; every later sibling (header, output, subagents) paints over it, so clicks on most of the card miss the button. The user expects clicking the card to open the session/agent modal.
3. **Project name is unreadable.** `friendlyProjectName(agent.projectName)` is shown, but in an overcrowded single header row (badge + emoji + name + provider + model + cost + health) it truncates to ~3 chars ("Age…").
4. **The metric row is cryptic.** `$6.09 · 1.85M tok · 28m · 27m ago` / `$0.21/min · W $4.64 R $0.08` has no labels — a viewer can't tell total-cost from burn-rate, uptime from last-activity, or what W/R mean.
5. **The prompt input position is inconsistent.** When a card has an active-subagent section it pushes the prompt down; cards without it dock the prompt higher. The prompt should always be at the bottom; the output area absorbs the difference.

**Goals:** make the card glance-readable, never falsely empty, reliably clickable, with the project prominent and dense detail moved to hover/modal.

**Non-goals:** a full visual redesign; changing the grid/grouping; touching the modal beyond ensuring it shows the labeled metric breakdown.

---

## 2. Design

### A. Output body — real preview, never falsely empty

**Backend** (`server/internal/parser/parser.go`): track the **last non-empty assistant text block across the parsed tail**, not just the text block of the final message. Assign it to `SessionData.LastOutput`. Today `LastOutput` is set from a single message's `case "text"` and is lost when the most recent assistant message is tool-only. The fix: keep a running "last assistant text" as the tail is parsed and use it for `LastOutput` (BTW handling and `LastBtw`/`CurrentAction` stay as-is). Secrets continue to be scrubbed (`scrubSecrets`).

**Frontend** (`AgentCard.vue` body) — fallback chain, so an active agent never shows "No output yet" unless truly nothing exists:
1. `agent.lastOutput` → show it (existing behavior).
2. else `agent.currentAction` → `▶ running: {currentAction}` (currentAction is the last tool name).
3. else `agent.lastTools?.[0]` → `▶ {lastTools[0]}`.
4. else `No output yet` (only when all are empty).

The fallback lines render in the muted/italic style the empty state uses today.

### B. Reliable card-click → session modal

The click→modal wiring already exists (`select` event → `App.vue selectAgent` → `AgentModal` opens). The bug is the click target. Fix:
- Remove reliance on the `absolute inset-0` button for mouse clicks. Put `@click` on the card root (`AppCard`, which already has an `interactive` prop) emitting `select`.
- Interactive children — `PromptInput`, the dismiss `✕`, and the subagent-output expand toggle — get `@click.stop` so they don't bubble to the card select.
- Keep keyboard accessibility: retain a focusable control with the existing `aria-label` and `data-testid="agent-card-open"` (e.g. keep the hidden button purely as the keyboard/focus affordance, or move `role="button"` + `tabindex="0"` + `@keydown.enter/space` onto the card root). The `data-testid` must survive for existing tests.

### C. Header — project prominent, readable

Restructure the header from one crowded row into a clear hierarchy:
- **Primary row:** status badge · identity emoji · **project name** (gets the available width; truncates with ellipsis + a `title`/tooltip only when genuinely long) · provider badge · model chip.
- **Meta row** (see D): the three key metrics + health chip + info affordance.

### D. Metric row — three key metrics on the card, full breakdown on hover/in modal

On the card, show only the **three most important** metrics, lightly unit-labeled:
- **cost** (`$6.09`), **tokens** (`1.85M tok`), **uptime** (`28m`)
- plus the **health chip** and an **ⓘ** affordance.

Everything else — **last activity** (e.g. "27m ago"), **burn rate** (`$0.21/min`), **cache write/read cost** (`W $4.64` / `R $0.08`) — moves off the always-visible card into:
- a **hover popover** on the ⓘ (a small `MetricsPopover` showing every metric **with an explicit label**), and
- the **AgentModal** (full labeled breakdown — verify it already shows these; if not, add a labeled metrics block there).

Target card layout:

```
┌────────────────────────────────────────────────┐
│ ● Working 🤖 Agent-Dashboard       [opus-4.8] 79 │  primary: project prominent, model chip, health
│ $6.09 · 1.85M tok · 28m                       ⓘ  │  meta: 3 key metrics + info
├────────────────────────────────────────────────┤
│ <last assistant text …>                          │  output (flex-grow)
│ ▶ running: Bash      (fallback when no text)      │
│                                                  │
├────────────────────────────────────────────────┤
│ 1 ACTIVE SUBAGENT  …                              │  optional, between output and prompt
├────────────────────────────────────────────────┤
│ › Enter prompt …                            [↵]  │  always bottom-docked
└────────────────────────────────────────────────┘
```

ⓘ hover popover (labeled):
```
Uptime         28m
Last activity  27m ago
Burn rate      $0.21/min
Cache write    $4.64
Cache read     $0.08
```

### E. Prompt always docked at the bottom

Make the card a flex column with a consistent height:
- header (fixed) → **output body `flex-1`** (grows to fill) → optional subagent section (auto height) → **PromptInput pinned to the bottom** (`mt-auto`, or as the final flex child).
- Today the output body is a fixed `h-[150px]`; change it to flex-grow so that when there is no subagent section the output area is larger and the prompt still sits at the same bottom position across all cards.

---

## 3. Components & Data Flow

| Unit | Change |
|---|---|
| `server/internal/parser/parser.go` | Track last non-empty assistant text across the tail → `LastOutput` |
| `src/components/AgentCard.vue` | Output fallback chain; root `@click` + `@click.stop` on interactive children; header restructure; 3-metric meta row + ⓘ; flex-column with bottom-docked prompt |
| `src/components/MetricsPopover.vue` (new, small) | Labeled breakdown shown on ⓘ hover (uptime, last activity, burn, cache W/R) |
| `src/components/AgentModal.vue` | Ensure a labeled metrics block exists (add if missing) |
| `src/components/AgentCardGrid.vue`, `App.vue` | No change — they already forward `select` and open the modal |

Data flow is unchanged: SSE `Agent[]` → grid → card; card `select` → `App.selectAgent` → `AgentModal`. The only new backend field semantics is a better-populated `lastOutput`.

---

## 4. Error / Edge Handling

- **Truly idle/new agent** (no text, no action, no tools): shows "No output yet" — the only legitimate case.
- **Secrets:** `LastOutput` keeps passing through `scrubSecrets`.
- **Long project names:** ellipsis + `title` tooltip; the primary row never collapses the project to a few chars (model/health are chips that don't steal its width).
- **`machine` (remote) agents:** keep the existing `v-if="!agent.machine"` guard on `PromptInput`; for remote cards with no prompt, the output simply fills to the bottom.
- **ⓘ on touch devices:** the popover opens on tap as well as hover (no hover on touch); since card-click opens the full modal, this is a convenience, not the only path.

---

## 5. Testing

- **Backend (`parser` test):** a session whose tail ends in a tool-only assistant message still yields `LastOutput` = the preceding assistant text (regression for the core bug).
- **Frontend (Vitest, `AgentCard`):**
  - body fallback: `lastOutput` empty + `currentAction="Bash"` → renders `running: Bash`, not "No output yet"; all empty → "No output yet".
  - click: clicking the card root emits `select`; clicking inside `PromptInput`/dismiss does **not** emit `select` (`@click.stop`).
  - layout: prompt input is the last child / bottom-docked regardless of subagent presence.
  - ⓘ popover contains labeled uptime/last-activity/burn/cache values.
- Existing `data-testid="agent-card-open"` keyboard path still works.

---

## 6. Files Touched (orientation)

**Modified:** `server/internal/parser/parser.go`, `src/components/AgentCard.vue`, `src/components/AgentModal.vue` (metrics block if missing), plus their tests.
**New:** `src/components/MetricsPopover.vue`, `AgentCard` Vitest cases, a parser test.
**Docs:** `CHANGELOG.md` (Fixed: false "No output yet"; Changed: card layout/metrics).

**Branch:** `feat/agent-card-ux`, off `main` — independent of PR #213/#214.

---

## Self-review note

- §A is genuinely two-part (backend source + frontend fallback) — both required, documented.
- §B's keyboard/a11y retention is explicit so the redesign doesn't regress focus/testability.
- ⓘ behavior is fixed as a hover/tap popover (not "open modal") to avoid the spec's earlier ambiguity; the modal remains the full-detail path via card-click.
