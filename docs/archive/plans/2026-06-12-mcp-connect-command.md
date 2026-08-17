# MCP Connect Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the API-key token-reveal dialog, show two copy-ready artifacts — a `claude mcp add` CLI one-liner and an equivalent `mcpServers` JSON block — that register the dashboard task MCP server (`/api/mcp`) for the just-created/regenerated key, with the real token and a dynamic dashboard URL embedded.

**Architecture:** Pure frontend. A new SSOT util (`src/utils/mcpCommand.ts`) assembles both artifacts from `window.location.origin` + the revealed token. `ApiKeySettings.vue` renders them in the existing reveal dialog with per-target copy buttons. No backend changes.

**Tech Stack:** Vue 3 (`<script setup>`), TypeScript, Vite, Vitest, Tailwind.

---

## File Structure

- **Create** `src/utils/mcpCommand.ts` — two pure builder functions: `buildMcpAddCommand`, `buildMcpJsonConfig`. Single responsibility: turn `(origin, token)` into copy-ready strings.
- **Create** `src/utils/mcpCommand.test.ts` — unit tests for both builders.
- **Modify** `src/components/ApiKeySettings.vue` — render the two artifacts + per-target copy feedback + scope hint in the reveal dialog.

---

## Task 1: Command builder util (TDD)

**Files:**
- Create: `src/utils/mcpCommand.ts`
- Test: `src/utils/mcpCommand.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// src/utils/mcpCommand.test.ts
import { describe, expect, it } from 'vitest'
import { buildMcpAddCommand, buildMcpJsonConfig, MCP_SERVER_NAME } from './mcpCommand'

describe('buildMcpAddCommand', () => {
  it('embeds origin and token into a claude mcp add one-liner', () => {
    const cmd = buildMcpAddCommand('https://dash.example.com', 'mcp_abc123')
    expect(cmd).toBe(
      'claude mcp add --scope user --transport http dashboard-tasks '
      + 'https://dash.example.com/api/mcp '
      + '--header "Authorization: Bearer mcp_abc123"',
    )
  })

  it('strips a trailing slash on origin so the path is not doubled', () => {
    const cmd = buildMcpAddCommand('http://127.0.0.1:13120/', 'mcp_x')
    expect(cmd).toContain('http://127.0.0.1:13120/api/mcp')
    expect(cmd).not.toContain('//api/mcp')
  })

  it('uses the canonical server name', () => {
    expect(buildMcpAddCommand('http://h', 'mcp_x')).toContain(` ${MCP_SERVER_NAME} `)
  })
})

describe('buildMcpJsonConfig', () => {
  it('produces valid JSON that round-trips with the expected shape', () => {
    const json = buildMcpJsonConfig('https://dash.example.com', 'mcp_abc123')
    const parsed = JSON.parse(json)
    expect(parsed.mcpServers['dashboard-tasks']).toEqual({
      type: 'http',
      url: 'https://dash.example.com/api/mcp',
      headers: { Authorization: 'Bearer mcp_abc123' },
    })
  })

  it('strips a trailing slash on origin', () => {
    const parsed = JSON.parse(buildMcpJsonConfig('http://127.0.0.1:13120/', 'mcp_x'))
    expect(parsed.mcpServers['dashboard-tasks'].url).toBe('http://127.0.0.1:13120/api/mcp')
  })

  it('is pretty-printed with 2-space indentation', () => {
    const json = buildMcpJsonConfig('http://h', 'mcp_x')
    expect(json).toContain('\n  "mcpServers"')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/utils/mcpCommand.test.ts`
Expected: FAIL — `Failed to resolve import './mcpCommand'` (module not yet created).

- [ ] **Step 3: Write minimal implementation**

```ts
// src/utils/mcpCommand.ts

/** Canonical MCP server name — matches the server's own serverInfo.name. */
export const MCP_SERVER_NAME = 'dashboard-tasks'

function mcpUrl(origin: string): string {
  return `${origin.replace(/\/+$/, '')}/api/mcp`
}

/**
 * CLI one-liner that registers the dashboard task MCP server in the user's
 * global Claude config, so every session can author/refine tasks.
 */
export function buildMcpAddCommand(origin: string, token: string): string {
  return `claude mcp add --scope user --transport http ${MCP_SERVER_NAME} `
    + `${mcpUrl(origin)} `
    + `--header "Authorization: Bearer ${token}"`
}

/** Equivalent mcpServers config block for manual paste into a Claude config file. */
export function buildMcpJsonConfig(origin: string, token: string): string {
  return JSON.stringify(
    {
      mcpServers: {
        [MCP_SERVER_NAME]: {
          type: 'http',
          url: mcpUrl(origin),
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test src/utils/mcpCommand.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add src/utils/mcpCommand.ts src/utils/mcpCommand.test.ts
git commit -m "feat(api-keys): add MCP connect command builders"
```

---

## Task 2: Render artifacts + per-target copy in reveal dialog

**Files:**
- Modify: `src/components/ApiKeySettings.vue`

Context: the reveal dialog lives at the bottom of the template (`<!-- Token reveal dialog -->`). Current state uses a single `copyHint` ref and one `copyToken()` function. The dialog is opened by setting `revealedToken` in both `handleCreate` and `regenerateKey`, which each have `data.key.scopes` available.

- [ ] **Step 1: Import the builders and add reveal state**

In `<script setup>`, add to the imports near the other util imports (after `import { maskToken } from '../utils/format'`):

```ts
import { buildMcpAddCommand, buildMcpJsonConfig } from '../utils/mcpCommand'
import type { McpScope } from '../types'
```

(`McpScope` is already imported on line 2 via `import type { ApiKey, McpScope } from '../types'` — do NOT add a duplicate import; only add the `mcpCommand` import.)

Replace the token-reveal state block:

```ts
// Token reveal modal
const revealedToken = ref<string | null>(null)
const copyHint = ref<string | null>(null)
const tokenVisible = ref(false)
```

with:

```ts
// Token reveal modal
const revealedToken = ref<string | null>(null)
const revealedScopes = ref<McpScope[]>([])
const copiedTarget = ref<'token' | 'cli' | 'json' | '__error__' | null>(null)
const tokenVisible = ref(false)

const mcpAddCommand = computed(() =>
  revealedToken.value ? buildMcpAddCommand(window.location.origin, revealedToken.value) : '',
)
const mcpJsonConfig = computed(() =>
  revealedToken.value ? buildMcpJsonConfig(window.location.origin, revealedToken.value) : '',
)
const canAuthorTasks = computed(() => revealedScopes.value.includes('tasks:write'))
```

Add `computed` to the existing `vue` import (line 3 currently: `import { defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'`) → add `computed`.

- [ ] **Step 2: Replace `copyToken` with a generic per-target copy**

Replace the `copyToken` function:

```ts
// --- Copy token ---
async function copyToken() {
  if (!revealedToken.value)
    return
  try {
    await navigator.clipboard.writeText(revealedToken.value)
    copyHint.value = revealedToken.value
  }
  catch {
    copyHint.value = '__error__'
  }
  setTimeout(() => {
    copyHint.value = null
  }, 2000)
}
```

with:

```ts
// --- Copy helpers ---
async function copyValue(target: 'token' | 'cli' | 'json', value: string) {
  if (!value)
    return
  try {
    await navigator.clipboard.writeText(value)
    copiedTarget.value = target
  }
  catch {
    copiedTarget.value = '__error__'
  }
  setTimeout(() => {
    copiedTarget.value = null
  }, 2000)
}
```

- [ ] **Step 3: Update `dismissReveal` and the two openers**

Replace `dismissReveal`:

```ts
function dismissReveal() {
  revealedToken.value = null
  copyHint.value = null
  tokenVisible.value = false
}
```

with:

```ts
function dismissReveal() {
  revealedToken.value = null
  revealedScopes.value = []
  copiedTarget.value = null
  tokenVisible.value = false
}
```

In `handleCreate`, where it sets `revealedToken.value = data.token`, add the scopes line right after:

```ts
    revealedToken.value = data.token
    revealedScopes.value = data.key.scopes
```

In `regenerateKey`, where it sets `revealedToken.value = data.token`, add right after:

```ts
    revealedToken.value = data.token
    revealedScopes.value = data.key.scopes
```

- [ ] **Step 4: Update the template — token copy button**

In the Token reveal dialog, the existing token copy button reads:

```vue
            <AppButton variant="info" @click="copyToken">
              <span v-if="copyHint === '__error__'">Copy failed</span>
              <span v-else-if="copyHint">Copied!</span>
              <span v-else>Copy to clipboard</span>
            </AppButton>
```

Replace with:

```vue
            <AppButton variant="info" @click="copyValue('token', revealedToken ?? '')">
              <span v-if="copiedTarget === '__error__'">Copy failed</span>
              <span v-else-if="copiedTarget === 'token'">Copied!</span>
              <span v-else>Copy to clipboard</span>
            </AppButton>
```

- [ ] **Step 5: Template — add the two connect blocks + scope hint**

Immediately after the closing `</div>` of the token copy row (the `<div class="flex justify-end">…</div>` that wraps the token copy button), and before the dialog `<footer>`, insert:

```vue
          <div class="mt-5 border-t border-line pt-4">
            <p class="text-[13px] text-fg-mute mb-1">
              Connect a Claude Code session to this dashboard's task tools:
            </p>
            <p v-if="!canAuthorTasks" class="text-[11px] text-yellow-600 dark:text-yellow-400 mb-3">
              Read-only key — creating or refining tasks needs the Developer or Admin role.
            </p>

            <label class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">CLI command</label>
            <div class="relative font-mono text-xs bg-raised text-fg-soft p-3 pr-10 rounded border border-line break-all mt-1 mb-3">
              {{ mcpAddCommand }}
              <button
                type="button"
                class="absolute right-2 top-2 p-1 rounded hover:bg-app text-fg-mute hover:text-fg transition-colors"
                :aria-label="copiedTarget === 'cli' ? 'Copied' : 'Copy CLI command'"
                @click="copyValue('cli', mcpAddCommand)"
              >
                <span class="text-[11px]">{{ copiedTarget === 'cli' ? '✓' : '⧉' }}</span>
              </button>
            </div>

            <label class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">JSON config</label>
            <div class="relative font-mono text-xs bg-raised text-fg-soft p-3 pr-10 rounded border border-line whitespace-pre overflow-x-auto mt-1">
              {{ mcpJsonConfig }}
              <button
                type="button"
                class="absolute right-2 top-2 p-1 rounded hover:bg-app text-fg-mute hover:text-fg transition-colors"
                :aria-label="copiedTarget === 'json' ? 'Copied' : 'Copy JSON config'"
                @click="copyValue('json', mcpJsonConfig)"
              >
                <span class="text-[11px]">{{ copiedTarget === 'json' ? '✓' : '⧉' }}</span>
              </button>
            </div>
          </div>
```

- [ ] **Step 6: Verify build + typecheck + lint + unit tests**

Run: `pnpm test src/utils/mcpCommand.test.ts && pnpm build`
Expected: tests PASS; build succeeds with no TypeScript errors.

If the project has a lint script, run it too:
Run: `pnpm lint` (skip if no such script)
Expected: no new errors in `ApiKeySettings.vue` / `mcpCommand.ts`.

- [ ] **Step 7: Manual smoke (optional but recommended)**

Start dev server, open Settings → API Keys → Add Key (Developer role) → confirm the reveal dialog shows the raw token, the CLI command, and the JSON block, each with a working copy button; create a Viewer key and confirm the read-only hint appears.

- [ ] **Step 8: Commit**

```bash
git add src/components/ApiKeySettings.vue
git commit -m "feat(api-keys): show MCP connect command + JSON in reveal dialog"
```

---

## Self-Review Notes

- **Spec coverage:** CLI artifact (Task 1 + Task 2 step 5) ✓; JSON artifact (Task 1 + Task 2 step 5) ✓; dynamic URL via `window.location.origin` (Task 2 step 1) ✓; real token only in reveal dialog (Task 2, reveal-dialog only) ✓; scope hint for non-`tasks:write` keys (Task 2 steps 1+5) ✓; SSOT util + unit tests (Task 1) ✓; no backend change ✓; trailing-slash normalization (Task 1 tests) ✓.
- **Type consistency:** `copiedTarget` union, `buildMcpAddCommand`/`buildMcpJsonConfig`/`MCP_SERVER_NAME` names, and `revealedScopes: McpScope[]` are used identically across tasks.
- **No placeholders:** every code step shows full content.
```
