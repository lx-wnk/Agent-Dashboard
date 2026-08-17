# Plugin Redesign SP4b — Per-Plugin Settings UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A schema-driven per-plugin settings form (GET/PUT `/api/plugins/{id}/settings`) and migration of the broken enable/disable toggle from the SP2-removed `PATCH /api/settings/plugins-enabled/{id}` to the live `activate`/`deactivate` lifecycle endpoints.

**Architecture:** Rework `usePluginSettings` to read the lifecycle list (`/api/plugins`, carrying `hasSettings`/`state`) and call `activate`/`deactivate`; a new `PluginSettingsForm.vue` renders one control per setting field type with secret masking; `PluginSettings.vue` shows an expandable form per plugin with `hasSettings`.

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Vitest + @vue/test-utils.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `src/composables/usePluginSettings.ts` | lifecycle list + `setActive` (activate/deactivate) + `getSettings`/`putSettings` | Modify |
| `src/composables/usePluginSettings.test.ts` | list mapping, setActive verb, settings shapes | Modify/Create |
| `src/components/PluginSettingsForm.vue` | schema-driven form (field types, secret mask, sentinel-keep) | Create |
| `src/components/PluginSettingsForm.test.ts` | render per type, secret mask, PUT payload | Create |
| `src/components/PluginSettings.vue` | use lifecycle list + setActive; expandable form per `hasSettings` plugin | Modify |

**Commands:** `pnpm test`, `pnpm typecheck`, `pnpm lint` (0). Worktree `pnpm i`. Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

The lifecycle DTOs (from `server/internal/api/plugins/handler.go`):
- list `GET /api/plugins` → `PluginView[]`: `{ id, name, version, state, updateAvailable, capabilities, hasSettings }` (`state` ∈ discovered|inactive|active).
- `GET /api/plugins/{id}/settings` → `{ schema: SettingField[], values: Record<string,string> }`; `SettingField` = `{ key, type, label, secret, enum? }`, `type` ∈ string|url|int|bool|enum.
- `PUT /api/plugins/{id}/settings` body `{ values: Record<string,string> }`; an unchanged secret = send the masked sentinel (server keeps it) or omit it.
- `POST /api/plugins/{id}/activate|deactivate` → new state or 4xx/5xx.

---

### Task 1: Rework `usePluginSettings`

**Files:** Modify `src/composables/usePluginSettings.ts`, `src/composables/usePluginSettings.test.ts`

- [ ] **Step 1: Write the failing test**

Replace the toggle/PATCH test with lifecycle ones (read the existing test file first to match its harness):

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePluginSettings } from './usePluginSettings'

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('setActive posts activate and updates state', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce({ ok: true, json: async () => [{ id: 'p1', name: 'P1', version: '1', state: 'inactive', updateAvailable: false, capabilities: ['ui_extension'], hasSettings: true }] })
    .mockResolvedValueOnce({ ok: true, json: async () => ({ id: 'p1', state: 'active' }) })
  vi.stubGlobal('fetch', fetchMock)
  const s = usePluginSettings()
  await s.refetch()
  await s.setActive('p1', true)
  expect(fetchMock).toHaveBeenLastCalledWith('/api/plugins/p1/activate', expect.objectContaining({ method: 'POST' }))
  expect(s.plugins.value[0].state).toBe('active')
})

it('getSettings + putSettings hit the settings endpoints', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce({ ok: true, json: async () => ({ schema: [{ key: 'apiKey', type: 'string', label: 'API Key', secret: true }], values: { apiKey: '********' } }) })
    .mockResolvedValueOnce({ ok: true, status: 204 })
  vi.stubGlobal('fetch', fetchMock)
  const s = usePluginSettings()
  const got = await s.getSettings('p1')
  expect(got.schema[0].key).toBe('apiKey')
  await s.putSettings('p1', { apiKey: 'new-secret' })
  expect(fetchMock).toHaveBeenLastCalledWith('/api/plugins/p1/settings', expect.objectContaining({ method: 'PUT' }))
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/composables/usePluginSettings.test.ts`
Expected: FAIL — `setActive`/`getSettings`/`putSettings` undefined; list still hits `/api/settings/plugins`.

- [ ] **Step 3: Implement**

Rewrite `usePluginSettings.ts`:

```ts
import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface SettingField {
  key: string
  type: 'string' | 'url' | 'int' | 'bool' | 'enum'
  label: string
  secret: boolean
  enum?: string[]
}
export interface PluginView {
  id: string
  name: string
  version: string
  state: 'discovered' | 'inactive' | 'active'
  updateAvailable: boolean
  capabilities: string[]
  hasSettings: boolean
}

export function usePluginSettings() {
  const plugins = ref<PluginView[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchPlugins() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/plugins', { credentials: 'same-origin' })
      if (!res.ok)
        throw new Error(`Failed to load plugins (HTTP ${res.status})`)
      plugins.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load plugins')
    }
    finally {
      loading.value = false
    }
  }

  // Live for route/ui extensions; auth_provider needs a restart (SP3b badge).
  async function setActive(id: string, active: boolean): Promise<void> {
    const verb = active ? 'activate' : 'deactivate'
    const res = await fetch(`/api/plugins/${id}/${verb}`, {
      method: 'POST',
      headers: { 'Origin': window.location.origin },
    })
    if (!res.ok) {
      let detail = `HTTP ${res.status}`
      try { const b = await res.json(); if (b?.error) detail = b.error }
      catch { /* no body */ }
      throw new Error(detail)
    }
    plugins.value = plugins.value.map(p => (p.id === id ? { ...p, state: active ? 'active' : 'inactive' } : p))
  }

  async function getSettings(id: string): Promise<{ schema: SettingField[], values: Record<string, string> }> {
    const res = await fetch(`/api/plugins/${id}/settings`, { credentials: 'same-origin' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    return res.json()
  }

  async function putSettings(id: string, values: Record<string, string>): Promise<void> {
    const res = await fetch(`/api/plugins/${id}/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      body: JSON.stringify({ values }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
  }

  onMounted(fetchPlugins)
  return { plugins, loading, error, refetch: fetchPlugins, setActive, getSettings, putSettings }
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/composables/usePluginSettings.test.ts && pnpm typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/composables/usePluginSettings.ts src/composables/usePluginSettings.test.ts
git commit --no-gpg-sign -m "feat: drive plugin panel from lifecycle API with live activate/deactivate

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: `PluginSettingsForm.vue`

**Files:** Create `src/components/PluginSettingsForm.vue`, `src/components/PluginSettingsForm.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PluginSettingsForm from './PluginSettingsForm.vue'

const schema = [
  { key: 'endpoint', type: 'url', label: 'Endpoint', secret: false },
  { key: 'apiKey', type: 'string', label: 'API Key', secret: true },
  { key: 'mode', type: 'enum', label: 'Mode', secret: false, enum: ['a', 'b'] },
]

function mountForm(getSettings: any, putSettings = vi.fn().mockResolvedValue(undefined)) {
  return mount(PluginSettingsForm, { props: { pluginId: 'p1', getSettings, putSettings } })
}

it('renders a control per field and masks secrets', async () => {
  const w = mountForm(async () => ({ schema, values: { endpoint: 'http://x', apiKey: '********', mode: 'a' } }))
  await flushPromises()
  expect(w.find('[data-field="endpoint"]').exists()).toBe(true)
  expect(w.find('select[data-field="mode"]').exists()).toBe(true)
  expect((w.find('[data-field="apiKey"]').element as HTMLInputElement).value).toBe('********')
})

it('PUT omits an untouched secret and sends changed fields', async () => {
  const put = vi.fn().mockResolvedValue(undefined)
  const w = mountForm(async () => ({ schema, values: { endpoint: 'http://x', apiKey: '********', mode: 'a' } }), put)
  await flushPromises()
  await w.find('[data-field="endpoint"]').setValue('http://y')
  await w.find('[data-action="save"]').trigger('click')
  await flushPromises()
  const sent = put.mock.calls[0][1]
  expect(sent.endpoint).toBe('http://y')
  expect('apiKey' in sent).toBe(false) // untouched secret omitted
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/components/PluginSettingsForm.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

Create `src/components/PluginSettingsForm.vue`:

```vue
<script setup lang="ts">
import type { SettingField } from '../composables/usePluginSettings'
import { onMounted, reactive, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

const props = defineProps<{
  pluginId: string
  getSettings: (id: string) => Promise<{ schema: SettingField[], values: Record<string, string> }>
  putSettings: (id: string, values: Record<string, string>) => Promise<void>
}>()

const SECRET_SENTINEL = '********'
const schema = ref<SettingField[]>([])
const model = reactive<Record<string, string>>({})
const initial = reactive<Record<string, string>>({})
const error = ref<string | null>(null)
const saving = ref(false)

onMounted(async () => {
  try {
    const { schema: s, values } = await props.getSettings(props.pluginId)
    schema.value = s
    for (const f of s) {
      model[f.key] = values[f.key] ?? ''
      initial[f.key] = values[f.key] ?? ''
    }
  }
  catch (e) {
    error.value = errorMessage(e, 'Failed to load settings')
  }
})

async function save() {
  saving.value = true
  error.value = null
  try {
    const changed: Record<string, string> = {}
    for (const f of schema.value) {
      // Skip secrets the user did not touch (still the masked sentinel).
      if (f.secret && model[f.key] === SECRET_SENTINEL)
        continue
      if (model[f.key] !== initial[f.key])
        changed[f.key] = model[f.key]
    }
    await props.putSettings(props.pluginId, changed)
    for (const k of Object.keys(changed))
      initial[k] = model[k]
  }
  catch (e) {
    error.value = errorMessage(e, 'Failed to save settings')
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="plugin-settings-form" @submit.prevent="save">
    <p v-if="error" class="plugin-settings-form__error">{{ error }}</p>
    <div v-for="f in schema" :key="f.key" class="plugin-settings-form__field">
      <label :for="`pf-${pluginId}-${f.key}`">{{ f.label }}</label>
      <select v-if="f.type === 'enum'" :id="`pf-${pluginId}-${f.key}`" v-model="model[f.key]" :data-field="f.key">
        <option v-for="opt in f.enum ?? []" :key="opt" :value="opt">{{ opt }}</option>
      </select>
      <input v-else-if="f.type === 'bool'" :id="`pf-${pluginId}-${f.key}`" v-model="model[f.key]" type="checkbox" :data-field="f.key" true-value="true" false-value="false">
      <input v-else-if="f.type === 'int'" :id="`pf-${pluginId}-${f.key}`" v-model="model[f.key]" type="number" :data-field="f.key">
      <input v-else :id="`pf-${pluginId}-${f.key}`" v-model="model[f.key]" :type="f.secret ? 'password' : 'text'" :data-field="f.key">
    </div>
    <button type="submit" :disabled="saving" data-action="save">{{ saving ? 'Saving…' : 'Save' }}</button>
  </form>
</template>
```

> Match the project's form/input styling conventions — check an existing settings form component for class names/tokens and align. The `data-field`/`data-action` hooks are for tests; keep them.

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/components/PluginSettingsForm.test.ts && pnpm typecheck && pnpm lint`
Expected: PASS, lint 0.

- [ ] **Step 5: Commit**

```bash
git add src/components/PluginSettingsForm.vue src/components/PluginSettingsForm.test.ts
git commit --no-gpg-sign -m "feat: add schema-driven per-plugin settings form

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 3: Wire the form + migrated toggle into `PluginSettings.vue`

**Files:** Modify `src/components/PluginSettings.vue`

- [ ] **Step 1: Read the current component**

Read `src/components/PluginSettings.vue` fully. It currently uses `usePluginSettings().toggle` (the removed PATCH) + a `handleToggle` + a `notice`/`showNotice` helper + the SP3b restart badge. You will: swap `toggle`→`setActive`, render a `PluginSettingsForm` per plugin with `hasSettings`, and keep the SP3b restart badge for `auth_provider`.

- [ ] **Step 2: Update the script + template**

In `<script setup>`:
- Destructure the new API: `const { plugins, loading, error, setActive, getSettings, putSettings } = usePluginSettings()`.
- Replace `handleToggle` body to call `setActive(id, next)` (keep the success/restart-required notice; auth_provider still warns "restart required").
- Add `const expanded = ref<string | null>(null)` and `function toggleExpand(id) { expanded.value = expanded.value === id ? null : id }`.
- Import `PluginSettingsForm`.

In the template, for each plugin row:
- The enable/disable control calls the migrated `handleToggle`.
- If `plugin.hasSettings`, add a "Settings" expander; when `expanded === plugin.id` render:
  ```vue
  <PluginSettingsForm
    v-if="expanded === plugin.id"
    :plugin-id="plugin.id"
    :get-settings="getSettings"
    :put-settings="putSettings"
  />
  ```
- Keep the SP3b "restart required" badge/button for `auth_provider` capability.

> The plugin row now uses `state` (active|inactive|discovered), not the old `enabled` boolean — derive the toggle's checked state from `plugin.state === 'active'`.

- [ ] **Step 3: Run the full suite**

Run: `pnpm test && pnpm typecheck && pnpm lint`
Expected: all pass, lint 0. (If `PluginSettings` had a test asserting the old `enabled`/PATCH behaviour, update it to `state`/`setActive`.)

- [ ] **Step 4: Commit**

```bash
git add src/components/PluginSettings.vue
git commit --no-gpg-sign -m "feat: per-plugin settings panel with live enable and settings form

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 4: Docs

**Files:** Modify `CHANGELOG.md` (+ README plugins section if it describes enable/disable)

- [ ] **Step 1: Document + commit**

`CHANGELOG.md` Unreleased: per-plugin settings UI (schema-driven, secrets masked) + the enable/disable toggle now uses the live lifecycle endpoints (fixing the removed-PATCH regression). README: if it claims plugin enable is restart-to-apply, correct it to live activate/deactivate (auth_provider excepted).
```bash
git add CHANGELOG.md README.md
git commit --no-gpg-sign -m "docs: per-plugin settings UI and live enable/disable

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** lifecycle list + setActive (T1) ✓; getSettings/putSettings (T1) ✓; schema-driven form w/ field types + secret mask + sentinel-keep (T2) ✓; panel wiring + toggle migration (T3) ✓; docs (T4) ✓. Fixes the SP2 toggle regression (D3) ✓. Frontend-only ✓.
**Placeholder scan:** T3 references "read the current component" then gives concrete edits (swap toggle→setActive, add expander, render form) — the component's existing template isn't reproduced verbatim but every change is specified with code. Acceptable (the worker reads the file in Step 1). No vague TODOs.
**Type consistency:** `PluginView.{state,hasSettings,capabilities}`, `SettingField.{key,type,label,secret,enum}`, `setActive(id,active)`, `getSettings(id)→{schema,values}`, `putSettings(id,values)`, `SECRET_SENTINEL` — consistent across tasks.
