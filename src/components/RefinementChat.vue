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

      <div ref="chatEl" class="chat-messages" :class="{ empty: messages.length === 0 }">
        <div v-if="messages.length === 0" class="chat-empty">
          <div class="empty-icon">✦</div>
          <div class="empty-text">
            <p class="chat-greeting">Was möchtest du umsetzen?</p>
            <p class="chat-subtitle">Beschreibe deine Idee — ich führe dich durch Analyse, Spec und Umsetzungskonzept.</p>
          </div>
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
            ✓ {{ phaseLabel(isPhaseMarker(idx)!) }} abgeschlossen
          </div>

          <div :class="['bubble', msg.role]">
            <span class="bubble-content">{{ msg.content }}</span>
          </div>
        </template>

        <div v-if="isStreaming" class="bubble assistant streaming">
          <span class="dot-pulse"><span /><span /><span /></span>
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
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
  backdrop-filter: blur(2px);
}
.chat-modal {
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 16px; width: min(680px, 95vw);
  height: min(720px, 90vh);
  display: flex; flex-direction: column; overflow: hidden;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.5);
}
.chat-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 18px 22px; border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.chat-title { font-size: 1rem; font-weight: 600; letter-spacing: 0.01em; opacity: 0.9; }
.chat-close {
  background: none; border: none; cursor: pointer;
  font-size: 1rem; opacity: 0.5; padding: 4px 8px; border-radius: 6px;
  transition: opacity 0.15s, background 0.15s;
}
.chat-close:hover { opacity: 1; background: var(--bg-secondary); }

.chat-messages {
  flex: 1; overflow-y: auto; padding: 20px;
  display: flex; flex-direction: column; gap: 12px;
}
.chat-messages.empty { justify-content: center; }

/* ── Empty state ─────────────────────────────── */
.chat-empty {
  display: flex; flex-direction: column;
  align-items: center; gap: 20px;
  text-align: center; padding: 0 24px;
}
.empty-icon {
  font-size: 2rem; opacity: 0.35;
  line-height: 1;
}
.empty-text { display: flex; flex-direction: column; gap: 8px; }
.chat-greeting {
  font-size: 1.35rem; font-weight: 700;
  letter-spacing: -0.02em; margin: 0;
}
.chat-subtitle {
  font-size: 0.875rem; opacity: 0.5;
  line-height: 1.55; margin: 0;
  max-width: 380px;
}
.chip-row {
  display: flex; flex-wrap: wrap; gap: 8px;
  justify-content: center; max-width: 460px;
}
.chip {
  padding: 8px 16px; border-radius: 20px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  cursor: pointer; font-size: 0.85rem; font-weight: 500;
  color: inherit;
  transition: border-color 0.15s, background 0.15s, transform 0.1s;
}
.chip:hover {
  border-color: var(--accent-blue);
  background: var(--bg-tertiary);
  transform: translateY(-1px);
}
.chip:active { transform: translateY(0); }

/* ── Message bubbles ──────────────────────────── */
.bubble {
  max-width: 78%; padding: 10px 14px; border-radius: 14px;
  line-height: 1.55; white-space: pre-wrap; word-break: break-word;
  font-size: 0.92rem;
}
.bubble.user {
  align-self: flex-end;
  background: var(--accent-blue); color: white;
  border-bottom-right-radius: 4px;
}
.bubble.assistant {
  align-self: flex-start;
  background: var(--bg-secondary);
  border-bottom-left-radius: 4px;
}

/* ── Phase milestone ──────────────────────────── */
.phase-marker {
  display: flex; align-items: center; gap: 8px;
  font-size: 0.75rem; color: var(--accent-green);
  opacity: 0.75; padding: 4px 0; justify-content: center;
}
.phase-marker::before,
.phase-marker::after {
  content: ''; flex: 1; max-width: 60px;
  height: 1px; background: var(--accent-green); opacity: 0.4;
}

/* ── Footer areas ─────────────────────────────── */
.chat-error {
  padding: 8px 20px; color: var(--accent-red);
  font-size: 0.82rem; flex-shrink: 0;
}
.confirm-bar {
  padding: 12px 20px; border-top: 1px solid var(--border); flex-shrink: 0;
}
.btn-confirm {
  width: 100%; padding: 13px; border-radius: 10px;
  background: var(--accent-green); color: #000;
  border: none; cursor: pointer; font-size: 0.95rem; font-weight: 700;
  letter-spacing: 0.01em;
  transition: opacity 0.15s, transform 0.1s;
}
.btn-confirm:hover { opacity: 0.9; transform: translateY(-1px); }
.chat-input-bar {
  display: flex; gap: 8px; padding: 14px 20px;
  border-top: 1px solid var(--border); flex-shrink: 0;
}
.chat-input {
  flex: 1; padding: 10px 14px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: inherit; font-size: 0.92rem;
  transition: border-color 0.15s;
}
.chat-input:focus { outline: none; border-color: var(--accent-blue); }
.chat-input:disabled { opacity: 0.45; }
.btn-send {
  width: 40px; height: 40px; border-radius: 10px;
  background: var(--accent-blue); color: white;
  border: none; cursor: pointer; font-size: 1rem;
  display: flex; align-items: center; justify-content: center;
  transition: opacity 0.15s, transform 0.1s; flex-shrink: 0;
}
.btn-send:hover:not(:disabled) { opacity: 0.85; transform: translateY(-1px); }
.btn-send:disabled { opacity: 0.35; cursor: default; }

/* ── Streaming indicator ──────────────────────── */
.bubble.streaming { min-width: 52px; }
.dot-pulse {
  display: inline-flex; gap: 5px; align-items: center; padding: 2px 0;
}
.dot-pulse span {
  width: 6px; height: 6px; border-radius: 50%;
  background: currentColor; opacity: 0.35;
  animation: dotbounce 1.2s ease-in-out infinite;
}
.dot-pulse span:nth-child(2) { animation-delay: 0.15s; }
.dot-pulse span:nth-child(3) { animation-delay: 0.3s; }

@keyframes dotbounce {
  0%, 80%, 100% { opacity: 0.25; transform: translateY(0); }
  40% { opacity: 0.9; transform: translateY(-3px); }
}
@keyframes pulse {
  0%, 100% { opacity: 0.3; } 50% { opacity: 1; }
}
</style>
