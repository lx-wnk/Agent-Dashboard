<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AppButton from './ui/AppButton.vue'

interface NotifPref {
  eventType: string
  channels: string[]
  enabled: boolean
}

const KNOWN_EVENTS: { type: string; label: string; description: string }[] = [
  { type: 'on_hold', label: 'On Hold', description: 'Task paused — requires user input' },
  { type: 'approval_needed', label: 'Approval Needed', description: 'Stage agent requesting tool permission' },
  { type: 'completed', label: 'Completed', description: 'Task finished successfully' },
  { type: 'failed', label: 'Failed', description: 'Task or stage encountered an unrecoverable error' },
  { type: 'budget_exceeded', label: 'Budget Exceeded', description: 'Token or cost budget threshold reached' },
  { type: 'iteration_warning', label: 'Iteration Warning', description: 'Stage nearing retry limit' },
]

const CHANNELS = ['webhook', 'email', 'browser', 'system'] as const
type Channel = typeof CHANNELS[number]

const prefs = ref<Map<string, NotifPref>>(new Map())
const config = ref<Record<string, string>>({})
const loading = ref(true)
const error = ref<string | null>(null)

const savingPref = ref<string | null>(null)
const savingConfig = ref(false)
const configSaveOk = ref(false)
const prefSaveOk = ref<string | null>(null)

function getPref(eventType: string): NotifPref {
  return prefs.value.get(eventType) ?? { eventType, channels: [], enabled: false }
}

onMounted(async () => {
  try {
    const [prefRes, cfgRes] = await Promise.all([
      fetch('/api/tasks/settings/notifications'),
      fetch('/api/tasks/settings/notification-config'),
    ])
    if (!prefRes.ok || !cfgRes.ok)
      throw new Error(`HTTP ${prefRes.ok ? cfgRes.status : prefRes.status}`)

    const prefList: NotifPref[] = await prefRes.json()
    for (const p of prefList)
      prefs.value.set(p.eventType, p)

    config.value = await cfgRes.json()
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load notifications'
  }
  finally {
    loading.value = false
  }
})

async function toggleEnabled(eventType: string) {
  const current = getPref(eventType)
  await savePref(eventType, { ...current, enabled: !current.enabled })
}

async function toggleChannel(eventType: string, channel: Channel) {
  const current = getPref(eventType)
  const channels = current.channels.includes(channel)
    ? current.channels.filter(c => c !== channel)
    : [...current.channels, channel]
  await savePref(eventType, { ...current, channels })
}

async function savePref(eventType: string, updated: NotifPref) {
  savingPref.value = eventType
  prefSaveOk.value = null
  try {
    const res = await fetch(`/api/tasks/settings/notifications/${eventType}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channels: updated.channels, enabled: updated.enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: NotifPref = await res.json()
    prefs.value.set(eventType, saved)
    prefSaveOk.value = eventType
    setTimeout(() => { if (prefSaveOk.value === eventType) prefSaveOk.value = null }, 1500)
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  }
  finally {
    savingPref.value = null
  }
}

async function saveConfig() {
  savingConfig.value = true
  configSaveOk.value = false
  error.value = null
  try {
    const res = await fetch('/api/tasks/settings/notification-config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config.value),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    config.value = await res.json()
    configSaveOk.value = true
    setTimeout(() => { configSaveOk.value = false }, 2000)
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  }
  finally {
    savingConfig.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">
        Notifications
      </h3>
      <p class="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
        Configure which pipeline events trigger notifications and via which channels.
      </p>
    </div>

    <div v-if="loading" class="text-xs text-slate-400">
      Loading…
    </div>
    <div v-else-if="error" class="text-xs text-red-500">
      {{ error }}
    </div>

    <template v-else>
      <!-- Event preferences table -->
      <div class="border border-slate-200 dark:border-slate-700 rounded-lg overflow-hidden text-xs">
        <table class="w-full">
          <thead>
            <tr class="bg-slate-50 dark:bg-slate-800/50">
              <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 px-3 py-2 font-medium">
                Event
              </th>
              <th class="text-center text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 px-3 py-2 font-medium">
                Enabled
              </th>
              <th
                v-for="ch in CHANNELS"
                :key="ch"
                class="text-center text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-500 px-2 py-2 font-medium capitalize"
              >
                {{ ch }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="ev in KNOWN_EVENTS"
              :key="ev.type"
              class="border-t border-slate-200 dark:border-slate-700"
              :class="{ 'opacity-50': !getPref(ev.type).enabled }"
            >
              <td class="px-3 py-2.5">
                <p class="font-medium text-slate-800 dark:text-slate-200">
                  {{ ev.label }}
                </p>
                <p class="text-slate-400 dark:text-slate-500 text-[10px] mt-0.5">
                  {{ ev.description }}
                </p>
              </td>
              <td class="px-3 py-2.5 text-center">
                <button
                  type="button"
                  class="relative inline-flex h-4 w-7 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus:outline-none"
                  :class="getPref(ev.type).enabled ? 'bg-blue-500' : 'bg-slate-300 dark:bg-slate-600'"
                  :disabled="savingPref === ev.type"
                  @click="toggleEnabled(ev.type)"
                >
                  <span
                    class="pointer-events-none inline-block h-3 w-3 rounded-full bg-white shadow transform transition-transform"
                    :class="getPref(ev.type).enabled ? 'translate-x-3' : 'translate-x-0'"
                  />
                </button>
              </td>
              <td
                v-for="ch in CHANNELS"
                :key="ch"
                class="px-2 py-2.5 text-center"
              >
                <input
                  type="checkbox"
                  class="cursor-pointer accent-blue-500"
                  :checked="getPref(ev.type).channels.includes(ch)"
                  :disabled="!getPref(ev.type).enabled || savingPref === ev.type"
                  @change="toggleChannel(ev.type, ch)"
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Delivery config -->
      <div class="space-y-3">
        <h4 class="text-xs font-semibold text-slate-600 dark:text-slate-400 uppercase tracking-wide">
          Delivery Configuration
        </h4>
        <div class="space-y-2">
          <div class="flex flex-col gap-1">
            <label class="text-xs text-slate-500 dark:text-slate-400">Webhook URL</label>
            <input
              v-model="config['webhook_url']"
              type="url"
              placeholder="https://hooks.example.com/..."
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500 font-mono"
            >
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs text-slate-500 dark:text-slate-400">Email recipient</label>
            <input
              v-model="config['email_to']"
              type="email"
              placeholder="you@example.com"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-xs text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
            >
          </div>
        </div>
        <div class="flex items-center gap-2">
          <AppButton size="sm" :disabled="savingConfig" @click="saveConfig">
            {{ savingConfig ? 'Saving…' : configSaveOk ? 'Saved!' : 'Save Config' }}
          </AppButton>
        </div>
      </div>
    </template>
  </div>
</template>
