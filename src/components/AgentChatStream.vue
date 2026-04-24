<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import DOMPurify from 'dompurify'
import { Marked } from 'marked'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'

interface ToolGroup {
  kind: 'tool_group'
  calls: Array<{ toolName: string, filePath?: string, result?: string }>
}

interface TaskGroup {
  kind: 'task_group'
  tasks: Array<{ subject: string, status: string, taskId?: string }>
}

type ChatEntry = { kind: 'message', msg: OutputMessage } | ToolGroup | TaskGroup

const props = defineProps<{
  agent: Agent | null
  localMessages?: OutputMessage[]
  refreshIntervalMs?: number
}>()

const sessionMessages = ref<OutputMessage[]>([])
const isLoadingOutput = ref(false)
const outputEl = ref<HTMLElement | null>(null)
let lastReplyTimestamp: string | null = null
let fetchingReplies = false
let refreshInterval: ReturnType<typeof setInterval> | null = null
const channelReplies = ref<OutputMessage[]>([])

const md = new Marked({ breaks: true, gfm: true })

function renderMarkdown(text: string): string {
  return DOMPurify.sanitize(md.parse(text, { async: false }) as string)
}

const outputMessages = computed<OutputMessage[]>(() => {
  // Deduplicate: once a human message appears in sessionMessages (from JSONL),
  // remove it from localMessages to avoid showing it twice during the poll gap.
  const inSession = new Set(
    sessionMessages.value.filter(m => m.role === 'human').map(m => m.content),
  )
  const filteredLocal = (props.localMessages ?? []).filter(
    m => m.role !== 'human' || !inSession.has(m.content),
  )
  const all = [...sessionMessages.value, ...channelReplies.value, ...filteredLocal]
  all.sort((a, b) => {
    const ta = a.timestamp ? new Date(a.timestamp).getTime() : 0
    const tb = b.timestamp ? new Date(b.timestamp).getTime() : 0
    return ta - tb
  })
  return all
})

const chatEntries = computed<ChatEntry[]>(() => {
  const entries: ChatEntry[] = []
  let currentToolGroup: ToolGroup | null = null
  let currentTaskGroup: TaskGroup | null = null

  function flushToolGroup() {
    if (currentToolGroup) {
      entries.push(currentToolGroup)
      currentToolGroup = null
    }
  }
  function flushTaskGroup() {
    if (currentTaskGroup) {
      entries.push(currentTaskGroup)
      currentTaskGroup = null
    }
  }

  for (const msg of outputMessages.value) {
    if (msg.role === 'tool_call') {
      flushTaskGroup()
      if (!currentToolGroup)
        currentToolGroup = { kind: 'tool_group', calls: [] }
      currentToolGroup.calls.push({ toolName: msg.toolName || msg.content, filePath: msg.filePath })
    }
    else if (msg.role === 'tool_result') {
      if (currentToolGroup) {
        const lastCall = currentToolGroup.calls[currentToolGroup.calls.length - 1]
        if (lastCall)
          lastCall.result = msg.content
      }
    }
    else if (msg.role === 'task') {
      flushToolGroup()
      if (!currentTaskGroup)
        currentTaskGroup = { kind: 'task_group', tasks: [] }
      if (msg.taskStatus === 'pending') {
        currentTaskGroup.tasks.push({ subject: msg.content, status: 'pending', taskId: msg.taskId })
      }
      else {
        const existing = currentTaskGroup.tasks.find(t => t.taskId === msg.taskId)
        if (existing) {
          existing.status = msg.taskStatus || 'in_progress'
        }
        else {
          currentTaskGroup.tasks.push({ subject: msg.content, status: msg.taskStatus || 'in_progress', taskId: msg.taskId })
        }
      }
    }
    else {
      flushToolGroup()
      flushTaskGroup()
      entries.push({ kind: 'message', msg })
    }
  }
  flushToolGroup()
  flushTaskGroup()

  return entries
})

function scrollToBottom() {
  requestAnimationFrame(() => {
    if (outputEl.value)
      outputEl.value.scrollTop = outputEl.value.scrollHeight
  })
}

async function fetchOutput(sessionId: string) {
  isLoadingOutput.value = true
  try {
    const res = await fetch(`/api/agents/${sessionId}/output`)
    if (!res.ok)
      throw new Error('Failed to fetch')
    const data = await res.json()
    sessionMessages.value = data.messages
    await nextTick()
    scrollToBottom()
  }
  catch {
    sessionMessages.value = []
  }
  finally {
    isLoadingOutput.value = false
  }
}

async function fetchReplies(sessionId: string) {
  if (fetchingReplies)
    return
  fetchingReplies = true
  try {
    const url = lastReplyTimestamp
      ? `/api/agents/${sessionId}/replies?since=${encodeURIComponent(lastReplyTimestamp)}`
      : `/api/agents/${sessionId}/replies`
    const res = await fetch(url)
    if (!res.ok)
      return
    const data = await res.json()
    const replies: Array<{ message: string, timestamp: string }> = data.replies || []
    if (replies.length === 0)
      return
    for (const r of replies) {
      channelReplies.value.push({
        role: 'channel_reply',
        content: r.message,
        timestamp: r.timestamp,
      })
    }
    lastReplyTimestamp = replies[replies.length - 1].timestamp
    await nextTick()
    scrollToBottom()
  }
  catch { /* ignore */ }
  finally {
    fetchingReplies = false
  }
}

async function refreshOutput() {
  const agent = props.agent
  if (!agent || agent.status === 'idle')
    return
  try {
    const res = await fetch(`/api/agents/${agent.sessionId}/output`)
    if (!res.ok)
      return
    const data = await res.json()
    const newMessages: OutputMessage[] = data.messages
    if (newMessages.length > sessionMessages.value.length) {
      sessionMessages.value = newMessages
      await nextTick()
      scrollToBottom()
    }
  }
  catch { /* ignore */ }
  await fetchReplies(agent.sessionId)
}

function startRefresh() {
  stopRefresh()
  if (props.agent && props.agent.status !== 'idle')
    refreshInterval = setInterval(refreshOutput, props.refreshIntervalMs ?? 5000)
}

function stopRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval)
    refreshInterval = null
  }
}

watch(() => props.agent?.sessionId, (sessionId) => {
  channelReplies.value = []
  lastReplyTimestamp = null
  if (sessionId && !props.agent?.machine) {
    fetchOutput(sessionId)
    fetchReplies(sessionId)
    startRefresh()
  }
  else {
    sessionMessages.value = []
  }
}, { immediate: true })

watch(() => props.agent?.status, () => {
  if (props.agent)
    startRefresh()
})

onUnmounted(stopRefresh)

defineExpose({ scrollToBottom })
</script>

<template>
  <div ref="outputEl" class="chat-stream">
    <div v-if="agent?.machine" class="output-empty">
      Session output is not available for remote agents.
    </div>
    <div v-else-if="isLoadingOutput" class="output-loading">
      Loading session output...
    </div>
    <template v-else-if="chatEntries.length > 0">
      <template v-for="(entry, i) in chatEntries" :key="i">
        <div v-if="entry.kind === 'tool_group'" class="chat-row tool_call">
          <details class="tool-group">
            <summary class="tool-group-summary">
              {{ entry.calls.length }} tool call{{ entry.calls.length > 1 ? 's' : '' }}
            </summary>
            <div class="tool-group-list">
              <details v-for="(call, j) in entry.calls" :key="j" class="tool-group-item">
                <summary class="tool-item-summary">
                  {{ call.toolName }}<span v-if="call.filePath" class="tool-item-path">{{ call.filePath }}</span>
                </summary>
                <pre v-if="call.result" class="tool-result-content">{{ call.result }}</pre>
              </details>
            </div>
          </details>
        </div>

        <div v-else-if="entry.kind === 'task_group'" class="chat-row tool_call">
          <div class="task-group">
            <div v-for="(task, j) in entry.tasks" :key="j" class="task-item" :class="task.status">
              <span class="task-icon">{{ task.status === 'completed' ? '✓' : task.status === 'in_progress' ? '›' : '○' }}</span>
              <span class="task-subject">{{ task.subject }}</span>
            </div>
          </div>
        </div>

        <div v-else class="chat-row" :class="entry.msg.role">
          <div v-if="entry.msg.role === 'human'" class="chat-bubble human-bubble" :class="{ queued: entry.msg.queued }">
            {{ entry.msg.content }}
          </div>
          <div
            v-else-if="entry.msg.role === 'channel_reply'"
            class="chat-bubble reply-bubble markdown-body"
            v-html="renderMarkdown(entry.msg.content)"
          />
          <div
            v-else-if="entry.msg.role === 'assistant'"
            class="chat-bubble assistant-bubble markdown-body"
            v-html="renderMarkdown(entry.msg.content)"
          />
          <div v-else-if="entry.msg.role === 'subagent'" class="chat-subagent">
            <span class="subagent-icon">⑂</span>
            <span class="subagent-type">{{ entry.msg.subagentType }}</span>
            <span class="subagent-desc">{{ entry.msg.content }}</span>
          </div>
        </div>
      </template>
    </template>
    <div v-else class="output-empty">
      No output available for this session.
    </div>
    <div v-if="agent?.status === 'active' && agent.currentAction" class="chat-activity">
      {{ agent.currentAction }}...
    </div>
  </div>
</template>

<style scoped>
.chat-stream {
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.6;
}
.output-loading, .output-empty {
  color: var(--text-muted);
  text-align: center;
  padding: 48px;
}
.chat-row { display: flex; }
.chat-row.human { justify-content: flex-end; }
.chat-row.channel_reply,
.chat-row.assistant { justify-content: flex-start; }
.chat-row.tool_call,
.chat-row.tool_result { justify-content: center; }

.chat-bubble {
  max-width: 80%;
  padding: 8px 12px;
  border-radius: 12px;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-word;
  white-space: pre-wrap;
}
.human-bubble {
  background: var(--bg-tertiary);
  color: var(--text-muted);
  border-bottom-right-radius: 4px;
}
.human-bubble.queued { border: 1px solid rgba(234, 179, 8, 0.4); }
.assistant-bubble,
.reply-bubble {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border-bottom-left-radius: 4px;
}
.reply-bubble { border-left: 2px solid var(--accent-green); }

.chat-activity {
  color: var(--text-muted);
  font-size: 12px;
  font-style: italic;
  padding: 4px 0;
  animation: pulse 2s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

.tool-group {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-primary);
  font-size: 12px;
}
.tool-group-summary {
  padding: 4px 10px;
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
}
.tool-group-summary:hover { color: var(--text-secondary); }
.tool-group-list { border-top: 1px solid var(--border); padding: 4px 0; }
.tool-group-item { padding: 0 10px; }
.tool-item-summary {
  padding: 2px 0;
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
}
.tool-item-summary:hover { color: var(--text-primary); }
.tool-item-path { color: var(--text-muted); margin-left: 6px; font-size: 10px; }
.tool-result-content {
  background: var(--bg-tertiary);
  border-radius: 4px;
  padding: 8px;
  font-size: 11px;
  color: var(--text-secondary);
  max-height: 200px;
  overflow-y: auto;
  margin: 4px 0;
  white-space: pre-wrap;
  word-break: break-word;
}

.task-group {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-primary);
  padding: 6px 10px;
  font-size: 12px;
}
.task-item {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 1px 0;
  color: var(--text-secondary);
}
.task-icon {
  flex-shrink: 0;
  width: 14px;
  text-align: center;
  font-weight: 600;
}
.task-item.completed .task-icon { color: var(--accent-green); }
.task-item.in_progress .task-icon { color: var(--accent-blue); }
.task-item.pending .task-icon { color: var(--text-muted); }
.task-item.completed .task-subject { color: var(--text-muted); text-decoration: line-through; }
.task-subject { font-family: var(--font-mono); }

.chat-subagent {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-primary);
  font-size: 12px;
  color: var(--text-secondary);
}
.subagent-icon { font-size: 14px; color: var(--accent-blue); }
.subagent-type {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.subagent-desc { color: var(--text-muted); }

.markdown-body { white-space: normal; }
.markdown-body :deep(p) { margin: 0 0 0.4em; }
.markdown-body :deep(p:last-child) { margin-bottom: 0; }
.markdown-body :deep(code) {
  background: rgba(255, 255, 255, 0.08);
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 12px;
}
.markdown-body :deep(pre) {
  background: var(--bg-primary);
  border-radius: 4px;
  padding: 8px;
  overflow-x: auto;
  margin: 4px 0;
}
.markdown-body :deep(pre code) { background: none; padding: 0; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { margin: 4px 0; padding-left: 1.4em; }
.markdown-body :deep(strong) { color: var(--text-primary); }
.markdown-body :deep(a) { color: var(--accent-blue); }
.markdown-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 6px 0;
  font-size: 12px;
}
.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--border);
  padding: 4px 8px;
  text-align: left;
}
.markdown-body :deep(th) {
  background: var(--bg-primary);
  color: var(--text-primary);
  font-weight: 600;
}
.markdown-body :deep(blockquote) {
  border-left: 3px solid var(--border);
  margin: 4px 0;
  padding: 2px 10px;
  color: var(--text-muted);
}
.markdown-body :deep(hr) {
  border: none;
  border-top: 1px solid var(--border);
  margin: 8px 0;
}
</style>
