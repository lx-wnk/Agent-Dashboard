<script setup lang="ts">
import type { SettingView } from '../composables/useSettings'
import { computed, onMounted, ref } from 'vue'
import { useSettings } from '../composables/useSettings'
import { errorMessage } from '../utils/errorMessage'

const { items, loading, error, refetch, update } = useSettings()
const saving = ref<string | null>(null)

const notice = ref<{ kind: 'success' | 'warning', text: string } | null>(null)
let noticeTimer: ReturnType<typeof setTimeout> | null = null

function showNotice(kind: 'success' | 'warning', text: string) {
  notice.value = { kind, text }
  if (noticeTimer)
    clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => (notice.value = null), 5000)
}

const groups = computed(() => {
  const byCategory = new Map<string, SettingView[]>()
  for (const item of items.value) {
    const list = byCategory.get(item.category) ?? []
    list.push(item)
    byCategory.set(item.category, list)
  }
  return [...byCategory.entries()]
    .map(([category, settings]) => ({ category, settings }))
    .sort((a, b) => a.category.localeCompare(b.category))
})

async function apply(item: SettingView, value: string) {
  saving.value = item.key
  try {
    const applied = await update(item.key, value)
    if (applied === 'restart')
      showNotice('warning', `${item.key}: applies after a server restart`)
    else
      showNotice('success', `${item.key} updated`)
  }
  catch (e) {
    error.value = errorMessage(e, 'Failed to save setting')
  }
  finally {
    saving.value = null
  }
}

function onCheckbox(item: SettingView, e: Event) {
  apply(item, String((e.target as HTMLInputElement).checked))
}

function onValue(item: SettingView, e: Event) {
  apply(item, (e.target as HTMLInputElement | HTMLSelectElement).value)
}

onMounted(refetch)
</script>

<template>
  <div class="space-y-6">
    <div>
      <h3 class="text-sm font-semibold text-fg-soft">
        Server settings
      </h3>
      <p class="text-xs text-fg-mute mt-0.5">
        DB-backed runtime configuration. Some changes apply live; others take effect after a
        server restart.
      </p>
    </div>

    <div
      v-if="notice"
      role="status"
      aria-live="polite"
      class="text-xs rounded-md px-3 py-2"
      :class="notice.kind === 'warning' ? 'bg-warning-soft text-warning-text' : 'bg-success-soft text-success-text'"
    >
      {{ notice.text }}
    </div>

    <div role="status" aria-live="polite" aria-atomic="true" class="text-xs text-fg-faint" :class="{ 'sr-only': !loading }">
      {{ loading ? 'Loading…' : '' }}
    </div>
    <div role="alert" aria-atomic="true" class="text-xs text-danger-text" :class="{ 'sr-only': !error || loading }">
      {{ !loading ? (error ?? '') : '' }}
    </div>

    <div v-if="!loading && !error" class="space-y-6">
      <div v-for="group in groups" :key="group.category">
        <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
          {{ group.category }}
        </h4>
        <ul class="border border-line rounded-lg divide-y divide-line text-xs">
          <li
            v-for="item in group.settings"
            :key="item.key"
            class="flex items-center justify-between gap-3 px-3 py-2.5"
          >
            <div class="min-w-0">
              <p class="font-medium text-fg font-mono">
                {{ item.key }}
              </p>
              <p class="text-fg-faint text-[10px] mt-0.5">
                {{ item.apply === 'restart' ? 'Restart required' : 'Applies live' }} · default {{ item.default || '—' }}
              </p>
            </div>

            <input
              v-if="item.type === 'bool'"
              type="checkbox"
              :checked="item.value === 'true'"
              :aria-label="item.key"
              :disabled="saving === item.key"
              class="h-4 w-4 shrink-0 cursor-pointer accent-accent"
              @change="onCheckbox(item, $event)"
            >
            <select
              v-else-if="item.type === 'enum'"
              :value="item.value"
              :aria-label="item.key"
              :disabled="saving === item.key"
              class="w-44 shrink-0 bg-card border border-line rounded px-2 py-1 text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
              @change="onValue(item, $event)"
            >
              <option v-for="opt in item.enum" :key="opt" :value="opt">
                {{ opt }}
              </option>
            </select>
            <input
              v-else-if="item.type === 'int' || item.type === 'float'"
              type="number"
              :value="item.value"
              :step="item.type === 'float' ? 'any' : '1'"
              :aria-label="item.key"
              :disabled="saving === item.key"
              class="w-44 shrink-0 bg-card border border-line rounded px-2 py-1 text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
              @change="onValue(item, $event)"
            >
            <input
              v-else
              type="text"
              :value="item.value"
              :aria-label="item.key"
              :disabled="saving === item.key"
              class="w-44 shrink-0 bg-card border border-line rounded px-2 py-1 text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
              @change="onValue(item, $event)"
            >
          </li>
        </ul>
      </div>
    </div>
  </div>
</template>
