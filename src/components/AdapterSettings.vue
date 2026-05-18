<script setup lang="ts">
import { ref, onMounted, computed, onBeforeUnmount } from 'vue'
import AppButton from './ui/AppButton.vue'

interface ConfigKey {
  key: string
  type: string
  required: boolean
  note?: string
}

interface AdapterMeta {
  name: string
  description: string
  configKeys: ConfigKey[]
}

const adapters = ref<AdapterMeta[]>([])
const current = ref<string>('claude')
const selected = ref<string>('claude')
const saving = ref(false)
const loading = ref(true)
const error = ref<string | null>(null)
const saveOk = ref(false)

// F054 — track timer for cleanup
let saveOkTimer: ReturnType<typeof setTimeout> | null = null
onBeforeUnmount(() => {
  if (saveOkTimer) clearTimeout(saveOkTimer)
})

const selectedMeta = computed(() => adapters.value.find(a => a.name === selected.value) ?? null)

onMounted(async () => {
  try {
    const [listRes, curRes] = await Promise.all([
      fetch('/api/adapters', { credentials: 'same-origin' }),
      fetch('/api/adapters/current', { credentials: 'same-origin' }),
    ])
    // F010 — include HTTP status in error message
    if (!listRes.ok || !curRes.ok)
      throw new Error(`Failed to load adapter info (HTTP ${!listRes.ok ? listRes.status : curRes.status})`)
    adapters.value = await listRes.json()
    const curData = await curRes.json()
    current.value = curData.adapter ?? 'claude'
    selected.value = current.value
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
  } finally {
    loading.value = false
  }
})

async function save() {
  saving.value = true
  saveOk.value = false
  error.value = null
  try {
    const res = await fetch('/api/adapters/current', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ adapter: selected.value }),
      credentials: 'same-origin',
    })
    if (!res.ok) {
      // F010 — try to parse server error body before falling back to status text
      const body = await res.json().catch(() => null)
      throw new Error(body?.error ?? `HTTP ${res.status}: ${res.statusText}`)
    }
    current.value = selected.value
    saveOk.value = true
    // F054 — store timer reference so it can be cleared on unmount
    saveOkTimer = setTimeout(() => { saveOk.value = false }, 2000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">LLM Adapter</h3>
    <p class="text-xs text-slate-500 dark:text-slate-400">
      Select which LLM backend pipeline stage agents use. "claude" is the default and spawns the Claude CLI.
    </p>

    <!-- F011 — role="status" for loading, role="alert" for errors -->
    <div v-if="loading" role="status" aria-live="polite" class="text-xs text-slate-400">Loading adapters…</div>
    <div v-else-if="error" role="alert" class="text-xs text-red-500">{{ error }}</div>
    <div v-else class="space-y-3">
      <!-- F008 — radiogroup semantics for adapter selection -->
      <div role="radiogroup" aria-label="LLM Adapter" class="flex flex-wrap gap-2">
        <button
          v-for="a in adapters"
          :key="a.name"
          role="radio"
          :aria-checked="selected === a.name"
          :class="[
            'px-3 py-1.5 rounded text-xs font-medium border transition-colors focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-1',
            selected === a.name
              ? 'bg-blue-600 text-white border-blue-600'
              : 'bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-300 border-slate-300 dark:border-slate-600 hover:border-blue-400',
          ]"
          @click="selected = a.name"
        >
          {{ a.name }}
          <!-- F023 — visible checkmark + sr-only text replaces opacity "(active)" span -->
          <span v-if="a.name === current" class="ml-1" aria-hidden="true">✓</span>
          <span v-if="a.name === current" class="sr-only"> (currently active)</span>
        </button>
      </div>

      <div v-if="selectedMeta" class="bg-slate-50 dark:bg-slate-800/50 rounded p-3 text-xs space-y-2">
        <p class="text-slate-600 dark:text-slate-400">{{ selectedMeta.description }}</p>
        <div v-if="selectedMeta.configKeys.length" class="space-y-1">
          <p class="font-medium text-slate-700 dark:text-slate-300">Configuration</p>
          <!-- F020 — add caption + thead with scoped column headers -->
          <table class="w-full">
            <caption class="sr-only">Adapter configuration keys</caption>
            <thead>
              <tr>
                <th scope="col" class="py-1 pr-3 text-left font-medium text-slate-700 dark:text-slate-300 whitespace-nowrap">Key</th>
                <th scope="col" class="py-1 pr-3 text-left font-medium text-slate-700 dark:text-slate-300">Type</th>
                <th scope="col" class="py-1 text-left font-medium text-slate-700 dark:text-slate-300">Note</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="k in selectedMeta.configKeys" :key="k.key" class="border-b border-slate-200 dark:border-slate-700">
                <td class="py-1 pr-3 font-mono text-blue-600 dark:text-blue-400 whitespace-nowrap">{{ k.key }}</td>
                <td class="py-1 pr-3 text-slate-500">{{ k.type }}<span v-if="k.required" class="text-red-500 ml-1">*</span></td>
                <td class="py-1 text-slate-500">{{ k.note }}</td>
              </tr>
            </tbody>
          </table>
          <!-- F041 — inline code style with bg + rounded -->
          <p class="text-slate-400 mt-1">Set via env var or <code class="font-mono bg-slate-100 dark:bg-slate-800 px-1 rounded text-[11px]">adapter-config.json</code> (PUT /api/settings/adapters).</p>
        </div>
        <p v-else class="text-slate-400 italic">No additional configuration required.</p>
      </div>

      <div class="flex items-center gap-2">
        <AppButton
          size="sm"
          :disabled="saving || selected === current"
          @click="save"
        >
          <!-- F021 — consistent Apply → Applying → Applied microcopy -->
          {{ saving ? 'Applying…' : saveOk ? 'Applied!' : 'Apply Adapter' }}
        </AppButton>
        <span v-if="selected === current" class="text-xs text-slate-400">No changes</span>
        <!-- F012 — sr-only live region announces save confirmation to screen readers -->
        <p v-if="saveOk" role="status" aria-live="polite" class="sr-only">Adapter applied.</p>
      </div>
    </div>
  </div>
</template>
