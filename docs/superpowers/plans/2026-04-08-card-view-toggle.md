# Card View Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a list/card toggle, terminal-style agent cards with prompt input, a full-screen modal replacing AgentDetail, and a header search input to the Claude Agent Overview dashboard.

**Architecture:** Backend gets a new endpoint for full session output and extracts `lastOutput` during JSONL parsing. Frontend adds three new components (AgentCard, AgentCardGrid, AgentModal), extends useAgents with search/filter/view state, and rewires App.vue to use the modal instead of the off-canvas detail panel. Prompt submission uses channel messaging for running agents and spawn/resume for stopped ones.

**Tech Stack:** Vue 3 Composition API, TypeScript, Express, existing CSS custom properties (no framework)

**Note:** This project has no test framework configured. Verification steps use `npm run dev` and browser inspection instead of automated tests.

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `src/types.ts` | Add `lastOutput` to `Agent`, add `OutputMessage` interface |
| Modify | `server/jsonlParser.ts` | Extract last assistant message as `lastOutput`, new `parseFullSession()` function |
| Modify | `server/index.ts` | Add `GET /api/agents/:sessionId/output` endpoint |
| Modify | `server/agentMerger.ts` | Pass through `lastOutput` from session data |
| Modify | `src/composables/useAgents.ts` | Add `viewMode`, `searchQuery`, `filteredAgents` |
| Create | `src/components/AgentCard.vue` | Single terminal-style card tile |
| Create | `src/components/AgentCardGrid.vue` | Responsive grid of AgentCard components |
| Create | `src/components/AgentModal.vue` | Full-screen modal overlay (replaces AgentDetail) |
| Modify | `src/App.vue` | Add view toggle + search input in header, swap AgentDetail for AgentModal |
| Delete | `src/components/AgentDetail.vue` | Replaced by AgentModal |

---

### Task 1: Extend Types

**Files:**
- Modify: `src/types.ts`

- [ ] **Step 1: Add `lastOutput` to Agent interface and `OutputMessage` type**

In `src/types.ts`, add `lastOutput` field to `Agent` and add the `OutputMessage` interface:

```typescript
// Add to Agent interface, after channelAvailable:
  lastOutput: string | null

// Add at end of file:
export interface OutputMessage {
  role: 'assistant' | 'tool_call' | 'tool_result'
  content: string
  timestamp?: string
  toolName?: string
  filePath?: string
}
```

- [ ] **Step 2: Verify dev server starts**

Run: `npm run dev`
Expected: No TypeScript errors at startup. Existing pages work (browser shows dashboard).

- [ ] **Step 3: Commit**

```bash
git add src/types.ts
git commit -m "feat: add lastOutput and OutputMessage types"
```

---

### Task 2: Backend — Extract lastOutput from JSONL

**Files:**
- Modify: `server/jsonlParser.ts`

- [ ] **Step 1: Add `lastOutput` to `SessionData` interface**

In `server/jsonlParser.ts`, add the field to the `SessionData` interface:

```typescript
// Add after toolCounts in SessionData interface:
  lastOutput: string | null
```

- [ ] **Step 2: Extract last assistant text in `extractSessionInfo`**

In `server/jsonlParser.ts`, inside `extractSessionInfo()`, track the last assistant text message. Add a variable at the top of the function and update the assistant text block handler:

```typescript
// Add variable at top of extractSessionInfo, alongside existing ones:
  let lastOutput: string | null = null

// Inside the existing `if (entry.type === 'assistant')` block,
// inside the `for (const block of content)` loop,
// in the `else if (block.type === 'text' && block.text)` branch,
// add after `currentAction = text.substring(0, 300)`:
              lastOutput = text.substring(0, 500)
```

- [ ] **Step 3: Return `lastOutput` from `extractSessionInfo`**

Add `lastOutput` to the return object of `extractSessionInfo`:

```typescript
  return {
    sessionId, entrypoint, currentAction, lastTools, tasks,
    tokenUsage, model, codeVersion, conversationTurns, toolCounts, lastOutput,
  }
```

- [ ] **Step 4: Pass `lastOutput` through in `findSessionForProject`**

In the return object of `findSessionForProject`, add `lastOutput`:

```typescript
    lastOutput: info.lastOutput ?? null,
```

- [ ] **Step 5: Add `parseFullSession()` function for the output endpoint**

Add this new exported function at the end of `server/jsonlParser.ts`. It reuses the `OutputMessage` type from `src/types.ts` (shared between frontend and backend):

```typescript
import type { OutputMessage } from '../src/types.js'

export async function parseFullSession(sessionId: string, lastOnly: boolean = false): Promise<OutputMessage[]> {
  // Find the JSONL file for this session across all project dirs
  const projectDirs = await readdir(CLAUDE_PROJECTS_DIR, { withFileTypes: true })
  let sessionFilePath: string | null = null

  for (const dir of projectDirs) {
    if (!dir.isDirectory()) continue
    const candidate = join(CLAUDE_PROJECTS_DIR, dir.name, `${sessionId}.jsonl`)
    try {
      await stat(candidate)
      sessionFilePath = candidate
      break
    } catch {
      continue
    }
  }

  if (!sessionFilePath) return []

  const raw = await readFile(sessionFilePath, 'utf-8')
  const entries = parseJsonlLines(raw)
  const messages: OutputMessage[] = []

  for (const entry of entries) {
    if (entry.type !== 'assistant' || !entry.message?.content) continue
    if (!Array.isArray(entry.message.content)) continue

    for (const block of entry.message.content) {
      if (block.type === 'text' && block.text?.trim()) {
        messages.push({
          role: 'assistant',
          content: block.text.trim(),
          timestamp: entry.timestamp,
        })
      } else if (block.type === 'tool_use' && block.name) {
        const filePath = block.input?.file_path || block.input?.path || undefined
        messages.push({
          role: 'tool_call',
          content: block.name,
          timestamp: entry.timestamp,
          toolName: block.name,
          filePath,
        })
      }
    }

    // Tool results come from 'result' type entries
    if (entry.type === 'result' && entry.result) {
      messages.push({
        role: 'tool_result',
        content: typeof entry.result === 'string' ? entry.result : JSON.stringify(entry.result).substring(0, 1000),
        timestamp: entry.timestamp,
      })
    }
  }

  if (lastOnly) {
    // Return only the last assistant text message
    const lastAssistant = messages.filter(m => m.role === 'assistant').pop()
    return lastAssistant ? [lastAssistant] : []
  }

  return messages
}
```

- [ ] **Step 6: Verify dev server starts without errors**

Run: `npm run dev`
Expected: Server starts, `GET /api/agents` now returns `lastOutput` field per agent (may be `null` for agents with no parsed output).

- [ ] **Step 7: Commit**

```bash
git add server/jsonlParser.ts
git commit -m "feat: extract lastOutput from JSONL and add parseFullSession"
```

---

### Task 3: Backend — Wire lastOutput through agentMerger

**Files:**
- Modify: `server/agentMerger.ts`

- [ ] **Step 1: Read agentMerger.ts to find where SessionData is mapped to the API response**

Read `server/agentMerger.ts` and locate the function that builds the final `Agent` object returned by the API. Find where other SessionData fields like `currentAction`, `model`, `toolCounts` are mapped.

- [ ] **Step 2: Add `lastOutput` to the agent object construction**

In the same location where other session data fields are spread/mapped into the agent object, add:

```typescript
lastOutput: sessionData.lastOutput ?? null,
```

- [ ] **Step 3: Verify with curl**

Run: `curl -s http://localhost:13120/api/agents | jq '.[0].lastOutput'`
Expected: Either a string (last assistant message) or `null`.

- [ ] **Step 4: Commit**

```bash
git add server/agentMerger.ts
git commit -m "feat: pass lastOutput through agentMerger to API"
```

---

### Task 4: Backend — Session Output Endpoint

**Files:**
- Modify: `server/index.ts`

- [ ] **Step 1: Add the output endpoint**

In `server/index.ts`, import `parseFullSession` and add the new route. Place it before the existing `POST /api/agents/:sessionId/message` route:

```typescript
// Add to imports at top:
import { parseFullSession } from './jsonlParser.js'

// Add route:
app.get('/api/agents/:sessionId/output', async (req, res) => {
  try {
    const { sessionId } = req.params
    const lastOnly = req.query.last === '1'
    const messages = await parseFullSession(sessionId, lastOnly)
    res.json({ messages })
  } catch (err) {
    res.status(500).json({ error: 'Failed to read session output' })
  }
})
```

- [ ] **Step 2: Verify with curl**

Run (substitute a real session ID from `curl -s http://localhost:13120/api/agents | jq '.[0].sessionId'`):
```bash
curl -s http://localhost:13120/api/agents/SESSION_ID/output | jq '.messages | length'
curl -s http://localhost:13120/api/agents/SESSION_ID/output?last=1 | jq '.messages[0].content' | head -c 200
```
Expected: Message count > 0 for active sessions, last message content visible.

- [ ] **Step 3: Commit**

```bash
git add server/index.ts
git commit -m "feat: add GET /api/agents/:sessionId/output endpoint"
```

---

### Task 5: Frontend — Extend useAgents Composable

**Files:**
- Modify: `src/composables/useAgents.ts`

- [ ] **Step 1: Add viewMode, searchQuery, and filteredAgents**

Replace the full `src/composables/useAgents.ts` with:

```typescript
import { ref, computed, onUnmounted, watch } from 'vue'
import type { Agent } from '../types'

type ViewMode = 'list' | 'cards'

const agents = ref<Agent[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)
const searchQuery = ref('')
const viewMode = ref<ViewMode>(
  (localStorage.getItem('agent-view-mode') as ViewMode) || 'list'
)

let intervalId: ReturnType<typeof setInterval> | null = null
let subscriberCount = 0

async function fetchAgents() {
  try {
    const res = await fetch('/api/agents')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data: Agent[] = await res.json()
    agents.value = data
    error.value = null

    if (selectedAgent.value) {
      const updated = data.find(a => a.sessionId === selectedAgent.value!.sessionId)
      selectedAgent.value = updated ?? null
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
  } finally {
    isLoading.value = false
  }
}

const filteredAgents = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return agents.value
  return agents.value.filter(a =>
    a.projectName.toLowerCase().includes(q) ||
    a.projectPath.toLowerCase().includes(q) ||
    (a.lastOutput?.toLowerCase().includes(q) ?? false) ||
    a.sessionId.toLowerCase().includes(q) ||
    (a.currentAction?.toLowerCase().includes(q) ?? false)
  )
})

// Persist viewMode to localStorage
watch(viewMode, (v) => localStorage.setItem('agent-view-mode', v))

function startPolling() {
  subscriberCount++
  if (intervalId) return
  fetchAgents()
  intervalId = setInterval(fetchAgents, 3000)
}

function stopPolling() {
  subscriberCount--
  if (subscriberCount <= 0 && intervalId) {
    clearInterval(intervalId)
    intervalId = null
    subscriberCount = 0
  }
}

export function useAgents() {
  startPolling()
  onUnmounted(stopPolling)

  function selectAgent(agent: Agent | null) {
    selectedAgent.value = agent
  }

  return {
    agents,
    filteredAgents,
    selectedAgent,
    isLoading,
    error,
    searchQuery,
    viewMode,
    selectAgent,
  }
}
```

- [ ] **Step 2: Verify dev server starts**

Run: `npm run dev`
Expected: No errors. Dashboard still works — `filteredAgents` returns all agents when searchQuery is empty.

- [ ] **Step 3: Commit**

```bash
git add src/composables/useAgents.ts
git commit -m "feat: add viewMode, searchQuery, filteredAgents to useAgents"
```

---

### Task 6: Frontend — AgentCard Component

**Files:**
- Create: `src/components/AgentCard.vue`

- [ ] **Step 1: Create the terminal-style card component**

Create `src/components/AgentCard.vue`:

```vue
<template>
  <div class="agent-card" @click="$emit('select', agent)">
    <div class="card-titlebar">
      <div class="card-title-left">
        <StatusBadge :status="agent.status" />
        <span class="card-project">{{ agent.projectName }}</span>
        <span class="card-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }}</span>
      </div>
      <div class="card-title-right">
        <span class="card-meta">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
      </div>
    </div>
    <div class="card-output">
      <template v-if="agent.lastOutput">{{ agent.lastOutput }}</template>
      <span v-else class="card-no-output">No output yet</span>
    </div>
    <div class="card-prompt" @click.stop>
      <span class="prompt-cursor">❯</span>
      <input
        v-model="promptInput"
        class="prompt-input"
        placeholder="Enter prompt..."
        @keydown.enter.prevent="handleSend"
        :disabled="isSending"
      />
      <button
        class="prompt-send"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : '↵' }}
      </button>
    </div>
    <p v-if="sendStatus" class="card-send-status" :class="sendStatus">
      {{ sendStatus === 'sent' ? 'Sent' : sendError }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Agent } from '../types'
import { formatTokens, formatCost, formatUptime } from '../utils/format'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const promptInput = ref('')
const isSending = ref(false)
const sendStatus = ref<'sent' | 'error' | null>(null)
const sendError = ref('')

const totalTokens = computed(() => {
  const u = props.agent.tokenUsage
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
})

function shortModel(model: string | null): string {
  if (!model) return '—'
  return model.replace('claude-', '').replace(/-\d+$/, m => ' ' + m.slice(1))
}

async function handleSend() {
  const msg = promptInput.value.trim()
  if (!msg || isSending.value) return

  isSending.value = true
  sendStatus.value = null

  try {
    if (props.agent.channelAvailable && props.agent.status !== 'idle') {
      // Channel messaging for running agents
      const res = await fetch(`/api/agents/${props.agent.sessionId}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: msg }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Send failed')
    } else {
      // Resume via spawn for stopped/idle agents
      const res = await fetch('/api/agents/spawn', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt: msg,
          cwd: props.agent.cwd,
          resumeSessionId: props.agent.sessionId,
        }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Resume failed')
    }
    sendStatus.value = 'sent'
    promptInput.value = ''
  } catch (err) {
    sendStatus.value = 'error'
    sendError.value = err instanceof Error ? err.message : 'Failed'
  } finally {
    isSending.value = false
    setTimeout(() => { sendStatus.value = null }, 3000)
  }
}
</script>

<style scoped>
.agent-card {
  background: var(--bg-primary);
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  border: 1px solid var(--border);
  transition: border-color 0.15s, box-shadow 0.15s;
}
.agent-card:hover {
  border-color: var(--bg-tertiary);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.3);
}

.card-titlebar {
  background: var(--bg-secondary);
  padding: 8px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.card-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.card-title-right {
  flex-shrink: 0;
}
.card-project {
  font-weight: 600;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.card-meta {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}

.card-output {
  padding: 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-secondary);
  max-height: 120px;
  overflow: hidden;
  position: relative;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
}
.card-output::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 32px;
  background: linear-gradient(transparent, var(--bg-primary));
  pointer-events: none;
}
.card-no-output {
  color: var(--text-muted);
  font-style: italic;
}

.card-prompt {
  border-top: 1px solid var(--border);
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.prompt-cursor {
  color: #3b82f6;
  font-size: 13px;
  flex-shrink: 0;
}
.prompt-input {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  outline: none;
}
.prompt-input::placeholder { color: var(--text-muted); }
.prompt-input:disabled { opacity: 0.5; }
.prompt-send {
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 13px;
  font-weight: bold;
  cursor: pointer;
  flex-shrink: 0;
}
.prompt-send:disabled { opacity: 0.4; cursor: not-allowed; }
.prompt-send:not(:disabled):hover { filter: brightness(1.15); }

.card-send-status {
  font-size: 11px;
  padding: 2px 12px 6px;
}
.card-send-status.sent { color: var(--accent-green); }
.card-send-status.error { color: #f87171; }
</style>
```

- [ ] **Step 2: Verify component renders**

Not yet wired into App.vue — verify no TypeScript errors with `npm run dev`.

- [ ] **Step 3: Commit**

```bash
git add src/components/AgentCard.vue
git commit -m "feat: add terminal-style AgentCard component"
```

---

### Task 7: Frontend — AgentCardGrid Component

**Files:**
- Create: `src/components/AgentCardGrid.vue`

- [ ] **Step 1: Create the responsive grid component**

Create `src/components/AgentCardGrid.vue`:

```vue
<template>
  <div class="card-grid">
    <AgentCard
      v-for="agent in agents"
      :key="agent.pid"
      :agent="agent"
      @select="$emit('select', agent)"
    />
    <p v-if="agents.length === 0" class="empty">
      No running Claude agents found.
    </p>
  </div>
</template>

<script setup lang="ts">
import type { Agent } from '../types'
import AgentCard from './AgentCard.vue'

defineProps<{ agents: Agent[] }>()
defineEmits<{ select: [agent: Agent] }>()
</script>

<style scoped>
.card-grid {
  display: grid;
  grid-template-columns: repeat(1, 1fr);
  gap: 12px;
}

@media (min-width: 768px) {
  .card-grid { grid-template-columns: repeat(2, 1fr); }
}

@media (min-width: 1200px) {
  .card-grid { grid-template-columns: repeat(3, 1fr); }
}

.empty {
  grid-column: 1 / -1;
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentCardGrid.vue
git commit -m "feat: add AgentCardGrid responsive grid component"
```

---

### Task 8: Frontend — AgentModal Component

**Files:**
- Create: `src/components/AgentModal.vue`

- [ ] **Step 1: Create the modal component**

Create `src/components/AgentModal.vue`:

```vue
<template>
  <Transition name="modal">
    <div v-if="agent" class="modal-backdrop" @click.self="$emit('close')">
      <div class="modal-window">
        <div class="modal-titlebar">
          <div class="modal-title-left">
            <StatusBadge :status="agent.status" />
            <span class="modal-project">{{ agent.projectName }}</span>
            <span class="modal-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
          </div>
          <div class="modal-title-right">
            <button class="modal-close" @click="$emit('close')">✕</button>
          </div>
        </div>

        <div class="modal-output" ref="outputEl">
          <div v-if="isLoadingOutput" class="output-loading">Loading session output...</div>
          <template v-else-if="outputMessages.length > 0">
            <div
              v-for="(msg, i) in outputMessages"
              :key="i"
              class="output-msg"
              :class="msg.role"
            >
              <template v-if="msg.role === 'assistant'">{{ msg.content }}</template>
              <template v-else-if="msg.role === 'tool_call'">
                <span class="tool-divider">── Tool: {{ msg.toolName }}<template v-if="msg.filePath"> {{ msg.filePath }}</template> ──</span>
              </template>
              <template v-else-if="msg.role === 'tool_result'">
                <details class="tool-result-details">
                  <summary>Result (click to expand)</summary>
                  <pre class="tool-result-content">{{ msg.content }}</pre>
                </details>
              </template>
            </div>
          </template>
          <div v-else class="output-empty">No output available for this session.</div>
        </div>

        <div class="modal-prompt">
          <span class="prompt-cursor">❯</span>
          <textarea
            v-model="promptInput"
            class="prompt-textarea"
            rows="1"
            placeholder="Enter prompt..."
            @keydown.ctrl.enter.prevent="handleSend"
            @keydown.meta.enter.prevent="handleSend"
            :disabled="isSending"
            ref="promptEl"
          ></textarea>
          <button
            class="prompt-send"
            :disabled="isSending || promptInput.trim().length === 0"
            @click="handleSend"
          >
            {{ isSending ? '...' : '↵' }}
          </button>
        </div>
        <p v-if="sendStatus" class="modal-send-status" :class="sendStatus">
          {{ sendStatus === 'sent' ? 'Sent' : sendError }}
        </p>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import type { Agent, OutputMessage } from '../types'
import { formatTokens, formatCost, formatUptime } from '../utils/format'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ agent: Agent | null }>()

const outputMessages = ref<OutputMessage[]>([])
const isLoadingOutput = ref(false)
const outputEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLTextAreaElement | null>(null)
const promptInput = ref('')
const isSending = ref(false)
const sendStatus = ref<'sent' | 'error' | null>(null)
const sendError = ref('')

const totalTokens = computed(() => {
  if (!props.agent) return 0
  const u = props.agent.tokenUsage
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
})

function shortModel(model: string | null): string {
  if (!model) return '—'
  return model.replace('claude-', '').replace(/-\d+$/, m => ' ' + m.slice(1))
}

async function fetchOutput(sessionId: string) {
  isLoadingOutput.value = true
  try {
    const res = await fetch(`/api/agents/${sessionId}/output`)
    if (!res.ok) throw new Error('Failed to fetch')
    const data = await res.json()
    outputMessages.value = data.messages
    await nextTick()
    if (outputEl.value) {
      outputEl.value.scrollTop = outputEl.value.scrollHeight
    }
  } catch {
    outputMessages.value = []
  } finally {
    isLoadingOutput.value = false
  }
}

// Fetch output when agent changes
watch(() => props.agent?.sessionId, (sessionId) => {
  if (sessionId) {
    fetchOutput(sessionId)
    nextTick(() => promptEl.value?.focus())
  } else {
    outputMessages.value = []
  }
})

const emit = defineEmits<{ close: [] }>()

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.agent) {
    e.preventDefault()
    emit('close')
  }
}

// Register/unregister global keydown listener
watch(() => props.agent, (agent) => {
  if (agent) {
    window.addEventListener('keydown', onKeydown)
  } else {
    window.removeEventListener('keydown', onKeydown)
  }
}, { immediate: true })

onUnmounted(() => window.removeEventListener('keydown', onKeydown))

async function handleSend() {
  const msg = promptInput.value.trim()
  if (!msg || isSending.value || !props.agent) return

  isSending.value = true
  sendStatus.value = null

  try {
    if (props.agent.channelAvailable && props.agent.status !== 'idle') {
      const res = await fetch(`/api/agents/${props.agent.sessionId}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: msg }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Send failed')
    } else {
      const res = await fetch('/api/agents/spawn', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt: msg,
          cwd: props.agent.cwd,
          resumeSessionId: props.agent.sessionId,
        }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Resume failed')
    }
    sendStatus.value = 'sent'
    promptInput.value = ''
    // Refresh output after sending
    if (props.agent) {
      setTimeout(() => fetchOutput(props.agent!.sessionId), 2000)
    }
  } catch (err) {
    sendStatus.value = 'error'
    sendError.value = err instanceof Error ? err.message : 'Failed'
  } finally {
    isSending.value = false
    setTimeout(() => { sendStatus.value = null }, 3000)
  }
}
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 24px;
}

.modal-window {
  background: var(--bg-secondary);
  border-radius: 10px;
  border: 1px solid var(--bg-tertiary);
  box-shadow: 0 8px 40px rgba(0, 0, 0, 0.5);
  width: 100%;
  max-width: 900px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.modal-titlebar {
  background: var(--bg-tertiary);
  padding: 10px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-title-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.modal-project {
  font-weight: 600;
  font-size: 14px;
}
.modal-meta {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.modal-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.modal-close:hover {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.modal-output {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 13px;
  line-height: 1.6;
}

.output-loading, .output-empty {
  color: var(--text-muted);
  text-align: center;
  padding: 48px;
}

.output-msg {
  margin-bottom: 12px;
}
.output-msg.assistant {
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}
.output-msg.tool_call {
  margin: 8px 0;
}
.tool-divider {
  color: var(--text-muted);
  font-size: 11px;
}
.output-msg.tool_result {
  margin: 4px 0 12px;
}
.tool-result-details summary {
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
}
.tool-result-details summary:hover { color: var(--text-secondary); }
.tool-result-content {
  background: var(--bg-primary);
  border-radius: 4px;
  padding: 8px;
  font-size: 11px;
  color: var(--text-secondary);
  max-height: 200px;
  overflow-y: auto;
  margin-top: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}

.modal-prompt {
  border-top: 1px solid var(--border);
  padding: 10px 16px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.prompt-cursor {
  color: #3b82f6;
  font-size: 14px;
  flex-shrink: 0;
}
.prompt-textarea {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  outline: none;
  resize: none;
  line-height: 1.4;
}
.prompt-textarea::placeholder { color: var(--text-muted); }
.prompt-textarea:disabled { opacity: 0.5; }
.prompt-send {
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 4px;
  padding: 6px 14px;
  font-size: 14px;
  font-weight: bold;
  cursor: pointer;
  flex-shrink: 0;
}
.prompt-send:disabled { opacity: 0.4; cursor: not-allowed; }
.prompt-send:not(:disabled):hover { filter: brightness(1.15); }

.modal-send-status {
  font-size: 11px;
  padding: 0 16px 8px;
}
.modal-send-status.sent { color: var(--accent-green); }
.modal-send-status.error { color: #f87171; }

/* Transitions */
.modal-enter-active, .modal-leave-active { transition: opacity 0.2s; }
.modal-enter-active .modal-window, .modal-leave-active .modal-window { transition: transform 0.2s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-window { transform: scale(0.95); }
.modal-leave-to .modal-window { transform: scale(0.95); }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add src/components/AgentModal.vue
git commit -m "feat: add AgentModal full-screen overlay component"
```

---

### Task 9: Frontend — Rewire App.vue

**Files:**
- Modify: `src/App.vue`
- Delete: `src/components/AgentDetail.vue`

- [ ] **Step 1: Update App.vue template**

Replace the `<template>` section of `src/App.vue` with:

```vue
<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span class="agent-count">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</span>
      <span class="header-stat" v-if="totalCost > 0">${{ totalCost.toFixed(2) }}</span>
      <span class="header-stat" v-if="totalTokens > 0">{{ formatTokens(totalTokens) }} tokens</span>
      <input
        v-model="searchQuery"
        class="header-search"
        type="text"
        placeholder="Search agents..."
      />
      <div class="view-toggle">
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'list' }"
          @click="viewMode = 'list'"
          title="List view"
        >≡</button>
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'cards' }"
          @click="viewMode = 'cards'"
          title="Card view"
        >⊞</button>
      </div>
      <button class="sessions-btn" @click="showSessions = true">Sessions</button>
      <button class="spawn-btn" @click="showSpawnDialog = true">+ New Agent</button>
    </header>
    <ResourceBar />
    <div class="script-banner" v-if="scriptPath">
      <span class="script-label">Channel script:</span>
      <code class="script-path" @click="copyScript" :title="copied ? 'Copied!' : 'Click to copy'">{{ scriptPath }}</code>
      <span v-if="copied" class="copied-hint">Copied!</span>
    </div>
    <main>
      <p v-if="isLoading" class="loading">Loading agents...</p>
      <p v-else-if="error" class="error">Error: {{ error }}</p>
      <AgentTable
        v-else-if="viewMode === 'list'"
        :agents="filteredAgents"
        @select="selectAgent"
      />
      <AgentCardGrid
        v-else
        :agents="filteredAgents"
        @select="selectAgent"
      />
    </main>
    <AgentModal
      :agent="selectedAgent"
      @close="selectAgent(null)"
    />
    <SpawnDialog
      :open="showSpawnDialog"
      @close="showSpawnDialog = false"
      @spawned="onAgentSpawned"
    />
    <SessionList
      :open="showSessions"
      :home-dir="homeDir"
      @close="showSessions = false"
      @spawned="onAgentSpawned"
    />
  </div>
</template>
```

- [ ] **Step 2: Update App.vue script**

Replace the `<script setup>` section with:

```vue
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAgents } from './composables/useAgents'
import { formatTokens } from './utils/format'
import AgentTable from './components/AgentTable.vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import SessionList from './components/SessionList.vue'
import ResourceBar from './components/ResourceBar.vue'

const { agents, filteredAgents, selectedAgent, isLoading, error, searchQuery, viewMode, selectAgent } = useAgents()
const showSpawnDialog = ref(false)
const showSessions = ref(false)
const scriptPath = ref('')
const homeDir = ref('')
const copied = ref(false)

fetch('/api/config').then(r => r.json()).then(d => {
  scriptPath.value = d.scriptPath
  homeDir.value = d.homedir
}).catch(() => {})

function copyScript() {
  navigator.clipboard.writeText(scriptPath.value)
  copied.value = true
  setTimeout(() => { copied.value = false }, 2000)
}

function onAgentSpawned(_pid: number) {
  // Agent will appear in the next polling cycle (~3s)
}

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => {
  const u = a.tokenUsage
  return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
}, 0))
</script>
```

- [ ] **Step 3: Add CSS for search input and view toggle**

Add these styles to the `<style>` section in `App.vue`, after the existing `.spawn-btn:hover` rule:

```css
.header-search {
  margin-left: auto;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  color: var(--text-primary);
  font-family: inherit;
  width: 200px;
  transition: border-color 0.15s, width 0.2s;
}
.header-search::placeholder { color: var(--text-muted); }
.header-search:focus {
  outline: none;
  border-color: #3b82f6;
  width: 260px;
}

.view-toggle {
  display: flex;
  background: var(--bg-tertiary);
  border-radius: 6px;
  overflow: hidden;
}
.toggle-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  padding: 6px 10px;
  font-size: 14px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.toggle-btn.active {
  background: #3b82f6;
  color: white;
}
.toggle-btn:not(.active):hover {
  color: var(--text-primary);
}
```

Also remove the `margin-left: auto` from `.sessions-btn` since the search input now takes that role:

Change in the existing `.sessions-btn` rule:
```css
/* Remove: margin-left: auto; */
```

- [ ] **Step 4: Delete AgentDetail.vue**

```bash
rm src/components/AgentDetail.vue
```

- [ ] **Step 5: Verify everything works**

Run: `npm run dev`
Open browser at `http://localhost:13120`.

Verify:
1. Header shows search input + view toggle (≡/⊞)
2. Default view is list (or whatever was last saved in localStorage)
3. Clicking ⊞ switches to card grid with terminal-style cards
4. Cards show status, project name, model, cost, last output, prompt input
5. Clicking a card opens the modal overlay
6. Modal shows full session output, prompt input at bottom
7. Escape or backdrop click closes modal
8. Search filters agents in real-time in both views
9. Clicking ≡ switches back to table view

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: wire up card view toggle, search, and modal in App.vue

Replace AgentDetail off-canvas panel with AgentModal overlay.
Add header search input filtering by name, path, output, session ID.
Add view toggle persisted to localStorage."
```

---

### Task 10: Final Cleanup and Verification

- [ ] **Step 1: Check for remaining AgentDetail references**

Search the codebase for any remaining imports or references to `AgentDetail`:

```bash
grep -r "AgentDetail" src/
```

If any are found, remove them.

- [ ] **Step 2: End-to-end verification**

Run `npm run dev` and verify:
1. List view works as before (table with all columns)
2. Card view shows responsive grid of terminal-style cards
3. Search filters both views
4. Card prompt sends via channel (running) or resume (stopped)
5. Modal opens on card/row click with full session output
6. Modal prompt submission works
7. View toggle preference persists across page reloads
8. No console errors

- [ ] **Step 3: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: cleanup remaining AgentDetail references"
```
