<script setup lang="ts">
import type { ImageAttachment } from '../composables/useRefinementChat'
import type { PipelineTask } from '../types'
import DOMPurify from 'dompurify'
import { Marked } from 'marked'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRefinementChat } from '../composables/useRefinementChat'
import { createTask } from '../composables/useTasks'

const props = defineProps<{ open: boolean, task: PipelineTask | null }>()

const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask], taskCreated: [task: PipelineTask] }>()

const md = new Marked({ breaks: true, gfm: true })

const PHASE_DONE_RE = /__phase_done:\s*\w+/g
const REFINED_TITLE_RE = /^\*{0,2}[Rr]efined\s+[Tt]itle[^\n]*\n?/gm
const JSON_BLOCK_RE = /```json\n([\s\S]*?)```/

function cleanContent(text: string): string {
  return text
    .replace(PHASE_DONE_RE, '')
    .replace(REFINED_TITLE_RE, '')
    .trimEnd()
}

function renderMarkdown(text: string): string {
  return DOMPurify.sanitize(md.parse(cleanContent(text), { async: false }) as string)
}

const currentTask = ref<PipelineTask | null>(props.task)
watch(() => props.task, (t) => {
  currentTask.value = t
})

const inputText = ref('')
const chatEl = ref<HTMLElement | null>(null)
const textareaEl = ref<HTMLTextAreaElement | null>(null)
const fileInputEl = ref<HTMLInputElement | null>(null)
const pendingImages = ref<ImageAttachment[]>([])

function autoResize() {
  const el = textareaEl.value
  if (!el)
    return
  el.style.height = 'auto'
  el.style.height = `${el.scrollHeight}px`
}

watch(inputText, () => nextTick(autoResize))

const {
  messages,
  isStreaming,
  error,
  approvalReady,
  loadHistory,
  sendMessage,
  confirm,
  phaseLabel,
} = useRefinementChat(() => currentTask.value?.id ?? null)

const EXAMPLE_CHIPS = [
  'Implement a new feature',
  'Fix a bug',
  'Refactor code',
  'A new API integration',
]

// ── Dynamic title ─────────────────────────────
const chatTitle = computed(() => {
  // Prefer a refinedTitle extracted from the agent's JSON output
  for (const msg of [...messages.value].reverse()) {
    if (msg.role !== 'assistant' || !msg.content)
      continue
    const jsonMatch = msg.content.match(JSON_BLOCK_RE)
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
  if (!firstUser?.content)
    return 'New Ticket'
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
  if (!items)
    return
  for (const item of Array.from(items)) {
    if (!item.type.startsWith('image/'))
      continue
    e.preventDefault()
    const file = item.getAsFile()
    if (!file)
      continue
    const dataUrl = await readFileAsDataUrl(file)
    pendingImages.value.push({ dataUrl, mimeType: item.type })
  }
}

async function handleFileSelect(e: Event) {
  const files = (e.target as HTMLInputElement).files
  if (!files)
    return
  for (const file of Array.from(files)) {
    if (!file.type.startsWith('image/'))
      continue
    const dataUrl = await readFileAsDataUrl(file)
    pendingImages.value.push({ dataUrl, mimeType: file.type })
  }
  if (fileInputEl.value)
    fileInputEl.value.value = ''
}

function removeImage(idx: number) {
  pendingImages.value.splice(idx, 1)
}

watch(() => props.open, async (val) => {
  if (val && currentTask.value) {
    await loadHistory()
  }
})

onMounted(() => {
  if (props.open && currentTask.value)
    loadHistory()
})

watch(messages, async () => {
  await nextTick()
  chatEl.value?.scrollTo({ top: chatEl.value.scrollHeight, behavior: 'smooth' })
}, { deep: true })

async function handleSend() {
  const msg = inputText.value.trim()
  if (!msg || isStreaming.value)
    return
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  inputText.value = ''
  pendingImages.value = []
  await nextTick(autoResize)
  if (currentTask.value === null) {
    const newTask = await createTask({
      slug: `concept-${Date.now()}`,
      title: 'New Task',
      cwd: '/',
      stage: 'concept',
    })
    currentTask.value = newTask
    emit('taskCreated', newTask)
  }
  await sendMessage(msg, images)
}

async function handleConfirm() {
  const updated = await confirm()
  if (updated)
    emit('confirmed', updated)
}

function isPhaseMarker(idx: number): string | null {
  const msg = messages.value[idx]
  if (msg.role !== 'assistant' || !msg.phase)
    return null
  return msg.phase
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 bg-black/60 flex items-center justify-center z-[100] backdrop-blur-sm"
    @click.self="emit('close')"
  >
    <div
      class="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-2xl flex flex-col overflow-hidden shadow-2xl"
      style="width: min(900px, 96vw); height: min(88vh, 92vh)"
    >
      <!-- Header -->
      <div class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700 shrink-0">
        <span class="text-base font-semibold tracking-tight text-slate-800 dark:text-slate-200">{{ chatTitle }}</span>
        <button
          class="bg-transparent border-none cursor-pointer text-base text-slate-400 dark:text-slate-500 px-2 py-1 rounded-md transition-all hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800"
          @click="emit('close')"
        >
          ✕
        </button>
      </div>

      <!-- Messages -->
      <div
        ref="chatEl"
        class="flex-1 overflow-y-auto px-5 py-4 flex flex-col gap-2"
        :class="{ 'justify-center': messages.length === 0 }"
      >
        <!-- Empty state -->
        <div v-if="messages.length === 0" class="flex flex-col items-center gap-5 text-center px-6">
          <div class="text-3xl opacity-30 leading-none">
            ✦
          </div>
          <div class="flex flex-col gap-2">
            <p class="text-xl font-bold tracking-tight m-0 text-slate-800 dark:text-slate-100">
              What would you like to build?
            </p>
            <p class="text-sm text-slate-500 dark:text-slate-400 leading-relaxed m-0 max-w-[380px]">
              Describe your idea — I'll guide you through analysis, spec, and implementation plan.
            </p>
          </div>
          <div class="flex flex-wrap gap-2 justify-center max-w-[480px]">
            <button
              v-for="chip in EXAMPLE_CHIPS"
              :key="chip"
              class="px-4 py-2 rounded-full border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 cursor-pointer text-sm font-medium text-slate-600 dark:text-slate-400 transition-all hover:border-blue-400 dark:hover:border-blue-500 hover:bg-slate-100 dark:hover:bg-slate-700 hover:-translate-y-px active:translate-y-0"
              @click="inputText = chip"
            >
              {{ chip }}
            </button>
          </div>
        </div>

        <!-- Message list -->
        <template v-for="(msg, idx) in messages" :key="idx">
          <div v-if="isPhaseMarker(idx)" class="phase-marker">
            ✓ {{ phaseLabel(isPhaseMarker(idx)!) }} complete
          </div>

          <div
            v-if="msg.role === 'user'"
            class="self-end max-w-[85%] px-3 py-2 rounded-xl rounded-br-sm bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 leading-relaxed break-words text-[13px] font-mono whitespace-pre-wrap"
          >
            <div v-if="msg.images?.length" class="flex flex-wrap gap-1.5 mb-1.5">
              <img
                v-for="(img, i) in msg.images"
                :key="i"
                :src="img.dataUrl"
                class="max-w-[180px] max-h-[120px] rounded-md object-cover border border-slate-200 dark:border-slate-700"
                alt="attachment"
              >
            </div>
            {{ msg.content }}
          </div>
          <div
            v-else-if="msg.content"
            class="assistant-bubble markdown-body"
            v-html="renderMarkdown(msg.content)"
          />
        </template>

        <!-- Streaming indicator -->
        <div
          v-if="isStreaming && !messages.at(-1)?.content"
          class="self-start min-w-[52px] px-3 py-2 rounded-xl rounded-bl-sm bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-400 dark:text-slate-500"
        >
          <span class="dot-pulse"><span /><span /><span /></span>
        </div>
      </div>

      <!-- Error -->
      <div v-if="error" class="px-5 py-2 text-red-500 text-[0.82rem] shrink-0">
        {{ error }}
      </div>

      <!-- Confirm bar -->
      <div v-if="approvalReady" class="px-5 py-3 border-t border-slate-200 dark:border-slate-700 shrink-0">
        <button
          class="w-full py-3 px-4 rounded-xl bg-green-500 text-black font-bold text-[0.95rem] tracking-tight border-none cursor-pointer transition-all hover:opacity-90 hover:-translate-y-px"
          @click="handleConfirm"
        >
          Create Task →
        </button>
      </div>

      <!-- Image previews -->
      <div v-if="pendingImages.length > 0" class="flex flex-wrap gap-2 px-5 pt-2.5 shrink-0">
        <div v-for="(img, idx) in pendingImages" :key="idx" class="relative inline-flex">
          <img
            :src="img.dataUrl"
            class="max-w-[80px] max-h-[60px] rounded-md object-cover border border-slate-200 dark:border-slate-700"
            alt="attachment"
          >
          <button
            class="absolute -top-1.5 -right-1.5 w-[18px] h-[18px] rounded-full bg-slate-200 dark:bg-slate-700 border border-slate-300 dark:border-slate-600 text-slate-500 dark:text-slate-400 text-[10px] cursor-pointer flex items-center justify-center leading-none p-0 hover:bg-red-500 hover:text-white hover:border-red-500"
            @click="removeImage(idx)"
          >
            ✕
          </button>
        </div>
      </div>

      <!-- Input bar -->
      <div class="flex gap-2 px-5 py-2.5 pb-3.5 border-t border-slate-200 dark:border-slate-700 shrink-0 items-end">
        <button
          class="w-9 h-9 rounded-xl shrink-0 bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-400 text-lg cursor-pointer flex items-center justify-center transition-colors hover:enabled:border-blue-400 hover:enabled:text-blue-400 disabled:opacity-35 disabled:cursor-default"
          title="Attach image"
          :disabled="isStreaming || approvalReady"
          @click="fileInputEl?.click()"
        >
          ⊕
        </button>
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
          class="flex-1 px-3 py-2 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-slate-800 dark:text-slate-200 placeholder:text-slate-400 dark:placeholder:text-slate-600 text-[13px] font-mono leading-relaxed resize-none overflow-y-auto min-h-9 max-h-40 transition-colors focus:outline-none focus:border-blue-400 dark:focus:border-blue-500 disabled:opacity-45"
          placeholder="Message..."
          rows="1"
          :disabled="isStreaming || approvalReady"
          @keydown.enter.exact.prevent="handleSend"
          @paste="handlePaste"
        />
        <button
          v-if="messages.length > 0 && !approvalReady"
          class="w-10 h-10 rounded-xl shrink-0 bg-green-500 text-black border-none cursor-pointer text-base font-bold flex items-center justify-center transition-all hover:enabled:opacity-85 hover:enabled:-translate-y-px disabled:opacity-35 disabled:cursor-default"
          title="Ja, passt so — weiter"
          :disabled="isStreaming"
          @click="sendMessage('Ja, passt so. Mach weiter.')"
        >
          ✓
        </button>
        <button
          class="w-10 h-10 rounded-xl bg-blue-500 text-white border-none cursor-pointer text-base flex items-center justify-center transition-all hover:enabled:opacity-85 hover:enabled:-translate-y-px disabled:opacity-35 disabled:cursor-default shrink-0"
          :disabled="isStreaming || (!inputText.trim() && !pendingImages.length) || approvalReady"
          @click="handleSend"
        >
          →
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ── Assistant bubble ─────────────────────────── */
.assistant-bubble {
  align-self: flex-start;
  max-width: 88%;
  padding: 10px 14px;
  border-radius: 12px 12px 12px 4px;
  line-height: 1.65;
  word-break: break-word;
  font-size: 13.5px;
  background: #f8fafc;
  color: #334155;
  border: 1px solid #e2e8f0;
}
.dark .assistant-bubble {
  background: rgba(30, 41, 59, 0.45);
  color: #cbd5e1;
  border-color: rgba(51, 65, 85, 0.5);
}

/* ── Markdown overrides (must stay here for :deep()) ── */
.markdown-body { white-space: normal; }
.markdown-body :deep(p) { margin: 0 0 0.5em; }
.markdown-body :deep(p:last-child) { margin-bottom: 0; }
.markdown-body :deep(code) {
  background: #f1f5f9;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 0.9em;
  font-family: var(--font-mono);
}
.dark .markdown-body :deep(code) { background: rgba(15, 23, 42, 0.8); }
.markdown-body :deep(pre) {
  background: #f1f5f9;
  padding: 10px 12px;
  border-radius: 6px;
  overflow-x: auto;
  margin: 6px 0;
}
.dark .markdown-body :deep(pre) { background: #0f172a; }
.markdown-body :deep(pre code) { background: none; padding: 0; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { margin: 4px 0; padding-left: 1.4em; }
.markdown-body :deep(li) { margin: 2px 0; }
.markdown-body :deep(strong) { color: #1e293b; font-weight: 600; }
.dark .markdown-body :deep(strong) { color: #f1f5f9; }
.markdown-body :deep(a) { color: #3b82f6; }
.dark .markdown-body :deep(a) { color: #60a5fa; }
.markdown-body :deep(h1), .markdown-body :deep(h2), .markdown-body :deep(h3) {
  color: #1e293b;
  margin: 0.6em 0 0.3em;
  font-size: 1em;
  font-weight: 700;
}
.dark .markdown-body :deep(h1),
.dark .markdown-body :deep(h2),
.dark .markdown-body :deep(h3) { color: #f1f5f9; }
.markdown-body :deep(blockquote) {
  border-left: 3px solid #cbd5e1;
  padding-left: 10px;
  margin: 6px 0;
  color: #64748b;
}
.dark .markdown-body :deep(blockquote) { border-left-color: #475569; color: #94a3b8; }
.markdown-body :deep(hr) { border: none; border-top: 1px solid #e2e8f0; margin: 8px 0; }
.dark .markdown-body :deep(hr) { border-top-color: #334155; }
.markdown-body :deep(table) { border-collapse: collapse; width: 100%; margin: 6px 0; font-size: 12px; }
.markdown-body :deep(th), .markdown-body :deep(td) {
  border: 1px solid #e2e8f0;
  padding: 4px 8px;
  text-align: left;
}
.dark .markdown-body :deep(th), .dark .markdown-body :deep(td) { border-color: #334155; }
.markdown-body :deep(th) { background: #f1f5f9; color: #1e293b; font-weight: 600; }
.dark .markdown-body :deep(th) { background: #1e293b; color: #f1f5f9; }

/* ── Phase milestone ──────────────────────────── */
.phase-marker {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.75rem;
  color: #22c55e;
  opacity: 0.75;
  padding: 4px 0;
  justify-content: center;
}
.phase-marker::before,
.phase-marker::after {
  content: '';
  flex: 1;
  max-width: 60px;
  height: 1px;
  background: #22c55e;
  opacity: 0.4;
}

/* ── Streaming dots ───────────────────────────── */
.dot-pulse {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  padding: 2px 0;
}
.dot-pulse span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.35;
  animation: dotbounce 1.2s ease-in-out infinite;
}
.dot-pulse span:nth-child(2) { animation-delay: 0.15s; }
.dot-pulse span:nth-child(3) { animation-delay: 0.3s; }

@keyframes dotbounce {
  0%, 80%, 100% { opacity: 0.25; transform: translateY(0); }
  40% { opacity: 0.9; transform: translateY(-3px); }
}
</style>
