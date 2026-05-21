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
const dirty = ref(false)
const error = ref<string | null>(null)

async function loadFiles() {
  error.value = null
  try {
    const res = await fetch('/api/memory')
    if (res.ok) {
      const data = await res.json() as { files: MemoryFile[] }
      files.value = data.files
    }
    else {
      error.value = `Failed to load files (${res.status})`
    }
  }
  catch {
    error.value = 'Network error loading files.'
  }
}

async function openFile(path: string) {
  if (dirty.value && !confirm('Unsaved changes will be lost. Continue?'))
    return

  error.value = null
  selectedPath.value = path
  dirty.value = false

  try {
    const res = await fetch(`/api/memory/${encodeURIComponent(path)}`)
    if (res.ok) {
      const data = await res.json() as { content: string }
      content.value = data.content
    }
    else {
      error.value = `Failed to load file (${res.status})`
    }
  }
  catch {
    error.value = 'Network error loading file.'
  }
}

function onContentChange() {
  dirty.value = true
}

async function save() {
  if (!selectedPath.value)
    return
  saving.value = true
  error.value = null
  try {
    const res = await fetch(`/api/memory/${encodeURIComponent(selectedPath.value)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: content.value }),
    })
    if (res.ok) {
      dirty.value = false
      saved.value = true
      setTimeout(() => {
        saved.value = false
      }, 2000)
    }
    else {
      error.value = `Failed to save file (${res.status})`
    }
  }
  catch {
    error.value = 'Network error saving file.'
  }
  finally {
    saving.value = false
  }
}

onMounted(loadFiles)
</script>

<template>
  <div class="flex h-full min-h-0 border border-line rounded-lg overflow-hidden">
    <div class="w-64 flex-shrink-0 border-r border-line overflow-y-auto bg-raised">
      <div class="px-3 py-2 text-xs font-semibold text-slate-500 uppercase tracking-wide border-b border-line">
        Memory Files
      </div>
      <p v-if="error && !selectedPath" role="status" aria-live="polite" class="px-3 py-2 text-xs text-red-500 dark:text-red-400">
        {{ error }}
      </p>
      <div v-if="files.length === 0 && !error" class="px-3 py-4 text-xs text-slate-400">
        No memory files found
      </div>
      <button
        v-for="f in files"
        :key="f.path"
        type="button"
        class="w-full text-left px-3 py-1.5 text-xs truncate transition-colors"
        :class="selectedPath === f.path
          ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'
          : 'text-fg-soft hover:bg-raised'"
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
        <div class="flex items-center justify-between px-4 py-2 border-b border-line bg-card">
          <span class="text-xs font-mono text-slate-500 truncate">
            {{ selectedPath }}
            <span v-if="dirty" class="text-amber-500 ml-1" aria-label="Unsaved changes">*</span>
          </span>
          <AppButton size="sm" :disabled="saving" @click="save">
            {{ saved ? 'Saved!' : saving ? 'Saving…' : 'Save' }}
          </AppButton>
        </div>
        <p v-if="error" role="status" aria-live="polite" class="px-4 py-2 text-xs text-red-500 dark:text-red-400">
          {{ error }}
        </p>
        <textarea
          v-model="content"
          class="flex-1 resize-none p-4 font-mono text-xs bg-card text-fg outline-none"
          spellcheck="false"
          @input="onContentChange"
        />
      </template>
    </div>
  </div>
</template>
