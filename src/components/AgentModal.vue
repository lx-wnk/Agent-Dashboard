<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AgentChatStream from './AgentChatStream.vue'
import CrossLinkBanner from './CrossLinkBanner.vue'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'
import StatusBadge from './StatusBadge.vue'
import SubAgentList from './SubAgentList.vue'
import TaskList from './TaskList.vue'
import ToolTimeline from './ToolTimeline.vue'

const props = defineProps<{ agent: Agent | null }>()
const emit = defineEmits<{ close: [], navigate: [taskId: string] }>()

const localMessages = ref<OutputMessage[]>([])
const promptInputRef = ref<InstanceType<typeof PromptInput> | null>(null)
const chatStreamRef = ref<InstanceType<typeof AgentChatStream> | null>(null)

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
  if (e.key === 'Escape' && props.agent) {
    e.preventDefault()
    emit('close')
  }
}

watch(() => props.agent, (agent) => {
  if (agent)
    window.addEventListener('keydown', onKeydown)
  else
    window.removeEventListener('keydown', onKeydown)
}, { immediate: true })

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Transition name="modal">
    <div v-if="agent" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-window">
        <div class="modal-titlebar">
          <div class="modal-title-left">
            <StatusBadge :status="agent.status" />
            <span class="modal-project">{{ agent.projectName }}</span>
            <MachineBadge v-if="agent.machine" :machine="agent.machine" />
            <span class="modal-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
          </div>
          <div class="modal-title-right">
            <button class="modal-close" @click="emit('close')">
              ✕
            </button>
          </div>
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
          class="modal-output"
        />

        <div v-if="agent.tasks.length > 0 || agent.subagents.length > 0 || agent.lastTools.length > 0" class="modal-details">
          <details>
            <summary class="details-summary">
              Agent Details (Tasks, Tools, Subagents)
            </summary>
            <div class="details-content">
              <ToolTimeline v-if="agent.lastTools.length > 0" :tools="agent.lastTools" />
              <TaskList v-if="agent.tasks.length > 0" :tasks="agent.tasks" />
              <SubAgentList v-if="agent.subagents.length > 0" :subagents="agent.subagents" />
            </div>
          </details>
        </div>

        <PromptInput v-if="!agent.machine" ref="promptInputRef" :agent="agent" variant="full" @message-sent="onMessageSent" />
      </div>
    </div>
  </Transition>
</template>

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
.modal-project { font-weight: 600; font-size: 14px; }
.modal-meta { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.modal-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.modal-close:hover { background: var(--bg-secondary); color: var(--text-primary); }
.modal-output {
  flex: 1;
  padding: 16px;
}
.modal-details {
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.details-summary {
  padding: 8px 16px;
  font-size: 12px;
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
}
.details-summary:hover { color: var(--text-secondary); }
.details-content {
  padding: 8px 16px 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: 200px;
  overflow-y: auto;
}
.modal-enter-active, .modal-leave-active { transition: opacity 0.2s; }
.modal-enter-active .modal-window, .modal-leave-active .modal-window { transition: transform 0.2s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-window { transform: scale(0.95); }
.modal-leave-to .modal-window { transform: scale(0.95); }
</style>
