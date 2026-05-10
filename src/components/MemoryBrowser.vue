<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppButton from './ui/AppButton.vue'

interface MemoryFile {
  path: string
  name: string
}

const files = ref<MemoryFile[]>([])
const selectedPath = ref<string | null>(null)
const content = ref('')
const saving = ref(false)
const saved = ref(false)

async function loadFiles() {
  const res = await fetch('/api/memory')
  if (res.ok) {
    const data = await res.json() as { files: MemoryFile[] }
    files.value = data.files
  }
}

async function openFile(path: string) {
  selectedPath.value = path
  const res = await fetch(`/api/memory/${encodeURIComponent(path)}`)
  if (res.ok) {
    const data = await res.json() as { content: string }
    content.value = data.content
  }
}

async function save() {
  if (!selectedPath.value)
    return
  saving.value = true
  try {
    await fetch(`/api/memory/${encodeURIComponent(selectedPath.value)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: content.value }),
    })
    saved.value = true
    setTimeout(() => {
      saved.value = false
    }, 2000)
  }
  finally {
    saving.value = false
  }
}

onMounted(loadFiles)
</script>

<template>
  <div class="flex h-full min-h-0 border border-slate-200 dark:border-slate-700 rounded-lg overflow-hidden">
    <div class="w-64 flex-shrink-0 border-r border-slate-200 dark:border-slate-700 overflow-y-auto bg-slate-50 dark:bg-slate-800">
      <div class="px-3 py-2 text-xs font-semibold text-slate-500 uppercase tracking-wide border-b border-slate-200 dark:border-slate-700">
        Memory Files
      </div>
      <div v-if="files.length === 0" class="px-3 py-4 text-xs text-slate-400">
        No memory files found
      </div>
      <button
        v-for="f in files"
        :key="f.path"
        type="button"
        class="w-full text-left px-3 py-1.5 text-xs truncate transition-colors"
        :class="selectedPath === f.path
          ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'
          : 'text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700'"
        @click="openFile(f.path)"
      >
        {{ f.name }}
      </button>
    </div>
    <div class="flex-1 flex flex-col min-w-0">
      <div v-if="!selectedPath" class="flex-1 flex items-center justify-center text-sm text-slate-400">
        Select a file to edit
      </div>
      <template v-else>
        <div class="flex items-center justify-between px-4 py-2 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
          <span class="text-xs font-mono text-slate-500 truncate">{{ selectedPath }}</span>
          <AppButton size="sm" :disabled="saving" @click="save">
            {{ saved ? 'Saved!' : saving ? 'Saving…' : 'Save' }}
          </AppButton>
        </div>
        <textarea
          v-model="content"
          class="flex-1 resize-none p-4 font-mono text-xs bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 outline-none"
          spellcheck="false"
        />
      </template>
    </div>
  </div>
</template>
