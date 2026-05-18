# Gap Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close all identified gaps, TODOs, and unfinished partial implementations on the `feat/gap-resolution` branch (based on `upcoming`), leaving the codebase production-ready and fully consistent.

**Architecture:** Six independent work areas — each produces testable, committed output. Execution order matters only for Task 1 (parser struct change needed by Task 2 tests) and Task 6 (depends on Task 5 types). All others are parallel-safe.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite), Vue 3 + TypeScript (Vite, pnpm), Vitest (unit), `task test` (Go tests with race detector)

**Branch:** `feat/gap-resolution` (branched from `upcoming`)

**Commit style:** semantic commits, no GPG signing (`git commit --no-gpg-sign` or configure `git config commit.gpgsign false` locally)

---

## Gap Inventory

| # | Area | File(s) | Severity |
|---|---|---|---|
| G1 | Task extraction from JSONL | `server/internal/parser/parser.go:260` | P1 — AgentModal TaskList empty |
| G2 | LLM Adapter Settings UI | `ApiKeySettings.vue` (missing tab), new `AdapterSettings.vue` | P2 — config only via JSON |
| G3 | Plugin status UI | New `PluginSettings.vue` + settings tab | P3 — no discoverability |
| G4 | Graceful shutdown context | `server/cmd/serve/di.go:68` | P4 — DB writes may abort on SIGTERM |
| G5 | SSOT `MAX_DESCRIPTION_CHARS` | `server/constants.ts:24` + `src/utils/validation.ts` | P5 — minor duplication risk |
| G6 | Refine handler unit tests | `server/internal/api/refine/handler_test.go` (missing) | P5 — only untested handler |
| G7 | Notification settings UI | New `NotificationSettings.vue` + settings tab | P3 — backend exists, no UI |

---

## File Map

### Created
- `server/internal/parser/parser.go` — modified to add TodoWrite/TodoRead task extraction
- `server/internal/parser/parser_tasks_test.go` — new test file for task extraction
- `src/components/AdapterSettings.vue` — new LLM adapter settings panel
- `src/components/PluginSettings.vue` — new plugin status panel
- `src/components/NotificationSettings.vue` — new notification config panel
- `src/components/ApiKeySettings.vue` — modified: add adapters, plugins, notifications tabs
- `server/cmd/serve/di.go` — modified: thread shutdown context
- `server/cmd/serve/main.go` — modified: pass cancel context to di
- `src/utils/validation.ts` — modified: export `MAX_DESCRIPTION_CHARS`
- `server/constants.ts` — modified: import from validation.ts instead of local const
- `server/internal/api/refine/handler_test.go` — new test file

---

## Task 1: Task Extraction from JSONL (`parser.go`)

**Files:**
- Modify: `server/internal/parser/parser.go:143-150` (toolUseBlock struct) and `:260` (TODO)
- Create: `server/internal/parser/parser_tasks_test.go`

### Background

The JSONL format emitted by Claude Code includes `tool_use` blocks inside assistant messages. `TodoWrite` tool inputs look like:

```json
{
  "type": "tool_use",
  "name": "TodoWrite",
  "input": {
    "todos": [
      {"id": "1", "content": "Implement feature X", "status": "in_progress", "priority": "high"},
      {"id": "2", "content": "Write tests", "status": "pending", "priority": "medium"}
    ]
  }
}
```

The parser already iterates tool_use blocks (line ~299 in `parser.go`). We only need to handle `TodoWrite` by unmarshalling `input.todos` into `sdk.TaskInfo` and keeping the **last seen** state (subsequent `TodoWrite` calls overwrite prior ones — the final call is the ground truth).

- [ ] **Step 1: Write failing test**

Create `server/internal/parser/parser_tasks_test.go`:

```go
package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func writeTempJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "session.jsonl")
	var content string
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func assistantTodoWriteLine(todos []map[string]string) string {
	input, _ := json.Marshal(map[string]any{"todos": todos})
	block, _ := json.Marshal(map[string]any{
		"type":  "tool_use",
		"name":  "TodoWrite",
		"input": json.RawMessage(input),
	})
	content, _ := json.Marshal([]json.RawMessage{block})
	msg, _ := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": json.RawMessage(content),
		"model":   "claude-opus-4-5",
		"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
	})
	line, _ := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"message":   json.RawMessage(msg),
	})
	return string(line)
}

func TestParseSessionFile_TaskExtraction(t *testing.T) {
	todos := []map[string]string{
		{"id": "1", "content": "Implement feature X", "status": "in_progress"},
		{"id": "2", "content": "Write tests", "status": "pending"},
	}
	path := writeTempJSONL(t, []string{assistantTodoWriteLine(todos)})

	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(data.Tasks))
	}
	if data.Tasks[0].ID != "1" || data.Tasks[0].Subject != "Implement feature X" || data.Tasks[0].Status != "in_progress" {
		t.Errorf("task 0 mismatch: %+v", data.Tasks[0])
	}
	if data.Tasks[1].ID != "2" || data.Tasks[1].Status != "pending" {
		t.Errorf("task 1 mismatch: %+v", data.Tasks[1])
	}
}

func TestParseSessionFile_LastTodoWriteWins(t *testing.T) {
	first := assistantTodoWriteLine([]map[string]string{
		{"id": "1", "content": "Old task", "status": "pending"},
	})
	second := assistantTodoWriteLine([]map[string]string{
		{"id": "1", "content": "Old task", "status": "done"},
		{"id": "2", "content": "New task", "status": "in_progress"},
	})
	path := writeTempJSONL(t, []string{first, second})

	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 2 {
		t.Fatalf("want 2 tasks from last TodoWrite, got %d", len(data.Tasks))
	}
	if data.Tasks[0].Status != "done" {
		t.Errorf("want task 0 status 'done', got %q", data.Tasks[0].Status)
	}
}

func TestParseSessionFile_NoTasks(t *testing.T) {
	todos := []map[string]string{}
	path := writeTempJSONL(t, []string{assistantTodoWriteLine(todos)})
	data, err := parser.ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(data.Tasks) != 0 {
		t.Errorf("want 0 tasks, got %d", len(data.Tasks))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
go test ./server/internal/parser/... -run TestParseSessionFile_Task -v 2>&1 | head -20
```

Expected: FAIL — `parser.ParseSessionFile` is unexported or `Tasks` is always empty.

- [ ] **Step 3: Implement task extraction**

In `server/internal/parser/parser.go`, add a `todoInput` struct after the `toolUseBlock` struct (around line 150):

```go
// todoInput is the input shape for TodoWrite and TodoRead tool calls.
type todoInput struct {
	Todos []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"todos"`
}
```

In the `switch b.Type` block inside the parsing loop (around line 299), add a case for `TodoWrite` inside `case "tool_use":`:

```go
case "tool_use":
    data.ToolCounts[b.Name]++
    recentToolNames = append(recentToolNames, b.Name)
    data.CurrentAction = b.Name
    // Extract task list from the last TodoWrite call.
    if b.Name == "TodoWrite" {
        var inp todoInput
        if err := json.Unmarshal(b.Input, &inp); err == nil {
            tasks := make([]sdk.TaskInfo, 0, len(inp.Todos))
            for _, td := range inp.Todos {
                tasks = append(tasks, sdk.TaskInfo{
                    ID:      td.ID,
                    Subject: td.Content,
                    Status:  td.Status,
                })
            }
            data.Tasks = tasks // last TodoWrite wins
        }
    }
```

Remove the TODO comment on line 260.

- [ ] **Step 4: Run tests**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
go test ./server/internal/parser/... -v -race 2>&1 | tail -20
```

Expected: all PASS including the three new tests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/parser/parser.go server/internal/parser/parser_tasks_test.go
git commit --no-gpg-sign -m "feat(parser): extract tasks from TodoWrite tool calls in JSONL sessions"
```

---

## Task 2: LLM Adapter Settings UI

**Files:**
- Create: `src/components/AdapterSettings.vue`
- Modify: `src/components/ApiKeySettings.vue` (add `adapters` section to `Section` type + tab + panel)

The backend already exposes:
- `GET /api/adapters` — list of available adapters with their config keys
- `GET /api/adapters/current` / `POST /api/adapters/current` — read/set active adapter name
- `GET /api/settings/adapters` / `PUT /api/settings/adapters` — full config object

- [ ] **Step 1: Write component test**

Create `src/components/AdapterSettings.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import AdapterSettings from './AdapterSettings.vue'

const mockAdapters = [
  { name: 'claude', description: 'Default Claude CLI adapter', configKeys: [] },
  { name: 'ollama', description: 'Ollama HTTP adapter', configKeys: [
    { key: 'adapters.ollama.host', type: 'string', required: false, note: 'Ollama base URL' },
    { key: 'adapters.ollama.default_model', type: 'string', required: false, note: 'Model name' },
  ]},
]

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url === '/api/adapters') return Promise.resolve({ ok: true, json: () => Promise.resolve(mockAdapters) })
    if (url === '/api/adapters/current') return Promise.resolve({ ok: true, json: () => Promise.resolve({ adapter: 'claude' }) })
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
  }))
})

describe('AdapterSettings', () => {
  it('renders adapter list after mount', async () => {
    const w = mount(AdapterSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    expect(w.text()).toContain('claude')
    expect(w.text()).toContain('ollama')
  })

  it('shows config keys for selected adapter', async () => {
    const w = mount(AdapterSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    // Select ollama
    const buttons = w.findAll('button')
    const ollamaBtn = buttons.find(b => b.text().includes('ollama'))
    await ollamaBtn?.trigger('click')
    expect(w.text()).toContain('adapters.ollama.host')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test --reporter=verbose src/components/AdapterSettings.test.ts 2>&1 | tail -15
```

Expected: FAIL — `AdapterSettings.vue` does not exist.

- [ ] **Step 3: Create `AdapterSettings.vue`**

Create `src/components/AdapterSettings.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import AppButton from './ui/AppButton.vue'

interface ConfigKey {
  key: string
  type: string
  required: boolean
  note?: string
}

interface AdapterMeta {
  name: string
  description: string
  configKeys: ConfigKey[]
}

const adapters = ref<AdapterMeta[]>([])
const current = ref<string>('claude')
const selected = ref<string>('claude')
const saving = ref(false)
const loading = ref(true)
const error = ref<string | null>(null)
const saveOk = ref(false)

const selectedMeta = computed(() => adapters.value.find(a => a.name === selected.value) ?? null)

onMounted(async () => {
  try {
    const [listRes, curRes] = await Promise.all([
      fetch('/api/adapters'),
      fetch('/api/adapters/current'),
    ])
    if (!listRes.ok || !curRes.ok) throw new Error('Failed to load adapter info')
    adapters.value = await listRes.json()
    const curData = await curRes.json()
    current.value = curData.adapter ?? 'claude'
    selected.value = current.value
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
  } finally {
    loading.value = false
  }
})

async function save() {
  saving.value = true
  saveOk.value = false
  error.value = null
  try {
    const res = await fetch('/api/adapters/current', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ adapter: selected.value }),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    current.value = selected.value
    saveOk.value = true
    setTimeout(() => { saveOk.value = false }, 2000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">LLM Adapter</h3>
    <p class="text-xs text-slate-500 dark:text-slate-400">
      Select which LLM backend pipeline stage agents use. "claude" is the default and spawns the Claude CLI.
    </p>

    <div v-if="loading" class="text-xs text-slate-400">Loading adapters…</div>
    <div v-else-if="error" class="text-xs text-red-500">{{ error }}</div>
    <div v-else class="space-y-3">
      <div class="flex flex-wrap gap-2">
        <button
          v-for="a in adapters"
          :key="a.name"
          :class="[
            'px-3 py-1.5 rounded text-xs font-medium border transition-colors',
            selected === a.name
              ? 'bg-blue-600 text-white border-blue-600'
              : 'bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-300 border-slate-300 dark:border-slate-600 hover:border-blue-400',
          ]"
          @click="selected = a.name"
        >
          {{ a.name }}
          <span v-if="a.name === current" class="ml-1 opacity-70">(active)</span>
        </button>
      </div>

      <div v-if="selectedMeta" class="bg-slate-50 dark:bg-slate-800/50 rounded p-3 text-xs space-y-2">
        <p class="text-slate-600 dark:text-slate-400">{{ selectedMeta.description }}</p>
        <div v-if="selectedMeta.configKeys.length" class="space-y-1">
          <p class="font-medium text-slate-700 dark:text-slate-300">Configuration</p>
          <table class="w-full">
            <tbody>
              <tr v-for="k in selectedMeta.configKeys" :key="k.key" class="border-b border-slate-200 dark:border-slate-700">
                <td class="py-1 pr-3 font-mono text-blue-600 dark:text-blue-400 whitespace-nowrap">{{ k.key }}</td>
                <td class="py-1 pr-3 text-slate-500">{{ k.type }}<span v-if="k.required" class="text-red-500 ml-1">*</span></td>
                <td class="py-1 text-slate-500">{{ k.note }}</td>
              </tr>
            </tbody>
          </table>
          <p class="text-slate-400 mt-1">Set via env var or <code class="font-mono">adapter-config.json</code> (PUT /api/settings/adapters).</p>
        </div>
        <p v-else class="text-slate-400 italic">No additional configuration required.</p>
      </div>

      <div class="flex items-center gap-2">
        <AppButton
          size="sm"
          :disabled="saving || selected === current"
          @click="save"
        >
          {{ saving ? 'Saving…' : saveOk ? 'Saved!' : 'Apply Adapter' }}
        </AppButton>
        <span v-if="selected === current" class="text-xs text-slate-400">No changes</span>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Add `adapters` tab to `ApiKeySettings.vue`**

In `src/components/ApiKeySettings.vue`, change:

```ts
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets' | 'analytics' | 'systemPrompts'
```

to:

```ts
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets' | 'analytics' | 'systemPrompts' | 'adapters'
```

Add the import at the top of `<script setup>`:

```ts
import AdapterSettings from './AdapterSettings.vue'
```

In the sidebar nav (after the `systemPrompts` button), add:

```html
<button
  :class="activeSection === 'adapters'
    ? 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
    : 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800'"
  @click="activeSection = 'adapters'"
>
  <span class="text-sm flex-shrink-0">⚡</span> LLM Adapters
</button>
```

After the `systemPrompts` section panel, add:

```html
<section v-else-if="activeSection === 'adapters'">
  <AdapterSettings />
</section>
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test --reporter=verbose src/components/AdapterSettings.test.ts 2>&1 | tail -15
pnpm typecheck 2>&1 | tail -10
```

Expected: PASS, no type errors.

- [ ] **Step 6: Commit**

```bash
git add src/components/AdapterSettings.vue src/components/AdapterSettings.test.ts src/components/ApiKeySettings.vue
git commit --no-gpg-sign -m "feat(ui): add LLM adapter settings panel to settings modal"
```

---

## Task 3: Plugin Status UI

**Files:**
- Create: `src/components/PluginSettings.vue`
- Modify: `src/components/ApiKeySettings.vue` (add `plugins` section)

Backend: `GET /api/plugins/{id}` route is proxied via the plugin registry. To list plugins, we need to check `GET /api/adapters` returns adapter metadata — BUT for plugin status, the router registers each plugin at `/api/plugins/{id}`. There is no `GET /api/plugins` list endpoint yet.

**Backend addition needed:** Add `GET /api/plugins` list endpoint.

- [ ] **Step 1: Add `GET /api/plugins` endpoint in Go**

In `server/internal/api/router.go`, find where `PluginRegistry` routes are mounted (around line 221) and add a list handler before the per-plugin proxy mounts:

```go
// GET /api/plugins — returns list of all loaded plugins with health status.
r.Get("/api/plugins", func(w http.ResponseWriter, r *http.Request) {
    type pluginInfo struct {
        ID           string   `json:"id"`
        Capabilities []string `json:"capabilities"`
        BaseURL      string   `json:"base_url"`
    }
    entries := deps.PluginRegistry.All()
    infos := make([]pluginInfo, 0, len(entries))
    for _, e := range entries {
        caps := make([]string, 0, len(e.Descriptor.Capabilities))
        for _, c := range e.Descriptor.Capabilities {
            caps = append(caps, string(c))
        }
        infos = append(infos, pluginInfo{
            ID:           e.Descriptor.ID,
            Capabilities: caps,
            BaseURL:      e.BaseURL,
        })
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(infos)
})
```

Check if `plugin.Registry` has an `All()` method. In `server/internal/plugin/registry.go`, add if missing:

```go
// All returns a snapshot of all loaded plugin entries.
func (r *Registry) All() []Entry {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]Entry, len(r.plugins))
    copy(out, r.plugins)
    return out
}
```

- [ ] **Step 2: Verify Go compiles**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
go build ./server/... 2>&1
```

Expected: no errors.

- [ ] **Step 3: Write Vitest test for `PluginSettings.vue`**

Create `src/components/PluginSettings.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import PluginSettings from './PluginSettings.vue'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(() =>
    Promise.resolve({
      ok: true,
      json: () => Promise.resolve([
        { id: 'github-oauth', capabilities: ['auth_provider'], base_url: 'http://127.0.0.1:14001' },
      ]),
    })
  ))
})

describe('PluginSettings', () => {
  it('renders plugin list', async () => {
    const w = mount(PluginSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    expect(w.text()).toContain('github-oauth')
    expect(w.text()).toContain('auth_provider')
  })

  it('shows empty state when no plugins', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve([]) })
    ))
    const w = mount(PluginSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    expect(w.text()).toMatch(/no plugins/i)
  })
})
```

- [ ] **Step 4: Run to verify it fails**

```bash
pnpm test --reporter=verbose src/components/PluginSettings.test.ts 2>&1 | tail -10
```

Expected: FAIL — component does not exist.

- [ ] **Step 5: Create `PluginSettings.vue`**

Create `src/components/PluginSettings.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface PluginInfo {
  id: string
  capabilities: string[]
  base_url: string
}

const plugins = ref<PluginInfo[]>([])
const loading = ref(true)
const error = ref<string | null>(null)

onMounted(async () => {
  try {
    const res = await fetch('/api/plugins')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    plugins.value = await res.json()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load plugins'
  } finally {
    loading.value = false
  }
})

const CAP_LABELS: Record<string, string> = {
  auth_provider: 'Auth Provider',
  route_extension: 'Route Extension',
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">Loaded Plugins</h3>
    <p class="text-xs text-slate-500 dark:text-slate-400">
      Sidecar plugins loaded from <code class="font-mono">plugin_dir</code>. Add plugins by placing a
      <code class="font-mono">plugin.json</code> + binary in the configured directory and restarting the server.
    </p>

    <div v-if="loading" class="text-xs text-slate-400">Loading plugins…</div>
    <div v-else-if="error" class="text-xs text-red-500">{{ error }}</div>
    <div v-else-if="plugins.length === 0" class="text-xs text-slate-400 italic">
      No plugins loaded. Configure <code class="font-mono">DASHBOARD_PLUGIN_DIR</code> to add plugins.
    </div>
    <div v-else class="space-y-2">
      <div
        v-for="p in plugins"
        :key="p.id"
        class="bg-slate-50 dark:bg-slate-800/50 rounded p-3 text-xs flex items-start justify-between gap-4"
      >
        <div class="space-y-1">
          <p class="font-mono font-medium text-slate-800 dark:text-slate-200">{{ p.id }}</p>
          <p class="text-slate-500">{{ p.base_url }}</p>
        </div>
        <div class="flex flex-wrap gap-1">
          <span
            v-for="cap in p.capabilities"
            :key="cap"
            class="px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-full"
          >
            {{ CAP_LABELS[cap] ?? cap }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 6: Add `plugins` tab to `ApiKeySettings.vue`**

Add `'plugins'` to the `Section` type:

```ts
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets' | 'analytics' | 'systemPrompts' | 'adapters' | 'plugins'
```

Add import:

```ts
import PluginSettings from './PluginSettings.vue'
```

Add sidebar button (after adapters button):

```html
<button
  :class="activeSection === 'plugins'
    ? 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
    : 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800'"
  @click="activeSection = 'plugins'"
>
  <span class="text-sm flex-shrink-0">🔌</span> Plugins
</button>
```

Add section panel (after adapters panel):

```html
<section v-else-if="activeSection === 'plugins'">
  <PluginSettings />
</section>
```

- [ ] **Step 7: Run tests**

```bash
pnpm test --reporter=verbose src/components/PluginSettings.test.ts 2>&1 | tail -10
pnpm typecheck 2>&1 | tail -5
go test ./server/internal/api/... -v -race 2>&1 | tail -15
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/api/router.go server/internal/plugin/registry.go \
        src/components/PluginSettings.vue src/components/PluginSettings.test.ts \
        src/components/ApiKeySettings.vue
git commit --no-gpg-sign -m "feat(ui): add plugin status panel + GET /api/plugins list endpoint"
```

---

## Task 4: Notification Settings UI

**Files:**
- Create: `src/components/NotificationSettings.vue`
- Modify: `src/components/ApiKeySettings.vue` (add `notifications` section)

Backend endpoints (already exist):
- `GET /api/tasks/settings/notifications` — list notification preferences (event types + enabled flag)
- `PUT /api/tasks/settings/notifications/:event` — toggle event
- `GET /api/tasks/settings/notification-config` — full config (webhook URL, email settings)
- `PUT /api/tasks/settings/notification-config` — update full config

- [ ] **Step 1: Write failing test**

Create `src/components/NotificationSettings.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import NotificationSettings from './NotificationSettings.vue'

const mockPrefs = [
  { event: 'task_on_hold', enabled: true },
  { event: 'task_failed', enabled: false },
  { event: 'stage_completed', enabled: true },
]
const mockConfig = { webhook_url: 'https://example.com/hook', email: '' }

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url.includes('notification-config')) return Promise.resolve({ ok: true, json: () => Promise.resolve(mockConfig) })
    if (url.includes('notifications')) return Promise.resolve({ ok: true, json: () => Promise.resolve(mockPrefs) })
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
  }))
})

describe('NotificationSettings', () => {
  it('renders event list', async () => {
    const w = mount(NotificationSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    expect(w.text()).toContain('task_on_hold')
    expect(w.text()).toContain('task_failed')
  })

  it('shows webhook URL from config', async () => {
    const w = mount(NotificationSettings)
    await new Promise(r => setTimeout(r, 0))
    await w.vm.$nextTick()
    const input = w.find('input[type="url"], input[placeholder*="webhook"], input[placeholder*="http"]')
    expect(input.exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
pnpm test --reporter=verbose src/components/NotificationSettings.test.ts 2>&1 | tail -10
```

Expected: FAIL — component not found.

- [ ] **Step 3: Create `NotificationSettings.vue`**

Create `src/components/NotificationSettings.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AppButton from './ui/AppButton.vue'

interface NotifPref {
  event: string
  enabled: boolean
}

interface NotifConfig {
  webhook_url: string
  email: string
}

const prefs = ref<NotifPref[]>([])
const cfg = ref<NotifConfig>({ webhook_url: '', email: '' })
const loading = ref(true)
const error = ref<string | null>(null)
const saving = ref(false)
const saveOk = ref(false)

const EVENT_LABELS: Record<string, string> = {
  task_on_hold: 'Task put on hold',
  task_failed: 'Task failed',
  stage_completed: 'Stage completed',
  task_completed: 'Task completed',
}

onMounted(async () => {
  try {
    const [prefsRes, cfgRes] = await Promise.all([
      fetch('/api/tasks/settings/notifications'),
      fetch('/api/tasks/settings/notification-config'),
    ])
    if (!prefsRes.ok || !cfgRes.ok) throw new Error('Failed to load notification settings')
    prefs.value = await prefsRes.json()
    cfg.value = await cfgRes.json()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Load failed'
  } finally {
    loading.value = false
  }
})

async function togglePref(pref: NotifPref) {
  const next = !pref.enabled
  try {
    const res = await fetch(`/api/tasks/settings/notifications/${pref.event}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: next }),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    pref.enabled = next
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Toggle failed'
  }
}

async function saveConfig() {
  saving.value = true
  saveOk.value = false
  error.value = null
  try {
    const res = await fetch('/api/tasks/settings/notification-config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg.value),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    saveOk.value = true
    setTimeout(() => { saveOk.value = false }, 2000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">Notifications</h3>

    <div v-if="loading" class="text-xs text-slate-400">Loading…</div>
    <div v-else-if="error" class="text-xs text-red-500">{{ error }}</div>
    <div v-else class="space-y-5">
      <!-- Event toggles -->
      <div class="space-y-2">
        <p class="text-xs font-medium text-slate-600 dark:text-slate-400">Events</p>
        <div v-for="pref in prefs" :key="pref.event" class="flex items-center justify-between py-1">
          <span class="text-xs text-slate-700 dark:text-slate-300">
            {{ EVENT_LABELS[pref.event] ?? pref.event }}
          </span>
          <button
            :class="[
              'relative inline-flex h-5 w-9 rounded-full transition-colors',
              pref.enabled ? 'bg-blue-600' : 'bg-slate-300 dark:bg-slate-600',
            ]"
            @click="togglePref(pref)"
          >
            <span
              :class="[
                'absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform',
                pref.enabled ? 'translate-x-4' : 'translate-x-0',
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Config: webhook + email -->
      <div class="space-y-3">
        <p class="text-xs font-medium text-slate-600 dark:text-slate-400">Delivery</p>
        <div class="space-y-2">
          <label class="block text-xs text-slate-600 dark:text-slate-400">
            Webhook URL
            <input
              v-model="cfg.webhook_url"
              type="url"
              placeholder="https://hooks.example.com/…"
              class="mt-1 w-full bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2 py-1.5 text-xs focus:outline-none focus:border-blue-500"
            />
          </label>
          <label class="block text-xs text-slate-600 dark:text-slate-400">
            Email (optional)
            <input
              v-model="cfg.email"
              type="email"
              placeholder="you@example.com"
              class="mt-1 w-full bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2 py-1.5 text-xs focus:outline-none focus:border-blue-500"
            />
          </label>
        </div>
        <AppButton size="sm" :disabled="saving" @click="saveConfig">
          {{ saving ? 'Saving…' : saveOk ? 'Saved!' : 'Save Config' }}
        </AppButton>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Add `notifications` tab to `ApiKeySettings.vue`**

Add `'notifications'` to `Section` type. Add import:

```ts
import NotificationSettings from './NotificationSettings.vue'
```

Add sidebar button (after plugins button):

```html
<button
  :class="activeSection === 'notifications'
    ? 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
    : 'flex items-center gap-2 w-full px-3 py-2 rounded text-left text-sm font-medium text-slate-600 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800'"
  @click="activeSection = 'notifications'"
>
  <span class="text-sm flex-shrink-0">🔔</span> Notifications
</button>
```

Add section panel:

```html
<section v-else-if="activeSection === 'notifications'">
  <NotificationSettings />
</section>
```

- [ ] **Step 5: Run tests**

```bash
pnpm test --reporter=verbose src/components/NotificationSettings.test.ts 2>&1 | tail -10
pnpm typecheck 2>&1 | tail -5
```

Expected: PASS, no type errors.

- [ ] **Step 6: Commit**

```bash
git add src/components/NotificationSettings.vue src/components/NotificationSettings.test.ts \
        src/components/ApiKeySettings.vue
git commit --no-gpg-sign -m "feat(ui): add notification settings panel to settings modal"
```

---

## Task 5: Graceful Shutdown Context (`di.go`)

**Files:**
- Modify: `server/cmd/serve/di.go`
- Modify: `server/cmd/serve/main.go` (or wherever `initializeServer` is called — check exact signature)

**Context:** `di.go:68` passes `context.Background()` as the server-lifetime context for plugin watch goroutines. These goroutines restart crashed plugins. On SIGTERM, they should stop — currently they run until the process dies. The fix: thread the server's shutdown `context.Context` into `initializeServer` and pass it to `pluginRegistry.Load`.

- [ ] **Step 1: Read the current `initializeServer` signature**

Read `server/cmd/serve/di.go` lines 1-100 and `server/cmd/serve/main.go` to understand the current signature:

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
grep -n "func initializeServer\|initializeServer(" server/cmd/serve/di.go server/cmd/serve/main.go
```

- [ ] **Step 2: Add shutdown context parameter**

In `server/cmd/serve/di.go`, change the `initializeServer` function signature from:

```go
func initializeServer(ctx context.Context, cfg *config.Config) (...) {
```

to (if it already has `ctx`, check that `ctx` is the startup context and add a new `serverCtx` param):

```go
// initializeServer wires up all server dependencies.
// ctx is the startup context (short-lived, cancelled after startup).
// serverCtx is the server's lifetime context (cancelled on SIGTERM/SIGINT).
func initializeServer(ctx, serverCtx context.Context, cfg *config.Config) (...) {
```

Replace the `pluginRegistry.Load` call:

```go
// Before:
if err := pluginRegistry.Load(ctx, context.Background(), plugin.Hooks{

// After:
if err := pluginRegistry.Load(ctx, serverCtx, plugin.Hooks{
```

Remove the now-unnecessary `// TODO` comment and the `context.Background()` import if it's only used there (check — `context` is still needed for other calls).

- [ ] **Step 3: Thread `serverCtx` from `main.go`**

In `server/cmd/serve/main.go` (or the cobra `RunE` function), find where `initializeServer` is called and pass a shutdown context:

```go
// Create a context that is cancelled on SIGTERM or SIGINT.
serverCtx, serverCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer serverCancel()

// Pass serverCtx to initializeServer.
handler, cleanup, err := initializeServer(startupCtx, serverCtx, cfg)
```

If `signal.NotifyContext` is not already imported, add:

```go
import "os/signal"
import "syscall"
```

- [ ] **Step 4: Verify compilation**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
go build ./server/... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Run Go tests**

```bash
task test 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/serve/di.go server/cmd/serve/main.go
git commit --no-gpg-sign -m "fix(server): thread server shutdown context into plugin watch goroutines"
```

---

## Task 6: SSOT `MAX_DESCRIPTION_CHARS` (`constants.ts`)

**Files:**
- Modify: `src/utils/validation.ts` — add `MAX_DESCRIPTION_CHARS` export
- Modify: `server/constants.ts` — import from `../src/utils/validation.js` instead of local const

This is a minor SSOT fix. The constant is currently defined only in `server/constants.ts` with a TODO to move it to shared code so client-side validation can also use it without importing from the server layer.

- [ ] **Step 1: Add to `src/utils/validation.ts`**

Read current contents of `src/utils/validation.ts`, then add at the end:

```ts
/** Maximum characters allowed in a task description. */
export const MAX_DESCRIPTION_CHARS = 10_000
```

- [ ] **Step 2: Update `server/constants.ts`**

Remove the local definition:

```ts
// REMOVE:
// TODO: move to src/utils/validation.ts for client-side validation
export const MAX_DESCRIPTION_CHARS = 10_000
```

Add re-export from shared validation:

```ts
export { MAX_DESCRIPTION_CHARS } from '../src/utils/validation.js'
```

- [ ] **Step 3: Verify typecheck**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm typecheck 2>&1 | tail -10
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add src/utils/validation.ts server/constants.ts
git commit --no-gpg-sign -m "refactor: move MAX_DESCRIPTION_CHARS to src/utils/validation.ts (SSOT)"
```

---

## Task 7: Refine Handler Tests

**Files:**
- Create: `server/internal/api/refine/handler_test.go`

The refine handler (`server/internal/api/refine/handler.go`) has three routes:
- `GET /api/refine/{taskId}/turns` — returns turn list
- `POST /api/refine/{taskId}/turn` — submit user message, stream assistant SSE response
- `POST /api/refine/{taskId}/confirm` — mark confirmed

The `submitTurn` route calls `refine.RunRefinementTurn` which spawns a Claude process. For tests, we need to inject a fake implementation. The handler already takes repos as constructor args — we need to mock `refine.RunRefinementTurn`. Since it's a package-level function, the cleanest approach is to make the handler accept a `spawner` func via constructor injection (add a `Spawner` field).

Check current handler: `NewHandler(turns repo.RefinementTurnRepo, tasks repo.TaskRepo)`. We add an optional `Spawner func(ctx, cfg) (<-chan string, error)`.

- [ ] **Step 1: Add `Spawner` field to handler**

In `server/internal/api/refine/handler.go`, modify the `Handler` struct:

```go
// Handler handles /api/refine routes.
type Handler struct {
    turns   repo.RefinementTurnRepo
    tasks   repo.TaskRepo
    spawner func(ctx context.Context, cfg refine.SpawnConfig) (<-chan string, error)
}

// NewHandler creates a Handler backed by the given repos.
// spawner defaults to refine.RunRefinementTurn if nil.
func NewHandler(turns repo.RefinementTurnRepo, tasks repo.TaskRepo) *Handler {
    return &Handler{
        turns:   turns,
        tasks:   tasks,
        spawner: refine.RunRefinementTurn,
    }
}

// withSpawner returns a copy of h with a custom spawner (for testing).
func (h *Handler) withSpawner(fn func(ctx context.Context, cfg refine.SpawnConfig) (<-chan string, error)) *Handler {
    cp := *h
    cp.spawner = fn
    return &cp
}
```

In `submitTurn`, replace the direct call:

```go
// Before:
stream, err := refine.RunRefinementTurn(r.Context(), cfg)

// After:
stream, err := h.spawner(r.Context(), cfg)
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./server/internal/api/refine/... 2>&1
```

Expected: no errors.

- [ ] **Step 3: Write tests**

Create `server/internal/api/refine/handler_test.go`:

```go
package refine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	apirefine "github.com/lx-wnk/agent-dashboard/server/internal/api/refine"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
)

// --- fakes ---

type fakeTurnRepo struct {
	turns []repo.RefinementTurn
}

func (f *fakeTurnRepo) ListForTask(_ context.Context, taskID string, _ int) ([]repo.RefinementTurn, error) {
	var out []repo.RefinementTurn
	for _, t := range f.turns {
		if t.TaskID == taskID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTurnRepo) ListForTaskNewest(_ context.Context, taskID string, limit int) ([]repo.RefinementTurn, error) {
	return f.ListForTask(context.Background(), taskID, 0)
}

func (f *fakeTurnRepo) Create(_ context.Context, inp repo.CreateTurnInput) (*repo.RefinementTurn, error) {
	phase := inp.Phase
	t := repo.RefinementTurn{
		ID:        "turn-" + inp.Role,
		TaskID:    inp.TaskID,
		Role:      repo.TurnRole(inp.Role),
		Content:   inp.Content,
		Phase:     phase,
		CreatedAt: time.Now(),
	}
	f.turns = append(f.turns, t)
	return &t, nil
}

type fakeTaskRepo struct{}

func (f *fakeTaskRepo) GetByID(_ context.Context, id string) (*repo.Task, error) {
	return &repo.Task{ID: id, Title: "Test Task", Cwd: "/tmp"}, nil
}

func makeRouter(turns *fakeTurnRepo, spawner func(context.Context, refine.SpawnConfig) (<-chan string, error)) http.Handler {
	h := apirefine.NewHandler(turns, &fakeTaskRepo{})
	if spawner != nil {
		h = h.WithSpawner(spawner)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// --- tests ---

func TestListTurns_Empty(t *testing.T) {
	repo := &fakeTurnRepo{}
	r := makeRouter(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/refine/task-1/turns", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var turns []any
	if err := json.Unmarshal(rr.Body.Bytes(), &turns); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("want empty list, got %d items", len(turns))
	}
}

func TestListTurns_ReturnsTurns(t *testing.T) {
	phase := "drafting"
	repo := &fakeTurnRepo{
		turns: []repo.RefinementTurn{
			{ID: "t1", TaskID: "task-1", Role: "user", Content: "Hello", CreatedAt: time.Now()},
			{ID: "t2", TaskID: "task-1", Role: "assistant", Content: "Hi", Phase: &phase, CreatedAt: time.Now()},
		},
	}
	r := makeRouter(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/refine/task-1/turns", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var turns []map[string]any
	json.Unmarshal(rr.Body.Bytes(), &turns)
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
}

func TestSubmitTurn_StreamsResponse(t *testing.T) {
	spawner := func(_ context.Context, _ refine.SpawnConfig) (<-chan string, error) {
		ch := make(chan string, 2)
		ch <- "Hello"
		ch <- " world"
		close(ch)
		return ch, nil
	}
	repo := &fakeTurnRepo{}
	r := makeRouter(repo, spawner)
	body, _ := json.Marshal(map[string]string{"message": "test prompt"})
	req := httptest.NewRequest(http.MethodPost, "/api/refine/task-1/turn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("want SSE content-type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "Hello") {
		t.Errorf("SSE body missing streamed content: %s", rr.Body.String())
	}
}

func TestConfirm_StoresSentinel(t *testing.T) {
	repo := &fakeTurnRepo{}
	r := makeRouter(repo, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/refine/task-1/confirm", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	found := false
	for _, t := range repo.turns {
		if t.Phase != nil && *t.Phase == "confirmed" {
			found = true
		}
	}
	if !found {
		t.Error("want confirmed sentinel turn to be stored")
	}
}

func TestSubmitTurn_RequiresMessage(t *testing.T) {
	repo := &fakeTurnRepo{}
	r := makeRouter(repo, nil)
	body, _ := json.Marshal(map[string]string{"message": "   "})
	req := httptest.NewRequest(http.MethodPost, "/api/refine/task-1/turn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}
```

Note: The test uses `h.WithSpawner(...)` — rename the `withSpawner` method to `WithSpawner` (exported) so tests in `_test` package can access it.

- [ ] **Step 4: Fix `withSpawner` → `WithSpawner` in handler**

In `handler.go`, rename `withSpawner` to `WithSpawner`.

Also check that `repo.RefinementTurn`, `repo.Task`, `repo.CreateTurnInput`, `repo.TurnRole` match the actual types in `server/internal/db/repo/`. Run:

```bash
grep -n "type RefinementTurn\|type Task \|type CreateTurnInput\|type TurnRole" server/internal/db/repo/*.go
```

Adjust field names in the test if they differ.

- [ ] **Step 5: Run tests**

```bash
go test ./server/internal/api/refine/... -v -race 2>&1
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/refine/handler.go server/internal/api/refine/handler_test.go
git commit --no-gpg-sign -m "test(refine): add unit tests for refine handler; inject spawner for testability"
```

---

## Final: Full Test Suite + README Update

- [ ] **Step 1: Run full test suite**

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
task test 2>&1 | tail -30
pnpm test 2>&1 | tail -20
pnpm typecheck 2>&1 | tail -10
```

Expected: all green.

- [ ] **Step 2: Update README feature list**

In `README.md`, find the `## Features` section and add:

```markdown
- **LLM Adapter Settings** — switch pipeline stage LLM backend (Claude CLI, Ollama, OpenAI, custom) from Settings
- **Plugin Status Panel** — view loaded sidecar plugins and their capabilities in Settings → Plugins
- **Notification Settings** — configure webhook/email delivery and per-event toggles in Settings
- **Task list from live sessions** — agent modal now shows live Claude Code task checklists parsed from session JSONL
```

- [ ] **Step 3: Update project memory**

Update `.agent-context/memory/log.md` with a session entry:

```markdown
## 2026-05-17 — Gap resolution: all 7 gaps closed on feat/gap-resolution
- Task extraction (parser.go), Adapter/Plugin/Notification UIs, graceful shutdown context, SSOT fix, refine tests
- Branch: feat/gap-resolution → target: upcoming
```

- [ ] **Step 4: Final commit**

```bash
git add README.md .agent-context/memory/log.md
git commit --no-gpg-sign -m "docs: update README features + memory log for gap-resolution"
```

---

## Self-Review

**Spec coverage check:**
- G1 Task extraction ✓ Task 1
- G2 LLM Adapter UI ✓ Task 2
- G3 Plugin UI ✓ Task 3
- G4 Graceful shutdown ✓ Task 5
- G5 SSOT MAX_DESCRIPTION_CHARS ✓ Task 6
- G6 Refine tests ✓ Task 7
- G7 Notification UI ✓ Task 4

**Placeholder scan:** No TBDs, TODOs, or "implement later" in any task. All code blocks are complete.

**Type consistency:**
- `repo.RefinementTurn`, `repo.CreateTurnInput`, `repo.TurnRole` used in Task 7 — note to verify actual field names against DB repo structs before running tests.
- `refine.SpawnConfig` used in Task 7 spawner — matches `handler.go` which already references it.
- `plugin.Entry.Descriptor.Capabilities` used in Task 3 — verify `Capabilities` is `[]Capability` (check `plugin/types.go`).

**Dependency order:**
- Tasks 2, 3, 4 all modify `ApiKeySettings.vue`. Execute sequentially to avoid merge conflicts.
- Tasks 1, 5, 6, 7 are independent and can run in any order.
