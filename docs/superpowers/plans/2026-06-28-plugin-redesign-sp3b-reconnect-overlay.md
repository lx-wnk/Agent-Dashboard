# Plugin Redesign SP3b — Frontend Reconnect Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the server restarts (SP3a), show a blocking "server restarting…" overlay that polls `/api/system/health` and reloads on recovery, plus a "restart required" badge + restart button for boot-wired (`auth_provider`) plugins.

**Architecture:** A `useServerReconnect` composable owns the "is the server down" signal: `triggerRestart()` POSTs the restart endpoint, `beginReconnect()` starts polling `/api/system/health`, and the first 200 after a drop triggers `window.location.reload()`. A `ServerReconnectOverlay.vue` mounted at the app root renders while reconnecting. The restart affordance lives in the plugins UI and is gated on the plugin's capabilities.

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Vitest, the existing fetch/composable conventions in `src/composables/`.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `src/utils/sse.ts` | add `RECONNECT_POLL_MS` const (SSOT for poll cadence) | Modify |
| `src/composables/useServerReconnect.ts` | own the down-signal: triggerRestart, beginReconnect, poll, reload | Create |
| `src/composables/useServerReconnect.test.ts` | Vitest: trigger/poll/reload/error | Create |
| `src/components/ServerReconnectOverlay.vue` | blocking overlay while reconnecting | Create |
| `src/components/ServerReconnectOverlay.test.ts` | renders only when reconnecting | Create |
| `src/App.vue` | mount the overlay + provide the reconnect singleton | Modify |
| Plugins panel component (the one rendering `/api/plugins` rows) | "restart required" badge + restart button for boot-wired caps | Modify |

**Commands:** `pnpm test` (Vitest), `pnpm lint`, `pnpm typecheck`. Worktree needs `pnpm i` first (no node_modules). Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

---

### Task 1: Poll-cadence constant

**Files:**
- Modify: `src/utils/sse.ts`

- [ ] **Step 1: Add the constant**

Append to `src/utils/sse.ts`:

```ts
// Cadence for polling /api/system/health while waiting for the server to come
// back after a restart (SP3 reconnect overlay).
export const RECONNECT_POLL_MS = 1_500
```

- [ ] **Step 2: Verify build + commit**

Run: `pnpm typecheck`
Expected: PASS (no usages yet; const exported).
```bash
git add src/utils/sse.ts
git commit --no-gpg-sign -m "feat: add reconnect poll cadence constant

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: `useServerReconnect` composable

**Files:**
- Create: `src/composables/useServerReconnect.ts`
- Create: `src/composables/useServerReconnect.test.ts`

- [ ] **Step 1: Read a sibling composable for conventions**

Run: `sed -n '1,40p' src/composables/usePlugins.ts` — match its import style, fetch usage (plain `fetch` vs a shared `api` helper), and how it exposes refs. Use the SAME fetch convention below.

- [ ] **Step 2: Write the failing test**

Create `src/composables/useServerReconnect.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useServerReconnect } from './useServerReconnect'

describe('useServerReconnect', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('triggerRestart posts and starts reconnecting on 202', async () => {
    ;(fetch as any).mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ status: 'restarting' }) })
    const { isReconnecting, triggerRestart } = useServerReconnect()
    await triggerRestart()
    expect(fetch).toHaveBeenCalledWith('/api/admin/restart', expect.objectContaining({ method: 'POST' }))
    expect(isReconnecting.value).toBe(true)
  })

  it('triggerRestart on non-2xx does not start reconnecting and throws', async () => {
    ;(fetch as any).mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'lockout' }) })
    const { isReconnecting, triggerRestart } = useServerReconnect()
    await expect(triggerRestart()).rejects.toThrow()
    expect(isReconnecting.value).toBe(false)
  })

  it('polls health and reloads on first 200', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload } as any)
    const { isReconnecting, beginReconnect } = useServerReconnect()
    ;(fetch as any).mockRejectedValueOnce(new Error('down')) // first poll: server still down
    ;(fetch as any).mockResolvedValueOnce({ ok: true, status: 200 }) // second poll: back
    beginReconnect()
    expect(isReconnecting.value).toBe(true)
    await vi.advanceTimersByTimeAsync(1_500) // first poll fails
    await vi.advanceTimersByTimeAsync(1_500) // second poll → reload
    expect(reload).toHaveBeenCalledOnce()
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `pnpm test src/composables/useServerReconnect.test.ts`
Expected: FAIL — module/file not found.

- [ ] **Step 4: Write minimal implementation**

Create `src/composables/useServerReconnect.ts` (use the fetch convention you confirmed in Step 1; if a shared `api` helper exists, route through it instead of bare `fetch`):

```ts
import { ref } from 'vue'
import { RECONNECT_POLL_MS } from '../utils/sse'

const isReconnecting = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | null = null

function poll() {
  pollTimer = setTimeout(async () => {
    try {
      const res = await fetch('/api/system/health')
      if (res.ok) {
        window.location.reload()
        return
      }
    }
    catch {
      // server still down — keep polling
    }
    poll()
  }, RECONNECT_POLL_MS)
}

function beginReconnect() {
  if (isReconnecting.value)
    return
  isReconnecting.value = true
  poll()
}

async function triggerRestart() {
  const res = await fetch('/api/admin/restart', {
    method: 'POST',
    headers: { 'Origin': window.location.origin },
  })
  if (!res.ok) {
    let detail = `restart failed (${res.status})`
    try {
      const body = await res.json()
      if (body?.error)
        detail = body.error
    }
    catch { /* no body */ }
    throw new Error(detail)
  }
  beginReconnect()
}

// Singleton: the down-signal is process-wide, shared by the overlay and any
// trigger site.
export function useServerReconnect() {
  return { isReconnecting, beginReconnect, triggerRestart }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm test src/composables/useServerReconnect.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add src/composables/useServerReconnect.ts src/composables/useServerReconnect.test.ts
git commit --no-gpg-sign -m "feat: add server reconnect composable

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 3: Reconnect overlay component

**Files:**
- Create: `src/components/ServerReconnectOverlay.vue`
- Create: `src/components/ServerReconnectOverlay.test.ts`

- [ ] **Step 1: Write the failing test**

Create `src/components/ServerReconnectOverlay.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { useServerReconnect } from '../composables/useServerReconnect'
import ServerReconnectOverlay from './ServerReconnectOverlay.vue'

describe('ServerReconnectOverlay', () => {
  it('is hidden when not reconnecting and shown when reconnecting', async () => {
    const { isReconnecting } = useServerReconnect()
    isReconnecting.value = false
    const wrapper = mount(ServerReconnectOverlay)
    expect(wrapper.text()).not.toContain('restarting')

    isReconnecting.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.text().toLowerCase()).toContain('restarting')
    isReconnecting.value = false // reset shared singleton
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/components/ServerReconnectOverlay.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Write minimal implementation**

Create `src/components/ServerReconnectOverlay.vue` (match the project's existing overlay/modal styling conventions — check an existing component like `AppModal.vue` for class patterns):

```vue
<script setup lang="ts">
import { useServerReconnect } from '../composables/useServerReconnect'

const { isReconnecting } = useServerReconnect()
</script>

<template>
  <div v-if="isReconnecting" class="server-reconnect-overlay" role="alertdialog" aria-live="assertive">
    <div class="server-reconnect-overlay__panel">
      <div class="server-reconnect-overlay__spinner" aria-hidden="true" />
      <p class="server-reconnect-overlay__title">
        Server is restarting…
      </p>
      <p class="server-reconnect-overlay__sub">
        Reconnecting automatically.
      </p>
    </div>
  </div>
</template>

<style scoped>
.server-reconnect-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(0 0 0 / 60%);
}
.server-reconnect-overlay__panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 2rem;
  border-radius: 0.5rem;
  background: var(--color-surface, #1e1e1e);
  color: var(--color-text, #fff);
}
.server-reconnect-overlay__spinner {
  width: 2rem;
  height: 2rem;
  border: 3px solid rgb(255 255 255 / 25%);
  border-top-color: #fff;
  border-radius: 50%;
  animation: server-reconnect-spin 0.8s linear infinite;
}
.server-reconnect-overlay__title { font-weight: 600; }
.server-reconnect-overlay__sub { font-size: 0.85rem; opacity: 0.7; }
@keyframes server-reconnect-spin { to { transform: rotate(360deg); } }
</style>
```

> Replace the inline colors/`var(--…)` with the project's actual CSS-variable tokens if they differ (grep an existing component's `<style>` for the token names). Keep the structure + `v-if`.

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test src/components/ServerReconnectOverlay.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/components/ServerReconnectOverlay.vue src/components/ServerReconnectOverlay.test.ts
git commit --no-gpg-sign -m "feat: add server reconnect overlay component

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 4: Mount overlay + restart affordance for boot-wired plugins

**Files:**
- Modify: `src/App.vue` (mount the overlay)
- Modify: the plugins panel component (restart button + "restart required" badge)

- [ ] **Step 1: Mount the overlay in App.vue**

In `src/App.vue`, import and render `ServerReconnectOverlay` near the app root (alongside other top-level singletons like modals). Add to the script imports:

```ts
import ServerReconnectOverlay from './components/ServerReconnectOverlay.vue'
```
and render it once in the template (top level, outside the routed content):
```vue
    <ServerReconnectOverlay />
```

- [ ] **Step 2: Locate the plugins panel**

Run: `grep -rln "api/plugins\|usePlugins\|PluginView\|capabilities" src/components/ src/composables/usePlugins.ts | head`
Identify the component that lists plugins (renders `/api/plugins` rows with `capabilities`). Read it to learn the row shape (`capabilities: string[]`).

- [ ] **Step 3: Add the restart-required badge + button**

In that plugins component: for a plugin whose `capabilities` includes `'auth_provider'` (boot-wired), after an activate/deactivate show a "Restart required to apply" badge and a "Restart server" button. Wire the button to `useServerReconnect().triggerRestart()`, and on a thrown error surface it via the component's existing error/toast mechanism (match how the component already reports errors). Minimal example wiring in the component's `<script setup>`:

```ts
import { useServerReconnect } from '../composables/useServerReconnect'
const { triggerRestart } = useServerReconnect()

async function onRestart() {
  try {
    await triggerRestart()
  }
  catch (e) {
    // surface via the component's existing error handling
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function isBootWired(caps: string[]) {
  return caps.includes('auth_provider')
}
```

Render the badge/button conditionally on `isBootWired(plugin.capabilities)`.

- [ ] **Step 4: Verify**

Run: `pnpm test && pnpm typecheck && pnpm lint`
Expected: all PASS (existing tests + the two new test files; no type/lint errors).

- [ ] **Step 5: Commit**

```bash
git add src/App.vue src/components/
git commit --no-gpg-sign -m "feat: surface server restart UI for boot-wired plugins

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 5: Docs

**Files:**
- Modify: `CHANGELOG.md` (and README plugins/UX section if it describes plugin enable/disable)

- [ ] **Step 1: Document + commit**

Add a `CHANGELOG.md` Unreleased `### Added` bullet: a reconnect overlay that auto-recovers the UI across a server restart, plus a "restart required" prompt when a boot-wired (`auth_provider`) plugin is toggled. If README's plugin section claims everything is live, clarify that `auth_provider` needs a restart (surfaced in the UI).
```bash
git add CHANGELOG.md README.md
git commit --no-gpg-sign -m "docs: reconnect overlay + restart-required UX

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** poll const (T1) ✓; composable trigger/poll/reload/error (T2) ✓; blocking overlay (T3) ✓; mount + restart-required badge/button from capabilities (T4) ✓; graceful 404/409 handling = error toast, no overlay (T2 error test + T4 wiring) ✓; docs (T5) ✓. Frontend-only, no backend/ent change ✓. Builds independently of SP3a (endpoint may 404 → error path) ✓.
**Placeholder scan:** Step "match existing fetch helper / CSS tokens / error mechanism" are explicit adaptation points against named files, not vague TODOs. All test + component code is complete.
**Type consistency:** `useServerReconnect` returns `{ isReconnecting, beginReconnect, triggerRestart }` used identically in composable, overlay, and App/plugins wiring; `RECONNECT_POLL_MS` from `utils/sse`; `/api/system/health` + `/api/admin/restart` paths consistent.
