<script setup lang="ts">
import { createTwoFilesPatch } from 'diff'
import { computed, onUnmounted, ref } from 'vue'
import { useVisibilityPolling } from '../composables/useVisibilityPolling'

interface PendingEdit {
  id: string
  sessionId: string
  toolName: string
  filePath: string
  oldContent: string
  newContent: string
  createdAt: number
}

const props = defineProps<{ sessionId?: string }>()

const edits = ref<PendingEdit[]>([])
const current = computed(() => edits.value[0] ?? null)

function diffLines(edit: PendingEdit): Array<{ type: 'add' | 'remove' | 'context', text: string }> {
  const patch = createTwoFilesPatch(
    edit.filePath,
    edit.filePath,
    edit.oldContent,
    edit.newContent,
    'original',
    'modified',
  )
  return patch
    .split('\n')
    .slice(4)
    .map((line) => {
      if (line.startsWith('+'))
        return { type: 'add' as const, text: line }
      if (line.startsWith('-'))
        return { type: 'remove' as const, text: line }
      return { type: 'context' as const, text: line }
    })
}

async function respond(decision: 'accept' | 'reject') {
  if (!current.value)
    return
  await fetch('/api/hooks/respond', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: current.value.id, decision }),
  })
  edits.value.shift()
}

const abortCtrl = new AbortController()
onUnmounted(() => abortCtrl.abort())

async function pollPending() {
  const url = props.sessionId
    ? `/api/hooks/pending?sessionId=${encodeURIComponent(props.sessionId)}`
    : '/api/hooks/pending'
  try {
    const res = await fetch(url, { signal: abortCtrl.signal })
    if (!res.ok)
      return
    const data = await res.json() as { edits: PendingEdit[] }
    if (!abortCtrl.signal.aborted)
      edits.value = data.edits
  }
  catch {
    // ignore poll errors (includes AbortError)
  }
}

useVisibilityPolling(pollPending, 3000)
</script>

<template>
  <Teleport to="body">
    <div
      v-if="current"
      class="fixed inset-0 z-[1100] flex items-center justify-center bg-black/60"
    >
      <div class="bg-card rounded-xl border border-line shadow-2xl w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden">
        <header class="px-5 py-3.5 border-b border-line flex items-center justify-between flex-shrink-0">
          <div>
            <h2 class="text-sm font-semibold text-fg">
              Edit Gate — {{ current.toolName }}
            </h2>
            <p class="text-xs text-slate-500 font-mono mt-0.5">
              {{ current.filePath }}
            </p>
          </div>
        </header>
        <div class="flex-1 overflow-y-auto bg-slate-950 text-xs font-mono p-4">
          <div
            v-for="(line, idx) in diffLines(current)"
            :key="idx"
            class="leading-5 whitespace-pre"
            :class="{
              'text-green-400 bg-green-900/20': line.type === 'add',
              'text-red-400 bg-red-900/20': line.type === 'remove',
              'text-slate-400': line.type === 'context',
            }"
          >
            {{ line.text }}
          </div>
        </div>
        <footer class="px-5 py-3 border-t border-line flex justify-end gap-2 flex-shrink-0">
          <button
            type="button"
            class="px-4 py-1.5 text-sm rounded border border-line-strong text-fg-soft hover:bg-raised"
            @click="respond('reject')"
          >
            Reject
          </button>
          <button
            type="button"
            class="px-4 py-1.5 text-sm rounded bg-blue-600 text-white hover:bg-blue-700"
            @click="respond('accept')"
          >
            Accept
          </button>
        </footer>
      </div>
    </div>
  </Teleport>
</template>
