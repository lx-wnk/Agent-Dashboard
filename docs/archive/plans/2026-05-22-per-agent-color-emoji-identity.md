# Per-Agent Color/Emoji Identity (IP-1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
> **Source spec:** `docs/superpowers/specs/2026-05-22-per-agent-color-emoji-identity-design.md`

**Goal:** Ship per-agent color stripe + emoji identity, persisted in `localStorage`, surfaced across all agent-display components.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Vitest + `@vue/test-utils`, Tailwind CSS.

---

## File Structure

| File | Responsibility |
|---|---|
| `src/utils/agentIdentityPalette.ts` (create) | 12-entry palette constant + fnv1a helper |
| `src/composables/useAgentIdentity.ts` (create) | localStorage R/W, debounce, defaults, cross-tab sync |
| `src/composables/useAgentIdentity.test.ts` (create) | Composable tests |
| `src/components/IdentityStripe.vue` (create) | 4px left-edge color bar |
| `src/components/IdentityBadge.vue` (create) | `{emoji} {slug}` inline span |
| `src/components/IdentityEditor.vue` (create) | Color swatch grid + emoji picker |
| `src/components/IdentityStripe.test.ts` (create) | Stripe color tests |
| `src/components/IdentityBadge.test.ts` (create) | Badge rendering tests |
| `src/components/IdentityEditor.test.ts` (create) | Editor interaction tests |
| `src/components/AgentRow.vue` (modify) | Wire stripe + badge |
| `src/components/AgentCard.vue` (modify) | Wire stripe + badge |
| `src/components/AgentModal.vue` (modify) | Header stripe + badge + editor toggle |
| `src/components/SubAgentRow.vue` (modify) | Badge in slug column |
| `src/components/TaskCard.vue` (modify) | Badge when linked-agent slug present |

---

### Task 1: Palette + composable

- [ ] **Step 1: Create `src/utils/agentIdentityPalette.ts`**

Export `AGENT_IDENTITY_PALETTE` (12 colors, Tailwind 500-tier hex) and a pure `fnv1a(s: string): number` hash function. Add a `defaultColorFor(slug: string): string` helper using palette + hash.

- [ ] **Step 2: Create `src/composables/useAgentIdentity.ts`**

```ts
const STORAGE_KEY = 'agent-identities'
const state = ref<Record<string, Partial<AgentIdentity>>>(loadFromStorage())

window.addEventListener('storage', e => {
  if (e.key === STORAGE_KEY) state.value = loadFromStorage()
})

const persist = useDebounceFn(() => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state.value))
}, 200)
```

Expose `identity(slug)`, `setIdentity(slug, partial)`, `clearIdentity(slug)`. `identity()` returns a `ComputedRef<AgentIdentity>` with default fallback.

- [ ] **Step 3: Composable tests**

`useAgentIdentity.test.ts` — happy path R/W, debounce timing (use `vi.useFakeTimers`), default derivation reproducibility, `storage` event handling.

---

### Task 2: Display components

- [ ] **Step 1: `IdentityStripe.vue`**

Single-prop component (`slug: string`). Renders `<span class="block w-1 self-stretch" :style="{background: color}" />`. Color from `useAgentIdentity().identity(slug).color`.

- [ ] **Step 2: `IdentityBadge.vue`**

Two-prop component (`slug: string`, optional `emojiOnly: boolean`). Renders `<span class="inline-flex items-center gap-1"><span v-if="emoji">{{ emoji }}</span><span v-if="!emojiOnly">{{ slug }}</span></span>`.

- [ ] **Step 3: Component tests**

Stripe: assert background-color matches palette derivation. Badge: assert emoji rendered only when set, slug rendered unless `emojiOnly`.

---

### Task 3: Editor component

- [ ] **Step 1: `IdentityEditor.vue`**

Props: `slug: string`. Internal layout:
- Top: live preview row (`<IdentityStripe>` + `<IdentityBadge>` over a gray strip).
- Middle: 12-cell color grid (3×4). Active cell has ring outline.
- Below grid: emoji `<input maxlength="2">` + 16-button shortlist (`🚀 🐛 🧪 ✨ 🔐 📦 📝 🎨 🛠️ 🧹 🔥 ⚡ 🦀 🐍 🦄 🤖`).
- Footer: "Reset to default" link → calls `clearIdentity(slug)`.

Click on swatch → `setIdentity(slug, {color})`. Emoji input change → `setIdentity(slug, {emoji})`.

- [ ] **Step 2: Editor tests**

Mount editor, click a swatch → identity store updated. Type emoji → store updated. Click reset → override cleared, default returns.

---

### Task 4: Integrate into agent-display components

For each file below: prepend `<IdentityStripe :slug="agent.slug" />` to the row container (set the row to `flex` so the 4px stripe takes the full height), and replace the slug text node with `<IdentityBadge :slug="agent.slug" />`.

- [ ] `AgentRow.vue`
- [ ] `AgentCard.vue`
- [ ] `AgentModal.vue` header — also add the pencil button next to the badge that toggles `<IdentityEditor>` in a popover.
- [ ] `SubAgentRow.vue` — badge only, no stripe.
- [ ] `TaskCard.vue` — only when `task.linkedAgentSlug` (or whichever current field carries the linked-agent slug — check `useTasks.ts` for the canonical name) is present.

- [ ] Visually verify in `task dev`: spawn or open a few agents, set distinct identities, switch views (list/card/kanban), confirm stripes + badges propagate.

---

### Task 5: Wire-up sanity tests

- [ ] **Step 1: Component-integration test for `AgentModal`**

Mount with a mock agent, click pencil, change color, assert preview reflects + `setIdentity` called with new color.

- [ ] **Step 2: E2E (optional, only if low-cost)**

Single Playwright spec navigating to dashboard, opening an agent, setting an identity, reloading, asserting persistence.

---

### Task 6: Memory updates

- [ ] Mark IP-1 done in `.agent-context/memory/todo.md`.
- [ ] Append entry to `.agent-context/memory/log.md`.

---

## Risk Register

| Risk | Mitigation |
|---|---|
| Emoji grapheme width inconsistent across OSes | Use `<span>` flex layout, no fixed width |
| localStorage quota exceeded | Single key, JSON map — at 100 agents × ~30 bytes = 3KB, far under quota |
| Cross-tab race when both tabs edit | Last-writer-wins via `storage` event is acceptable for single-user case |
