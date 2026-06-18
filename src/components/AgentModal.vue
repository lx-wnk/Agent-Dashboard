<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import { computed, defineAsyncComponent, nextTick, ref, watch } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { usePermissionResolve } from '../composables/usePermissionResolve'
import { useRovingTabList } from '../composables/useRovingTabList'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AgentChatStream from './AgentChatStream.vue'
import CrossLinkBanner from './CrossLinkBanner.vue'
import MachineBadge from './MachineBadge.vue'
import PluginSlot from './PluginSlot.vue'
import PromptInput from './PromptInput.vue'
import SubAgentList from './SubAgentList.vue'
import TaskList from './TaskList.vue'
import ToolTimeline from './ToolTimeline.vue'
import AppBadge from './ui/AppBadge.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ agent: Agent | null }>()

const emit = defineEmits<{ close: [], navigate: [taskId: string], toast: [message: string] }>()

// Waterfall chart is heavy (d3) — split into its own chunk, loaded when the tab is first opened.
const ExecutionWaterfall = defineAsyncComponent(() => import('./ExecutionWaterfall.vue'))

const localMessages = ref<OutputMessage[]>([])
const { activeTab: activeDetailsTab, tabAttrs, panelAttrs, onKeydown, select } = useRovingTabList(
  ['details', 'waterfall'],
  { idPrefix: 'agent-details' },
)
const promptInputRef = ref<InstanceType<typeof PromptInput> | null>(null)
const chatStreamRef = ref<InstanceType<typeof AgentChatStream> | null>(null)

const { getIdentity } = useAgentIdentity()
const { resolveAgent } = usePermissionResolve()

const totalTokens = computed(() => props.agent ? totalTokenCount(props.agent.tokenUsage) : 0)

async function handleApprove() {
  if (!props.agent)
    return
  const err = await resolveAgent(props.agent, 'granted')
  if (err)
    emit('toast', err)
}

const approveHandler = computed(() =>
  props.agent?.pipelineTaskId && props.agent?.pendingPermissions?.length
    ? handleApprove
    : null,
)

function onMessageSent(msg: OutputMessage) {
  localMessages.value.push(msg)
  nextTick(() => chatStreamRef.value?.scrollToBottom())
}

// Reset local messages when the agent changes, focus prompt input
watch(() => props.agent?.sessionId, (sessionId) => {
  localMessages.value = []
  if (sessionId && !props.agent?.machine)
    nextTick(() => promptInputRef.value?.focus())
})

// Escape is handled by AppModal's @keydown.escape on its backdrop — no window listener needed.
</script>

<template>
  <AppModal :open="!!agent" :z-index="1000" :labelled-by="agent ? `agent-modal-title-${agent.pid}` : undefined" @close="emit('close')">
    <template v-if="agent">
      <div class="bg-raised px-4 py-2.5 flex justify-between items-center flex-shrink-0">
        <div class="flex items-center gap-2.5 min-w-0">
          <AppBadge :variant="agent.status" />
          <span class="mr-1" aria-hidden="true">{{ getIdentity(agent.projectPath).emoji }}</span>
          <span :id="`agent-modal-title-${agent.pid}`" class="font-semibold text-sm text-fg">{{ agent.projectName }}</span>
          <MachineBadge v-if="agent.machine" :machine="agent.machine" />
          <span class="text-[11px] font-mono text-fg-mute whitespace-nowrap">{{ shortModel(agent.model ?? null) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
        </div>
        <button type="button" aria-label="Close" class="bg-transparent border-none text-fg-mute text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-fg" @click="emit('close')">
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
        class="flex-1 min-h-0 overflow-y-auto p-4"
      />
      <div v-if="agent.tasks.length > 0 || agent.subagents.length > 0 || agent.lastTools.length > 0" class="border-t border-line flex-shrink-0">
        <details>
          <summary class="px-4 py-2 text-xs text-fg-soft cursor-pointer select-none hover:text-fg dark:hover:text-fg">
            Agent Details (Tasks, Tools, Subagents)
          </summary>
          <div role="tablist" aria-label="Agent details" class="flex gap-0 px-4 pt-2 border-b border-line" @keydown="onKeydown">
            <button
              v-bind="tabAttrs('details')"
              type="button"
              class="px-3 py-1.5 text-xs font-medium rounded-t border-b-2 transition-colors"
              :class="activeDetailsTab === 'details'
                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-fg-mute hover:text-fg-soft'"
              @click="select('details')"
            >
              Details
            </button>
            <button
              v-bind="tabAttrs('waterfall')"
              type="button"
              class="px-3 py-1.5 text-xs font-medium rounded-t border-b-2 transition-colors"
              :class="activeDetailsTab === 'waterfall'
                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-fg-mute hover:text-fg-soft'"
              @click="select('waterfall')"
            >
              Waterfall
            </button>
          </div>
          <div v-if="activeDetailsTab === 'details'" v-bind="panelAttrs('details')" class="px-4 pb-3 pt-2 flex flex-col gap-3 max-h-[200px] overflow-y-auto">
            <ToolTimeline v-if="agent.lastTools.length > 0" :tools="agent.lastTools" />
            <TaskList v-if="agent.tasks.length > 0" :tasks="agent.tasks" />
            <SubAgentList v-if="agent.subagents.length > 0" :subagents="agent.subagents" />
            <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-[13px]">
              <dt class="text-fg-mute">
                Input tokens
              </dt>
              <dd class="text-fg text-right font-mono">
                {{ formatTokens(agent.tokenUsage.inputTokens) }}
              </dd>
              <dt class="text-fg-mute">
                Output tokens
              </dt>
              <dd class="text-fg text-right font-mono">
                {{ formatTokens(agent.tokenUsage.outputTokens) }}
              </dd>
              <dt class="text-fg-mute">
                Cache write
              </dt>
              <dd class="text-fg text-right font-mono">
                {{ formatTokens(agent.tokenUsage.cacheCreationTokens) }}
                <span class="text-fg-mute ml-1">({{ formatCost(agent.cacheCreationCostEstimate) }})</span>
              </dd>
              <dt class="text-fg-mute">
                Cache read
              </dt>
              <dd class="text-fg text-right font-mono">
                {{ formatTokens(agent.tokenUsage.cacheReadTokens) }}
                <span class="text-fg-mute ml-1">({{ formatCost(agent.cacheReadCostEstimate) }})</span>
              </dd>
              <dt class="text-fg-soft font-medium border-t border-line pt-1">
                Total cost
              </dt>
              <dd class="text-fg text-right font-mono font-medium border-t border-line pt-1">
                {{ formatCost(agent.costEstimate) }}
              </dd>
            </dl>
          </div>
          <div v-if="activeDetailsTab === 'waterfall'" v-bind="panelAttrs('waterfall')" class="max-h-[300px] overflow-y-auto">
            <ExecutionWaterfall :session-id="agent.sessionId" />
          </div>
        </details>
      </div>
      <PromptInput v-if="!agent.machine" ref="promptInputRef" :agent="agent" variant="full" :approve-handler="approveHandler" @message-sent="onMessageSent" />
      <PluginSlot name="agent-modal-footer" :ctx="{ agent }" />
    </template>
  </AppModal>
</template>
