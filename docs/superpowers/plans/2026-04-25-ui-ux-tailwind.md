# Tailwind v4 UI/UX Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all handwritten CSS (CSS custom properties + scoped `<style>` blocks) across 27 Vue components with Tailwind CSS v4 utilities, using 5 shared primitive `ui/` wrapper components.

**Architecture:** Pure Tailwind v4 utilities (no `@apply`) with primitive Vue wrapper components in `src/components/ui/` that encapsulate dark/light variant logic. Feature components hold only business logic — no `<style>` blocks. Dark mode via `class="dark"` on `<html>`, toggled by the existing `useTheme.ts` composable.

**Tech Stack:** Tailwind CSS v4, `@tailwindcss/vite` plugin, Vue 3, TypeScript

---

## CSS Variable → Tailwind Class Reference

Every component migration uses this mapping. Refer back to it for each task.

| CSS Variable | Light (default) | Dark (`dark:`) |
|---|---|---|
| `var(--bg-primary)` | `bg-slate-50` | `dark:bg-slate-950` |
| `var(--bg-secondary)` | `bg-white` | `dark:bg-slate-900` |
| `var(--bg-tertiary)` | `bg-slate-100` | `dark:bg-slate-800` |
| `var(--text-primary)` | `text-slate-900` | `dark:text-slate-100` |
| `var(--text-secondary)` | `text-slate-500` | `dark:text-slate-400` |
| `var(--text-muted)` | `text-slate-400` | `dark:text-slate-600` |
| `var(--accent-green)` text | `text-green-600` | `dark:text-green-400` |
| `var(--accent-green)` bg btn | `bg-green-600` | `dark:bg-green-600` |
| `var(--accent-green)` bg badge | `bg-green-50` | `dark:bg-green-950` |
| `var(--accent-yellow)` text | `text-yellow-600` | `dark:text-yellow-400` |
| `var(--accent-yellow)` bg badge | `bg-yellow-50` | `dark:bg-yellow-950` |
| `var(--accent-blue)` text | `text-blue-600` | `dark:text-blue-400` |
| `var(--accent-blue)` bg btn | `bg-blue-600` | `dark:bg-blue-600` |
| `var(--accent-blue)` bg badge | `bg-blue-50` | `dark:bg-blue-950` |
| `var(--accent-red)` text | `text-red-600` | `dark:text-red-400` |
| `var(--accent-red)` bg badge | `bg-red-50` | `dark:bg-red-950` |
| `var(--accent-gray)` | `text-slate-400` | `dark:text-slate-500` |
| `var(--border)` | `border-slate-200` | `dark:border-slate-700` |
| `var(--font-mono)` | `font-mono` | — |

---

## File Structure

**Create:**
- `src/styles/main.css` — Tailwind entry point, `@theme`, global styles, markdown CSS
- `src/components/ui/AppBadge.vue` — status variant badge primitive
- `src/components/ui/AppButton.vue` — button with variant + size props
- `src/components/ui/AppCard.vue` — card wrapper (gradient dark, shadow light)
- `src/components/ui/AppModal.vue` — modal backdrop + box (replaces BaseModal.vue)
- `src/components/ui/AppInput.vue` — input/textarea with consistent dark+light styles

**Modify (setup):**
- `vite.config.ts` — add `@tailwindcss/vite` plugin
- `src/main.ts` — import `./styles/main.css`
- `src/composables/useTheme.ts` — `setAttribute('data-theme')` → `classList.toggle('dark')`
- `index.html` — update FOUC script to use `.classList.add('dark')`
- `src/App.vue` — remove `<style>` block, rewrite template classes

**Modify (migrate — each removes its `<style>` block):**
ToolTimeline, TaskList, MachineBadge, CrossLinkBanner, CostTrend, ResourceBar, SubAgentList, AgentRow, SubAgentRow, AgentCard, AgentCardGrid, AgentTable, KanbanBoard, TaskCard, PipelineBoard, PromptInput, AgentChatStream, StageOutputView, AgentModal, TaskModal, SpawnDialog, BacklogForm, SessionList, ApiKeySettings

**Delete:**
- `src/components/BaseModal.vue` — superseded by `src/components/ui/AppModal.vue`
- `src/components/StatusBadge.vue` — superseded by `src/components/ui/AppBadge.vue`

---

## Task 1: Install Tailwind v4 and wire the Vite plugin

**Files:**
- Modify: `package.json` (via pnpm)
- Modify: `vite.config.ts`

- [ ] **Step 1: Install Tailwind v4**

```bash
cd /Users/alexanderwink/code/_privat/claude-agent-overview
pnpm add -D tailwindcss @tailwindcss/vite
```

Expected: `tailwindcss` and `@tailwindcss/vite` appear in `devDependencies` in `package.json`.

- [ ] **Step 2: Add plugin to vite.config.ts**

Replace the entire file:

```ts
import process from 'node:process'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

const DASHBOARD_PORT = process.env.DASHBOARD_PORT || '13120'

export default defineConfig({
  plugins: [tailwindcss(), vue()],
  server: {
    proxy: {
      '/api': `http://localhost:${DASHBOARD_PORT}`,
    },
  },
})
```

- [ ] **Step 3: Verify dev server starts**

```bash
pnpm dev
```

Expected: Server starts on port 13120 without errors. The app renders (styles may be broken — that's expected until Task 2).

- [ ] **Step 4: Commit**

```bash
git add vite.config.ts pnpm-lock.yaml package.json
git commit -m "feat: install tailwindcss v4 + @tailwindcss/vite plugin"
```

---

## Task 2: Create CSS entry point + global styles

**Files:**
- Create: `src/styles/main.css`
- Modify: `src/main.ts`

- [ ] **Step 1: Create src/styles/main.css**

```css
@import "tailwindcss";

/* Dark mode activated by class="dark" on <html> */
@custom-variant dark (&:where(.dark, .dark *));

@theme {
  --font-mono: 'SF Mono', 'Fira Code', 'Cascadia Code', 'Menlo', monospace;
}

/* Scrollbar */
:root {
  scrollbar-width: thin;
  scrollbar-color: var(--color-slate-700) transparent;
}
::-webkit-scrollbar { width: 6px; height: 6px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--color-slate-700); border-radius: 3px; }
::-webkit-scrollbar-thumb:hover { background: var(--color-slate-500); }

/* Global markdown styles for v-html content (AgentChatStream) — cannot be scoped */
.markdown-body { white-space: normal; }
.markdown-body p { margin: 0 0 0.4em; }
.markdown-body p:last-child { margin-bottom: 0; }
.markdown-body code {
  background: rgb(255 255 255 / 0.08);
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 12px;
  font-family: var(--font-mono);
}
.markdown-body pre {
  background: var(--color-slate-950);
  border-radius: 4px;
  padding: 8px;
  overflow-x: auto;
  margin: 4px 0;
}
.markdown-body pre code { background: none; padding: 0; }
.markdown-body ul, .markdown-body ol { margin: 4px 0; padding-left: 1.4em; }
.markdown-body strong { color: var(--color-slate-100); }
.dark .markdown-body strong { color: var(--color-slate-100); }
.markdown-body a { color: var(--color-blue-400); }
.markdown-body table { border-collapse: collapse; width: 100%; margin: 6px 0; font-size: 12px; }
.markdown-body th, .markdown-body td {
  border: 1px solid var(--color-slate-700);
  padding: 4px 8px;
  text-align: left;
}
.markdown-body th { background: var(--color-slate-900); color: var(--color-slate-100); font-weight: 600; }
.markdown-body blockquote {
  border-left: 3px solid var(--color-slate-700);
  margin: 4px 0;
  padding: 2px 10px;
  color: var(--color-slate-500);
}
.markdown-body hr { border: none; border-top: 1px solid var(--color-slate-700); margin: 8px 0; }

/* Pulse animation reused across components */
@keyframes pulse {
  0%, 100% { opacity: 0.4; }
  50% { opacity: 1; }
}
```

- [ ] **Step 2: Update src/main.ts**

```ts
import './styles/main.css'
import { createApp } from 'vue'
import App from './App.vue'

createApp(App).mount('#app')
```

- [ ] **Step 3: Commit**

```bash
git add src/styles/main.css src/main.ts
git commit -m "feat: add tailwind v4 css entry point and global styles"
```

---

## Task 3: Update dark mode mechanism

**Files:**
- Modify: `src/composables/useTheme.ts`
- Modify: `index.html`

- [ ] **Step 1: Update applyTheme in useTheme.ts**

Change only the `applyTheme` function (line 17):

```ts
function applyTheme(t: Theme) {
  document.documentElement.classList.toggle('dark', t === 'dark')
}
```

- [ ] **Step 2: Update FOUC prevention in index.html**

Replace the existing `<script>` block inside `<body>`:

```html
<script>
  (function() {
    var stored = localStorage.getItem('agent-theme');
    var isDark = stored === 'dark'
      || (stored !== 'light' && !window.matchMedia('(prefers-color-scheme: light)').matches);
    if (isDark) document.documentElement.classList.add('dark');
  })();
</script>
```

- [ ] **Step 3: Verify theme toggle works**

```bash
pnpm dev
```

Open `http://localhost:13120`, open DevTools, check that `<html>` element gains/loses `class="dark"` when the theme toggle button is clicked (once one exists). For now just verify no JS errors.

- [ ] **Step 4: Commit**

```bash
git add src/composables/useTheme.ts index.html
git commit -m "feat: switch dark mode from data-theme attribute to class=dark"
```

---

## Task 4: Remove App.vue CSS variables block

**Files:**
- Modify: `src/App.vue`

- [ ] **Step 1: Delete the entire `<style>` block from App.vue**

Remove lines 208–521 (the entire `<style>` block starting with `:root {` and ending with the last closing brace). The `<script setup>` and `<template>` sections are untouched in this task.

After deletion, `src/App.vue` ends at the closing `</template>` tag with no `<style>` block.

- [ ] **Step 2: Verify build still compiles**

```bash
pnpm typecheck
```

Expected: 0 errors (template classes that reference removed CSS will look unstyled but TypeScript is fine).

- [ ] **Step 3: Commit**

```bash
git add src/App.vue
git commit -m "chore: remove css variable style block from App.vue (phase 1 complete)"
```

---

## Task 5: Create AppBadge.vue

**Files:**
- Create: `src/components/ui/AppBadge.vue`

- [ ] **Step 1: Create the component**

```vue
<script setup lang="ts">
type Variant = 'active' | 'waiting' | 'idle' | 'completed' | 'error' | 'info'

const props = defineProps<{ variant: Variant }>()

const dotClass: Record<Variant, string> = {
  active:    'bg-green-400 dark:bg-green-400',
  waiting:   'bg-yellow-400 dark:bg-yellow-400',
  idle:      'bg-slate-400 dark:bg-slate-500',
  completed: 'bg-slate-400 dark:bg-slate-500',
  error:     'bg-red-400 dark:bg-red-400',
  info:      'bg-blue-400 dark:bg-blue-400',
}

const labelClass: Record<Variant, string> = {
  active:    'text-green-600 dark:text-green-400',
  waiting:   'text-yellow-600 dark:text-yellow-400',
  idle:      'text-slate-400 dark:text-slate-500',
  completed: 'text-slate-400 dark:text-slate-500',
  error:     'text-red-600 dark:text-red-400',
  info:      'text-blue-600 dark:text-blue-400',
}
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-xs capitalize">
    <span class="size-2 rounded-full flex-shrink-0" :class="dotClass[variant]" />
    <span :class="labelClass[variant]">{{ variant }}</span>
  </span>
</template>
```

- [ ] **Step 2: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 3: Commit**

```bash
git add src/components/ui/AppBadge.vue
git commit -m "feat: add AppBadge primitive ui component"
```

---

## Task 6: Create AppButton.vue

**Files:**
- Create: `src/components/ui/AppButton.vue`

- [ ] **Step 1: Create the component**

```vue
<script setup lang="ts">
type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md'

withDefaults(defineProps<{
  variant?: Variant
  size?: Size
  disabled?: boolean
}>(), {
  variant: 'secondary',
  size: 'md',
  disabled: false,
})
</script>

<template>
  <button
    :disabled="disabled"
    :class="[
      'inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all font-sans border-0',
      'disabled:opacity-40 disabled:cursor-not-allowed',
      size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-1.5 text-sm',
      variant === 'primary'   && 'bg-green-600 text-white hover:brightness-110',
      variant === 'secondary' && 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 hover:brightness-110',
      variant === 'ghost'     && 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300',
      variant === 'danger'    && 'bg-red-600 text-white hover:brightness-110',
    ]"
  >
    <slot />
  </button>
</template>
```

- [ ] **Step 2: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 3: Commit**

```bash
git add src/components/ui/AppButton.vue
git commit -m "feat: add AppButton primitive ui component"
```

---

## Task 7: Create AppCard.vue

**Files:**
- Create: `src/components/ui/AppCard.vue`

- [ ] **Step 1: Create the component**

```vue
<script setup lang="ts">
withDefaults(defineProps<{
  padding?: string
  interactive?: boolean
}>(), {
  padding: 'p-4',
  interactive: false,
})
</script>

<template>
  <div
    :class="[
      'rounded-xl border border-slate-200 dark:border-slate-700/50',
      'bg-white dark:bg-gradient-to-br dark:from-slate-800 dark:to-slate-900',
      'shadow-sm dark:shadow-none',
      padding,
      interactive && 'cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-lg focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]',
    ]"
  >
    <slot />
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/ui/AppCard.vue
git commit -m "feat: add AppCard primitive ui component"
```

---

## Task 8: Create AppModal.vue

**Files:**
- Create: `src/components/ui/AppModal.vue`

- [ ] **Step 1: Create the component**

```vue
<script setup lang="ts">
withDefaults(defineProps<{
  open: boolean
  zIndex?: number
}>(), {
  zIndex: 200,
})

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog">
      <div
        v-if="open"
        class="fixed inset-0 flex items-center justify-center p-4 bg-black/55"
        :style="{ zIndex }"
        @click.self="emit('close')"
      >
        <div class="base-modal-box">
          <slot />
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style>
/* Transition styles must be global (not scoped) to target children */
.dialog-enter-active,
.dialog-leave-active {
  transition: opacity 0.2s ease;
}
.dialog-enter-active .base-modal-box,
.dialog-leave-active .base-modal-box {
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.dialog-enter-from,
.dialog-leave-to {
  opacity: 0;
}
.dialog-enter-from .base-modal-box,
.dialog-leave-to .base-modal-box {
  transform: scale(0.95);
  opacity: 0;
}
</style>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/ui/AppModal.vue
git commit -m "feat: add AppModal primitive ui component"
```

---

## Task 9: Create AppInput.vue

**Files:**
- Create: `src/components/ui/AppInput.vue`

- [ ] **Step 1: Create the component**

```vue
<script setup lang="ts">
withDefaults(defineProps<{
  type?: 'input' | 'textarea'
  modelValue?: string
  placeholder?: string
  disabled?: boolean
  rows?: number
}>(), {
  type: 'input',
  modelValue: '',
  disabled: false,
  rows: 1,
})

defineEmits<{
  'update:modelValue': [value: string]
}>()
</script>

<template>
  <textarea
    v-if="type === 'textarea'"
    :value="modelValue"
    :placeholder="placeholder"
    :disabled="disabled"
    :rows="rows"
    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans resize-none"
    @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
  />
  <input
    v-else
    :value="modelValue"
    :placeholder="placeholder"
    :disabled="disabled"
    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans"
    @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
  >
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/ui/AppInput.vue
git commit -m "feat: add AppInput primitive ui component"
```

---

## Task 10: Migrate ToolTimeline, TaskList, MachineBadge

**Files:**
- Modify: `src/components/ToolTimeline.vue`
- Modify: `src/components/TaskList.vue`
- Modify: `src/components/MachineBadge.vue`

- [ ] **Step 1: Rewrite ToolTimeline.vue**

```vue
<script setup lang="ts">
defineProps<{ tools: string[] }>()
</script>

<template>
  <div v-if="tools.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Recent Tools
    </h4>
    <div class="flex flex-wrap gap-1">
      <span
        v-for="(tool, i) in tools"
        :key="`${i}-${tool}`"
        class="text-[11px] px-2 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 font-mono"
      >
        {{ tool }}
      </span>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Rewrite TaskList.vue**

```vue
<script setup lang="ts">
import type { TaskInfo } from '../types'

defineProps<{ tasks: TaskInfo[] }>()

function statusIcon(status: string): string {
  switch (status) {
    case 'completed': return '✓'
    case 'in_progress': return '●'
    default: return '○'
  }
}
</script>

<template>
  <div v-if="tasks.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Tasks
    </h4>
    <ul class="list-none p-0 m-0">
      <li
        v-for="task in tasks"
        :key="task.id"
        class="flex items-center gap-2 py-1 text-xs text-slate-500 dark:text-slate-400"
      >
        <span class="text-[10px] w-3.5 text-center text-green-600 dark:text-green-400">{{ statusIcon(task.status) }}</span>
        <span>{{ task.subject }}</span>
      </li>
    </ul>
  </div>
</template>
```

- [ ] **Step 3: Rewrite MachineBadge.vue**

```vue
<script setup lang="ts">
defineProps<{ machine: string }>()
</script>

<template>
  <span
    :title="`Machine: ${machine}`"
    class="inline-block text-[9px] font-semibold text-blue-600 dark:text-blue-400 border border-blue-600 dark:border-blue-400 rounded px-1 max-w-[100px] overflow-hidden text-ellipsis whitespace-nowrap align-middle"
  >
    {{ machine }}
  </span>
</template>
```

- [ ] **Step 4: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
git add src/components/ToolTimeline.vue src/components/TaskList.vue src/components/MachineBadge.vue
git commit -m "feat: migrate ToolTimeline, TaskList, MachineBadge to tailwind"
```

---

## Task 11: Migrate CrossLinkBanner, CostTrend, ResourceBar

**Files:**
- Modify: `src/components/CrossLinkBanner.vue`
- Modify: `src/components/CostTrend.vue`
- Modify: `src/components/ResourceBar.vue`

- [ ] **Step 1: Rewrite CrossLinkBanner.vue**

```vue
<script setup lang="ts">
defineProps<{
  label: string
  targetName: string
  buttonText?: string
}>()

const emit = defineEmits<{ click: [] }>()
</script>

<template>
  <div class="flex items-center justify-between flex-shrink-0 px-4 py-1.5 bg-blue-50/50 dark:bg-blue-950/20 border-b border-blue-200/50 dark:border-blue-800/40">
    <span class="text-[11px] text-slate-500 dark:text-slate-400">
      ⬡ {{ label }} <strong class="text-blue-600 dark:text-blue-400">{{ targetName }}</strong>
    </span>
    <button
      class="bg-transparent border-none text-[11px] text-blue-600 dark:text-blue-400 cursor-pointer underline p-0 whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-300"
      @click="emit('click')"
    >
      {{ buttonText ?? 'Open →' }}
    </button>
  </div>
</template>
```

- [ ] **Step 2: Rewrite CostTrend.vue**

```vue
<script setup lang="ts">
import type { TrendPoint } from '../composables/useAgents'
import { computed } from 'vue'
import { formatTokens } from '../utils/format'

const props = defineProps<{ trend: TrendPoint[] }>()

const sparkData = computed(() => {
  const points = props.trend.slice(-60)
  if (points.length < 2) return []
  return points
})

const maxCost = computed(() => Math.max(...sparkData.value.map(p => p.cost), 0.01))

const costDelta = computed(() => {
  const pts = props.trend
  if (pts.length < 2) return null
  return pts[pts.length - 1].cost - pts[Math.max(0, pts.length - 61)].cost
})

const tokenDelta = computed(() => {
  const pts = props.trend
  if (pts.length < 2) return null
  return pts[pts.length - 1].tokens - pts[Math.max(0, pts.length - 61)].tokens
})
</script>

<template>
  <div v-if="sparkData.length >= 2" class="flex flex-col gap-1 px-6 py-1.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700">
    <div class="flex items-center gap-2">
      <span class="text-[11px] text-slate-400 dark:text-slate-600">Cost trend (3min)</span>
      <span v-if="costDelta !== null" class="text-[11px] font-mono" :class="costDelta > 0 ? 'text-red-600 dark:text-red-400' : 'text-slate-400 dark:text-slate-600'">
        {{ costDelta > 0 ? '+' : '' }}${{ costDelta.toFixed(2) }}
      </span>
      <span v-if="tokenDelta !== null && tokenDelta > 0" class="text-[11px] font-mono text-blue-600 dark:text-blue-400">
        +{{ formatTokens(tokenDelta) }} tok
      </span>
    </div>
    <div class="flex items-end gap-px h-6">
      <div
        v-for="(point, i) in sparkData"
        :key="i"
        class="flex-1 min-w-0.5 bg-green-600 dark:bg-green-400 rounded-t-px opacity-70 hover:opacity-100 transition-opacity"
        :style="{ height: `${Math.max(2, (point.cost / maxCost) * 100)}%` }"
        :title="`$${point.cost.toFixed(2)}`"
        :class="{ 'opacity-100': i === sparkData.length - 1 }"
      />
    </div>
  </div>
</template>
```

- [ ] **Step 3: Rewrite ResourceBar.vue**

Replace only the `<template>` and delete the `<style scoped>` block. Keep the `<script setup>` unchanged.

```vue
<template>
  <div v-if="info" class="flex items-center gap-4 px-6 py-1.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700">
    <div class="flex items-center gap-1.5" :title="`CPU: ${info.cpu.usage}% (${info.cpu.cores} cores)`">
      <span class="text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-600 w-8">CPU</span>
      <div class="w-15 h-1.5 bg-slate-100 dark:bg-slate-950 rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.cpu.usage > 85 ? 'bg-red-600 dark:bg-red-400' : info.cpu.usage > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.cpu.usage}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-slate-500 dark:text-slate-400 w-7 text-right">{{ Math.round(info.cpu.usage) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Memory: ${fmtBytes(info.memory.used)} / ${fmtBytes(info.memory.total)}`">
      <span class="text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-600 w-8">MEM</span>
      <div class="w-15 h-1.5 bg-slate-100 dark:bg-slate-950 rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.memory.usagePercent > 85 ? 'bg-red-600 dark:bg-red-400' : info.memory.usagePercent > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.memory.usagePercent}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-slate-500 dark:text-slate-400 w-7 text-right">{{ Math.round(info.memory.usagePercent) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Disk: ${fmtBytes(info.disk.used)} / ${fmtBytes(info.disk.total)}`">
      <span class="text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-600 w-8">DISK</span>
      <div class="w-15 h-1.5 bg-slate-100 dark:bg-slate-950 rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.disk.usagePercent > 85 ? 'bg-red-600 dark:bg-red-400' : info.disk.usagePercent > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.disk.usagePercent}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-slate-500 dark:text-slate-400 w-7 text-right">{{ Math.round(info.disk.usagePercent) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Load: ${info.loadAvg.map(l => l.toFixed(2)).join(', ')}`">
      <span class="text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-600 w-8">LOAD</span>
      <span class="text-[11px] font-mono text-slate-500 dark:text-slate-400">{{ info.loadAvg[0].toFixed(1) }}</span>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Commit**

```bash
git add src/components/CrossLinkBanner.vue src/components/CostTrend.vue src/components/ResourceBar.vue
git commit -m "feat: migrate CrossLinkBanner, CostTrend, ResourceBar to tailwind"
```

---

## Task 12: Migrate SubAgentList

**Files:**
- Modify: `src/components/SubAgentList.vue`

- [ ] **Step 1: Rewrite SubAgentList.vue**

```vue
<script setup lang="ts">
import type { SubAgent } from '../types'
import AppBadge from './ui/AppBadge.vue'

defineProps<{ subagents: SubAgent[] }>()
</script>

<template>
  <div v-if="subagents.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Subagents ({{ subagents.length }})
    </h4>
    <div v-for="sa in subagents" :key="sa.id" class="p-2 rounded-md bg-slate-50 dark:bg-slate-950 mb-1.5">
      <div class="flex items-center gap-2">
        <AppBadge :variant="sa.status" />
        <span class="font-mono text-[11px] text-slate-500 dark:text-slate-400">{{ sa.id.substring(0, 16) }}</span>
      </div>
      <div v-if="sa.type !== 'unknown'" class="text-[11px] text-slate-400 dark:text-slate-600 mt-1 truncate">
        {{ sa.type }}
      </div>
      <div v-if="sa.currentAction" class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
        Last tool: {{ sa.currentAction }}
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/SubAgentList.vue
git commit -m "feat: migrate SubAgentList to tailwind + AppBadge"
```

---

## Task 13: Migrate AgentRow and SubAgentRow

**Files:**
- Modify: `src/components/AgentRow.vue`
- Modify: `src/components/SubAgentRow.vue`

- [ ] **Step 1: Rewrite AgentRow.vue**

```vue
<script setup lang="ts">
import type { Agent } from '../types'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AppBadge from './ui/AppBadge.vue'
import MachineBadge from './MachineBadge.vue'

defineProps<{ agent: Agent, expanded: boolean }>()
defineEmits<{ select: [agent: Agent], toggleSubagents: [] }>()
</script>

<template>
  <tr
    class="cursor-pointer transition-colors hover:bg-slate-50 dark:hover:bg-slate-900 focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
    tabindex="0"
    role="row"
    :aria-label="`Open details for ${agent.projectName}`"
    @click="$emit('select', agent)"
    @keydown.enter="$emit('select', agent)"
    @keydown.space.prevent="$emit('select', agent)"
  >
    <td class="w-24 px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-sm">
      <AppBadge :variant="agent.status" />
    </td>
    <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-sm text-slate-900 dark:text-slate-100 font-medium">
      {{ agent.projectName }}
      <span
        v-if="agent.channelAvailable"
        title="Channel active"
        class="inline-block ml-1.5 px-1 text-[9px] font-semibold text-green-600 dark:text-green-400 border border-green-600 dark:border-green-400 rounded align-middle tracking-wider"
      >CH</span>
      <MachineBadge v-if="agent.machine" :machine="agent.machine" />
    </td>
    <td class="max-w-[250px] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-sm text-slate-500 dark:text-slate-400">
      {{ agent.currentAction || '—' }}
    </td>
    <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 whitespace-nowrap">
      {{ shortModel(agent.model) }}
    </td>
    <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-xs font-mono text-slate-400 dark:text-slate-600 whitespace-nowrap">
      {{ formatTokens(totalTokenCount(agent.tokenUsage)) }}
    </td>
    <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-xs font-mono text-green-600 dark:text-green-400 whitespace-nowrap">
      {{ formatCost(agent.costEstimate) }}
    </td>
    <td class="w-20 px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600">
      {{ formatUptime(agent.uptime) }}
    </td>
    <td class="w-[70px] px-3 py-2.5 border-b border-slate-200 dark:border-slate-800 text-xs font-mono text-slate-400 dark:text-slate-600">
      {{ agent.pid }}
    </td>
    <td class="w-[50px] text-center px-3 py-2.5 border-b border-slate-200 dark:border-slate-800">
      <button
        v-if="agent.subagents.length > 0"
        class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-[11px] px-1.5 py-0.5 rounded hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-slate-300"
        @click.stop="$emit('toggleSubagents')"
      >
        {{ expanded ? '▼' : '▶' }} {{ agent.subagents.length }}
      </button>
    </td>
  </tr>
</template>
```

- [ ] **Step 2: Rewrite SubAgentRow.vue**

```vue
<script setup lang="ts">
import type { SubAgent } from '../types'
import AppBadge from './ui/AppBadge.vue'

defineProps<{ subagent: SubAgent }>()
</script>

<template>
  <tr>
    <td class="px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900">
      <AppBadge :variant="subagent.status" />
    </td>
    <td colspan="2" class="pl-9 pr-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900 max-w-[350px] overflow-hidden text-ellipsis whitespace-nowrap">
      <span class="text-slate-400 dark:text-slate-600 mr-1.5">↳</span>
      {{ subagent.type === 'unknown' ? subagent.id.substring(0, 16) : subagent.type }}
    </td>
    <td class="px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900">
      {{ subagent.currentAction || '—' }}
    </td>
    <td colspan="5" class="bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800" />
  </tr>
</template>
```

- [ ] **Step 3: Commit**

```bash
git add src/components/AgentRow.vue src/components/SubAgentRow.vue
git commit -m "feat: migrate AgentRow, SubAgentRow to tailwind + AppBadge"
```

---

## Task 14: Migrate AgentCard

**Files:**
- Modify: `src/components/AgentCard.vue`

- [ ] **Step 1: Rewrite AgentCard.vue**

```vue
<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AppBadge from './ui/AppBadge.vue'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const totalTokens = computed(() => totalTokenCount(props.agent.tokenUsage))
</script>

<template>
  <div
    class="rounded-lg overflow-hidden cursor-pointer border border-slate-200 dark:border-slate-700/50 bg-white dark:bg-slate-900 transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_12px_rgba(0,0,0,0.3)] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
    tabindex="0"
    role="button"
    :aria-label="`Open details for ${agent.projectName}`"
    @click="$emit('select', agent)"
    @keydown.enter="$emit('select', agent)"
    @keydown.space.prevent="$emit('select', agent)"
  >
    <div class="bg-slate-50 dark:bg-slate-800 px-3 py-2 flex justify-between items-center gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <AppBadge :variant="agent.status" />
        <span class="font-semibold text-[13px] text-slate-900 dark:text-slate-100 whitespace-nowrap overflow-hidden text-ellipsis">{{ agent.projectName }}</span>
        <span class="text-[11px] text-slate-400 dark:text-slate-600 whitespace-nowrap">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }}</span>
        <MachineBadge v-if="agent.machine" :machine="agent.machine" />
      </div>
      <div class="flex-shrink-0">
        <span class="text-[11px] text-slate-400 dark:text-slate-600 whitespace-nowrap">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
      </div>
    </div>
    <div class="relative px-3 py-3 h-[150px] overflow-hidden text-[13px] leading-relaxed text-slate-500 dark:text-slate-400 font-mono">
      <template v-if="agent.lastOutput">{{ agent.lastOutput }}</template>
      <span v-else class="text-slate-400 dark:text-slate-600 italic">No output yet</span>
      <div class="absolute bottom-0 left-0 right-0 h-8 bg-gradient-to-t from-white dark:from-slate-900 to-transparent pointer-events-none" />
    </div>
    <div v-if="agent.lastBtw" class="border-t border-slate-200 dark:border-slate-700 px-3 py-2 flex flex-col gap-1 text-[12px] font-mono" @click.stop>
      <div class="text-slate-400 dark:text-slate-600 border-l-2 border-yellow-400/60 pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.message }}
      </div>
      <div v-if="agent.lastBtw.response" class="text-slate-500 dark:text-slate-400 border-l-2 border-yellow-400/60 pl-2 whitespace-nowrap overflow-hidden text-ellipsis">
        {{ agent.lastBtw.response }}
      </div>
      <div v-else class="text-slate-400 dark:text-slate-600 pl-2.5" style="animation: pulse 2s ease-in-out infinite;">...</div>
    </div>
    <PromptInput v-if="!agent.machine" :agent="agent" variant="compact" @click.stop @keydown.enter.stop @keydown.space.stop />
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentCard.vue
git commit -m "feat: migrate AgentCard to tailwind + AppBadge"
```

---

## Task 15: Migrate AgentCardGrid

**Files:**
- Modify: `src/components/AgentCardGrid.vue`

- [ ] **Step 1: Rewrite AgentCardGrid.vue**

```vue
<script setup lang="ts">
import type { Agent } from '../types'
import AgentCard from './AgentCard.vue'

defineProps<{ agents: Agent[] }>()
defineEmits<{ select: [agent: Agent] }>()
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
    <AgentCard
      v-for="agent in agents"
      :key="agent.pid"
      :agent="agent"
      @select="$emit('select', agent)"
    />
    <p v-if="agents.length === 0" class="col-span-full text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentCardGrid.vue
git commit -m "feat: migrate AgentCardGrid to tailwind"
```

---

## Task 16: Migrate AgentTable

**Files:**
- Modify: `src/components/AgentTable.vue`

- [ ] **Step 1: Replace `<template>` and delete `<style scoped>` block**

Keep the `<script setup>` unchanged. Replace the entire `<template>` section and delete `<style scoped>`:

```vue
<template>
  <div class="overflow-x-auto">
    <table class="w-full border-collapse">
      <thead>
        <tr>
          <th
            v-for="[field, label] in ([['status','Status'],['projectName','Project'],['currentAction','Current Action'],['model','Model'],['tokens','Tokens'],['costEstimate','Cost'],['uptime','Uptime'],['pid','PID']] as const)"
            :key="field"
            class="px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 border-b border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 sticky top-0 z-[1] cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-400 transition-colors focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
            tabindex="0"
            @click="toggleSort(field as SortField)"
            @keydown.enter="toggleSort(field as SortField)"
            @keydown.space.prevent="toggleSort(field as SortField)"
          >
            {{ label }}{{ sortIndicator(field as SortField) }}
          </th>
          <th class="px-3 py-2 bg-slate-50 dark:bg-slate-950 sticky top-0 z-[1] border-b border-slate-200 dark:border-slate-800" />
        </tr>
      </thead>
      <tbody>
        <template v-for="agent in sortedAgents" :key="agent.pid">
          <AgentRow
            :agent="agent"
            :expanded="expandedPids.has(agent.pid)"
            @select="$emit('select', agent)"
            @toggle-subagents="toggleSubagents(agent.pid)"
          />
          <template v-if="expandedPids.has(agent.pid)">
            <SubAgentRow
              v-for="sub in agent.subagents"
              :key="sub.id"
              :subagent="sub"
            />
          </template>
        </template>
      </tbody>
    </table>
    <p v-if="agents.length === 0" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentTable.vue
git commit -m "feat: migrate AgentTable to tailwind"
```

---

## Task 17: Migrate KanbanBoard

**Files:**
- Modify: `src/components/KanbanBoard.vue`

- [ ] **Step 1: Replace `<template>`, update import, delete `<style scoped>`**

Change `import StatusBadge from './StatusBadge.vue'` to `import AppBadge from './ui/AppBadge.vue'`. Replace template:

```vue
<template>
  <div v-if="totalTasks > 0" class="grid grid-cols-1 md:grid-cols-3 gap-4">
    <div
      v-for="col in columns"
      :key="col.key"
      class="bg-white dark:bg-slate-900 rounded-lg border border-slate-200 dark:border-slate-700 flex flex-col min-h-[200px] max-h-[calc(100vh-250px)]"
    >
      <div class="flex items-center gap-2 px-3.5 py-3 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <span
          class="text-[10px] w-3.5 text-center"
          :class="col.key === 'inProgress' ? 'text-yellow-600 dark:text-yellow-400' : col.key === 'completed' ? 'text-green-600 dark:text-green-400' : 'text-slate-400 dark:text-slate-600'"
        >{{ col.icon }}</span>
        <span class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">{{ col.title }}</span>
        <span class="ml-auto text-[11px] font-mono text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-2 py-0.5 rounded-full">{{ col.cards.length }}</span>
      </div>
      <div class="p-2.5 flex flex-col gap-2 flex-1 min-h-0 overflow-y-auto">
        <div
          v-for="card in col.cards"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)]"
          @click="$emit('select', card.agent)"
        >
          <div class="text-[13px] text-slate-900 dark:text-slate-100 leading-snug mb-2 break-words">{{ card.task.subject }}</div>
          <div class="flex items-center justify-between gap-2">
            <span class="text-[11px] font-mono text-slate-400 dark:text-slate-600 whitespace-nowrap overflow-hidden text-ellipsis min-w-0">{{ card.agent.projectName }}</span>
            <AppBadge :variant="card.agent.status" />
          </div>
        </div>
        <div v-if="col.cards.length === 0" class="py-6 text-center text-[13px] text-slate-400 dark:text-slate-600 italic">
          No tasks
        </div>
      </div>
    </div>
  </div>
  <p v-else class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
    No tasks found across agents.
  </p>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/KanbanBoard.vue
git commit -m "feat: migrate KanbanBoard to tailwind + AppBadge"
```

---

## Task 18: Migrate TaskCard

**Files:**
- Modify: `src/components/TaskCard.vue`

- [ ] **Step 1: Replace `<template>`, delete `<style scoped>`. Keep `<script setup>` unchanged.**

```vue
<template>
  <div
    class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer transition-all flex flex-col gap-1.5"
    :class="task.isBlocked ? 'opacity-60 hover:opacity-85' : 'hover:border-blue-500 dark:hover:border-blue-400 hover:-translate-y-px'"
    tabindex="0"
    role="button"
    :aria-label="`Open task ${task.title}`"
    @click="$emit('select', task)"
    @keydown.enter="$emit('select', task)"
    @keydown.space.prevent="$emit('select', task)"
  >
    <div class="flex justify-between items-baseline gap-2">
      <span class="font-mono text-[11px] text-blue-600 dark:text-blue-400 font-semibold overflow-hidden text-ellipsis whitespace-nowrap">{{ task.slug }}</span>
      <span class="text-[10px] text-slate-400 dark:text-slate-600">{{ shortDate(task.createdAt) }}</span>
    </div>
    <div class="text-[13px] font-semibold text-slate-900 dark:text-slate-100 leading-tight line-clamp-2">{{ task.title }}</div>
    <div v-if="task.description" class="text-[11px] text-slate-400 dark:text-slate-600 leading-snug line-clamp-2">{{ task.description }}</div>
    <div class="flex flex-wrap gap-1 mt-0.5">
      <span
        class="text-[10px] font-mono px-1.5 py-px rounded border"
        :class="{
          'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700': !['on_hold','approval1','approval2','umsetzung','done','cancelled'].includes(task.currentStage),
          'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/60': ['on_hold','approval1','approval2'].includes(task.currentStage),
          'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-700': task.currentStage === 'umsetzung',
          'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-300 dark:border-green-700': task.currentStage === 'done',
          'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-300 dark:border-red-700': task.currentStage === 'cancelled',
        }"
      >{{ stageLabel(task.currentStage) }}</span>
      <span
        v-if="task.latestStageRunStatus"
        class="text-[10px] font-mono font-bold uppercase tracking-wide px-1.5 py-px rounded border"
        :class="{
          'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-600/50': task.latestStageRunStatus === 'running',
          'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700': task.latestStageRunStatus === 'pending',
          'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-700/50': ['awaiting_user','on_hold'].includes(task.latestStageRunStatus),
          'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-200 dark:border-green-700/50': task.latestStageRunStatus === 'done',
          'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-200 dark:border-red-700/50': task.latestStageRunStatus === 'failed',
        }"
        :title="`Latest stage run: ${runStatusLabel(task.latestStageRunStatus)}`"
      >{{ runStatusLabel(task.latestStageRunStatus) }}</span>
      <span v-if="task.worktreePath" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700" title="Has worktree">WT</span>
      <span v-if="task.sourceBranch" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700">{{ task.sourceBranch }}</span>
      <span v-if="task.parentTaskId" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700" title="Follow-up task">↳</span>
      <span v-if="task.isUnsatisfiable" class="text-[10px] font-mono px-1.5 py-px rounded border bg-yellow-50 dark:bg-yellow-950/30 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/50" title="Unsatisfiable dep">⚠ Unsatisfiable dep</span>
      <span v-else-if="task.isBlocked" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700/50" title="Waiting for prerequisite">🔒 Blocked</span>
      <span v-if="task.currentStage === 'umsetzung'" class="text-[10px] font-mono px-1.5 py-px rounded border bg-yellow-50 dark:bg-yellow-950/30 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/50">
        max iter {{ task.maxIterations }}
      </span>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/TaskCard.vue
git commit -m "feat: migrate TaskCard to tailwind"
```

---

## Task 19: Migrate PipelineBoard

**Files:**
- Modify: `src/components/PipelineBoard.vue`

- [ ] **Step 1: Replace `<template>`, delete `<style scoped>`. Keep `<script setup>` unchanged.**

```vue
<template>
  <div class="flex gap-3 overflow-x-auto pb-4 min-h-[calc(100vh-200px)]">
    <div
      v-for="{ col, tasks } in columnsWithTasks"
      :key="col.id"
      class="flex-[1_1_260px] min-w-[240px] rounded-lg flex flex-col max-h-[calc(100vh-220px)]"
      :class="col.group === 'needs-you'
        ? 'bg-yellow-50/30 dark:bg-yellow-950/10 border border-yellow-300/60 dark:border-yellow-700/40'
        : col.group === 'terminal'
          ? 'bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 opacity-70'
          : 'bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700'"
    >
      <div class="flex justify-between items-center px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <span class="text-[11px] font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider">{{ col.label }}</span>
        <span class="text-[11px] text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-950 px-2 py-px rounded-full font-mono">{{ tasks.length }}</span>
      </div>
      <div v-if="col.hint" class="text-[10px] text-yellow-600 dark:text-yellow-400 px-3 pb-1.5 uppercase tracking-wider font-semibold">
        {{ col.hint }}
      </div>
      <div class="p-2.5 flex flex-col gap-2 overflow-y-auto">
        <TaskCard
          v-for="task in tasks"
          :key="task.id"
          :task="task"
          @select="(t) => $emit('select', t)"
        />
        <div v-if="!tasks.length" class="text-center text-slate-400 dark:text-slate-600 text-[11px] py-5">
          —
        </div>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/PipelineBoard.vue
git commit -m "feat: migrate PipelineBoard to tailwind"
```

---

## Task 20: Migrate PromptInput

**Files:**
- Modify: `src/components/PromptInput.vue`

- [ ] **Step 1: Replace `<template>`, delete `<style scoped>`. Keep `<script setup>` unchanged.**

```vue
<template>
  <div class="relative" :class="variant">
    <div v-if="showSuggestions" class="absolute bottom-full left-0 right-0 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 border-b-0 rounded-t-md max-h-60 overflow-y-auto z-10">
      <button
        v-for="(cmd, i) in slashSuggestions"
        :key="cmd.name"
        class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-slate-500 dark:text-slate-400 text-[13px] font-mono cursor-pointer text-left hover:bg-slate-100 dark:hover:bg-slate-800"
        :class="{ 'bg-slate-100 dark:bg-slate-800': i === selectedIndex }"
        @mousedown.prevent="selectSuggestion(cmd)"
      >
        <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
        <span class="text-slate-400 dark:text-slate-600 text-xs">{{ cmd.description }}</span>
      </button>
    </div>
    <div
      class="border-t border-slate-200 dark:border-slate-700 flex items-end"
      :class="variant === 'full' ? 'px-4 py-2.5 gap-2 flex-shrink-0' : 'px-3 py-2 gap-1.5 items-center'"
    >
      <span
        class="text-blue-600 dark:text-blue-400 flex-shrink-0 pb-0.5"
        :class="variant === 'full' ? 'text-[14px]' : 'text-[13px] pb-0'"
      >❯</span>
      <textarea
        v-if="variant === 'full'"
        ref="inputEl"
        v-model="promptInput"
        rows="1"
        placeholder="Enter prompt..."
        :disabled="isSending"
        class="flex-1 bg-transparent border-none text-slate-900 dark:text-slate-100 text-[13px] font-mono outline-none placeholder:text-slate-400 dark:placeholder:text-slate-600 disabled:opacity-50 resize-none leading-snug min-h-[22px] max-h-36 overflow-y-auto"
        @keydown="onKeydown"
        @input="autoResize"
      />
      <input
        v-else
        ref="inputEl"
        v-model="promptInput"
        placeholder="Enter prompt..."
        :disabled="isSending"
        class="flex-1 bg-transparent border-none text-slate-900 dark:text-slate-100 text-[13px] font-mono outline-none placeholder:text-slate-400 dark:placeholder:text-slate-600 disabled:opacity-50"
        @keydown="onKeydown"
      >
      <button
        class="bg-blue-600 text-white border-none rounded font-bold cursor-pointer flex-shrink-0 hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed"
        :class="variant === 'full' ? 'px-3.5 py-1.5 text-[14px]' : 'px-2.5 py-1 text-[13px]'"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : '↵' }}
      </button>
    </div>
    <p
      v-if="sendStatus"
      class="text-[11px]"
      :class="[
        variant === 'full' ? 'px-4 pb-2' : 'px-3 pb-1.5 pt-0.5',
        sendStatus === 'sent' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400',
      ]"
    >
      {{ sendStatus === 'sent' ? 'Sent' : sendError }}
    </p>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/PromptInput.vue
git commit -m "feat: migrate PromptInput to tailwind"
```

---

## Task 21: Migrate AgentChatStream

**Files:**
- Modify: `src/components/AgentChatStream.vue`

- [ ] **Step 1: Replace `<template>`, delete `<style scoped>`. Keep `<script setup>` unchanged.**

The `markdown-body` deep styles are already in `src/styles/main.css` (Task 2). Only the structural component classes need Tailwind here.

```vue
<template>
  <div ref="outputEl" class="flex flex-col gap-1.5 overflow-y-auto font-mono text-[13px] leading-relaxed">
    <div v-if="agent?.machine" class="text-slate-400 dark:text-slate-600 text-center py-12">
      Session output is not available for remote agents.
    </div>
    <div v-else-if="isLoadingOutput" class="text-slate-400 dark:text-slate-600 text-center py-12">
      Loading session output...
    </div>
    <template v-else-if="chatEntries.length > 0">
      <template v-for="(entry, i) in chatEntries" :key="i">
        <div v-if="entry.kind === 'tool_group'" class="flex justify-center">
          <details class="w-full border border-slate-200 dark:border-slate-700 rounded-md bg-slate-50 dark:bg-slate-950 text-xs">
            <summary class="px-2.5 py-1 text-slate-400 dark:text-slate-600 cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-400">
              {{ entry.calls.length }} tool call{{ entry.calls.length > 1 ? 's' : '' }}
            </summary>
            <div class="border-t border-slate-200 dark:border-slate-700 py-1">
              <details v-for="(call, j) in entry.calls" :key="j" class="px-2.5">
                <summary class="py-0.5 text-slate-500 dark:text-slate-400 text-[11px] cursor-pointer hover:text-slate-700 dark:hover:text-slate-300">
                  {{ call.toolName }}<span v-if="call.filePath" class="text-slate-400 dark:text-slate-600 ml-1.5 text-[10px]">{{ call.filePath }}</span>
                </summary>
                <pre v-if="call.result" class="bg-slate-100 dark:bg-slate-800 rounded p-2 text-[11px] text-slate-500 dark:text-slate-400 max-h-[200px] overflow-y-auto mt-1 mb-1 whitespace-pre-wrap break-words">{{ call.result }}</pre>
              </details>
            </div>
          </details>
        </div>
        <div v-else-if="entry.kind === 'task_group'" class="flex justify-center">
          <div class="w-full border border-slate-200 dark:border-slate-700 rounded-md bg-slate-50 dark:bg-slate-950 px-2.5 py-1.5 text-xs">
            <div
              v-for="(task, j) in entry.tasks"
              :key="j"
              class="flex items-baseline gap-1.5 py-px text-slate-500 dark:text-slate-400"
            >
              <span
                class="flex-shrink-0 w-3.5 text-center font-semibold"
                :class="{
                  'text-green-600 dark:text-green-400': task.status === 'completed',
                  'text-blue-600 dark:text-blue-400': task.status === 'in_progress',
                  'text-slate-400 dark:text-slate-600': task.status === 'pending',
                }"
              >{{ task.status === 'completed' ? '✓' : task.status === 'in_progress' ? '›' : '○' }}</span>
              <span
                class="font-mono"
                :class="task.status === 'completed' ? 'text-slate-400 dark:text-slate-600 line-through' : ''"
              >{{ task.subject }}</span>
            </div>
          </div>
        </div>
        <div
          v-else
          class="flex"
          :class="{
            'justify-end': entry.msg.role === 'human',
            'justify-start': entry.msg.role === 'channel_reply' || entry.msg.role === 'assistant',
            'justify-center': entry.msg.role === 'tool_call' || entry.msg.role === 'tool_result',
          }"
        >
          <div
            v-if="entry.msg.role === 'human'"
            class="max-w-[80%] px-3 py-2 rounded-xl rounded-br-sm text-[13px] leading-relaxed break-words whitespace-pre-wrap bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600"
            :class="{ 'border border-yellow-400/40': entry.msg.queued }"
          >
            {{ entry.msg.content }}
          </div>
          <div
            v-else-if="entry.msg.role === 'channel_reply'"
            class="max-w-[80%] px-3 py-2 rounded-xl rounded-bl-sm text-[13px] leading-relaxed break-words bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-l-2 border-green-500 dark:border-green-400 markdown-body"
            v-html="renderMarkdown(entry.msg.content)"
          />
          <div
            v-else-if="entry.msg.role === 'assistant'"
            class="max-w-[80%] px-3 py-2 rounded-xl rounded-bl-sm text-[13px] leading-relaxed break-words bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 markdown-body"
            v-html="renderMarkdown(entry.msg.content)"
          />
          <div v-else-if="entry.msg.role === 'subagent'" class="flex items-center gap-1.5 px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md bg-slate-50 dark:bg-slate-950 text-xs text-slate-500 dark:text-slate-400">
            <span class="text-[14px] text-blue-600 dark:text-blue-400">⑂</span>
            <span class="font-semibold text-slate-900 dark:text-slate-100 text-[11px] uppercase tracking-wide">{{ entry.msg.subagentType }}</span>
            <span class="text-slate-400 dark:text-slate-600">{{ entry.msg.content }}</span>
          </div>
        </div>
      </template>
    </template>
    <div v-else class="text-slate-400 dark:text-slate-600 text-center py-12">
      No output available for this session.
    </div>
    <div v-if="agent?.status === 'active' && agent.currentAction" class="text-slate-400 dark:text-slate-600 text-xs italic py-1" style="animation: pulse 2s ease-in-out infinite;">
      {{ agent.currentAction }}...
    </div>
  </div>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentChatStream.vue
git commit -m "feat: migrate AgentChatStream to tailwind"
```

---

## Task 22: Migrate StageOutputView

**Files:**
- Modify: `src/components/StageOutputView.vue`

- [ ] **Step 1: Read the template section (lines 1–280) to understand current structure, then replace `<template>` and delete `<style scoped>`**

CSS class → Tailwind mapping for StageOutputView:

| CSS class | Tailwind replacement |
|---|---|
| `.stage-section` | `mb-4 last:mb-0` |
| `.section-title` | `text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2` |
| `.subtask-list` | `list-none p-0 m-0 flex flex-col gap-2.5` |
| `.subtask` | `bg-slate-50 dark:bg-slate-950 rounded p-2 px-2.5 border-l-2 border-blue-500 dark:border-blue-400` |
| `.subtask-head` | `flex gap-2 items-baseline flex-wrap` |
| `.id-pill` | `font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold` |
| `.id-pill.ok` | `bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400` |
| `.id-pill.fail` | `bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400` |
| `.subtask-title` | `text-xs text-slate-900 dark:text-slate-100 leading-snug` |
| `.file-list` | `list-none p-0 pt-1.5 m-0 flex flex-wrap gap-1` |
| `.file-list code` | `font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 px-1 py-px rounded-sm inline-block` |
| `.checklist` | `list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400` |
| `.checklist li` | `py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed` |
| `.check` | `text-slate-400 dark:text-slate-600 mr-1` |
| `.kv` | `grid gap-1 text-xs mb-1` style `grid-template-columns: auto 1fr; gap: 4px 12px` |
| `.kv dt` | `text-slate-400 dark:text-slate-600 uppercase text-[10px]` |
| `.kv dd` | `text-slate-900 dark:text-slate-100` |
| `.prose` | `text-xs leading-relaxed text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2.5 py-2 rounded` |
| `.tool-list` | `list-none p-0 m-0 flex flex-col gap-1 text-[11px]` |
| `.tool-list li` | `py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded` |
| `.tool-name` | `font-mono text-blue-600 dark:text-blue-400 font-semibold` |
| `.tool-reason` | `text-slate-400 dark:text-slate-600 ml-1` |
| `.file-ref` | `font-mono text-[10px] text-slate-400 dark:text-slate-600 ml-1` |
| `.raw-fallback summary` | `cursor-pointer text-slate-400 dark:text-slate-600 text-[11px]` |
| `.raw-fallback pre` | `bg-slate-100 dark:bg-slate-800 p-2 rounded text-[11px] max-h-60 overflow-auto mt-1.5` |

Apply these mappings to each element in the template using the class attribute, then delete `<style scoped>`.

- [ ] **Step 2: Commit**

```bash
git add src/components/StageOutputView.vue
git commit -m "feat: migrate StageOutputView to tailwind"
```

---

## Task 23: Migrate AgentModal

**Files:**
- Modify: `src/components/AgentModal.vue`

- [ ] **Step 1: Update import, replace `<template>`, delete `<style scoped>`**

Change `import BaseModal from './BaseModal.vue'` → `import AppModal from './ui/AppModal.vue'`
Change `import StatusBadge from './StatusBadge.vue'` → `import AppBadge from './ui/AppBadge.vue'`

Replace `<template>`:

```vue
<template>
  <AppModal :open="!!agent" :z-index="1000" @close="emit('close')">
    <div v-if="agent" class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[900px] max-h-[80vh] flex flex-col overflow-hidden">
      <div class="bg-slate-50 dark:bg-slate-800 px-4 py-2.5 flex justify-between items-center flex-shrink-0">
        <div class="flex items-center gap-2.5 min-w-0">
          <AppBadge :variant="agent.status" />
          <span class="font-semibold text-sm text-slate-900 dark:text-slate-100">{{ agent.projectName }}</span>
          <MachineBadge v-if="agent.machine" :machine="agent.machine" />
          <span class="text-[11px] text-slate-400 dark:text-slate-600 whitespace-nowrap">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
        </div>
        <button class="bg-transparent border-none text-slate-500 dark:text-slate-400 text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
          ✕
        </button>
      </div>
      <CrossLinkBanner
        v-if="agent.pipelineTaskId"
        label="Part of"
        :target-name="agent.pipelineTaskTitle ?? `Task ${agent.pipelineTaskId.slice(0, 8)}`"
        button-text="Open →"
        @click="emit('navigate', agent.pipelineTaskId)"
      />
      <AgentChatStream
        ref="chatStreamRef"
        :agent="agent"
        :local-messages="localMessages"
        class="flex-1 p-4"
      />
      <div v-if="agent.tasks.length > 0 || agent.subagents.length > 0 || agent.lastTools.length > 0" class="border-t border-slate-200 dark:border-slate-700 flex-shrink-0">
        <details>
          <summary class="px-4 py-2 text-xs text-slate-400 dark:text-slate-600 cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-400">
            Agent Details (Tasks, Tools, Subagents)
          </summary>
          <div class="px-4 pb-3 pt-2 flex flex-col gap-3 max-h-[200px] overflow-y-auto">
            <ToolTimeline v-if="agent.lastTools.length > 0" :tools="agent.lastTools" />
            <TaskList v-if="agent.tasks.length > 0" :tasks="agent.tasks" />
            <SubAgentList v-if="agent.subagents.length > 0" :subagents="agent.subagents" />
          </div>
        </details>
      </div>
      <PromptInput v-if="!agent.machine" ref="promptInputRef" :agent="agent" variant="full" @message-sent="onMessageSent" />
    </div>
  </AppModal>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentModal.vue
git commit -m "feat: migrate AgentModal to tailwind + AppModal + AppBadge"
```

---

## Task 24: Migrate TaskModal

**Files:**
- Modify: `src/components/TaskModal.vue`

- [ ] **Step 1: Update imports**

In `<script setup>`, change:
- `import BaseModal from './BaseModal.vue'` → `import AppModal from './ui/AppModal.vue'`

- [ ] **Step 2: Replace `<template>` wrapper and delete `<style scoped>`**

CSS class → Tailwind mapping for TaskModal (apply throughout the template):

| CSS class | Tailwind |
|---|---|
| `.modal-window` | `bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[860px] max-h-[90vh] flex flex-col overflow-hidden` |
| `.modal-header` | `flex items-center justify-between px-5 py-4 border-b border-slate-200 dark:border-slate-700` |
| `.modal-header h2` | `text-lg font-semibold text-slate-900 dark:text-slate-100` |
| `.close-btn` | `bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100` |
| `.tab-bar` | `flex border-b border-slate-200 dark:border-slate-700 flex-shrink-0` |
| `.tab-btn` | `px-4 py-2.5 text-xs font-semibold text-slate-400 dark:text-slate-600 bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors` |
| `.tab-btn.active` | `text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400` |
| `.tab-content` | `flex-1 overflow-y-auto p-5 flex flex-col gap-4 min-h-0` |
| `.field` | `flex flex-col gap-1.5` |
| `.field-label` | `text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600` |
| `.field-value` | `text-sm text-slate-900 dark:text-slate-100` |
| `.field-input` (textarea/input) | `w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:border-blue-500` |
| `.textarea` | add `resize-y leading-snug` |
| `.char-count` | `text-[10px] text-slate-400 dark:text-slate-600 font-mono` |
| `.stage-run` | `px-3 py-2.5 bg-slate-50 dark:bg-slate-950 rounded-md mb-2` |
| `.stage-run-head` | `flex items-center gap-2.5 mb-1` |
| `.stage-label` | `font-semibold text-xs text-slate-900 dark:text-slate-100` |
| `.iteration` | `font-mono text-[11px] text-slate-400 dark:text-slate-600` |
| `.stage-status` | `text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono` |
| `.status-running` | `bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400` |
| `.status-done` | `bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400` |
| `.status-failed` | `bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400` |
| `.status-on_hold` | `bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400` |
| `.stage-meta` | `text-[11px] text-slate-400 dark:text-slate-600 mt-0.5` |
| `.perm-request` | `bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded-md p-3 mb-2` |
| `.perm-input` | `flex-1 min-w-0 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2 py-1.5 text-slate-900 dark:text-slate-100 text-xs focus:outline-none focus:border-blue-500` |
| `.btn` | `border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed hover:not-disabled:brightness-110` |
| `.btn-sm` | `px-2.5 py-1 text-[11px]` |
| `.btn-primary` | `bg-blue-600 text-white` |
| `.btn-secondary` | `bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400` |
| `.btn-green` | `bg-green-600 text-white` |
| `.btn-red` | `bg-red-600 text-white` |
| `.modal-actions` | `px-5 py-3 border-t border-slate-200 dark:border-slate-700 flex-shrink-0` |
| `.action-error` | `text-red-600 dark:text-red-400 text-xs mb-2` |
| `.action-info` | `text-green-600 dark:text-green-400 text-xs mb-2` |
| `.action-buttons` | `flex gap-2 justify-end` |
| `.dep-section` | `mt-4 border-t border-slate-200 dark:border-slate-700 pt-3` |
| `.dep-heading` | `text-sm font-semibold text-slate-500 dark:text-slate-400 mb-2` |
| `.dep-row` | `flex items-center gap-2 py-1 text-xs` |
| `.dep-title` | `flex-1 text-slate-900 dark:text-slate-100` |
| `.dep-met` | `bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-700` |
| `.dep-unmet` | `bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border border-red-300 dark:border-red-700/50` |
| `.dep-remove` | `bg-transparent border-none cursor-pointer text-slate-400 dark:text-slate-600 px-1 py-px text-[10px] rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400` |
| `.dep-add-form` | `flex gap-1.5 items-center flex-wrap mt-2` |
| `.dep-input` | `flex-1 min-w-0 px-2 py-1 border border-slate-200 dark:border-slate-700 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-xs` |
| `.dep-select` | `px-1.5 py-1 border border-slate-200 dark:border-slate-700 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-[11px]` |
| `.dep-add-btn` | `px-2.5 py-1 bg-blue-600 text-white border-none rounded text-xs cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed` |
| `.dep-error` | `text-[11px] text-red-600 dark:text-red-400 mt-1` |

Also change `<BaseModal` → `<AppModal` in the template.

- [ ] **Step 3: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
git add src/components/TaskModal.vue
git commit -m "feat: migrate TaskModal to tailwind + AppModal"
```

---

## Task 25: Migrate SpawnDialog and BacklogForm

**Files:**
- Modify: `src/components/SpawnDialog.vue`
- Modify: `src/components/BacklogForm.vue`

- [ ] **Step 1: Migrate SpawnDialog.vue**

Update import: `import BaseModal` → `import AppModal from './ui/AppModal.vue'`

Replace `<BaseModal` with `<AppModal` in template.

CSS class → Tailwind mapping for SpawnDialog:

| CSS class | Tailwind |
|---|---|
| `.modal-window` | `bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-xl` |
| `.modal-header` | `flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700` |
| `.modal-header h2` | `text-lg font-semibold text-slate-900 dark:text-slate-100` |
| `.close-btn` | `bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100` |
| `.modal-body` | `p-5` |
| `.field` | `mb-4` |
| `.field-label` | `block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5` |
| `.field-input` | `w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug resize-y focus:outline-none focus:border-green-500` |
| `.field-checkbox` | `flex items-center gap-2 mb-4` |
| `.yolo-hint` | `text-[10px] text-slate-400 dark:text-slate-600 font-mono` |
| `.danger-warning` | `bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded p-2 px-3 text-xs leading-relaxed text-yellow-600 dark:text-yellow-400 mb-3` |
| `.danger-confirm` | `text-xs text-red-600 dark:text-red-400 font-semibold mb-2` |
| `.status-msg` | `text-xs text-green-600 dark:text-green-400 mt-1 leading-snug` |
| `.error-msg` | `text-xs text-red-600 dark:text-red-400 mt-1 leading-snug whitespace-pre-wrap break-words max-h-[120px] overflow-y-auto` |
| `.modal-footer` | `flex justify-end gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-700` |
| `.btn` | `border-none rounded px-4 py-2 text-[13px] font-semibold cursor-pointer whitespace-nowrap font-sans` |
| `.btn-secondary` | `bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:brightness-110` |
| `.btn-primary` | `bg-green-600 text-white hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed` |

Delete `<style scoped>` block.

- [ ] **Step 2: Migrate BacklogForm.vue**

Apply the same BaseModal→AppModal swap and the same `.modal-*`, `.field-*`, `.btn-*` class patterns as SpawnDialog (they share the same visual structure). Delete `<style scoped>`.

- [ ] **Step 3: Commit**

```bash
git add src/components/SpawnDialog.vue src/components/BacklogForm.vue
git commit -m "feat: migrate SpawnDialog, BacklogForm to tailwind + AppModal"
```

---

## Task 26: Migrate SessionList and ApiKeySettings

**Files:**
- Modify: `src/components/SessionList.vue`
- Modify: `src/components/ApiKeySettings.vue`

- [ ] **Step 1: Migrate SessionList.vue**

Update import: `BaseModal` → `AppModal`. Replace `<BaseModal` with `<AppModal`.

CSS class → Tailwind mapping for SessionList:

| CSS class | Tailwind |
|---|---|
| `.modal-window` | `bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden` |
| `.modal-header` | `flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700 flex-shrink-0` |
| `.modal-header h2` | `text-lg font-semibold text-slate-900 dark:text-slate-100` |
| `.close-btn` | `bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100` |
| `.modal-body` | `flex-1 overflow-y-auto p-5` |
| `.session-list` | `flex flex-col gap-2` |
| `.session-item` | `bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5` |
| `.session-path` | `font-mono text-xs text-slate-500 dark:text-slate-400 truncate` |
| `.session-meta` | `flex gap-3 mt-1 text-[11px] text-slate-400 dark:text-slate-600` |
| `.empty-state` | `text-center py-12 text-slate-400 dark:text-slate-600 text-sm` |

Delete `<style scoped>`.

- [ ] **Step 2: Migrate ApiKeySettings.vue**

Update import: `BaseModal` → `AppModal`. Replace `<BaseModal` with `<AppModal`.

CSS class → Tailwind mapping for ApiKeySettings:

| CSS class | Tailwind |
|---|---|
| `.modal-window` | `bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-2xl max-h-[85vh] flex flex-col overflow-hidden` |
| `.settings-header` | `flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700 flex-shrink-0` |
| `.settings-header h2` | `text-lg font-semibold text-slate-900 dark:text-slate-100` |
| `.close-btn` | `bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100` |
| `.settings-body` | `flex-1 overflow-y-auto p-5 flex flex-col gap-6` |
| `.section-title` | `text-xs font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-3` |
| `.key-row` | `flex items-center gap-3 py-2 border-b border-slate-200 dark:border-slate-700 text-sm` |
| `.key-name` | `flex-1 font-mono text-xs text-slate-900 dark:text-slate-100` |
| `.key-scope` | `text-[10px] font-mono bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 px-1.5 py-px rounded` |
| `.key-created` | `text-[11px] text-slate-400 dark:text-slate-600` |
| `.delete-btn` | `bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400` |
| `.create-form` | `flex flex-col gap-3 bg-slate-50 dark:bg-slate-800/50 rounded-lg p-4` |
| `.form-row` | `flex gap-2 items-end` |
| `.form-field` | `flex flex-col gap-1 flex-1` |
| `.form-label` | `text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600` |
| `.form-input` / `.form-select` | `w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500` |
| `.create-btn` | `px-4 py-1.5 bg-green-600 text-white border-none rounded text-sm font-semibold cursor-pointer hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed` |
| `.token-display` | `font-mono text-xs bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400 p-3 rounded border border-green-200 dark:border-green-800/50 break-all` |
| `.error-msg` | `text-xs text-red-600 dark:text-red-400` |
| `.empty-state` | `text-center py-8 text-slate-400 dark:text-slate-600 text-sm` |

Delete `<style scoped>`.

- [ ] **Step 3: Commit**

```bash
git add src/components/SessionList.vue src/components/ApiKeySettings.vue
git commit -m "feat: migrate SessionList, ApiKeySettings to tailwind + AppModal"
```

---

## Task 27: Migrate App.vue template

**Files:**
- Modify: `src/App.vue`

- [ ] **Step 1: Rewrite the `<template>` in App.vue**

The `<style>` block was already deleted in Task 4. Now replace the template:

```vue
<template>
  <div class="min-h-screen bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 font-sans">
    <header class="flex items-center gap-3 px-6 py-4 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
      <h1 class="text-[18px] font-semibold text-slate-900 dark:text-slate-100">Claude Agent Overview</h1>
      <span class="text-xs text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full">
        <template v-if="viewMode !== 'pipeline'">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</template>
        <template v-else>{{ tasks.length }} task{{ tasks.length !== 1 ? 's' : '' }}</template>
      </span>
      <span v-if="totalCost > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">${{ totalCost.toFixed(2) }}</span>
      <span v-if="totalTokens > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">{{ formatTokens(totalTokens) }} tokens</span>
      <input
        v-model="searchQuery"
        type="text"
        :placeholder="viewMode === 'pipeline' ? 'Search tasks...' : 'Search agents...'"
        class="ml-auto bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-[13px] text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 w-[200px] focus:outline-none focus:border-blue-500 focus:w-[260px] transition-[width] duration-200"
      >
      <div class="flex bg-slate-100 dark:bg-slate-800 rounded-md overflow-hidden">
        <button
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode !== 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Agent monitoring dashboard"
          @click="viewMode = viewMode === 'pipeline' ? 'cards' : viewMode"
        >Dashboard</button>
        <button
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode === 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Task pipeline kanban"
          @click="viewMode = 'pipeline'"
        >Kanban</button>
      </div>
      <button
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        @click="showSessions = true"
      >Sessions</button>
      <button
        v-if="viewMode === 'pipeline'"
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="showBacklog = true"
      >+ New Task</button>
      <button
        v-else
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="showSpawnDialog = true"
      >+ New Agent</button>
      <button
        class="bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-none rounded-md px-2.5 py-1.5 text-base cursor-pointer leading-none hover:text-slate-700 dark:hover:text-slate-300 hover:brightness-110"
        title="Settings"
        @click="showSettings = true; selectAgent(null); selectTask(null); showSessions = false; showSpawnDialog = false"
      >⚙</button>
    </header>

    <ResourceBar />
    <CostTrend :trend="costTrend" />

    <div v-if="scriptPath" class="flex items-center gap-2 px-6 py-1.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 text-xs">
      <span class="text-slate-400 dark:text-slate-600 whitespace-nowrap">Channel script:</span>
      <code
        class="font-mono text-[11px] text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2 py-0.5 rounded cursor-pointer select-all transition-colors hover:text-green-600 dark:hover:text-green-400 focus-visible:outline-2 focus-visible:outline-blue-500"
        tabindex="0"
        role="button"
        :title="copied ? 'Copied!' : 'Click to copy'"
        @click="copyScript"
        @keydown.enter="copyScript"
        @keydown.space.prevent="copyScript"
      >{{ scriptPath }}</code>
      <span v-if="copied" class="text-green-600 dark:text-green-400 text-[11px]">Copied!</span>
    </div>

    <div
      class="flex items-center gap-1 px-6 py-2 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900"
      :class="{ 'invisible pointer-events-none': showSettings || viewMode === 'pipeline' }"
    >
      <button
        class="border-none px-2.5 py-1 text-xs cursor-pointer rounded-md font-sans transition-all"
        :class="viewMode === 'cards' ? 'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-500 dark:hover:text-slate-400'"
        title="Card view"
        @click="viewMode = 'cards'"
      >⊞ Cards</button>
      <button
        class="border-none px-2.5 py-1 text-xs cursor-pointer rounded-md font-sans transition-all"
        :class="viewMode === 'list' ? 'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-500 dark:hover:text-slate-400'"
        title="List view"
        @click="viewMode = 'list'"
      >≡ List</button>
    </div>

    <main class="p-6">
      <p v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600">Loading agents...</p>
      <p v-else-if="error" class="text-center py-12 text-red-600 dark:text-red-400">Error: {{ error }}</p>
      <AgentTable v-else-if="viewMode === 'list'" :agents="filteredAgents" @select="selectAgent" />
      <PipelineBoard v-else-if="viewMode === 'pipeline'" @select="selectTask" />
      <AgentCardGrid v-else :agents="filteredAgents" @select="selectAgent" />
    </main>

    <AgentModal :agent="selectedAgent" @close="selectAgent(null)" @navigate="(taskId: string) => navigateTo({ taskId })" />
    <TaskModal :task="selectedTask" @close="selectTask(null)" @navigate="(agent: Agent) => navigateTo({ agent })" />

    <Transition name="toast">
      <div
        v-if="toastMessage"
        class="fixed bottom-6 left-1/2 -translate-x-1/2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100 px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)] pointer-events-none"
      >
        {{ toastMessage }}
      </div>
    </Transition>

    <SpawnDialog :open="showSpawnDialog" @close="showSpawnDialog = false" />
    <BacklogForm :open="showBacklog" @close="showBacklog = false" />
    <SessionList :open="showSessions" :home-dir="homeDir" @close="showSessions = false" />
    <ApiKeySettings :open="showSettings" @close="showSettings = false" />
  </div>
</template>

<style>
.toast-enter-active, .toast-leave-active { transition: opacity 0.2s, transform 0.2s; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(-50%) translateY(8px); }
</style>
```

- [ ] **Step 2: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 3: Commit**

```bash
git add src/App.vue
git commit -m "feat: migrate App.vue template to tailwind"
```

---

## Task 28: Final cleanup and build verification

**Files:**
- Delete: `src/components/BaseModal.vue`
- Delete: `src/components/StatusBadge.vue`

- [ ] **Step 1: Delete superseded components**

```bash
rm src/components/BaseModal.vue src/components/StatusBadge.vue
```

- [ ] **Step 2: Verify no remaining imports of deleted files**

```bash
grep -r "BaseModal\|StatusBadge" src/ --include="*.vue" --include="*.ts"
```

Expected: no output (0 matches).

- [ ] **Step 3: Verify no remaining CSS variables or style blocks in feature components**

```bash
grep -r "var(--" src/components/ --include="*.vue" | grep -v "ui/"
```

Expected: no output.

```bash
grep -r "<style" src/components/ --include="*.vue" | grep -v "ui/AppModal"
```

Expected: only `AppModal.vue` which has a non-scoped `<style>` block for the dialog transition (intentional).

- [ ] **Step 4: Run full build**

```bash
pnpm build
```

Expected: Build succeeds with 0 errors. CSS output bundle is significantly smaller than before (Tailwind purges unused utilities).

- [ ] **Step 5: Run typecheck**

```bash
pnpm typecheck
```

Expected: 0 errors.

- [ ] **Step 6: Verify visually**

```bash
pnpm dev
```

Open `http://localhost:13120`. Verify:
- Dark mode applies when `class="dark"` on `<html>`
- Light mode works (toggle via theme button)
- Agent cards render correctly
- Modals open/close correctly
- Pipeline board renders correctly

- [ ] **Step 7: Final commit**

```bash
git add -A
git commit -m "feat: complete tailwind v4 ui/ux refactor — remove BaseModal, StatusBadge"
```
