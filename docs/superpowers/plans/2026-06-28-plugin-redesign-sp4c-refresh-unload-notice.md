# Plugin Redesign SP4c — Refresh-to-Unload Notice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a `ui_extension` plugin is deactivated, show a non-blocking "reload the page to fully unload this plugin" notice with a "Reload now" button (the browser ES-module registry is permanent until reload).

**Architecture:** Hook the existing deactivate path in `PluginSettings.vue`: after a successful deactivate of a plugin whose `capabilities` include `ui_extension`, raise a persistent (non-auto-dismiss) notice carrying a reload action.

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Vitest + @vue/test-utils.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `src/components/PluginSettings.vue` | ui_extension deactivate → refresh notice + reload button | Modify |
| `src/components/PluginSettings.test.ts` | notice only for ui_extension deactivation; reload button reloads | Modify/Create |

**Commands:** `pnpm test`, `pnpm typecheck`, `pnpm lint` (0). Worktree `pnpm i`. Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

> Build-order note: SP4b reworks `PluginSettings.vue`'s toggle to `setActive`. SP4c hooks the deactivate result. If SP4b is merged into `feat/plugin-sp4` first, branch SP4c off the updated base. If SP4c is built before SP4b lands, hook whatever deactivate handler currently exists (`handleToggle`/`setActive`) — the capability check is independent. State which base you built on in the PR.

---

### Task 1: Refresh notice on ui_extension deactivation

**Files:** Modify `src/components/PluginSettings.vue`, `src/components/PluginSettings.test.ts`

- [ ] **Step 1: Write the failing test**

Add to `PluginSettings.test.ts` (adapt mounting/mock to the component's existing test harness — read it first; mock `usePluginSettings` so `setActive` resolves and the plugin list contains a `ui_extension` plugin):

```ts
it('shows a reload notice when a ui_extension plugin is deactivated', async () => {
  // mount PluginSettings with a plugin { id:'p1', capabilities:['ui_extension'], state:'active' }
  // trigger its deactivate control, await, then:
  expect(wrapper.text().toLowerCase()).toContain('reload')
  const reload = vi.fn()
  vi.stubGlobal('location', { reload } as any)
  await wrapper.find('[data-action="reload-now"]').trigger('click')
  expect(reload).toHaveBeenCalledOnce()
})

it('does not show the reload notice for a non-ui_extension plugin', async () => {
  // plugin { capabilities:['route_extension'] } deactivated → no reload button
  expect(wrapper.find('[data-action="reload-now"]').exists()).toBe(false)
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/components/PluginSettings.test.ts`
Expected: FAIL — no reload notice/button.

- [ ] **Step 3: Implement**

In `PluginSettings.vue`:
- Add a dedicated reactive ref for the reload notice (separate from the auto-dismissing `notice`, because this one must NOT auto-dismiss and carries an action):
```ts
const reloadNotice = ref<string | null>(null)
function reloadPage() { window.location.reload() }
```
- In the deactivate path (the `handleToggle`/`setActive` success branch when `next === false`), after success check the plugin's capabilities:
```ts
if (!next && plugin?.capabilities.includes('ui_extension'))
  reloadNotice.value = 'Plugin UI disabled — reload the page to fully unload its code'
```
- In the template, render the reload notice when set, with a button:
```vue
<div v-if="reloadNotice" class="plugin-reload-notice" role="status">
  <span>{{ reloadNotice }}</span>
  <button type="button" data-action="reload-now" @click="reloadPage">Reload now</button>
  <button type="button" aria-label="Dismiss" @click="reloadNotice = null">×</button>
</div>
```

> Match the existing notice styling. Keep the reload notice distinct from SP3b's blocking `ServerReconnectOverlay` — this is a client-only reload, it must NOT call `/api/admin/restart`.

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/components/PluginSettings.test.ts && pnpm typecheck && pnpm lint`
Expected: PASS, lint 0.

- [ ] **Step 5: Commit**

```bash
git add src/components/PluginSettings.vue src/components/PluginSettings.test.ts
git commit --no-gpg-sign -m "feat: prompt to reload after disabling a UI extension plugin

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: Docs

**Files:** Modify `CHANGELOG.md`

- [ ] **Step 1: Document + commit**

`CHANGELOG.md` Unreleased `### Added`: disabling a UI-extension plugin now prompts a page reload to fully unload its code (browser ES modules persist until reload).
```bash
git add CHANGELOG.md
git commit --no-gpg-sign -m "docs: note reload-to-unload prompt for UI extensions

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** notice only for `ui_extension` deactivate (D1) ✓; non-blocking + reload button (D2) ✓; reuse existing notice area but non-auto-dismiss (D3 + error-handling decision) ✓; docs (T2) ✓. Distinct from SP3b overlay ✓. Frontend-only ✓.
**Placeholder scan:** test stubs say "adapt to the component's existing harness" — bounded against the real file (the assertions are concrete). Implementation edits are concrete code. No vague TODOs.
**Type consistency:** `reloadNotice` ref, `reloadPage()`, `data-action="reload-now"`, capability check `'ui_extension'` — consistent. Relies on `plugin.capabilities` from the SP4b `PluginView` (or the current list if built first).
