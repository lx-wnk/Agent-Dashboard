# Per-Agent Color/Emoji Identity (IP-1)

**Date:** 2026-05-22
**Status:** SUPERSEDED — feature already shipped in `upcoming` HEAD via `src/composables/useAgentIdentity.ts` (inline COLORS/EMOJIS palette) + integration in `AgentRow.vue`/`AgentCard.vue`/`AgentModal.vue`. Spec was written against a stale roadmap audit that missed the existing inline impl. Only the explicit `IdentityEditor.vue` (§C.3) remains as an optional follow-up.
**Roadmap ref:** `2026-05-09-agent-dashboard-unified-roadmap-design.md` → IP-1 (P3)

## Problem

The dashboard shows all agents using the same neutral styling. With 10+ concurrent agents across views, a user has no fast visual cue to recognize "the docs-rewrite agent" vs "the auth refactor agent" at a glance. Slug + title scan demands reading; a colored stripe + emoji prefix shifts that to peripheral vision.

## Goals

- User can assign a color + emoji to any agent slug.
- Color appears as a left-edge stripe on `AgentRow`, `AgentCard`, and the `AgentModal` header.
- Emoji prefixes the agent slug everywhere the slug is shown.
- Assignment persists across reloads.
- Sensible default per slug when unset.

## Non-Goals

- No backend persistence in v1. localStorage is the canonical store; cross-machine sync stays out.
- No team-shared identities. Single-user scope.
- No automatic palette generation from slug hashes — defaults are deterministic (slug hash → palette pick) but the user override always wins.
- No drag-to-recolor on the kanban board.

## Decisions

| # | Question | Choice |
|---|---|---|
| 1 | Storage location | `localStorage` key `agent-identities` (JSON map slug → {color, emoji}) |
| 2 | Default behavior | Deterministic hash(slug) → palette index, no emoji |
| 3 | Edit UX | Pencil icon on `AgentModal` header opens inline editor (color swatch grid + emoji picker) |
| 4 | Color palette | 12-entry curated palette (Tailwind 500-tier hex) to avoid garish picks |
| 5 | Emoji source | Native browser emoji picker fallback + small built-in shortlist (16 popular work emojis) |
| 6 | Migration | None — feature is additive |

## Section A — Data Model

`src/composables/useAgentIdentity.ts` exposes:

```ts
export interface AgentIdentity {
  color: string   // hex
  emoji: string   // single grapheme; '' = no emoji
}

export function useAgentIdentity() {
  return {
    identity: (slug: string) => ComputedRef<AgentIdentity>,
    setIdentity: (slug: string, value: Partial<AgentIdentity>) => void,
    clearIdentity: (slug: string) => void,
  }
}
```

Storage is a single localStorage entry, JSON-encoded. Writes debounce (200ms) to avoid thrash on rapid edits. A `storage` event listener picks up cross-tab writes.

Default color derivation (no override): `palette[fnv1a(slug) % palette.length]`.

## Section B — Palette

Twelve curated colors in `src/utils/agentIdentityPalette.ts`:

```ts
export const AGENT_IDENTITY_PALETTE = [
  '#ef4444', '#f97316', '#f59e0b', '#eab308',
  '#84cc16', '#22c55e', '#10b981', '#14b8a6',
  '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899',
] as const
```

## Section C — Components

### C.1 `IdentityStripe.vue` (new)

Renders a 4px left-edge colored bar. Reused by `AgentRow`, `AgentCard`. Props: `slug`.

### C.2 `IdentityBadge.vue` (new)

Renders `{emoji} {slug}` as inline span. Reused everywhere the slug is shown (`AgentRow`, `AgentCard`, `AgentModal` header, `SubAgentRow`, kanban `TaskCard` when linked to agent slug).

### C.3 `IdentityEditor.vue` (new)

Inline editor mounted from `AgentModal` header. Color swatch grid (12 cells) + emoji input with shortlist buttons + "Reset" link. Live preview at top. Wired through `useAgentIdentity().setIdentity`.

## Section D — Integration Points

| File | Change |
|---|---|
| `src/components/AgentRow.vue` | Prepend `<IdentityStripe>`; replace slug text with `<IdentityBadge>` |
| `src/components/AgentCard.vue` | Prepend `<IdentityStripe>`; replace slug text with `<IdentityBadge>` |
| `src/components/AgentModal.vue` | Header gets `<IdentityStripe>` + `<IdentityBadge>` + pencil-icon toggling `<IdentityEditor>` |
| `src/components/SubAgentRow.vue` | Slug text → `<IdentityBadge>` (no stripe — sub-agents inherit parent identity visually) |
| `src/components/TaskCard.vue` | When `task.linkedAgentSlug` set → `<IdentityBadge>` on the linked-agent row |

## Section E — Testing

- `src/composables/useAgentIdentity.test.ts` — localStorage R/W, debounce, default derivation, cross-tab event.
- `src/components/IdentityStripe.test.ts` — color derivation, override application.
- `src/components/IdentityBadge.test.ts` — emoji rendering, empty-emoji handling, slug fallback.
- `src/components/IdentityEditor.test.ts` — palette click sets color, emoji input persists, Reset clears override.

## Acceptance

User opens dashboard, clicks pencil on an `AgentModal` for slug `feat-auth`, picks a red swatch + 🔐 emoji. Stripe + emoji appear immediately on all views (modal, row, card, kanban link). Reload — identity persists. Open same dashboard in second tab — same identity (storage event).
