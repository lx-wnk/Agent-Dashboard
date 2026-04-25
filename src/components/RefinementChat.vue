<script setup lang="ts">
import type { ImageAttachment } from '../composables/useRefinementChat'
import type { PipelineTask } from '../types'
import DOMPurify from 'dompurify'
import { Marked } from 'marked'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRefinementChat } from '../composables/useRefinementChat'

const md = new Marked({ breaks: true, gfm: true })

function cleanContent(text: string): string {
  return text
    .replace(/__phase_done:\s*\w+/g, '')
    .replace(/^\*{0,2}[Rr]efined\s+[Tt]itle\*{0,2}:?[^\n]*\n?/gm, '')
    .trimEnd()
}

function renderMarkdown(text: string): string {
  return DOMPurify.sanitize(md.parse(cleanContent(text), { async: false }) as string)
}

const props = defineProps<{ open: boolean, task: PipelineTask | null }>()
const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask] }>()

const inputText = ref('')
const chatEl = ref<HTMLElement | null>(null)
const textareaEl = ref<HTMLTextAreaElement | null>(null)
const fileInputEl = ref<HTMLInputElement | null>(null)
const pendingImages = ref<ImageAttachment[]>([])

function autoResize() {
  const el = textareaEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${el.scrollHeight}px`
}

watch(inputText, () => nextTick(autoResize))

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

// ── Dynamic title ─────────────────────────────
const chatTitle = computed(() => {
  // Prefer a refinedTitle extracted from the agent's JSON output
  for (const msg of [...messages.value].reverse()) {
    if (msg.role !== 'assistant' || !msg.content) continue
    const jsonMatch = msg.content.match(/```json\n([\s\S]*?)```/)
    if (jsonMatch) {
      try {
        const parsed = JSON.parse(jsonMatch[1])
        if (typeof parsed.refinedTitle === 'string' && parsed.refinedTitle.trim()) {
          const t = parsed.refinedTitle.trim()
          return t.length > 30 ? `${t.slice(0, 27)}…` : t
        }
      }
      catch {}
    }
  }
  // Fall back to first user message
  const firstUser = messages.value.find(m => m.role === 'user')
  if (!firstUser?.content) return 'Neues Ticket'
  const t = firstUser.content.replace(/\s+/g, ' ').trim()
  return t.length > 30 ? `${t.slice(0, 27)}…` : t
})

// ── Image handling ────────────────────────────
function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.onload = ev => resolve(ev.target?.result as string)
    reader.readAsDataURL(file)
  })
}

async function handlePaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items
  if (!items) return
  for (const item of Array.from(items)) {
    if (!item.type.startsWith('image/')) continue
    e.preventDefault()
    const file = item.getAsFile()
    if (!file) continue
    const dataUrl = await readFileAsDataUrl(file)
    pendingImages.value.push({ dataUrl, mimeType: item.type })
  }
}

async function handleFileSelect(e: Event) {
  const files = (e.target as HTMLInputElement).files
  if (!files) return
  for (const file of Array.from(files)) {
    if (!file.type.startsWith('image/')) continue
    const dataUrl = await readFileAsDataUrl(file)
    pendingImages.value.push({ dataUrl, mimeType: file.type })
  }
  if (fileInputEl.value) fileInputEl.value.value = ''
}

function removeImage(idx: number) {
  pendingImages.value.splice(idx, 1)
}

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
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  inputText.value = ''
  pendingImages.value = []
  await nextTick(autoResize)
  await sendMessage(msg, images)
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
        <span class="chat-title">{{ chatTitle }}</span>
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

          <div v-if="msg.role === 'user'" class="bubble user">
            <div v-if="msg.images?.length" class="bubble-images">
              <img v-for="(img, i) in msg.images" :key="i" :src="img.dataUrl" class="bubble-img" alt="attachment" />
            </div>
            {{ msg.content }}
          </div>
          <div
            v-else-if="msg.content"
            class="bubble assistant markdown-body"
            v-html="renderMarkdown(msg.content)"
          />
        </template>

        <div v-if="isStreaming && !messages.at(-1)?.content" class="bubble assistant streaming">
          <span class="dot-pulse"><span /><span /><span /></span>
        </div>
      </div>

      <div v-if="error" class="chat-error">{{ error }}</div>

      <div v-if="approvalReady" class="confirm-bar">
        <button class="btn-confirm" @click="handleConfirm">
          Task erstellen →
        </button>
      </div>

      <div v-if="pendingImages.length > 0" class="image-preview-row">
        <div v-for="(img, idx) in pendingImages" :key="idx" class="image-preview">
          <img :src="img.dataUrl" alt="attachment" />
          <button class="remove-img" @click="removeImage(idx)">✕</button>
        </div>
      </div>

      <div class="chat-input-bar">
        <button
          class="btn-attach"
          title="Bild anhängen"
          :disabled="isStreaming || approvalReady"
          @click="fileInputEl?.click()"
        >⊕</button>
        <input
          ref="fileInputEl"
          type="file"
          accept="image/*"
          multiple
          style="display:none"
          @change="handleFileSelect"
        >
        <textarea
          ref="textareaEl"
          v-model="inputText"
          class="chat-input"
          placeholder="Nachricht..."
          rows="1"
          :disabled="isStreaming || approvalReady"
          @keydown.enter.exact.prevent="handleSend"
          @paste="handlePaste"
        />
        <button
          v-if="messages.length > 0 && !approvalReady"
          class="btn-ok"
          title="Ja, passt so — weiter"
          :disabled="isStreaming"
          @click="sendMessage('Ja, passt so. Mach weiter.')"
        >✓</button>
        <button
          class="btn-send"
          :disabled="isStreaming || (!inputText.trim() && !pendingImages.length) || approvalReady"
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
  flex: 1; overflow-y: auto; padding: 16px 20px;
  display: flex; flex-direction: column; gap: 6px;
  font-family: var(--font-mono); font-size: 13px;
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
  max-width: 80%; padding: 8px 12px; border-radius: 12px;
  line-height: 1.5; word-break: break-word;
  font-size: 13px; font-family: var(--font-mono);
}
.bubble.user {
  align-self: flex-end;
  background: var(--bg-tertiary); color: var(--text-muted);
  border-bottom-right-radius: 4px;
  white-space: pre-wrap;
}
.bubble-images {
  display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px;
}
.bubble-img {
  max-width: 180px; max-height: 120px; border-radius: 6px;
  object-fit: cover; border: 1px solid var(--border);
}
.bubble.assistant {
  align-self: flex-start;
  background: var(--bg-tertiary); color: var(--text-secondary);
  border-bottom-left-radius: 4px;
}

/* ── Markdown body ────────────────────────────── */
.markdown-body { white-space: normal; }
.markdown-body :deep(p) { margin: 0 0 0.4em; }
.markdown-body :deep(p:last-child) { margin-bottom: 0; }
.markdown-body :deep(code) {
  background: var(--bg-primary); padding: 1px 5px;
  border-radius: 4px; font-size: 0.9em;
}
.markdown-body :deep(pre) {
  background: var(--bg-primary); padding: 10px 12px;
  border-radius: 6px; overflow-x: auto; margin: 6px 0;
}
.markdown-body :deep(pre code) { background: none; padding: 0; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { margin: 4px 0; padding-left: 1.4em; }
.markdown-body :deep(li) { margin: 2px 0; }
.markdown-body :deep(strong) { color: var(--text-primary); }
.markdown-body :deep(a) { color: var(--accent-blue); }
.markdown-body :deep(h1), .markdown-body :deep(h2), .markdown-body :deep(h3) {
  color: var(--text-primary); margin: 0.5em 0 0.25em;
  font-size: 1em; font-weight: 700;
}
.markdown-body :deep(blockquote) {
  border-left: 3px solid var(--border); padding-left: 10px;
  margin: 6px 0; color: var(--text-muted);
}
.markdown-body :deep(hr) { border: none; border-top: 1px solid var(--border); margin: 8px 0; }

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
/* ── Image preview row ────────────────────────── */
.image-preview-row {
  display: flex; flex-wrap: wrap; gap: 8px;
  padding: 10px 20px 0; flex-shrink: 0;
}
.image-preview {
  position: relative; display: inline-flex;
}
.image-preview img {
  max-width: 80px; max-height: 60px; border-radius: 6px;
  object-fit: cover; border: 1px solid var(--border);
}
.remove-img {
  position: absolute; top: -6px; right: -6px;
  width: 18px; height: 18px; border-radius: 50%;
  background: var(--bg-tertiary); border: 1px solid var(--border);
  color: var(--text-muted); font-size: 10px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  line-height: 1; padding: 0;
}
.remove-img:hover { background: var(--accent-red); color: white; border-color: var(--accent-red); }

.chat-input-bar {
  display: flex; gap: 8px; padding: 10px 20px 14px;
  border-top: 1px solid var(--border); flex-shrink: 0;
  align-items: flex-end;
}
.btn-attach {
  width: 38px; height: 38px; border-radius: 10px; flex-shrink: 0;
  background: var(--bg-secondary); border: 1px solid var(--border);
  color: var(--text-muted); font-size: 1.1rem; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: border-color 0.15s, color 0.15s;
}
.btn-attach:hover:not(:disabled) { border-color: var(--accent-blue); color: var(--accent-blue); }
.btn-attach:disabled { opacity: 0.35; cursor: default; }
.chat-input {
  flex: 1; padding: 8px 12px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: var(--text-primary); font-size: 13px; font-family: var(--font-mono);
  line-height: 1.5; resize: none; overflow-y: auto;
  min-height: 38px; max-height: 160px;
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
.btn-ok {
  width: 40px; height: 40px; border-radius: 10px; flex-shrink: 0;
  background: var(--accent-green); color: #000;
  border: none; cursor: pointer; font-size: 1rem; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  transition: opacity 0.15s, transform 0.1s;
}
.btn-ok:hover:not(:disabled) { opacity: 0.85; transform: translateY(-1px); }
.btn-ok:disabled { opacity: 0.35; cursor: default; }

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
