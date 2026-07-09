<script setup lang="ts">
import type { PermissionItem } from '../composables/usePendingPermissions'
import type { Agent, PendingPermission, PermissionRequest } from '../types'
import type { AnswerIntent } from '../utils/answerKeys'
import { computed, nextTick, ref, watch } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { useNow } from '../composables/useNow'
import { usePermissionResolve } from '../composables/usePermissionResolve'
import { toast } from '../composables/useToast'
import { attentionFor } from '../utils/attention'
import { formatErrorState, formatRelativeActivity, secondsSince, shortModel } from '../utils/format'
import { friendlyProjectName } from '../utils/friendlyProjectName'
import QuestionCard from './QuestionCard.vue'
import AppButton from './ui/AppButton.vue'

const props = defineProps<{
  agents: Agent[]
  permissionItems: PermissionItem[]
  focusedSessionId?: string | null
}>()
const emit = defineEmits<{
  select: [agent: Agent]
  remembered: []
  approve: [taskId: string, ids: string[], remember: boolean]
  deny: [taskId: string, ids: string[]]
}>()

const { getIdentity } = useAgentIdentity()
const { nowMs } = useNow()
const { resolveAgent } = usePermissionResolve()

// Tone → Tailwind border + text classes
const toneBorderClass: Record<string, string> = {
  warning: 'border-warning-dot',
  danger: 'border-danger-dot',
}

const toneLeftClass: Record<string, string> = {
  warning: 'border-l-warning-dot',
  danger: 'border-l-danger-dot',
}

const toneLabelClass: Record<string, string> = {
  warning: 'text-warning-text',
  danger: 'text-danger-text',
}

// Agent cards: only render agents where task-driven permission items don't already cover them,
// AND whose attention kind is error/stalled — or free agents with pendingToolUse.
const orchestratedTaskIds = computed(() => new Set(props.permissionItems.map(i => i.taskId)))

const visibleAgentCards = computed(() =>
  props.agents.filter((agent) => {
    const secs = secondsSince(agent.lastActivity, nowMs.value)
    const att = attentionFor(agent, secs)
    if (!att)
      return false
    // A pending, answerable question always surfaces — regardless of orchestration.
    if (att.kind === 'question')
      return true
    // Free agent with pendingToolUse — always show
    if (!agent.pipelineTaskId && agent.pendingToolUse)
      return true
    // Orchestrated agent whose task is already represented in permissionItems — skip
    if (agent.pipelineTaskId && orchestratedTaskIds.value.has(agent.pipelineTaskId) && att.kind === 'permission')
      return false
    return att.kind === 'error' || att.kind === 'stalled' || att.kind === 'permission'
  }),
)

// Breakdown string: "2 permissions · 1 failed run · 1 stalled"
const breakdown = computed(() => {
  const counts: Record<string, number> = {}
  // Count task-level permission requests
  const permTotal = props.permissionItems.reduce((s, i) => s + i.requests.length, 0)
  if (permTotal > 0)
    counts.permission = permTotal
  for (const agent of visibleAgentCards.value) {
    const att = attentionFor(agent, secondsSince(agent.lastActivity, nowMs.value))
    if (att && att.kind !== 'permission')
      counts[att.kind] = (counts[att.kind] ?? 0) + 1
  }
  const PARTS: [string, string, string][] = [
    ['question', 'question', 'questions'],
    ['permission', 'permission request', 'permission requests'],
    ['error', 'failed run', 'failed runs'],
    ['stalled', 'stalled', 'stalled'],
  ]
  return PARTS
    .filter(([k]) => counts[k])
    .map(([k, s, p]) => `${counts[k]} ${counts[k] === 1 ? s : p}`)
    .join(' · ')
})

const totalCount = computed(() => props.permissionItems.length + visibleAgentCards.value.length)

function blockedDetail(agent: Agent): string {
  if (agent.pendingToolUse)
    return agent.pendingToolUse.pattern ? `${agent.pendingToolUse.tool}(${agent.pendingToolUse.pattern})` : agent.pendingToolUse.tool
  const secs = secondsSince(agent.lastActivity, nowMs.value)
  const att = attentionFor(agent, secs)
  if (!att)
    return ''
  if (att.kind === 'permission')
    return agent.currentAction || 'Waiting for permission'
  if (att.kind === 'error')
    return agent.errorState ? formatErrorState(agent.errorState) : (agent.currentAction || 'Run failed')
  // stalled
  return `Running but silent — last output ${formatRelativeActivity(secs)}`
}

function permissionLabel(p: PendingPermission | PermissionRequest): string {
  return p.pattern ? `${p.tool}(${p.pattern})` : p.tool
}

// --- Task-driven approve-all bar ---
const approveAllOpen = ref(false)
const rememberAll = ref(false)
const approveAllInFlight = ref(false)

// Per-task selection: taskId → Set of request ids selected for bulk approve
const selectedByTask = ref<Map<string, Set<string>>>(new Map())

// Keep selectedByTask in sync: new items default all selected; removed tasks pruned
watch(
  () => props.permissionItems,
  (items) => {
    const next = new Map<string, Set<string>>()
    for (const item of items) {
      const existing = selectedByTask.value.get(item.taskId)
      const allIds = new Set(item.requests.map(r => r.id))
      if (existing) {
        // Keep only request ids that still exist
        next.set(item.taskId, new Set([...existing].filter(id => allIds.has(id))))
      }
      else {
        next.set(item.taskId, new Set(allIds))
      }
    }
    selectedByTask.value = next
  },
  { immediate: true, deep: true },
)

const selectedTotal = computed(() => {
  let total = 0
  for (const ids of selectedByTask.value.values()) total += ids.size
  return total
})

function toggleRequestSelection(taskId: string, requestId: string) {
  const next = new Map(selectedByTask.value)
  const taskSet = new Set(next.get(taskId) ?? [])
  if (taskSet.has(requestId))
    taskSet.delete(requestId)
  else
    taskSet.add(requestId)
  next.set(taskId, taskSet)
  selectedByTask.value = next
}

async function handleApproveAll() {
  approveAllInFlight.value = true
  try {
    for (const item of props.permissionItems) {
      const ids = [...(selectedByTask.value.get(item.taskId) ?? [])]
      if (ids.length === 0)
        continue
      emit('approve', item.taskId, ids, rememberAll.value)
    }
    if (rememberAll.value)
      emit('remembered')
  }
  finally {
    approveAllInFlight.value = false
  }
}

// Per-task "don't ask again" toggle
const rememberPerTask = ref<Record<string, boolean>>({})

async function handleApproveTask(taskId: string, ids: string[]) {
  emit('approve', taskId, ids, rememberPerTask.value[taskId] ?? false)
  if (rememberPerTask.value[taskId])
    emit('remembered')
}

async function handleDenyTask(taskId: string, ids: string[]) {
  emit('deny', taskId, ids)
}

// hasBulk drives collapse default and bar visibility
const hasBulk = computed(() => props.permissionItems.reduce((s, i) => s + i.requests.length, 0) >= 2)
const showCards = ref(!hasBulk.value)

watch(hasBulk, (bulk) => {
  if (bulk)
    showCards.value = false
  else
    showCards.value = true
})

const cardsVisible = computed(() => showCards.value || !!props.focusedSessionId)

// --- Agent card resolve (legacy path for agents not covered by task items) ---
const { resolving: agentResolving } = usePermissionResolve()
const rememberPerAgent = ref<Record<string, boolean>>({})

async function handleResolveAgent(agent: Agent, outcome: 'granted' | 'denied') {
  const remember = rememberPerAgent.value[agent.sessionId] ?? false
  const err = await resolveAgent(agent, outcome, remember)
  if (err) {
    toast.error(err)
  }
  else if (remember && outcome === 'granted') {
    emit('remembered')
  }
}

const allowing = ref<Record<string, boolean>>({})
async function allowTool(agent: Agent) {
  const pending = agent.pendingToolUse
  if (!pending)
    return
  allowing.value[agent.sessionId] = true
  try {
    const res = await fetch(`/api/agents/${agent.pid}/allow-tool`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tool: pending.tool, pattern: pending.pattern || null }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    toast.success(`Allowed ${pending.tool} for ${friendlyProjectName(agent.projectName)} — future runs won't ask (the paused run still needs your reply in its terminal)`)
    emit('remembered')
  }
  catch (err) {
    toast.error(`Couldn't save allow rule: ${err instanceof Error ? err.message : String(err)}`)
  }
  finally {
    allowing.value[agent.sessionId] = false
  }
}

const answeringQuestion = ref<Record<string, boolean>>({})

async function answerQuestion(agent: Agent, intent: AnswerIntent) {
  answeringQuestion.value[agent.sessionId] = true
  try {
    const res = await fetch(`/api/agents/${agent.pid}/answer-question`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      body: JSON.stringify(intent),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
  }
  catch (err) {
    toast.error(`Couldn't send answer: ${err instanceof Error ? err.message : String(err)}`)
  }
  finally {
    answeringQuestion.value[agent.sessionId] = false
  }
}

const cardRefs = ref<Record<string, HTMLElement | null>>({})

function setCardRef(sessionId: string, el: HTMLElement | null) {
  cardRefs.value[sessionId] = el
}

watch(() => props.focusedSessionId, (id) => {
  if (!id)
    return
  nextTick(() => {
    cardRefs.value[id]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  })
})
</script>

<template>
  <section class="mb-4" aria-label="Needs your attention">
    <!-- Empty state -->
    <div
      v-if="totalCount === 0 && permissionItems.length === 0"
      class="flex items-center gap-2 px-4 py-2.5 rounded-md bg-success-soft border border-success-line"
    >
      <span aria-hidden="true" class="text-success-text">✓</span>
      <span class="text-[13px] font-medium text-success-text">All clear — no agent is waiting on you.</span>
    </div>

    <template v-else>
      <!-- Header row -->
      <div class="flex items-center gap-2 px-0.5 pb-2 flex-wrap">
        <h2 class="text-[10px] font-bold uppercase tracking-wider text-fg-mute m-0">
          Needs you
        </h2>
        <span
          class="text-[10px] font-bold font-mono bg-red-500 text-white rounded-full px-1.5 leading-[16px]"
          :aria-label="`${totalCount} items need attention`"
        >{{ totalCount }}</span>
        <span v-if="breakdown" class="text-[11px] text-fg-faint">{{ breakdown }}</span>
        <span class="ml-auto text-[10px] text-fg-faint font-mono select-none hidden sm:inline" aria-hidden="true">
          <kbd class="not-italic">n</kbd> next ·
          <kbd class="not-italic">a</kbd> approve ·
          <kbd class="not-italic">d</kbd> deny ·
          <kbd class="not-italic">⇧A</kbd> approve all ·
          <kbd class="not-italic">c</kbd> density ·
          <kbd class="not-italic">⌘K</kbd> search
        </span>
      </div>

      <!-- Approve-all bar: shown when total task permission requests ≥ 2 -->
      <div
        v-if="hasBulk"
        class="mb-3 rounded-lg border border-warning-line bg-warning-soft overflow-hidden"
      >
        <div class="flex items-center gap-3 px-3 py-2">
          <span class="text-[12px] font-semibold text-warning-text">
            {{ permissionItems.reduce((s, i) => s + i.requests.length, 0) }} requests across {{ permissionItems.length }} {{ permissionItems.length === 1 ? 'task' : 'tasks' }} waiting on permission
          </span>
          <label class="ml-auto flex items-center gap-1.5 cursor-pointer select-none text-[11px] text-fg-faint">
            <input
              v-model="rememberAll"
              type="checkbox"
              class="accent-success"
              aria-label="Don't ask again for all selected tasks"
            >
            Don't ask again
          </label>
          <button
            type="button"
            class="text-[11px] text-fg-mute underline-offset-2 hover:text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
            :aria-expanded="approveAllOpen"
            @click="approveAllOpen = !approveAllOpen"
          >
            {{ approveAllOpen ? 'Hide what\'s affected' : 'Review what\'s affected' }}
          </button>
          <AppButton
            variant="success"
            size="sm"
            :disabled="approveAllInFlight || selectedTotal === 0"
            @click="handleApproveAll"
          >
            ✓ Approve all{{ selectedTotal < permissionItems.reduce((s, i) => s + i.requests.length, 0) ? ` (${selectedTotal})` : '' }}
          </AppButton>
        </div>

        <!-- Disclosure: per-task + per-request with deselect checkboxes -->
        <div v-if="approveAllOpen" class="border-t border-warning-line px-3 py-2 flex flex-col gap-3">
          <div
            v-for="item in permissionItems"
            :key="item.taskId"
            class="flex flex-col gap-1"
          >
            <div class="text-[12px] font-medium text-fg">
              {{ item.projectName }} — {{ item.title }}
            </div>
            <div
              v-for="req in item.requests"
              :key="req.id"
              class="flex items-center gap-2"
            >
              <input
                :id="`approve-all-${req.id}`"
                type="checkbox"
                class="accent-success"
                :checked="selectedByTask.get(item.taskId)?.has(req.id) ?? false"
                :aria-label="`Include ${permissionLabel(req)} in approve-all`"
                @change="toggleRequestSelection(item.taskId, req.id)"
              >
              <label :for="`approve-all-${req.id}`" class="font-mono text-[11px] text-fg-mute cursor-pointer">
                {{ permissionLabel(req) }}
                <span
                  v-if="req.outsideSafeList"
                  class="font-sans text-[10px] text-danger-text ml-1"
                  title="Outside the server's safe allow-list"
                >⚠ outside safe-list</span>
              </label>
            </div>
          </div>
        </div>
      </div>

      <!-- Toggle: Review individually / Hide individual requests -->
      <button
        type="button"
        class="inline-flex items-center gap-1.5 mb-2 bg-transparent border-none cursor-pointer text-xs text-fg-mute hover:text-fg font-sans focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent rounded"
        :aria-expanded="cardsVisible"
        @click="showCards = !showCards"
      >
        <span
          aria-hidden="true"
          class="text-[10px] transition-transform duration-150"
          :class="cardsVisible ? 'rotate-90' : ''"
        >▸</span>
        {{ cardsVisible ? 'Hide individual requests' : `Review individually (${permissionItems.length + visibleAgentCards.length})` }}
      </button>

      <div v-if="cardsVisible" class="flex flex-wrap gap-2.5">
        <!-- Task permission cards -->
        <div
          v-for="item in permissionItems"
          :key="item.taskId"
          class="min-w-[280px] flex-1 basis-[280px] max-w-[420px] rounded-lg bg-card border border-l-[3px] border-warning-dot border-l-warning-dot p-3 flex flex-col gap-2"
        >
          <div class="flex items-center gap-2 min-w-0">
            <span class="font-semibold text-[13px] text-fg truncate">{{ item.projectName }}</span>
            <span class="text-[11px] text-fg-faint truncate ml-1">{{ item.title }}</span>
            <span class="ml-auto text-[10px] font-bold uppercase tracking-wide shrink-0 text-warning-text">Needs permission</span>
          </div>

          <ul class="m-0 p-0 list-none flex flex-col gap-1">
            <li
              v-for="req in item.requests"
              :key="req.id"
              class="flex flex-col gap-0.5"
            >
              <span class="font-mono text-[12px] text-fg bg-app border border-line rounded px-2.5 py-1 leading-snug">
                {{ permissionLabel(req) }}
                <span
                  v-if="req.outsideSafeList"
                  class="font-sans text-[10px] text-danger-text ml-1"
                  title="Outside the server's safe allow-list"
                >⚠</span>
              </span>
              <span v-if="req.reason" class="text-[11px] text-fg-faint px-0.5">{{ req.reason }}</span>
            </li>
          </ul>

          <div class="flex items-center gap-2 flex-wrap">
            <AppButton
              variant="success"
              size="sm"
              :aria-label="`Approve all permissions for ${item.title}`"
              @click="handleApproveTask(item.taskId, item.requests.map(r => r.id))"
            >
              Approve
            </AppButton>
            <AppButton
              variant="danger"
              size="sm"
              :aria-label="`Deny all permissions for ${item.title}`"
              @click="handleDenyTask(item.taskId, item.requests.map(r => r.id))"
            >
              Deny
            </AppButton>
            <label class="flex items-center gap-1 cursor-pointer select-none text-[11px] text-fg-faint ml-auto">
              <input
                v-model="rememberPerTask[item.taskId]"
                type="checkbox"
                class="accent-success"
                :aria-label="`Don't ask again for ${item.projectName}`"
              >
              <span class="font-mono">Don't ask again for {{ item.projectName }}</span>
            </label>
          </div>
        </div>

        <!-- Agent attention cards (error, stalled, free-agent pendingToolUse) -->
        <div
          v-for="agent in visibleAgentCards"
          :key="agent.sessionId"
          :ref="(el) => setCardRef(agent.sessionId, el as HTMLElement | null)"
          class="min-w-[280px] flex-1 basis-[280px] max-w-[420px] rounded-lg bg-card border border-l-[3px] p-3 flex flex-col gap-2 transition-shadow"
          :class="[
            toneBorderClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'warning'],
            toneLeftClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'warning'],
            agent.sessionId === focusedSessionId ? 'ring-2 ring-accent shadow-md' : '',
          ]"
        >
          <div class="flex items-center gap-2 min-w-0">
            <span aria-hidden="true" class="text-[15px] shrink-0">{{ getIdentity(agent.projectPath).emoji }}</span>
            <span class="font-semibold text-[13px] text-fg truncate">{{ friendlyProjectName(agent.projectName) }}</span>
            <span class="font-mono text-[11px] text-fg-faint shrink-0">{{ shortModel(agent.model ?? null) }}</span>
            <span
              class="ml-auto text-[10px] font-bold uppercase tracking-wide shrink-0"
              :class="toneLabelClass[attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.tone ?? 'warning']"
            >{{ attentionFor(agent, secondsSince(agent.lastActivity, nowMs))?.label }}</span>
          </div>

          <!-- Answerable question, detected directly in the session's terminal buffer -->
          <template v-if="agent.pendingQuestion">
            <div :class="answeringQuestion[agent.sessionId] ? 'opacity-60 pointer-events-none' : ''">
              <QuestionCard
                :detected-question="agent.pendingQuestion"
                @answer="(intent) => answerQuestion(agent, intent)"
              />
            </div>
          </template>

          <!-- Orchestrated agent with pending permissions (not covered by task items) -->
          <template v-else-if="agent.pipelineTaskId && agent.pendingPermissions?.length">
            <ul class="m-0 p-0 list-none flex flex-col gap-1" :aria-label="`Pending permissions for ${agent.projectName}`">
              <li
                v-for="p in agent.pendingPermissions"
                :key="p.id"
                class="flex flex-col gap-0.5"
              >
                <span class="font-mono text-[12px] text-fg bg-app border border-line rounded px-2.5 py-1 leading-snug">{{ permissionLabel(p) }}</span>
                <span v-if="p.reason" class="text-[11px] text-fg-faint px-0.5">{{ p.reason }}</span>
              </li>
            </ul>
          </template>

          <template v-else>
            <div class="font-mono text-[12px] text-fg-mute bg-app border border-line rounded px-2.5 py-1.5 leading-snug break-words">
              {{ blockedDetail(agent) }}
            </div>
          </template>

          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[11px] text-fg-faint">{{ formatRelativeActivity(secondsSince(agent.lastActivity, nowMs)) }}</span>

            <template v-if="agent.pipelineTaskId && agent.pendingPermissions?.length">
              <AppButton
                variant="success"
                size="sm"
                :disabled="agentResolving[agent.sessionId]"
                :aria-label="`Approve permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'granted')"
              >
                Approve
              </AppButton>
              <AppButton
                variant="danger"
                size="sm"
                :disabled="agentResolving[agent.sessionId]"
                :aria-label="`Deny permissions for ${agent.projectName}`"
                @click="handleResolveAgent(agent, 'denied')"
              >
                Deny
              </AppButton>
              <label class="flex items-center gap-1 cursor-pointer select-none text-[11px] text-fg-faint ml-auto">
                <input
                  v-model="rememberPerAgent[agent.sessionId]"
                  type="checkbox"
                  class="accent-success"
                  :aria-label="`Don't ask again for ${agent.projectName}`"
                >
                <span class="font-mono">{{ friendlyProjectName(agent.projectName) }}</span>
              </label>
            </template>

            <!-- Free agent: allow the paused tool for future runs -->
            <AppButton
              v-if="agent.pendingToolUse && !(agent.pipelineTaskId && agent.pendingPermissions?.length)"
              variant="success"
              size="sm"
              class="ml-auto"
              :disabled="allowing[agent.sessionId]"
              :title="`Allow ${blockedDetail(agent)} for this project going forward. The paused run still needs your reply in its terminal.`"
              :aria-label="`Allow ${agent.pendingToolUse.tool} for ${friendlyProjectName(agent.projectName)} going forward`"
              @click="allowTool(agent)"
            >
              Allow {{ agent.pendingToolUse.tool }}
            </AppButton>

            <AppButton
              variant="outline"
              size="sm"
              :class="!(agent.pipelineTaskId && agent.pendingPermissions?.length) && !agent.pendingToolUse ? 'ml-auto' : ''"
              :aria-label="`Open details for ${agent.projectName}`"
              @click="emit('select', agent)"
            >
              Open ↗
            </AppButton>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
