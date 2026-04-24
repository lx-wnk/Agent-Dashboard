<script setup lang="ts">
import type { PipelineTask } from '../types'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRefinementChat } from '../composables/useRefinementChat'

const props = defineProps<{ open: boolean, task: PipelineTask | null }>()
const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask] }>()

const inputText = ref('')
const chatEl = ref<HTMLElement | null>(null)

const taskId = computed(() => props.task?.id ?? null)
const {
  messages, completedPhases, isStreaming, error,
  approvalReady, loadHistory, sendMessage, confirm, phaseLabel,
} = useRefinementChat(() => taskId.value)

const EXAMPLE_CHIPS = [
  'Ein neues Feature implementieren',
  'Einen Bug beheben',
  'Code refactoren',
  'Eine neue API-Integration',
]

watch(() => props.open, async (val) => {
  if (val && props.task) {
    await loadHistory()
  }
})

onMounted(() => {
  if (props.open && props.task) loadHistory()
})

watch(messages, async () => {
  await nextTick()
  chatEl.value?.scrollTo({ top: chatEl.value.scrollHeight, behavior: 'smooth' })
}, { deep: true })

async function handleSend() {
  const msg = inputText.value.trim()
  if (!msg || isStreaming.value) return
  inputText.value = ''
  await sendMessage(msg)
}

async function handleConfirm() {
  const updated = await confirm()
  if (updated) emit('confirmed', updated)
}

function isPhaseMarker(idx: number): string | null {
  const msg = messages.value[idx]
  if (msg.role !== 'assistant' || !msg.phase) return null
  return msg.phase
}
</script>

<template>
  <div v-if="open" class="chat-backdrop" @click.self="emit('close')">
    <div class="chat-modal">
      <div class="chat-header">
        <span class="chat-title">Neues Ticket</span>
        <button class="chat-close" @click="emit('close')">✕</button>
      </div>

      <div ref="chatEl" class="chat-messages">
        <div v-if="messages.length === 0" class="chat-empty">
          <p class="chat-greeting">Was möchtest du umsetzen?</p>
          <div class="chip-row">
            <button
              v-for="chip in EXAMPLE_CHIPS"
              :key="chip"
              class="chip"
              @click="inputText = chip"
            >
              {{ chip }}
            </button>
          </div>
        </div>

        <template v-for="(msg, idx) in messages" :key="idx">
          <div v-if="isPhaseMarker(idx)" class="phase-marker">
            ── ✓ {{ phaseLabel(isPhaseMarker(idx)!) }} abgeschlossen ──
          </div>

          <div :class="['bubble', msg.role]">
            <span class="bubble-content">{{ msg.content }}</span>
          </div>
        </template>

        <div v-if="isStreaming" class="bubble assistant streaming">
          <span class="dot-pulse" />
        </div>
      </div>

      <div v-if="error" class="chat-error">{{ error }}</div>

      <div v-if="approvalReady" class="confirm-bar">
        <button class="btn-confirm" @click="handleConfirm">
          Task erstellen →
        </button>
      </div>

      <div class="chat-input-bar">
        <input
          v-model="inputText"
          class="chat-input"
          placeholder="Nachricht..."
          :disabled="isStreaming || approvalReady"
          @keydown.enter.exact.prevent="handleSend"
        />
        <button
          class="btn-send"
          :disabled="isStreaming || !inputText.trim() || approvalReady"
          @click="handleSend"
        >→</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chat-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.chat-modal {
  background: var(--color-surface, #1a1a2e);
  border-radius: 12px; width: min(680px, 95vw);
  height: min(720px, 90vh);
  display: flex; flex-direction: column; overflow: hidden;
}
.chat-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 16px 20px; border-bottom: 1px solid var(--color-border, #333);
}
.chat-title { font-size: 1.1rem; font-weight: 600; }
.chat-close { background: none; border: none; cursor: pointer; font-size: 1.2rem; }
.chat-messages {
  flex: 1; overflow-y: auto; padding: 20px;
  display: flex; flex-direction: column; gap: 12px;
}
.chat-empty { text-align: center; margin-top: 40px; }
.chat-greeting { font-size: 1.1rem; margin-bottom: 16px; }
.chip-row { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; }
.chip {
  padding: 8px 14px; border-radius: 20px; border: 1px solid var(--color-border, #444);
  background: none; cursor: pointer; font-size: 0.9rem;
}
.chip:hover { background: var(--color-hover, #2a2a3e); }
.bubble {
  max-width: 80%; padding: 10px 14px; border-radius: 12px; line-height: 1.5;
  white-space: pre-wrap; word-break: break-word;
}
.bubble.user { align-self: flex-end; background: var(--color-primary, #4c6ef5); color: white; }
.bubble.assistant { align-self: flex-start; background: var(--color-surface-alt, #252540); }
.phase-marker {
  text-align: center; font-size: 0.8rem; color: var(--color-success, #69db7c);
  opacity: 0.8; padding: 4px 0;
}
.chat-error { padding: 8px 20px; color: var(--color-error, #ff6b6b); font-size: 0.85rem; }
.confirm-bar { padding: 12px 20px; border-top: 1px solid var(--color-border, #333); }
.btn-confirm {
  width: 100%; padding: 12px; border-radius: 8px;
  background: var(--color-success, #69db7c); color: #000;
  border: none; cursor: pointer; font-size: 1rem; font-weight: 600;
}
.chat-input-bar {
  display: flex; gap: 8px; padding: 12px 20px;
  border-top: 1px solid var(--color-border, #333);
}
.chat-input {
  flex: 1; padding: 10px 14px; border-radius: 8px;
  border: 1px solid var(--color-border, #444); background: var(--color-surface-alt, #252540);
  color: inherit; font-size: 0.95rem;
}
.chat-input:disabled { opacity: 0.5; }
.btn-send {
  padding: 10px 16px; border-radius: 8px;
  background: var(--color-primary, #4c6ef5); color: white;
  border: none; cursor: pointer; font-size: 1.1rem;
}
.btn-send:disabled { opacity: 0.4; cursor: default; }
.dot-pulse::before { content: '●●●'; animation: pulse 1s infinite; }
@keyframes pulse { 0%, 100% { opacity: 0.3 } 50% { opacity: 1 } }
</style>
