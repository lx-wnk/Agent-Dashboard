<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AgentChatStream from './AgentChatStream.vue'
import CrossLinkBanner from './CrossLinkBanner.vue'
import ExecutionWaterfall from './ExecutionWaterfall.vue'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'
import SubAgentList from './SubAgentList.vue'
import TaskList from './TaskList.vue'
import ToolTimeline from './ToolTimeline.vue'
import AppBadge from './ui/AppBadge.vue'
import AppModal from './ui/AppModal.vue'

type DetailsTab = 'details' | 'waterfall'

const props = defineProps<{ agent: Agent | null }>()
const emit = defineEmits<{ close: [], navigate: [taskId: string] }>()

const localMessages = ref<OutputMessage[]>([])
const activeDetailsTab = ref<DetailsTab>('details')
const promptInputRef = ref<InstanceType<typeof PromptInput> | null>(null)
const chatStreamRef = ref<InstanceType<typeof AgentChatStream> | null>(null)

const { getIdentity } = useAgentIdentity()

const totalTokens = computed(() => props.agent ? totalTokenCount(props.agent.tokenUsage) : 0)

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

function onKeydown(e: KeyboardEvent) {
  if (!props.agent)
    return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <AppModal :open="!!agent" :z-index="1000" @close="emit('close')">
    <div v-if="agent" class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[900px] max-h-[80vh] flex flex-col overflow-hidden">
      <div class="bg-slate-50 dark:bg-slate-800 px-4 py-2.5 flex justify-between items-center flex-shrink-0">
        <div class="flex items-center gap-2.5 min-w-0">
          <AppBadge :variant="agent.status" />
          <span class="mr-1">{{ getIdentity(agent.projectPath).emoji }}</span>
          <span class="font-semibold text-sm text-slate-900 dark:text-slate-100">{{ agent.projectName }}</span>
          <MachineBadge v-if="agent.machine" :machine="agent.machine" />
          <span class="text-[11px] text-slate-400 dark:text-slate-600 whitespace-nowrap">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
        </div>
        <button type="button" class="bg-transparent border-none text-slate-500 dark:text-slate-400 text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
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
          <div class="flex gap-0 px-4 pt-2 border-b border-slate-200 dark:border-slate-700">
            <button
              type="button"
              class="px-3 py-1.5 text-xs font-medium rounded-t border-b-2 transition-colors"
              :class="activeDetailsTab === 'details'
                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300'"
              @click="activeDetailsTab = 'details'"
            >
              Details
            </button>
            <button
              type="button"
              class="px-3 py-1.5 text-xs font-medium rounded-t border-b-2 transition-colors"
              :class="activeDetailsTab === 'waterfall'
                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300'"
              @click="activeDetailsTab = 'waterfall'"
            >
              Waterfall
            </button>
          </div>
          <div v-if="activeDetailsTab === 'details'" class="px-4 pb-3 pt-2 flex flex-col gap-3 max-h-[200px] overflow-y-auto">
            <ToolTimeline v-if="agent.lastTools.length > 0" :tools="agent.lastTools" />
            <TaskList v-if="agent.tasks.length > 0" :tasks="agent.tasks" />
            <SubAgentList v-if="agent.subagents.length > 0" :subagents="agent.subagents" />
            <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-[13px]">
              <dt class="text-slate-500 dark:text-slate-400">
                Input tokens
              </dt>
              <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
                {{ formatTokens(agent.tokenUsage.inputTokens) }}
              </dd>
              <dt class="text-slate-500 dark:text-slate-400">
                Output tokens
              </dt>
              <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
                {{ formatTokens(agent.tokenUsage.outputTokens) }}
              </dd>
              <dt class="text-slate-500 dark:text-slate-400">
                Cache write
              </dt>
              <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
                {{ formatTokens(agent.tokenUsage.cacheCreationTokens) }}
                <span class="text-slate-400 dark:text-slate-600 ml-1">({{ formatCost(agent.cacheCreationCostEstimate) }})</span>
              </dd>
              <dt class="text-slate-500 dark:text-slate-400">
                Cache read
              </dt>
              <dd class="text-slate-900 dark:text-slate-100 text-right font-mono">
                {{ formatTokens(agent.tokenUsage.cacheReadTokens) }}
                <span class="text-slate-400 dark:text-slate-600 ml-1">({{ formatCost(agent.cacheReadCostEstimate) }})</span>
              </dd>
              <dt class="text-slate-700 dark:text-slate-300 font-medium border-t border-slate-200 dark:border-slate-700 pt-1">
                Total cost
              </dt>
              <dd class="text-slate-900 dark:text-slate-100 text-right font-mono font-medium border-t border-slate-200 dark:border-slate-700 pt-1">
                {{ formatCost(agent.costEstimate) }}
              </dd>
            </dl>
          </div>
          <div v-if="activeDetailsTab === 'waterfall'" class="max-h-[300px] overflow-y-auto">
            <ExecutionWaterfall :session-id="agent.sessionId" />
          </div>
        </details>
      </div>
      <PromptInput v-if="!agent.machine" ref="promptInputRef" :agent="agent" variant="full" @message-sent="onMessageSent" />
    </div>
  </AppModal>
</template>
