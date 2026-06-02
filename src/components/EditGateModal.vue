<script setup lang="ts">
import { createTwoFilesPatch } from 'diff'
import { computed, onUnmounted, ref } from 'vue'
import { useVisibilityPolling } from '../composables/useVisibilityPolling'
import AppModal from './ui/AppModal.vue'

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
  <!-- No @close binding — this is a non-dismissable permission gate.
       AppModal will emit 'close' on Escape/backdrop, but with no handler
       bound it is simply ignored, keeping the gate open. -->
  <AppModal :open="!!current" :z-index="1100">
    <header class="shrink-0 px-5 py-3.5 border-b border-line flex items-center justify-between">
      <div>
        <h2 class="text-sm font-semibold text-fg">
          Edit Gate — {{ current?.toolName }}
        </h2>
        <p class="text-xs text-slate-500 font-mono mt-0.5">
          {{ current?.filePath }}
        </p>
      </div>
    </header>
    <div class="flex-1 min-h-0 overflow-y-auto bg-slate-950 text-xs font-mono p-4">
      <div
        v-for="(line, idx) in current ? diffLines(current) : []"
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
    <footer class="shrink-0 px-5 py-3 border-t border-line flex justify-end gap-2">
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
  </AppModal>
</template>
